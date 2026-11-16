package main

// Connection lifecycle and query execution. Connecting, listing metadata and
// running a query all happen off the main thread; their results reach the
// view through Window.Update.

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Ben-guanzhi/dbcore"
	"github.com/egoist/mygo/ui"
)

// connect opens the named saved connection and loads its namespaces.
func (app *App) connect(name string) {
	app.connectIn(name, app.schema)
}

// connectIn opens the named connection with schema as its default database (or
// search_path). An empty schema keeps the connection's own.
func (app *App) connectIn(name, schema string) {
	if app.open || name == "" {
		return
	}
	conn, ok := app.store.Get(name)
	if !ok {
		return
	}
	if schema != "" {
		conn = connInSchema(conn, schema)
	}
	app.status = "Connecting…"
	app.errMsg = ""
	app.invalidate()
	// S-01：connect 是"成功/失败"两态任务；耗时为从点击到对象树就绪。
	started := time.Now()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		db, err := OpenDB(ctx, conn)
		if err != nil {
			app.post(func() {
				app.status = ""
				app.errMsg = "Connect failed: " + err.Error()
				app.record("connect", "error", started, map[string]string{"engine": conn.Kind, "ok": "false"})
			})
			return
		}
		cached := newCached(db)
		if err := cached.LoadSchemas(ctx); err != nil {
			_ = db.Close()
			app.post(func() {
				app.status = ""
				app.errMsg = "Load schemas: " + err.Error()
				app.record("connect", "error", started, map[string]string{"engine": conn.Kind, "ok": "false"})
			})
			return
		}
		names := cached.SchemaNames()

		app.post(func() {
			app.db = db
			app.cached = cached
			app.open = true
			app.conn = conn
			app.errMsg = ""
			app.SetSchemas(names)
			// The working schema: the one the connection was opened with (a
			// previous choice, or the saved connection's own default), if the
			// server still has it. Otherwise "every schema".
			app.schema = ""
			if want := schemaOfConn(conn); want != "" {
				for _, n := range names {
					if strings.EqualFold(n, want) {
						app.schema = n
						break
					}
				}
			}
			app.objects = nil
			app.tree.Open.Clear()
			app.tree.List.Selected = nil
			app.search = treeSearch{}
			app.treeGen++
			// Loads still in flight belong to the old connection: their answers
			// are dropped by the generation, so their in-flight marks are
			// released here or they would block those keys forever.
			app.pending = nil
			app.clearTabs()
			// A new connection has no transaction: autocommit starts on, and any
			// registry left over from the old one belongs to the closed driver.
			app.autocommit = true
			app.txActive = false
			app.txID = ""
			app.txBusy = false
			app.errMsg = ""
			app.txIsolation = ""
			app.health = ""
			app.healthGen++
			app.readOnlyOn = conn.ReadOnly
			app.clearTreeErr()
			app.status = conn.label()
			app.refreshTitle()
			app.record("connect", "ok", started, map[string]string{"engine": conn.Kind, "ok": "true"})
		})
	}()
}

// disconnect closes the current connection.
func (app *App) disconnect() {
	if !app.open {
		return
	}
	// A run started against this connection must not land its result: cancel its
	// context (the kernel's QueryContext honors it) and move the run generation,
	// so a late applyRun is dropped instead of writing into the tabs cleared
	// below. Mirrors the guards Stop and the tree/health generations use.
	if app.runCancel != nil {
		app.runCancel()
		app.runCancel = nil
	}
	app.runGen++
	if app.db != nil {
		_ = app.db.Close()
	}
	app.open = false
	app.db = nil
	app.cached = nil
	app.Reset()
	app.objects = nil
	app.tree.Open.Clear()
	app.search = treeSearch{}
	app.clearTabs()
	app.errMsg = ""
	app.status = "Disconnected"
	app.refreshTitle()
	app.autocommit = true
	app.txActive = false
	app.txID = ""
	app.txBusy = false
	app.errMsg = ""
	app.txIsolation = ""
	app.health = ""
	app.healthGen++
	app.treeGen++
	app.pending = nil
	app.clearTreeErr()
	app.invalidate()
}

// runQuery executes every statement in the editor, in order, and shows the
// last one's tab.
func (app *App) runQuery() {
	if !app.open || app.db == nil || app.db.SQL == nil {
		app.errMsg = "Not connected"
		app.status = "Not connected"
		app.invalidate()
		return
	}
	sql := app.edBuf.String()
	if strings.TrimSpace(sql) == "" {
		return
	}
	app.runSQL(sql)
}

