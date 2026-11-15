package main

// The structure viewer (波 100): a table's columns as a grid — the "Structure"
// page TablePlus and DataGrip put beside the data. The facts are the columns
// the kernel's ListColumns returns; the modal shows them as rows, and the tree
// row's menu is the way in.

import (
	"testing"

	"github.com/Ben-guanzhi/dbcore"
	"github.com/egoist/mygo/ui"
)

// A table's Structure opens the modal and shows one row per column: name,
// type, key mark, nullability, default. A column that is neither PK nor
// auto-increment leaves the key cell empty.
func TestStructureViewShowsColumns(t *testing.T) {
	app, _ := refreshApp(t)
	app.initConsoles()
	app.initEditorState()
	app.stOpen = true
	app.stTable = "main.employees"
	app.stCols = []dbcore.ColumnInfo{
		{Name: "id", Type: "INTEGER", IsPrimaryKey: true, IsAutoIncrement: true},
		{Name: "name", Type: "TEXT", Nullable: true},
		{Name: "salary", Type: "NUMERIC", Default: strPtr("0")},
	}
	tt := ui.NewTester(app.structureView, 720, 480)
	for _, want := range []string{"Structure · main.employees", "Name", "Type", "Key", "Null", "Default", "Comment", "id", "name", "salary", "PK", "NOT NULL", "0"} {
		if !tt.HasText(want) {
			t.Errorf("the structure view is missing %q; texts = %v", want, tt.Texts())
		}
	}
}

// The heading splits back into schema and table for the DDL hand-off.
func TestStructureHeadingParts(t *testing.T) {
	app := &App{stTable: "gs_cloud.api_log"}
	if got := app.stSchema(); got != "gs_cloud" {
		t.Errorf("schema = %q, want gs_cloud", got)
	}
	if got := app.stTableOnly(); got != "api_log" {
		t.Errorf("table = %q, want api_log", got)
	}
	app.stTable = "plain"
	if got := app.stSchema(); got != "" {
		t.Errorf("schema without a dot = %q, want empty", got)
	}
	if got := app.stTableOnly(); got != "plain" {
		t.Errorf("table without a dot = %q", got)
	}
}

// openStructure needs a connection; without one it reports, not crashes.
func TestStructureNeedsConnection(t *testing.T) {
	app := &App{}
	app.openStructure(treeNode{label: "t", parent: "main", kind: nodeTable})
	if app.stOpen {
		t.Error("structure opened without a connection")
	}
	if app.errMsg != "Not connected" {
		t.Errorf("errMsg = %q, want the not-connected note", app.errMsg)
	}
}

// A table row's menu offers Structure beside Browse Data and View DDL: the
// wiring is static, so this asserts the source's own menu table through the
// app-level entry the item calls.
func TestStructureEntryWired(t *testing.T) {
	app, _ := refreshApp(t)
	app.initConsoles()
	app.initEditorState()
	n := treeNode{label: "employees", parent: "main", kind: nodeTable}
	app.openStructure(n)
	if !app.stOpen || app.stTable != "main.employees" {
		t.Fatalf("openStructure: open=%v table=%q", app.stOpen, app.stTable)
	}
	// refreshApp 的 App 没有 window，loadAsync 走同步路径：断言已加载完成
	// 而非 in-flight（SQLite 种子库里 employees 有列）。
	if app.stBusy {
		t.Error("the synchronous load left the busy mark set")
	}
	if len(app.stCols) == 0 {
		t.Error("the synchronous load produced no columns")
	}
}

func strPtr(s string) *string { return &s }
