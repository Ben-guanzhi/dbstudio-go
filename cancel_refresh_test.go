package main

// Tests for the two gap-analysis fixes: the run's generation (a superseded or
// cancelled run must not overwrite a newer one) and Refresh Schema (every
// cached table/column/object answer is dropped, the generation moves, and the
// tree asks again).

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/dbcore/dbcore"
)

// --- P1-2: cancellation and the run generation -------------------------------

// A result that answers after a newer run started is dropped: the newer run
// owns the tabs.
func TestApplyRunDropsSuperseded(t *testing.T) {
	app := &App{pager: newResultPager(), gridSel: -1}
	app.initModalEditors()
	tab := newTab("Result 1", tabRun)
	app.tabs = []*resTab{tab}
	app.runGen = 2 // the newer run's generation
	app.beginBusy("run")

	res := &dbcore.QueryResult{
		Columns: []dbcore.ColumnInfo{{Name: "id"}},
		Rows:    [][]dbcore.Value{{{Kind: dbcore.ValInt, I: 1}}},
	}
	// A stale run (generation 1) reports its result.
	app.applyRun(1, []*resTab{tab}, []string{"SELECT 1"}, []runOut{{sets: []*dbcore.QueryResult{res}}}, 0, false, false)
	if tab.result != nil {
		t.Fatal("a superseded run wrote its result into the tab")
	}
	if !app.busy {
		t.Fatal("a superseded run cleared the newer run's busy state")
	}
	// The current run's answer lands and ends the busy state.
	app.applyRun(2, []*resTab{tab}, []string{"SELECT 1"}, []runOut{{sets: []*dbcore.QueryResult{res}}}, 0, false, false)
	if tab.result == nil || app.busy {
		t.Fatalf("the current run's result did not land: busy=%v result=%v", app.busy, tab.result)
	}
}

// A cancelled run marks its tab Cancelled rather than Failed: the user asked for
// the stop, so it is not an error.
func TestApplyRunCancelledStatus(t *testing.T) {
	app := &App{pager: newResultPager(), gridSel: -1}
	app.initModalEditors()
	tab := newTab("Result 1", tabRun)
	app.tabs = []*resTab{tab}
	app.runGen = 1
	app.beginBusy("run")

	app.applyRun(1, []*resTab{tab}, []string{"SELECT pg_sleep(600)"},
		[]runOut{{err: context.Canceled}}, 0, false, true)
	if app.busy {
		t.Fatal("a cancelled run left the app busy")
	}
	if app.status != "Query cancelled" {
		t.Fatalf("status = %q, want Query cancelled", app.status)
	}
	if tab.status != "Cancelled" {
		t.Fatalf("tab status = %q, want Cancelled", tab.status)
	}
	if tab.errMsg != "" {
		t.Fatalf("a cancelled run is not an error: %q", tab.errMsg)
	}
}

// Stop with no run in flight is a no-op; the button is disabled then anyway.
func TestCancelRunNoopWhenIdle(t *testing.T) {
	app := &App{}
	app.cancelRun() // must not panic on a nil handle
	if app.status != "" {
		t.Fatalf("an idle stop changed the status to %q", app.status)
	}
}

// Stop while busy pulls the handle the run installed.
func TestCancelRunPullsHandle(t *testing.T) {
	app := &App{busy: true}
	called := false
	app.runCancel = func() { called = true }
	app.cancelRun()
	if !called {
		t.Fatal("Stop did not cancel the running query")
	}
	if app.status != "Cancelling…" {
		t.Fatalf("status = %q", app.status)
	}
}

// A finished run clears its handle, so a late Stop cannot cancel a dead context
// and a new run starts from a clean slate.
func TestApplyRunClearsCancel(t *testing.T) {
	app := &App{pager: newResultPager(), gridSel: -1}
	app.initModalEditors()
	tab := newTab("Result 1", tabRun)
	app.tabs = []*resTab{tab}
	app.runGen = 1
	app.beginBusy("run")
	app.runCancel = func() {}
	app.applyRun(1, []*resTab{tab}, []string{"SELECT 1"}, []runOut{{}}, 0, false, false)
	if app.runCancel != nil {
		t.Fatal("a finished run left its cancel handle installed")
	}
}

// --- P1-1: Refresh Schema ---------------------------------------------------

// refreshApp opens the seeded SQLite file with a loaded schema, so the refresh
// has something to drop.
func refreshApp(t *testing.T) (*App, string) {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "refresh.db")
	seed(t, path)
	db, err := OpenDB(ctx, Conn{Name: "r", Kind: "sqlite", File: path})
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	cached := newCached(db)
	if err := cached.LoadSchemas(ctx); err != nil {
		t.Fatalf("LoadSchemas: %v", err)
	}
	schema := cached.SchemaNames()[0]
	if err := cached.setTables(ctx, schema); err != nil {
		t.Fatalf("setTables: %v", err)
	}
	if err := cached.setColumns(ctx, schema, "employees"); err != nil {
		t.Fatalf("setColumns: %v", err)
	}
	app := &App{db: db, conn: Conn{Kind: "sqlite"}, open: true,
		schemas: []string{schema},
		tables:  map[string][]tableInfo{},
		cols:    map[string][]dbcore.ColumnInfo{},
		objects: objectCache{}}
	app.initModalEditors()
	for _, tab := range cached.tablesOf(schema) {
		app.tables[schema] = append(app.tables[schema],
			tableInfo{Name: tab.Name, Type: tab.Type, Comment: tab.Comment})
	}
	app.cols[tableKey(schema, "employees")] = cached.columnsOf(schema, "employees")
	return app, schema
}