// A run's shape.
//
// runSQL and runBoundSQL do the same things in the same order — open one tab per
// statement, set the busy state, join or open a transaction, execute each
// statement, keep its history, deliver the results into the tabs — and differ in
// exactly one place: how one statement is executed. That difference is the
// parameter (stmtRunner), so the shape lives once instead of twice. A third
// runner (an EXPLAIN, a compare) would then need no new copy of the plumbing.

// runOut is one statement's outcome: the result sets it answered with, in the
// order the engine emitted them (a batch or a stored procedure can answer with
// several), and the error that stopped it.
type runOut struct {
	sets []*dbcore.QueryResult
	err  error
}

// txEnv is the transaction a run joins, resolved before the statements go so the
// runner does not re-read App from the goroutine.
type txEnv struct {
	autocommit bool
	mgr        *dbcore.TxManager
	id         string
}

// stmtRunner executes one statement of a run. env says whether a transaction is
// open and which one, so the runner can route the statement into it.
type stmtRunner func(ctx context.Context, env txEnv, stmt string) ([]*dbcore.QueryResult, error)

// runScript is the shared shape of a run: one tab per statement, in the order
// the statements were written, the last one chosen. allowEdit says whether a
// statement that is one table's rows may become an editable working copy; a
// bound statement may not, as its placeholders are not table columns.
//
// The run carries a generation and a cancel handle: Stop cancels the context —
// the kernel's QueryContext honors it and the driver interrupts the statement —
// and a result that answers after the run was replaced is dropped by its
// generation, so a cancelled run cannot overwrite a newer one.
func (app *App) runScript(stmts []string, run stmtRunner, allowEdit bool) {
	app.runScriptGuard(stmts, run, allowEdit, true)
}

// runScriptGuard is runScript with the destructive-statement question in front
// of it. guard is false only for the run the confirmation itself starts, so one
// press asks once.
func (app *App) runScriptGuard(stmts []string, run stmtRunner, allowEdit bool, guard bool) {
	if app.readOnly() {
		if stmt := writeStatement(stmts); stmt != "" {
			app.refuseWrite(stmt)
			return
		}
	}
	if guard {
		if why := riskyStatement(stmts); why != "" {
			app.confirm = confirmRun{
				open: true,
				why:  why,
				sql:  strings.Join(stmts, ";\n\n"),
				again: func() {
					app.runScriptGuard(stmts, run, allowEdit, false)
				},
			}
			app.invalidate()
			return
		}
	}
	// The tab bar names the console after what it holds, the way a workbench
	// does: the first statement's first line, once there is one.
	app.renameConsoleFromContent()
	tabs := app.startRunSQL(stmts)
	app.beginBusy("run")
	app.errMsg = ""
	app.result = nil
	app.pager.reset(false)
	app.start = time.Now()
	app.status = "Running…"
	app.runGen++
	gen := app.runGen
	app.invalidate()

	env := txEnv{autocommit: app.autocommit, mgr: app.db.Tx, id: app.txID}
	go func() {
		// The timeout is the safety net; the cancel is what Stop pulls. Both end
		// the same ctx, so RunQuery's QueryContext interrupts the statement
		// whichever one fires.
		base, cancelTimeout := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancelTimeout()
		ctx, cancel := context.WithCancel(base)
		defer cancel()
		// The handle Stop pulls; a finished run leaves it nil, so Stop is a
		// no-op then and the button is disabled anyway.
		app.post(func() { app.runCancel = cancel })

		// With autocommit off the script's statements run in one transaction that
		// stays open afterwards, so its changes can be committed or rolled back by
		// hand. It is opened on the connection's registry before the first
		// statement; a transaction the user already opened is left as it is.
		if !env.autocommit {
			var err error
			if env, err = app.openRunTx(ctx, env); err != nil {
				app.failRun(gen)
				return
			}
		}

		out := make([]runOut, len(stmts))
		cancelled := false
		for i, stmt := range stmts {
			t0 := time.Now()
			sets, err := run(ctx, env, stmt)
			if err != nil && ctx.Err() == context.Canceled {
				// Stop was pressed: the statement's own error is the driver's
				// interruption noise, and what the user asked for is a stop.
				cancelled = true
			}
			// Every statement that ran is kept, whether it worked or not: the
			// history is where a failed statement is found again. The store locks
			// for itself, so this runs beside the UI without asking it.
			if app.store != nil {
				app.store.AddHistory(historyEntry(stmt, err, t0))
			}
			if err != nil {
				if !cancelled {
					out[i].err = fmt.Errorf("statement %d: %w", i+1, err)
				}
				break
			}
			out[i].sets = sets
		}
		elapsed := time.Since(app.start)
		app.post(func() {
			app.applyRun(gen, tabs, stmts, out, elapsed, allowEdit, cancelled)
			app.saveSession()
		})
	}()
}

