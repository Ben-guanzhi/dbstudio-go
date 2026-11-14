package main

// Tests for hiding and reordering a result's columns: the arrangement is per
// tab, the drawn column maps back to the right value, and the last column cannot
// be hidden into an empty grid.

import (
	"testing"

	"github.com/dbcore/dbcore"
	"github.com/egoist/mygo/ui"
)

// gridApp is an App with one result tab of three columns.
func gridApp(t *testing.T) (*App, *resTab) {
	t.Helper()
	app := newViewApp(t, nil)
	// A SQLite connection: the generated SQL needs an engine to quote for.
	app.conn = Conn{Name: "local", Kind: "sqlite"}
	res := &dbcore.QueryResult{
		Columns: []dbcore.ColumnInfo{
			{Name: "id", Type: "INTEGER"},
			{Name: "name", Type: "TEXT"},
			{Name: "note", Type: "TEXT"},
		},
		Rows: [][]dbcore.Value{
			{{Kind: dbcore.ValInt, I: 1}, {Kind: dbcore.ValText, S: "ada"}, {Kind: dbcore.ValText, S: "first"}},
		},
	}
	tab := newTab("Result 1", tabRun)
	tab.result = res
	tab.raw = res
	tab.pager.reset(false)
	tab.pager.total = 1
	app.tabs = []*resTab{tab}
	app.tabSel = 0
	app.loadTab(tab)
	app.pager = tab.pager
	return app, tab
}

// No arrangement means every column, in the result's own order.
func TestVisibleColumnsDefault(t *testing.T) {
	app, tab := gridApp(t)
	got := app.visibleColumns(tab, tab.result)
	want := []int{0, 1, 2}
	if !intsEqual(got, want) {
		t.Fatalf("visible = %v, want %v", got, want)
	}
}

// Hiding removes a column from the drawn set, and the mapping from a drawn
// position to the result's column still points at the right value.
func TestHideColumnKeepsMapping(t *testing.T) {
	app, tab := gridApp(t)
	app.hideColumn(tab, "name", true)
	visible := app.visibleColumns(tab, tab.result)
	if !intsEqual(visible, []int{0, 2}) {
		t.Fatalf("visible = %v, want id and note", visible)
	}
	// Drawn position 2 (after the row marker) is the note column, which is
	// result column 2 — not 1, which is hidden.
	if got := t2col(visible, 2); got != 2 {
		t.Fatalf("drawn column 2 maps to %d, want 2 (note)", got)
	}
	if got := t2col(visible, 1); got != 0 {
		t.Fatalf("drawn column 1 maps to %d, want 0 (id)", got)
	}
	if got := t2col(visible, 0); got != -1 {
		t.Fatalf("the row marker maps to %d, want -1", got)
	}
	// And the value the grid would draw for drawn column 2 is the note.
	v := tab.result.Rows[0][t2col(visible, 2)]
	if v.S != "first" {
		t.Fatalf("drawn column 2 shows %q, want first", v.S)
	}
}

// Showing it again puts it back in its place.
func TestShowColumnAgain(t *testing.T) {
	app, tab := gridApp(t)
	app.hideColumn(tab, "name", true)
	app.hideColumn(tab, "name", false)
	if !intsEqual(app.visibleColumns(tab, tab.result), []int{0, 1, 2}) {
		t.Fatalf("visible = %v after showing again", app.visibleColumns(tab, tab.result))
	}
}

// The last visible column stays: hiding every column would leave a grid of
// nothing.
func TestLastColumnCannotBeHidden(t *testing.T) {
	app, tab := gridApp(t)
	app.hideColumn(tab, "id", true)
	app.hideColumn(tab, "name", true)
	app.hideColumn(tab, "note", true)
	visible := app.visibleColumns(tab, tab.result)
	if len(visible) != 1 {
		t.Fatalf("visible = %v, want exactly one column left", visible)
	}
	if app.status == "" {
		t.Fatal("nothing explained the refusal")
	}
}

