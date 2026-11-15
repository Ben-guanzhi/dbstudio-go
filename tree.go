package main

// The schema tree: one Outline whose rows are schemas, their tables and the
// tables' columns. Tables and columns load when the branch opens, in the
// background, so a large schema does not block the frame that opens it.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Ben-guanzhi/dbcore"
	"github.com/egoist/mygo/ui"
)

type treeNode struct {
	label  string
	sub    string
	kind   int
	parent string // schema of a table or column; schema \x00 table of a column
	table  string // table of a column
	group  int    // object section kind (objIndex..objEnum) for section/leaf nodes
}

const (
	nodeSchema = iota
	nodeTable
	nodeColumn
	nodeSection // an object section header: Indexes, Foreign Keys, ...
	nodeObject  // a leaf object inside a section: one index, sequence, ...
)

type tableInfo struct {
	Name    string
	Type    string // dbcore.TableInfo.Type: "table", "view" or "materialized_view"
	Comment string
	// Rows is the engine's estimate from catalog statistics, -1 when it has
	// none. It is what the pager shows instead of "?" for a table too large to
	// count: an estimate before a scan is worth more than a question mark.
	Rows int64
}

// treeRoots are the tree's first level: every schema, or — when the working
// schema is chosen — just that one, opened. Scoping the navigator to the chosen
// schema is what makes the selector mean something in the window: its other
// effects (the search's scope, the bare-name defaults, the connection's default
// database) are real but invisible, and a control whose effect cannot be seen
// reads as broken.
func (app *App) treeRoots() []treeNode {
	names := app.SchemaNames()
	if app.schema != "" {
		for _, n := range names {
			if strings.EqualFold(n, app.schema) {
				root := treeNode{label: n, sub: "schema", kind: nodeSchema}
				// Opened, so the tables it holds are the next thing on screen.
				app.tree.Open.Add(root)
				return []treeNode{root}
			}
		}
	}
	roots := make([]treeNode, len(names))
	for i, s := range names {
		roots[i] = treeNode{label: s, sub: "schema", kind: nodeSchema}
	}
	return roots
}

func (app *App) treeChildren(n treeNode) []treeNode {
	switch n.kind {
	case nodeSchema:
		tabs, st := app.TablesOf(n.label)
		if st == TablesNotLoaded {
			app.loadTables(n.label)
			// An empty slice marks the schema as a branch, so its arrow shows
			// and its tables appear once the load lands.
			return []treeNode{}
		}
		out := make([]treeNode, 0, len(tabs)+len(schemaSections))
		for _, t := range tabs {
			child := treeNode{label: t.Name, kind: nodeTable, parent: n.label}
			switch {
			case t.Comment != "":
				child.sub = t.Comment
			case t.Type == "view":
				// 波 99（DataGrip 惯例）：视图在树里可辨——无注释时标注类型。
				child.sub = "view"
			case t.Type == "materialized_view":
				child.sub = "materialized view"
			}
			out = append(out, child)
		}
		// The schema-level sections (sequences, functions, enums) hang off the
		// schema, after its tables. A section already known not to exist is
		// dropped; one not yet asked is kept so it can be opened.
		for _, s := range schemaSections {
			if !app.sectionUsable("", n.label, s) {
				continue
			}
			out = append(out, treeNode{label: s.label, kind: nodeSection, parent: n.label, group: s.kind})
		}
		return out
	case nodeTable:
		cols, st := app.ColumnsOf(n.parent, n.label)
		if st == ColsNotLoaded {
			app.loadColumns(n.parent, n.label)
			return []treeNode{}
		}
		out := make([]treeNode, 0, len(cols)+len(tableSections))
		for _, col := range cols {
			out = append(out, treeNode{label: col.Name, sub: col.Type, kind: nodeColumn, parent: n.parent, table: n.label})
		}
		// The table-level sections (indexes, FKs, CHECK constraints, partitions).
		for _, s := range tableSections {
			if !app.sectionUsable(n.parent, n.label, s) {
				continue
			}
			out = append(out, treeNode{label: s.label, kind: nodeSection, parent: n.parent + "\x00" + n.label, group: s.kind})
		}
		return out
	case nodeSection:
		schema, table := sectionTarget(n)
		names, loaded, notSupported := app.objects.leaves(n.group, schema, table)
		if notSupported {
			return nil // hidden; the parent already dropped it, nothing to show
		}
		if !loaded {
			app.loadObjectSection(schema, table, sectionOf(n.group))
			return []treeNode{} // a branch claiming children until the load lands
		}
		out := make([]treeNode, len(names))
		for i, name := range names {
			out[i] = treeNode{label: name, kind: nodeObject, parent: n.parent, group: n.group}
		}
		return out
	}
	return nil
}