// cancelRun is the Stop button: cancel the running query's context, which the
// kernel's QueryContext honors and the driver turns into an interruption. A run
// that already finished has no handle, and the button is disabled then anyway.
func (app *App) cancelRun() {
	if app.runCancel == nil {
		return
	}
	if app.runCancel != nil {
		app.runCancel()
	}
	app.status = "Cancelling…"
	app.invalidate()
}

// applyRun puts a run's outcomes into their tabs, on the main thread. A run
// whose generation has moved on — the user ran something else, or stopped it
// and started again — drops its results here, so a late answer never overwrites
// a newer one. cancelled says the run ended by Stop rather than by error.
func (app *App) applyRun(gen uint64, tabs []*resTab, stmts []string, out []runOut, elapsed time.Duration, allowEdit bool, cancelled bool) {
	app.runCancel = nil
	if gen != app.runGen {
		return // a newer run owns the tabs now
	}
	app.endBusy("run")
	// S-01：一次执行 = 一次 query 任务尝试。elapsed 就是用户可感知的耗时；
	// cancelled 与"有语句报错"都进 outcome，所以"取消率/失败率"不需要另埋点。
	{
		outcome := "ok"
		if cancelled {
			outcome = "cancelled"
		}
		rows, failed := 0, false
		for _, o := range out {
			if o.err != nil {
				failed = true
			}
			for _, set := range o.sets {
				rows += len(set.Rows)
			}
		}
		if failed && !cancelled {
			outcome = "error"
		}
		if app.um != nil {
			first := ""
			if len(stmts) > 0 {
				first = stmts[0]
			}
			app.um.Record("query", outcome, elapsed, map[string]string{
				"statement_type": stmtType(first),
				"rows":           strconv.Itoa(rows),
				"truncated":      "false",
			})
		}
	}
	app.errMsg = ""
	last := tabs[0]
	for i, o := range out {
		t := tabs[i]
		stmt := stmts[i]
		t.elapsed = elapsed
		t.errMsg = ""
		if o.err != nil {
			t.errMsg = o.err.Error()
			app.errMsg = t.errMsg
			app.status = "Query failed"
			last = t
			continue
		}
		if len(o.sets) == 0 {
			t.result = nil
			t.status = "Query done"
			t.edit.clear()
			last = t
			continue
		}
		// The statement answered with one set or several: the first goes in the
		// tab the run opened for this statement, and each further set gets a tab
		// of its own right after it — a batch or a stored procedure is several
		// grids, and folding them into one would show only the first.
		app.applySet(t, o.sets[0], stmt, elapsed, allowEdit)
		last = t
		for j := 1; j < len(o.sets); j++ {
			extra := newTab(fmt.Sprintf("%s · %d/%d", tabShortLabel(stmt), j+1, len(o.sets)), tabRun)
			app.applySet(extra, o.sets[j], stmt, elapsed, false)
			tabs = append(tabs, extra)
			app.tabs = append(app.tabs, extra)
			last = extra
		}
	}
	if cancelled {
		// The statement(s) that did not finish are the run's report, not an
		// error: the user asked for the stop.
		last.errMsg = ""
		last.status = "Cancelled"
		app.status = "Query cancelled"
	}
	app.showTab(last)
}

// applySet puts one result set into a tab: its rows, its pager, and — when the
// statement is one table's rows and the caller allows it — the editable working
// copy. A later set of a multi-set answer is never editable: only the first can
// be the table's own grid.
func (app *App) applySet(t *resTab, res *dbcore.QueryResult, stmt string, elapsed time.Duration, allowEdit bool) {
	t.result = res
	// The rows as the driver returned them, before the filter bar keeps its
	// rows: a client-paged result is filtered from this again.
	t.raw = res
	t.filter = nil
	t.filterRows = nil
	t.status = resultStatus(res, elapsed)
	// An arbitrary statement has no server-side page: the pager walks the rows
	// the driver loaded, and Truncated says whether there were more.
	t.pager.reset(false)
	t.pager.total = len(res.Rows)
	if allowEdit {
		if schema, table, ok := app.tableOf(stmt); ok {
			cols, _ := app.ColumnsOf(schema, table)
			t.edit.init(res, schema, table, cols, func() { app.runSQL(stmt) })
			return
		}
	}
	t.edit.clear()
}

