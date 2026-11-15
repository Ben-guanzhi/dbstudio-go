package main

// Tests for exporting and copying only what the grid shows, and for freezing
// columns at the left.

import (
	"github.com/Ben-guanzhi/dbcore"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// --- export and copy follow the visible columns -----------------------------

// A hidden column and a moved one are both respected: the file holds the visible
// columns in the order they are drawn.
func TestVisibleResultProjects(t *testing.T) {
	app, tab := gridApp(t)                      // id, name, note
	app.moveColumn(tab, tab.result, "note", -1) // id, note, name
	app.hideColumn(tab, "note", true)           // id, name

	got := app.visibleResult(tab.result, tab)
	if len(got.Columns) != 2 {
		t.Fatalf("columns = %d, want 2", len(got.Columns))
	}
	if got.Columns[0].Name != "id" || got.Columns[1].Name != "name" {
		t.Fatalf("columns = %v, want id, name", got.Columns)
	}
	if len(got.Rows) != 1 || len(got.Rows[0]) != 2 {
		t.Fatalf("rows = %v", got.Rows)
	}
	// The name cell is the second one, not the original third.
	if got.Rows[0][1].S != "ada" {
		t.Fatalf("second cell = %+v, want ada", got.Rows[0][1])
	}
}

// Nothing hidden or moved: the result passes through untouched (no copy).
func TestVisibleResultPassesThrough(t *testing.T) {
	app, tab := gridApp(t)
	if got := app.visibleResult(tab.result, tab); got != tab.result {
		t.Fatal("an unarranged result was copied")
	}
}

// The CSV export of a hidden column's result leaves the column out.
func TestExportCsvFollowsVisibleColumns(t *testing.T) {
	app, tab := gridApp(t)
	app.hideColumn(tab, "name", true)
	res := app.visibleResult(tab.result, tab)
	text, err := csvText(res)
	if err != nil {
		t.Fatalf("csvText: %v", err)
	}
	if strings.Contains(text, "name") {
		t.Errorf("the hidden column is in the CSV:\n%s", text)
	}
	if !strings.Contains(text, "id") || !strings.Contains(text, "note") {
		t.Errorf("a visible column is missing:\n%s", text)
	}
}

// The INSERT script follows the visible columns too: it must not write a column
// the user hid.
func TestInsertScriptFollowsVisibleColumns(t *testing.T) {
	app, tab := gridApp(t)
	tab.schema, tab.table = "main", "employees"
	tab.sql = "SELECT * FROM employees"
	// An INSERT needs the result to be one table's rows, which the working copy
	// is what records.
	app.edit.init(tab.result, "main", "employees",
		[]dbcore.ColumnInfo{{Name: "id", Type: "INTEGER"}, {Name: "name", Type: "TEXT"}, {Name: "note", Type: "TEXT"}},
		func() {})
	app.hideColumn(tab, "note", true)
	res := app.visibleResult(tab.result, tab)
	script, err := app.insertScript(res)
	if err != nil {
		t.Fatalf("insertScript: %v", err)
	}
	if strings.Contains(script, "note") {
		t.Errorf("the hidden column is in the INSERT:\n%s", script)
	}
}

// Hiding a key column refuses UPDATE/DELETE rather than writing a statement that
// addresses no row.
func TestCopyUpdateRefusesHiddenKey(t *testing.T) {
	app, tab := gridApp(t)
	app.edit.init(tab.result, "main", "employees", []dbcore.ColumnInfo{
		{Name: "id", Type: "INTEGER", IsPrimaryKey: true},
		{Name: "name", Type: "TEXT"},
		{Name: "note", Type: "TEXT"},
	}, func() {})
	app.conn = Conn{Kind: "sqlite"}
	// id is the key; hiding it leaves an UPDATE that cannot name its row.
	app.hideColumn(tab, "id", true)
	res := app.visibleResult(tab.result, tab)
	if _, err := app.copyText(copyUpdate, res, nil); err == nil {
		t.Fatal("an UPDATE was generated with the key hidden")
	} else if !strings.Contains(err.Error(), "key column") {
		t.Fatalf("the refusal does not explain itself: %v", err)
	}
	// Showing it again lets the statement through.
	app.hideColumn(tab, "id", false)
	res = app.visibleResult(tab.result, tab)
	if _, err := app.copyText(copyUpdate, res, nil); err != nil {
		t.Fatalf("an UPDATE with the key shown failed: %v", err)
	}
}

// --- freezing columns -------------------------------------------------------

// Freezing holds the visible columns up to a position at the left, in the order
// they are in, and moves them there.
func TestFreezeTo(t *testing.T) {
	app, tab := gridApp(t)                      // id, name, note
	app.moveColumn(tab, tab.result, "note", -1) // id, note, name
	// Freeze up to drawn position 1, which is note.
	app.freezeTo(tab, tab.result, 1)
	if !tab.frozen("id") || !tab.frozen("note") {
		t.Fatalf("frozen = %v, want id and note", tab.colFrozen)
	}
	if tab.frozen("name") {
		t.Fatal("name was frozen")
	}
	// The frozen ones come first, in their previous relative order.
	if order := app.columnOrder(tab, tab.result); !strsEqual(order, []string{"id", "note", "name"}) {
		t.Fatalf("order = %v", order)
	}
	// The drawn columns are frozen: the grid marks them Fixed, which is what
	// keeps them first and unresizable.
	visible := app.visibleColumns(tab, tab.result)
	if got := tab.frozen(tab.result.Columns[visible[0]].Name); !got {
		t.Fatal("the first drawn column is not frozen")
	}
}

// A frozen column cannot be moved, and says why.
func TestFrozenColumnCannotMove(t *testing.T) {
	app, tab := gridApp(t)
	app.freezeTo(tab, tab.result, 0) // id
	before := app.columnOrder(tab, tab.result)
	app.moveColumn(tab, tab.result, "id", +1)
	if after := app.columnOrder(tab, tab.result); !strsEqual(before, after) {
		t.Fatalf("a frozen column moved: %v -> %v", before, after)
	}
	if !strings.Contains(app.status, "frozen") {
		t.Fatalf("no reason given: %q", app.status)
	}
}

// Unfreezing drops every hold but keeps the order.
func TestUnfreeze(t *testing.T) {
	app, tab := gridApp(t)
	app.freezeTo(tab, tab.result, 1)
	app.unfreeze(tab)
	if len(tab.colFrozen) != 0 {
		t.Fatalf("frozen = %v after unfreezing", tab.colFrozen)
	}
	// The order the freeze left (the natural one) stays.
	if order := app.columnOrder(tab, tab.result); !strsEqual(order, []string{"id", "name", "note"}) {
		t.Fatalf("order = %v", order)
	}
}

// Freezing is remembered per table, like hiding and ordering.
func TestFreezePersists(t *testing.T) {
	app, tab := gridApp(t)
	store, err := NewStoreAt(filepath.Join(t.TempDir(), "connections.json"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	app.store = store
	tab.schema, tab.table = "main", "employees"
	app.freezeTo(tab, tab.result, 0)

	got := loadSession(app.sessionPath())
	l, ok := got.ColLayouts["main\x00employees"]
	if !ok {
		t.Fatalf("no layout saved: %v", got.ColLayouts)
	}
	if !strsEqual(l.Frozen, []string{"id"}) {
		t.Fatalf("saved frozen = %v", l.Frozen)
	}
	// Reopening the table freezes it again.
	restored := &App{}
	restored.applySession(got)
	fresh := newTab("employees", tabBrowse)
	fresh.schema, fresh.table = "main", "employees"
	fresh.result = tab.result
	restored.applyColumnLayout(fresh)
	if !fresh.frozen("id") {
		t.Fatalf("the reopened tab is not frozen: %v", fresh.colFrozen)
	}
}

// The header menu offers freezing, and the dialog lists it.
func TestFreezeMenuAndDialog(t *testing.T) {
	app, tab := gridApp(t)
	// Out-of-range columns are ignored by the menu.
	app.headerMenu(&ui.Menu{}, -1)
	app.headerMenu(&ui.Menu{}, 99)
	app.freezeTo(tab, tab.result, 0)
	app.openColumns()
	tt := ui.NewTester(app.columnsView, 700, 600)
	if !tt.HasText("Columns") {
		t.Fatalf("the dialog does not draw; texts = %v", tt.Texts())
	}
}