func (app *App) treeRow(c *ui.Context, n treeNode) {
	t := c.Theme()
	switch n.kind {
	case nodeSchema:
		ui.Text(c, n.label).Bold()
		if n.sub != "" {
			ui.Text(c, "  "+n.sub).TextColor(t.TextMuted)
		}
	case nodeTable:
		// Double-click a table to browse its rows, as in DataGrip; its right
		// click opens the menu that shows its DDL. One Text element, probed for
		// the double click — a second unconditioned Text repeated the name and
		// every table row read "erp_product erp_product…".
		//
		// The comment is the row's tooltip, not a second column: side by side the
		// two halved each other and both read as "单..." / "客户跟...".
		tw := ui.Text(c, n.label).Bold().ContextMenu(func(m *ui.Menu) {
			app.treeNodeMenu(m, n)
		})
		if n.sub != "" {
			tw.Tooltip(n.sub)
		}
		if tw.RowDoubleClicked() {
			app.browseTable(n)
			return
		}
	case nodeColumn:
		cw := ui.Text(c, n.label)
		if n.sub != "" {
			cw.Tooltip(n.sub)
		}
	case nodeSection:
		// A section header: muted, so the schema's objects read as a grouping
		// rather than siblings of the tables.
		ui.Text(c, n.label).Bold().TextColor(t.TextMuted).FontSize(11)
	case nodeObject:
		// An object leaf: its name, and a right-click menu to view its
		// definition. Double-clicking also opens the definition, as it opens a
		// table's DDL. One Text element, probed for the double click — the same
		// repeat-the-name bug the table rows had.
		ow := ui.Text(c, n.label).ContextMenu(func(m *ui.Menu) {
			app.objectMenu(m, n)
		})
		if ow.RowDoubleClicked() {
			app.openObjectDDL(n)
			return
		}
		if n.sub != "" {
			ui.Text(c, "  "+n.sub).TextColor(t.TextMuted).SingleLine()
		}
	}
}

func (app *App) treeSelect(n treeNode) {
	if n.kind != nodeTable {
		return
	}
	key := n.parent + "\x00" + n.label
	if key == app.selTable {
		return
	}
	app.selTable = key
	app.edBuf.Set(browseSQL(app.conn.Kind, n.parent, n.label))
	// The console tab now holds this table's browse; name it so the tab bar
	// says which console is which.
	app.renameConsoleFromContent()
}

// loadAsync is the shape every metadata load shares: a read off the main
// thread, its answer delivered back to it, and a stale answer dropped when the
// connection changed under it. work runs on its own goroutine with a timeout and
// must not touch App; apply runs on the main thread and gets work's value and
// error.
//
// Putting it here rather than in each loader is what keeps the three rules every
// load must obey — post through the window, check the generation, refresh the
// view — in one place, so a fourth loader cannot forget one of them.
//
// With no window (an App a test built) there is nothing to defer through, so the
// read runs inline: a test that opens a branch sees the answer when it returns.
func loadAsync[T any](app *App, timeout time.Duration, work func(ctx context.Context) (T, error), apply func(T, error)) {
	if app.win == nil {
		v, err := work(context.Background())
		apply(v, err)
		return
	}
	gen := app.treeGen
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		v, err := work(ctx)
		app.post(func() {
			if gen != app.treeGen {
				return
			}
			apply(v, err)
		})
	}()
}

// metadataTimeout bounds one catalog read. It is generous on purpose: a large
// schema's table list can take a while on a busy server, and a catalog read has
// no result to show before it finishes.
const metadataTimeout = 30 * time.Second