// tabShortLabel is a statement's first line, cut for a tab title: the shape
// renameConsoleFromContent gives a console.
func tabShortLabel(stmt string) string {
	text := strings.TrimSpace(firstLine(stmt))
	if r := []rune(text); len(r) > 24 {
		return string(r[:24]) + "…"
	}
	return text
}

// failRun ends a run that could not start, on the main thread. A run that has
// been superseded does not report.
func (app *App) failRun(gen uint64) {
	app.post(func() {
		app.runCancel = nil
		if gen != app.runGen {
			return
		}
		app.endBusy("run")
		app.errMsg = ""
		app.status = "Transaction failed"
		app.invalidate()
	})
}

// openRunTx joins the transaction a run is to use, or opens one, and reports the
// error a run stops at.
func (app *App) openRunTx(ctx context.Context, env txEnv) (txEnv, error) {
	if env.mgr == nil {
		return env, fmt.Errorf("this driver cannot open a transaction")
	}
	if env.id == "" {
		info, err := env.mgr.Begin(ctx, dbcore.TxMode{IsolationLevel: app.txIsolation})
		if err != nil {
			return env, fmt.Errorf("begin transaction: %w", err)
		}
		env.id = info.ID
	}
	app.post(func() { app.txActive, app.txID = true, env.id })
	return env, nil
}

// runSQL runs a script: every statement gets a tab of its own, so several
// results can be side by side and a failure does not hide the statements before
// it. A statement whose SQL is a plain "SELECT * FROM t" turns its tab into an
// editable working copy of that table; any other statement leaves it read-only.
func (app *App) runSQL(sql string) {
	stmts := splitSQL(sql)
	if len(stmts) == 0 {
		return
	}
	app.runScript(stmts, func(ctx context.Context, env txEnv, stmt string) ([]*dbcore.QueryResult, error) {
		return executeStatementSets(ctx, env.autocommit, env.mgr, env.id, app.db, stmt, nil, true)
	}, true)
}

// runBoundSQL runs one statement with bound parameter values (the parameter
// panel's Run): the SQL already carries the engine's own markers, so the driver
// binds each value natively. The result shows the way a plain run's does; a
// statement with a transaction open joins it, as a run would.
func (app *App) runBoundSQL(sql string, args []dbcore.Value) {
	if !app.open || app.db == nil || app.db.SQL == nil {
		app.errMsg = "Not connected"
		app.status = "Not connected"
		app.invalidate()
		return
	}
	stmts := splitSQL(sql)
	if len(stmts) == 0 {
		return
	}
	// The panel binds values to the one statement it showed; a rewritten
	// statement is always one.
	stmt := stmts[0]
	app.runScript([]string{stmt}, func(ctx context.Context, env txEnv, _ string) ([]*dbcore.QueryResult, error) {
		// A bound statement may still answer with several sets (a CALL with
		// parameters); the driver decides, and the bound path does not take the
		// multi-statement route that needs the whole text unbound.
		return executeStatementSets(ctx, env.autocommit, env.mgr, env.id, app.db, stmt, args, false)
	}, false)
}

// resultStatus is the one-line report of a statement, for the message panel.
func resultStatus(res *dbcore.QueryResult, elapsed time.Duration) string {
	switch {
	case res == nil:
		return "Query done"
	case res.AffectedRows > 0:
		return fmt.Sprintf("%s affected, %s", pluralRows(int(res.AffectedRows)), elapsed.Round(time.Millisecond))
	case res.Truncated:
		return fmt.Sprintf("%s, truncated, %s", pluralRows(len(res.Rows)), elapsed.Round(time.Millisecond))
	default:
		return fmt.Sprintf("%s, %s", pluralRows(len(res.Rows)), elapsed.Round(time.Millisecond))
	}
}