// Moving a column swaps it with its neighbor, hidden ones included, so a hidden
// column keeps its place in the order.
func TestMoveColumn(t *testing.T) {
	app, tab := gridApp(t)
	app.moveColumn(tab, tab.result, "note", -1)
	if got := app.columnOrder(tab, tab.result); !strsEqual(got, []string{"id", "note", "name"}) {
		t.Fatalf("order = %v", got)
	}
	// The drawn order follows: note now comes before name.
	if got := app.visibleColumns(tab, tab.result); !intsEqual(got, []int{0, 2, 1}) {
		t.Fatalf("visible = %v", got)
	}
	// Moving past the edge changes nothing.
	app.moveColumn(tab, tab.result, "id", -1)
	if got := app.columnOrder(tab, tab.result); !strsEqual(got, []string{"id", "note", "name"}) {
		t.Fatalf("a move past the edge changed the order: %v", got)
	}
}

// Two tabs arrange independently: hiding `id` in one leaves it in the other.
func TestColumnArrangementIsPerTab(t *testing.T) {
	app, first := gridApp(t)
	app.hideColumn(first, "id", true)

	second := newTab("Result 2", tabRun)
	second.result = first.result
	second.raw = first.result
	app.tabs = append(app.tabs, second)

	if got := app.visibleColumns(second, second.result); len(got) != 3 {
		t.Fatalf("the second tab inherited the first tab's hidden column: %v", got)
	}
	if got := app.visibleColumns(first, first.result); len(got) != 2 {
		t.Fatalf("the first tab lost its arrangement: %v", got)
	}
}

// Showing all columns clears the hidden set but keeps the order.
func TestShowAllKeepsOrder(t *testing.T) {
	app, tab := gridApp(t)
	app.moveColumn(tab, tab.result, "note", -1)
	app.hideColumn(tab, "id", true)
	app.showAllColumns(tab)
	if got := app.columnOrder(tab, tab.result); !strsEqual(got, []string{"id", "note", "name"}) {
		t.Fatalf("order = %v after showing all", got)
	}
	if got := app.visibleColumns(tab, tab.result); len(got) != 3 {
		t.Fatalf("visible = %v, want every column", got)
	}
}

// An order that names a column the result does not have keeps the rest in the
// result's own order: a stale arrangement must not lose columns.
func TestOrderWithUnknownName(t *testing.T) {
	app, tab := gridApp(t)
	tab.colOrder = []string{"note", "gone", "id"}
	got := app.visibleColumns(tab, tab.result)
	if !intsEqual(got, []int{2, 0, 1}) {
		t.Fatalf("visible = %v, want note, id, name", got)
	}
	if order := app.columnOrder(tab, tab.result); !strsEqual(order, []string{"note", "id", "name"}) {
		t.Fatalf("order = %v", order)
	}
}

// The Columns dialog draws every column with its state, and the header menu
// offers the actions.
func TestColumnsDialogAndHeaderMenu(t *testing.T) {
	app, tab := gridApp(t)
	app.hideColumn(tab, "name", true)
	app.openColumns()
	if !app.columns.open {
		t.Fatal("the dialog did not open")
	}
	tt := ui.NewTester(app.columnsView, 700, 600)
	for _, want := range []string{"Columns", "id", "name", "note", "Show All", "Reset Order", "Close"} {
		if !tt.HasText(want) {
			t.Errorf("the dialog does not show %q; texts = %v", want, tt.Texts())
		}
	}

	// The header menu works on the result's column index: an out-of-range one
	// (the row marker) is ignored rather than panicking.
	app.headerMenu(&ui.Menu{}, -1)
	app.headerMenu(&ui.Menu{}, 99)
	if got := app.visibleColumns(tab, tab.result); len(got) != 2 {
		t.Fatalf("building the menu changed the arrangement: %v", got)
	}
}

// intsEqual and strsEqual keep the assertions readable.
func intsEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func strsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