// beginLoad marks a metadata load as in flight, answering false when one is
// already running for the same key. The tree asks for a branch's children every
// frame, so without this a slow load is re-issued per frame and floods the
// server; the cache guard alone cannot stop it, because the branch only stops
// being "not loaded" once the answer arrives.
func (app *App) beginLoad(key string) bool {
	if app.pending == nil {
		app.pending = map[string]bool{}
	}
	if app.pending[key] {
		return false
	}
	app.pending[key] = true
	return true
}

// endLoad releases the in-flight mark. It runs on the main thread, where the
// answer is applied.
func (app *App) endLoad(key string) {
	delete(app.pending, key)
}

// loadTables fills the table list of a schema from the server.
func (app *App) loadTables(schema string) {
	if app.db == nil || app.db.SQL == nil {
		return
	}
	if _, st := app.TablesOf(schema); st != TablesNotLoaded {
		return
	}
	key := "t\x00" + schema
	if !app.beginLoad(key) {
		return // already on its way
	}
	db := app.db
	loadAsync(app, metadataTimeout, func(ctx context.Context) ([]tableInfo, error) {
		tabs, err := db.SQL.ListTables(ctx, schema)
		if err != nil {
			return nil, fmt.Errorf("list tables in %q: %w", schema, err)
		}
		out := make([]tableInfo, len(tabs))
		for i, t := range tabs {
			out[i] = tableInfo{Name: t.Name, Type: t.Type, Comment: t.Comment, Rows: t.Rows}
		}
		return out, nil
	}, func(out []tableInfo, err error) {
		app.endLoad(key)
		if err != nil {
			// A catalog read's failure belongs to the tree, not to the results
			// pane: a timed-out table list is not a failed query. The schema
			// stays unloaded, so it can be asked again — the tree's error row
			// does exactly that.
			app.setTreeErr(err, func() { app.loadTables(schema) })
			app.invalidate()
			return
		}
		app.PutTables(schema, out)
		app.clearTreeErr()
		app.invalidate()
	})
}

// loadColumns fills the column list of a table from the server.
func (app *App) loadColumns(schema, table string) {
	if app.db == nil || app.db.SQL == nil {
		return
	}
	if _, st := app.ColumnsOf(schema, table); st != ColsNotLoaded {
		return
	}
	key := "c\x00" + schema + "\x00" + table
	if !app.beginLoad(key) {
		return
	}
	db := app.db
	loadAsync(app, metadataTimeout, func(ctx context.Context) ([]dbcore.ColumnInfo, error) {
		cols, err := db.SQL.ListColumns(ctx, schema, table)
		if err != nil {
			return nil, fmt.Errorf("list columns in %q: %w", table, err)
		}
		return cols, nil
	}, func(cols []dbcore.ColumnInfo, err error) {
		app.endLoad(key)
		if err != nil {
			app.setTreeErr(err, func() { app.loadColumns(schema, table) })
			app.invalidate()
			return
		}
		app.PutColumns(schema, table, cols)
		app.clearTreeErr()
		app.invalidate()
	})
}

// setTreeErr records a failed metadata load and the retry that asks again. It
// deliberately leaves errMsg alone: that one belongs to queries.
func (app *App) setTreeErr(err error, retry func()) {
	app.treeErr = firstLine(err.Error())
	app.treeErrRetry = retry
	app.status = app.treeErr
}

// clearTreeErr drops the tree's error once a load has succeeded.
func (app *App) clearTreeErr() {
	app.treeErr = ""
	app.treeErrRetry = nil
}

// browseSQL is the SELECT a click on a table puts in the editor. The schema
// of the default namespace is left out, as writing it would be redundant.
func browseSQL(kind, schema, table string) string {
	name := table
	defaultSchema := ""
	switch kind {
	case "postgres":
		defaultSchema = "public"
	case "mssql":
		defaultSchema = "dbo"
	}
	if schema != "" && schema != "main" && schema != defaultSchema {
		name = schema + "." + table
	}
	switch kind {
	case "mssql":
		return fmt.Sprintf("SELECT TOP 100 * FROM %s;\n", name)
	case "oracle":
		return fmt.Sprintf("SELECT * FROM %s FETCH FIRST 100 ROWS ONLY;\n", name)
	default:
		return fmt.Sprintf("SELECT * FROM %s LIMIT 100;\n", name)
	}
}
