package main

// Tests for the redesigned chrome: the DBeaver arrangement (toolbar, navigator,
// workbench, status bar) with DataGrip's regions inside it (object tree with a
// schema scope, results with a Messages tab).
//
// The rules the layout must keep:
//   - the toolbar changes state, the status bar reports it: one fact, one place;
//   - every region has a title and an empty state, so no region is ever blank
//     for an unexplained reason;
//   - the messages cost no room until their tab is chosen.

import (
	"testing"

	"github.com/dbcore/dbcore"
	"github.com/egoist/mygo/ui"
)

// The status bar reports the connection, the working schema and the engine —
// the "where am I" that used to be scattered.
func TestStatusBarReportsContext(t *testing.T) {
	app := newViewApp(t, nil)
	app.open = true
	app.conn = Conn{Name: "prod", Kind: "mysql", Database: "gs_prod"}
	app.connName = "prod"
	app.schema = "gs_prod"
	app.autocommit = true

	tt := ui.NewTester(app.statusBar, 1200, 32)
	for _, want := range []string{"prod", "schema: gs_prod", "MySQL"} {
		if !tt.HasText(want) {
			t.Errorf("the status bar does not report %q; texts = %v", want, tt.Texts())
		}
	}
}

// With no connection it says so, rather than showing empty fields.
func TestStatusBarNotConnected(t *testing.T) {
	app := newViewApp(t, nil)
	tt := ui.NewTester(app.statusBar, 1200, 32)
	if !tt.HasText("Not connected") {
		t.Fatalf("the status bar claims a connection; texts = %v", tt.Texts())
	}
}

// An open transaction is the one state worth colouring: the bar names it.
func TestStatusBarShowsTransaction(t *testing.T) {
	app := newViewApp(t, nil)
	app.open = true
	app.conn = Conn{Name: "pg", Kind: "postgres", Schema: "public"}
	app.txActive = true
	tt := ui.NewTester(app.statusBar, 1200, 32)
	if !tt.HasText("in transaction") {
		t.Fatalf("an open transaction is not reported; texts = %v", tt.Texts())
	}
}

// The navigator is titled, and with no connection it explains itself instead of
// drawing an empty tree.
func TestNavigatorHeaderAndEmptyState(t *testing.T) {
	app := newViewApp(t, nil)
	tt := ui.NewTester(app.navigator, 320, 600)
	if !tt.HasText("Navigator") {
		t.Fatalf("the navigator has no title; texts = %v", tt.Texts())
	}
	if !tt.HasText("Not connected — pick a connection above") {
		t.Fatalf("the navigator has no empty state; texts = %v", tt.Texts())
	}
}

// The results area is a body with two tabs: the grid, and the messages for the
// statement on screen. Choosing Messages shows the report; the grid's tab shows
// the data.
func TestResultsMessagesTab(t *testing.T) {
	app := newViewApp(t, nil)
	app.tabs = nil
	res := &dbcore.QueryResult{
		Columns: []dbcore.ColumnInfo{{Name: "id"}},
		Rows:    [][]dbcore.Value{{{Kind: dbcore.ValInt, I: 7}}},
	}
	tb := newTab("Result 1", tabRun)
	tb.result = res
	tb.status = "1 row(s)"
	tb.elapsed = 1
	app.tabs = []*resTab{tb}
	app.tabSel = 0
	app.result = res
	app.pager.reset(false)
	app.pager.total = 1

	// The data tab: the grid is drawn and the messages are not.
	app.resView = 0
	data := ui.NewTester(app.resultsPane, 900, 500)
	if !data.HasText("Data") || !data.HasText("Messages") {
		t.Fatalf("the body's tabs are missing; texts = %v", data.Texts())
	}
	if !data.HasText("id") {
		t.Errorf("the data tab does not draw the grid's column; texts = %v", data.Texts())
	}

	// The messages tab: the statement's report replaces the grid.
	app.resView = 1
	msgs := ui.NewTester(app.resultsPane, 900, 500)
	if !msgs.HasText("1 row(s)") {
		t.Errorf("the messages tab does not report the statement's outcome; texts = %v", msgs.Texts())
	}
	if msgs.HasText("id") {
		t.Errorf("the messages tab still draws the grid; texts = %v", msgs.Texts())
	}
}

// A result that has not run yet says how to make one.
func TestResultsEmptyState(t *testing.T) {
	app := newViewApp(t, nil)
	app.tabs = nil
	tt := ui.NewTester(app.resultsPane, 900, 400)
	if !tt.HasText("No results yet — run a statement (Ctrl+Enter)") {
		t.Fatalf("the results area has no empty state; texts = %v", tt.Texts())
	}
}

// The toolbar is actions only: no status line, no health dot (the status bar
// owns both), and the groups are all reachable. 波 100：图标按钮的可见文本为空，
// 但 accessible name（Label）进 Texts()，断言照旧成立。
func TestToolbarIsActionsOnly(t *testing.T) {
	app := newViewApp(t, nil)
	app.open = true
	app.conn = Conn{Name: "c", Kind: "sqlite"}
	app.health = "ok"
	tt := ui.NewTester(app.toolbar, 1600, 40)
	for _, want := range []string{"Run", "Run Selection", "Run to Caret", "Clear", "Refresh", "Export", "History"} {
		if !tt.HasText(want) {
			t.Errorf("the toolbar is missing %q; texts = %v", want, tt.Texts())
		}
	}
	if tt.HasText("●") {
		t.Error("the toolbar still draws the health dot; the status bar owns it")
	}
}

// The navigator keeps the object tree's own tools: schema scope and search.
func TestNavigatorHasSchemaAndSearch(t *testing.T) {
	app := newViewApp(t, nil)
	app.open = true
	app.conn = Conn{Name: "c", Kind: "postgres"}
	app.schemas = []string{"public", "sales"}
	tt := ui.NewTester(app.navigator, 360, 700)
	if !tt.HasText("Schema") {
		t.Errorf("the navigator has no schema scope; texts = %v", tt.Texts())
	}
	// The tree's roots prove the object area drew. (The search field is a
	// TextInput whose placeholder is not part of the readable text.)
	if !tt.HasText("public") || !tt.HasText("sales") {
		t.Errorf("the navigator does not draw the schema roots; texts = %v", tt.Texts())
	}
}
