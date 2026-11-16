package main

// Tests for splitting one statement's several result sets into several tabs.
//
// A batch (SQL Server) or a stored procedure (MySQL's CALL) answers with one set
// per SELECT it ran; before this the grid showed the first and the rest were
// left on the wire. The client walks what the kernel's MultiResultReader gives
// and opens a tab per set, in the order the engine emitted them.

import (
	"context"
	"testing"
	"time"

	"github.com/Ben-guanzhi/dbcore"
)

// multiFakeDriver is a driver whose ExecuteMulti answers with whatever sets the
// test put in it, and which records how often it was asked.
type multiFakeDriver struct {
	sets  []*dbcore.QueryResult
	calls int
}

func (d *multiFakeDriver) Kind() dbcore.DatabaseKind       { return dbcore.KindSQLite }
func (d *multiFakeDriver) Ping(context.Context) error      { return nil }
func (d *multiFakeDriver) Close() error                    { return nil }
func (d *multiFakeDriver) AsSQL() (dbcore.SQLDriver, bool) { return nil, false }
func (d *multiFakeDriver) AsKV() (dbcore.KVDriver, bool)   { return nil, false }
func (d *multiFakeDriver) ExecuteMulti(context.Context, string, ...dbcore.Value) ([]*dbcore.QueryResult, error) {
	d.calls++
	return d.sets, nil
}

// waitRun waits for a run's goroutine to land its results: runScript does the
// work off the view thread, and a headless test has no frame to wait on.
func waitRun(t *testing.T, app *App) {
	t.Helper()
	for i := 0; i < 400; i++ {
		app.post(func() {}) // 排空 post 队列（headless 下 inline）：busy 的写先行于读
		if !app.busy {
			app.post(func() {}) // busy 翻 false 的那次 post 也排掉
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("the run did not finish")
}
func setOf(cols []string, rows ...[]dbcore.Value) *dbcore.QueryResult {
	res := &dbcore.QueryResult{}
	for _, c := range cols {
		res.Columns = append(res.Columns, dbcore.ColumnInfo{Name: c})
	}
	res.Rows = rows
	return res
}

func multiApp(t *testing.T, sets ...*dbcore.QueryResult) (*App, *multiFakeDriver) {
	t.Helper()
	d := &multiFakeDriver{sets: sets}
	app := &App{db: &DB{Driver: d, Kind: "sqlite"}, conn: Conn{Kind: "sqlite"}, open: true,
		pager:   newResultPager(),
		gridSel: -1,
		tables:  map[string][]tableInfo{},
		cols:    map[string][]dbcore.ColumnInfo{},
		objects: objectCache{}}
	app.initModalEditors()
	app.initConsoles()
	app.initEditorState()
	// Autocommit: the run's plain path, which is the one that takes the multi route.
	app.autocommit = true
	return app, d
}

// Two sets become two tabs, the first in the statement's own tab and the second
// right after it, titled with which set of how many it is.
func TestMultiSetRunSplitsIntoTabs(t *testing.T) {
	app, d := multiApp(t,
		setOf([]string{"id"}, []dbcore.Value{{Kind: dbcore.ValInt, I: 1}}),
		setOf([]string{"name"}, []dbcore.Value{{Kind: dbcore.ValText, S: "ada"}}),
	)
	app.runScript([]string{"SELECT 1; SELECT 2"}, func(ctx context.Context, env txEnv, stmt string) ([]*dbcore.QueryResult, error) {
		return executeStatementSets(ctx, env.autocommit, env.mgr, env.id, app.db, stmt, nil, true)
	}, false)
	waitRun(t, app)

	if d.calls != 1 {
		t.Fatalf("the driver was asked %d time(s), want 1", d.calls)
	}
	if len(app.tabs) != 2 {
		t.Fatalf("tabs = %d, want 2 (one statement, two sets)", len(app.tabs))
	}
	if got := app.tabs[0].result.Columns[0].Name; got != "id" {
		t.Fatalf("first tab holds %q, want the first set", got)
	}
	if got := app.tabs[1].result.Columns[0].Name; got != "name" {
		t.Fatalf("second tab holds %q, want the second set", got)
	}
	if !contains(app.tabs[1].label, "2/2") {
		t.Fatalf("the second set's tab is not marked as the second of two: %q", app.tabs[1].label)
	}
	// The last set is the one on screen.
	if app.tab().result != app.tabs[1].result {
		t.Fatal("the run did not leave the last set on screen")
	}
}

// A single-set answer keeps the old shape exactly: one tab, the statement's own.
func TestSingleSetKeepsOneTab(t *testing.T) {
	app, d := multiApp(t, setOf([]string{"id"}, []dbcore.Value{{Kind: dbcore.ValInt, I: 42}}))
	app.runScript([]string{"SELECT 42"}, func(ctx context.Context, env txEnv, stmt string) ([]*dbcore.QueryResult, error) {
		return executeStatementSets(ctx, env.autocommit, env.mgr, env.id, app.db, stmt, nil, true)
	}, false)
	waitRun(t, app)
	if d.calls != 1 || len(app.tabs) != 1 {
		t.Fatalf("calls=%d tabs=%d, want one of each", d.calls, len(app.tabs))
	}
	if app.tabs[0].result.Rows[0][0].I != 42 {
		t.Fatalf("the tab holds %+v", app.tabs[0].result.Rows)
	}
}

// A driver that cannot answer with several sets still works: the fallback is the
// single-statement path, which is what Execute is for.
func TestMultiSetFallsBackWhenUnsupported(t *testing.T) {
	app := &App{db: &DB{Kind: "sqlite"}, conn: Conn{Kind: "sqlite"}, open: true,
		pager: newResultPager(), gridSel: -1,
		tables: map[string][]tableInfo{}, cols: map[string][]dbcore.ColumnInfo{}}
	app.initModalEditors()
	app.initConsoles()
	app.initEditorState()
	app.autocommit = true
	// No Driver at all: the SQL path is nil, so the call reports the connection
	// rather than panicking — the same thing a closed connection does.
	if _, err := executeStatementSets(context.Background(), true, nil, "", app.db, "SELECT 1", nil, true); err == nil {
		t.Fatal("a db with no driver answered as if it had one")
	}
}

// The per-set tab titles name the set and cut the statement, so a stored
// procedure's ten SELECTs still read as ten distinguishable tabs.
func TestMultiSetTabLabels(t *testing.T) {
	long := "SELECT * FROM api_po_message_send_log WHERE created_at > now()"
	if got := tabShortLabel(long); len([]rune(got)) > 25 || !contains(got, "…") {
		t.Fatalf("tabShortLabel(%q) = %q, want it cut with an ellipsis", long, got)
	}
	if got := tabShortLabel("SELECT 1"); got != "SELECT 1" {
		t.Fatalf("tabShortLabel = %q", got)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