// runSelection runs the text the editor has selected. With no selection it
// runs to the caret, as DataGrip's Run Selection does.
func (app *App) runSelection() {
	if !app.open || app.db == nil || app.db.SQL == nil {
		app.errMsg = "Not connected"
		app.status = "Not connected"
		app.invalidate()
		return
	}
	sel := app.edState.Selection()
	if sel.Anchor == sel.Head {
		app.runToCaret()
		return
	}
	text := app.edBuf.String()
	a, b := sel.Anchor, sel.Head
	if a > b {
		a, b = b, a
	}
	if b > len(text) {
		b = len(text)
	}
	if strings.TrimSpace(text[a:b]) == "" {
		app.runToCaret()
		return
	}
	app.runSQL(text[a:b])
}

// runToCaret runs the statement the caret is in, from its start to the caret.
func (app *App) runToCaret() {
	if !app.open || app.db == nil || app.db.SQL == nil {
		app.errMsg = "Not connected"
		app.status = "Not connected"
		app.invalidate()
		return
	}
	text := app.edBuf.String()
	pos := app.edState.Cursor()
	if pos > len(text) {
		pos = len(text)
	}
	start, ok := statementStart(text, pos)
	if !ok {
		return
	}
	if strings.TrimSpace(text[start:pos]) == "" {
		return
	}
	app.runSQL(text[start:pos])
}

// statementStart is the byte offset of the statement that contains pos, or the
// offset of the statement before it when pos sits in the whitespace after one.
// It reports false when there is no statement before pos.
func statementStart(s string, pos int) (int, bool) {
	ranges := statementRanges(s)
	start := -1
	for _, r := range ranges {
		if r[0] > pos {
			break
		}
		start = r[0]
	}
	return start, start >= 0
}