// The refresh drops every cached answer: tables, columns and the object
// sections, and moves the generation so a load in flight is dropped.
func TestRefreshSchemaClearsCatalog(t *testing.T) {
	app, schema := refreshApp(t)
	if _, st := app.TablesOf(schema); st == TablesNotLoaded {
		t.Fatal("the fixture did not load the schema")
	}
	if _, st := app.ColumnsOf(schema, "employees"); st != ColsLoaded {
		t.Fatal("the fixture did not load the columns")
	}
	app.objects.putNotSupported(objSequence, schema, "")

	genBefore := app.treeGen
	app.refreshSchema()
	if app.treeGen == genBefore {
		t.Fatal("the refresh did not move the generation")
	}
	if _, st := app.ColumnsOf(schema, "employees"); st != ColsNotLoaded {
		t.Fatalf("the columns survived the refresh: state=%v", st)
	}
	if app.objects != nil {
		t.Fatalf("the object sections survived the refresh: %v", app.objects)
	}
	if app.status != "Schema refreshed" {
		t.Fatalf("status = %q", app.status)
	}
	// The table list is asked for again (the schema the refresh knows is the
	// first one), so the tree shows current tables without a second click.
	if _, st := app.TablesOf(schema); st == TablesNotLoaded {
		t.Fatal("the refresh did not reload the first schema's tables")
	}
}

// The refresh keeps the connection, the consoles and the results: it is about
// the schema, not the session.
func TestRefreshSchemaKeepsSession(t *testing.T) {
	app, _ := refreshApp(t)
	app.initConsoles()
	app.edBuf = app.current().buf
	app.edBuf.Set("SELECT 1;")
	buf := app.edBuf
	tab := newTab("Result 1", tabRun)
	app.tabs = []*resTab{tab}

	app.refreshSchema()
	if app.db == nil || !app.open {
		t.Fatal("the refresh dropped the connection")
	}
	if app.edBuf != buf || app.edBuf.String() != "SELECT 1;" {
		t.Fatal("the refresh touched the editor")
	}
	if len(app.tabs) != 1 || app.tabs[0] != tab {
		t.Fatal("the refresh dropped the results")
	}
	// The tree's selection no longer addresses anything.
	if app.treeSel != -1 || app.selTable != "" {
		t.Fatalf("the refresh kept a stale selection: sel=%d table=%q", app.treeSel, app.selTable)
	}
}

func TestRefreshSchemaNotConnected(t *testing.T) {
	app := &App{}
	app.refreshSchema()
	if app.errMsg == "" {
		t.Fatal("a refresh with no connection reported no reason")
	}
	if app.status != "Not connected" {
		t.Fatalf("status = %q", app.status)
	}
}

// A second refresh is harmless: the generation moves again and the cache stays
// empty until the tree asks.
func TestRefreshSchemaTwice(t *testing.T) {
	app, schema := refreshApp(t)
	g1 := app.treeGen
	app.refreshSchema()
	g2 := app.treeGen
	app.refreshSchema()
	if app.treeGen <= g2 || g2 <= g1 {
		t.Fatalf("generations: %d → %d → %d, want each to move", g1, g2, app.treeGen)
	}
	if _, st := app.ColumnsOf(schema, "employees"); st != ColsNotLoaded {
		t.Fatalf("columns came back: state=%v", st)
	}
}

// A disconnect cancels the in-flight run and moves the generation: a result
// already on its way must not be written into the tabs the disconnect clears.
func TestDisconnectCancelsInflightRun(t *testing.T) {
	app := &App{pager: newResultPager(), gridSel: -1}
	app.initModalEditors()
	app.open = true
	app.autocommit = true
	cancelled := false
	app.runCancel = func() { cancelled = true }
	app.runGen = 3

	app.disconnect()

	if !cancelled {
		t.Fatal("disconnect did not cancel the in-flight run")
	}
	if app.runCancel != nil {
		t.Fatal("disconnect left the cancel handle behind")
	}
	if app.runGen != 4 {
		t.Fatalf("runGen = %d, want the disconnect to move it to 4", app.runGen)
	}
	if app.open {
		t.Fatal("the app is still open after disconnect")
	}
}

// A short task finishing must not clear a longer task's busy state: each
// asynchronous layer owns its mark and busy is the union. Before ownership, a
// browse or a load-more that finished first cleared the flag a script run still
// held — Stop went disabled mid-run while the status bar disagreed.
func TestBusyOwnership(t *testing.T) {
	app := &App{pager: newResultPager(), gridSel: -1}
	app.initModalEditors()

	app.beginBusy("run")
	app.beginBusy("browse")
	app.endBusy("browse")
	if !app.busy {
		t.Fatal("the browse finishing cleared the run's busy state")
	}
	app.endBusy("run")
	if app.busy {
		t.Fatal("busy stayed set with no owners left")
	}
	// clearBusy drops every mark (the tab bar or connection is going away).
	app.beginBusy("run")
	app.beginBusy("edit")
	app.clearBusy()
	if app.busy {
		t.Fatal("clearBusy left busy set")
	}
}
