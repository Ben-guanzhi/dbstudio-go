package main

// Tests for the header's column tooltip and for remembering a table's column
// arrangement across visits.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ben-guanzhi/dbcore"
	"github.com/egoist/mygo/ui"
)

// --- the header tooltip -----------------------------------------------------

// The tooltip says what the column is: its name, type, nullability and key. A
// browse adds what the catalog knows beyond the result set (key, default,
// comment).
func TestColumnInfoText(t *testing.T) {
	app, tab := gridApp(t)
	res := &dbcore.QueryResult{
		Columns: []dbcore.ColumnInfo{
			{Name: "id", Type: "INTEGER", Nullable: false},
			{Name: "name", Type: "TEXT", Nullable: true},
		},
	}
	if got := app.columnInfoText(tab, res, 0); !strings.Contains(got, "id") ||
		!strings.Contains(got, "INTEGER") || !strings.Contains(got, "not null") {
		t.Fatalf("tooltip = %q, want the name, type and nullability", got)
	}
	if got := app.columnInfoText(tab, res, 1); !strings.Contains(got, "null") {
		t.Fatalf("a nullable column's tooltip = %q", got)
	}
	// Out of range is empty, not a panic.
	if got := app.columnInfoText(tab, res, 9); got != "" {
		t.Fatalf("an out-of-range column produced %q", got)
	}
	// A nil result is the same.
	if got := app.columnInfoText(tab, nil, 0); got != "" {
		t.Fatalf("a nil result produced %q", got)
	}

	// A browse tab's tooltip uses the catalog for the primary key and comment.
	app, browseTab := gridApp(t)
	app.tables["main"] = []tableInfo{{Name: "employees", Type: "table"}}
	dflt := "0"
	app.cols[tableKey("main", "employees")] = []dbcore.ColumnInfo{
		{Name: "id", Type: "INTEGER", IsPrimaryKey: true, Default: &dflt, Comment: "the key"},
	}
	browseTab.schema, browseTab.table = "main", "employees"
	bres := &dbcore.QueryResult{
		Columns: []dbcore.ColumnInfo{{Name: "id", Type: "INTEGER"}},
	}
	got := app.columnInfoText(browseTab, bres, 0)
	for _, want := range []string{"primary key", "default 0", "the key"} {
		if !strings.Contains(got, want) {
			t.Errorf("the browse tooltip lacks %q: %s", want, got)
		}
	}
}

// The tooltip is attached as the grid draws: drawing a result through the pane
// must not panic with the header hook installed (the hook runs on a real cell).
// TestFrozenColumnInfoSaysFrozen：冻结的列要在自己身上说出来——网格不画分隔线
// （mygo 的 Fixed 只固定位置），tooltip 的 "frozen" 是屏幕阅读器与悬停能读到
// 的那一半，标题上的强调线是眼睛能读到的那一半。
func TestFrozenColumnInfoSaysFrozen(t *testing.T) {
	app, tab := gridApp(t)
	// 冻结前两列（id、name），note 保持活动。
	app.freezeTo(tab, tab.result, 1)

	if got := app.columnInfoText(tab, tab.result, 0); !strings.Contains(got, "frozen") {
		t.Errorf("冻结列的说明没有 frozen：%q", got)
	}
	if got := app.columnInfoText(tab, tab.result, 1); !strings.Contains(got, "frozen") {
		t.Errorf("冻结列的说明没有 frozen：%q", got)
	}
	if got := app.columnInfoText(tab, tab.result, 2); strings.Contains(got, "frozen") {
		t.Errorf("没冻结的列被说成 frozen：%q", got)
	}
}

func TestHeaderInfoRendersInPane(t *testing.T) {
	app, _ := gridApp(t)
	// Headers are built by the framework; this draws the whole pane so the
	// hook is exercised for the row marker and every column.
	ui.NewTester(app.resultsPane, 900, 400)
}

// --- the arrangement, remembered per table ----------------------------------

// A browse tab's arrangement is written to the session under its table, and the
// next tab for that table opens with it.
func TestColumnLayoutPersistsPerTable(t *testing.T) {
	app, tab := gridApp(t)
	store, err := NewStoreAt(filepath.Join(t.TempDir(), "connections.json"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	app.store = store
	tab.schema, tab.table = "main", "employees"

	app.hideColumn(tab, "note", true)
	app.moveColumn(tab, tab.result, "name", -1)

	// The session holds the layout under "main\x00employees".
	got := loadSession(app.sessionPath())
	key := "main\x00employees"
	l, ok := got.ColLayouts[key]
	if !ok {
		t.Fatalf("no layout saved; layouts = %v", got.ColLayouts)
	}
	if !strsEqual(l.Order, []string{"name", "id", "note"}) {
		t.Fatalf("saved order = %v", l.Order)
	}
	if !strsEqual(l.Hidden, []string{"note"}) {
		t.Fatalf("saved hidden = %v", l.Hidden)
	}

	// A new tab for the same table opens with that arrangement.
	restored := &App{}
	restored.applySession(got)
	fresh := newTab("employees", tabBrowse)
	fresh.schema, fresh.table = "main", "employees"
	fresh.result = tab.result
	restored.applyColumnLayout(fresh)
	if !strsEqual(fresh.colOrder, []string{"name", "id", "note"}) {
		t.Fatalf("the reopened tab's order = %v", fresh.colOrder)
	}
	if !fresh.colHidden["note"] {
		t.Fatalf("the reopened tab's hidden = %v", fresh.colHidden)
	}
	// The arrangement narrows what is drawn: two columns, note hidden.
	if got := restored.visibleColumns(fresh, tab.result); len(got) != 2 {
		t.Fatalf("the reopened tab draws %d columns, want 2", len(got))
	}
}

// A query result has no table to remember it by: its arrangement stays with the
// tab, and nothing is written.
func TestColumnLayoutNotSavedForQueries(t *testing.T) {
	app, tab := gridApp(t)
	store, err := NewStoreAt(filepath.Join(t.TempDir(), "connections.json"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	app.store = store
	// tab.schema/tab.table are empty: an ordinary query result.
	app.hideColumn(tab, "note", true)
	if len(app.colLayouts) != 0 {
		t.Fatalf("a query result was saved as a table: %v", app.colLayouts)
	}
	if _, err := os.Stat(app.sessionPath()); err == nil {
		t.Fatal("a query result wrote a session file")
	}
}

// Writing the same arrangement twice does not rewrite the session: a header drag
// changes the order many times, and each real change should be one write.
func TestColumnLayoutWritesOnChangeOnly(t *testing.T) {
	app, tab := gridApp(t)
	store, err := NewStoreAt(filepath.Join(t.TempDir(), "connections.json"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	app.store = store
	tab.schema, tab.table = "main", "employees"
	app.moveColumn(tab, tab.result, "note", -1)

	// The file's bytes, not its timestamp: a write inside one filesystem tick
	// has the same mtime, which made a timestamp comparison flaky under load.
	before := readBytes(t, app.sessionPath())
	app.saveColumnLayout(tab) // the same arrangement: no write
	app.saveColumnLayout(tab)
	if !bytes.Equal(readBytes(t, app.sessionPath()), before) {
		t.Fatal("an unchanged arrangement rewrote the session")
	}

	app.hideColumn(tab, "id", true) // a real change does write
	if bytes.Equal(readBytes(t, app.sessionPath()), before) {
		t.Fatal("a changed arrangement did not write the session")
	}
}

// readBytes is the session file's content, for the write-only-on-change check.
func readBytes(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}