// statementRanges is splitSQL's boundaries as byte ranges into the original
// text, without the trimming: [start, end) is the statement's text as it was
// written.
func statementRanges(s string) [][2]int {
	var out [][2]int
	i, n := 0, len(s)
	start := -1
	for i < n {
		ch := s[i]
		if start < 0 && !isSpaceByte(ch) {
			start = i
		}
		if ch == '-' && i+1 < n && s[i+1] == '-' {
			for i < n && s[i] != '\n' {
				i++
			}
			continue
		}
		if ch == '#' {
			for i < n && s[i] != '\n' {
				i++
			}
			continue
		}
		if ch == '/' && i+1 < n && s[i+1] == '*' {
			i += 2
			for i+1 < n && !(s[i] == '*' && s[i+1] == '/') {
				i++
			}
			i += 2
			continue
		}
		if ch == '\'' || ch == '"' || ch == '`' {
			quote := ch
			i++
			for i < n {
				if s[i] == quote {
					if i+1 < n && s[i+1] == quote {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
			continue
		}
		if ch == ';' {
			if start >= 0 {
				out = append(out, [2]int{start, i})
			}
			start = -1
			i++
			continue
		}
		i++
	}
	if start >= 0 {
		out = append(out, [2]int{start, n})
	}
	return out
}

func isSpaceByte(ch byte) bool {
	return ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' || ch == '\v' || ch == '\f'
}

// browseTable opens the table a tree row names: its tab either already exists
// or is created now, and the first page is read into it.
func (app *App) browseTable(n treeNode) {
	t := app.findBrowseTab(n)
	if t == nil {
		app.saveTab()
		t = newTab(n.label, tabBrowse)
		t.node = n
		t.schema, t.table = n.parent, n.label
		// The arrangement this table was left in, if it has been visited.
		app.applyColumnLayout(t)
		app.tabs = append(app.tabs, t)
	}
	t.pager.reset(true)
	app.tabSel = app.indexOf(t)
	app.loadTab(t)
	app.beginEditMode()
	app.fetchBrowse(n)
}

// fetchBrowse loads the page the pager points at into the tab it belongs to. It
// is also what a commit re-reads and what a page change asks for, so it leaves
// the pager alone. A fetch that is not for the tab on screen is dropped: the
// user moved on before the database answered.
func (app *App) fetchBrowse(n treeNode) {
	if !app.open || app.db == nil || app.db.SQL == nil {
		return
	}
	t := app.tab()
	if t == nil || t.kind != tabBrowse || t.node.label != n.label || t.node.parent != n.parent {
		return
	}
	table := n.label
	schema := n.parent // the namespace the tree calls it, and the columns cache
	query := schema
	if app.conn.Kind == "sqlite" {
		query = "" // SQLite has one namespace, addressed without a name
	}
	app.beginBusy("browse")
	app.errMsg = ""
	app.result = nil
	app.status = "Loading " + table + "…"
	// The pager will want to know how big this table is; ask now, beside the
	// page, so the answer is there when the rows land.
	app.requestEstimate(schema, table)
	app.invalidate()

	db := app.db
	page := app.pager.page
	size := app.pager.pageSize
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		res, err := db.SQL.BrowseRows(ctx, query, table, &dbcore.TableRowsRequest{
			Page:     page,
			PageSize: size,
			// The order the grid's header chose and the rows its filter bar keeps,
			// which the database applies so both hold across pages.
			Sort:   sortKeys(t.sortBy),
			Filter: t.filter,
			// 交互式表格不该为每一页付一次全表 COUNT(*)：pager 已有"未知总数"
			// 与目录估算两种呈现，精确值由 Σ 按钮按需取（见 pagerCountExact）。
			NoCount: true,
		})
		app.post(func() {
			app.endBusy("browse")
			if app.tab() != t {
				return
			}
			if err != nil {
				app.errMsg = "Browse failed: " + err.Error()
				app.status = "Browse failed"
				t.errMsg = app.errMsg
				return
			}
			total := unknownTotal
			if res.TotalFound {
				total = int(res.Total)
			}
			// Without a total, a full page says more may follow; a short one
			// is the end of the table as far as this page can tell.
			more := total < 0 && len(res.Rows) >= size
			out := &dbcore.QueryResult{
				Columns: res.Columns,
				Rows:    res.Rows,
				// A page that came back full may hold more rows after it; when
				// the total is known the pager's page count says so instead.
				Truncated: total < 0 && len(res.Rows) >= size,
			}
			if total >= 0 {
				tt := total
				out.TotalRows = &tt
			}
			t.result = out
			t.errMsg = ""
			t.pager.total = total
			t.pager.more = more
			// The incremental browse: this is the first segment, and there may be
			// more behind it.
			t.pagesLoaded = 1
			t.fetchSize = size
			t.moreRows = total < 0 && len(res.Rows) >= size || total > len(res.Rows)
			t.loadingMore = false
			t.status = statusPageText(table, page, total, len(res.Rows))
			app.pager = t.pager
			t.pager.page = app.clampPage()
			app.pager = t.pager
			app.result = t.result
			app.errMsg = ""
			app.status = t.status
			// A page the database filtered is the filter's own copy; there is no
			// unfiltered result to keep here.
			t.raw = nil
			// A browse is one table's rows, so the grid becomes its working copy
			// and can be committed or rolled back.
			app.startEdit(out, schema, table, func() { app.fetchBrowse(n) })
			app.saveTab()
		})
	}()
}

// deleteConn asks before removing the selected connection.
func (app *App) deleteConn(c *ui.Context) {
	if app.connName == "" {
		return
	}
	open := true
	switch ui.AlertDialog(c, &open, "Delete connection",
		"Delete "+app.connName+"? This cannot be undone.", "Cancel", "Delete") {
	case 1:
		if err := app.store.Remove(app.connName); err != nil {
			app.errMsg = err.Error()
			return
		}
		app.disconnect()
		app.connName = ""
		if c2, ok := app.store.Active(); ok {
			app.connName = c2.Name
		}
		app.invalidate()
	}
}

// splitSQL cuts a buffer into statements on semicolons outside strings and
// comments.
func splitSQL(s string) []string {
	var out []string
	var cur strings.Builder
	i, n := 0, len(s)
	for i < n {
		ch := s[i]
		if ch == '-' && i+1 < n && s[i+1] == '-' {
			for i < n && s[i] != '\n' {
				i++
			}
			continue
		}
		if ch == '#' {
			for i < n && s[i] != '\n' {
				i++
			}
			continue
		}
		if ch == '/' && i+1 < n && s[i+1] == '*' {
			i += 2
			for i+1 < n && !(s[i] == '*' && s[i+1] == '/') {
				i++
			}
			i += 2
			continue
		}
		if ch == '\'' || ch == '"' || ch == '`' {
			quote := ch
			cur.WriteByte(ch)
			i++
			for i < n {
				cur.WriteByte(s[i])
				if s[i] == quote {
					if i+1 < n && s[i+1] == quote {
						cur.WriteByte(s[i+1])
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
			continue
		}
		if ch == ';' {
			if t := strings.TrimSpace(cur.String()); t != "" {
				out = append(out, t)
			}
			cur.Reset()
			i++
			continue
		}
		cur.WriteByte(ch)
		i++
	}
	if t := strings.TrimSpace(cur.String()); t != "" {
		out = append(out, t)
	}
	return out
}
