package main

// Tests for the P1 slice: the DDL viewer, the cell value editor, and the
// refresh, paste and delete the results grid gained.
//
// The DDL reader is driven against a real SQLite file, because the statement it
// reads is the server's own. The cell editor's state is the working copy the
// grid holds, so those tests build that copy by hand, and the modal draws are
// driven through the framework's Tester, as the sidebar's are.

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ben-guanzhi/dbcore"
	"github.com/egoist/mygo/ui"
)

// --- the DDL viewer -----------------------------------------------------------

// ddlApp opens a seeded SQLite file and gives the App the tables the tree would
// have loaded, so the viewer's read can be asked for a table or a view.
func ddlApp(t *testing.T) (*App, *DB, string) {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "ddl.db")
	seed(t, path)
	db, err := OpenDB(ctx, Conn{Name: "ddl", Kind: "sqlite", File: path})
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
	app := &App{db: db, conn: Conn{Kind: "sqlite"}, tables: map[string][]tableInfo{}}
	for _, tab := range cached.tablesOf(schema) {
		app.tables[schema] = append(app.tables[schema],
			tableInfo{Name: tab.Name, Type: tab.Type, Comment: tab.Comment})
	}
	return app, db, schema
}

func TestReadDDLTable(t *testing.T) {
	app, _, schema := ddlApp(t)
	n := treeNode{label: "employees", parent: schema, kind: nodeTable}

	ddl, err := app.readDDL(context.Background(), app.db, n)
	if err != nil {
		t.Fatalf("readDDL: %v", err)
	}
	upper := strings.ToUpper(ddl)
	if !strings.Contains(upper, "CREATE TABLE") {
		t.Fatalf("DDL = %q, want a CREATE TABLE statement", ddl)
	}
	if !strings.Contains(upper, "ID") || !strings.Contains(upper, "SALARY") {
		t.Fatalf("DDL is missing its columns: %q", ddl)
	}
}

// A view's DDL is not stored as a table, so the viewer asks the extra metadata
// reader, which composes the CREATE VIEW from the definition it returns.
func TestReadDDLView(t *testing.T) {
	app, db, schema := ddlApp(t)
	if _, err := db.SQL.Execute(context.Background(),
		`CREATE VIEW salaries AS SELECT id, salary FROM employees`); err != nil {
		t.Fatalf("create view: %v", err)
	}
	app.tables[schema] = []tableInfo{
		{Name: "employees", Type: "table"},
		{Name: "salaries", Type: "view"},
	}

	n := treeNode{label: "salaries", parent: schema, kind: nodeTable}
	ddl, err := app.readDDL(context.Background(), db, n)
	if err != nil {
		t.Fatalf("readDDL: %v", err)
	}
	upper := strings.ToUpper(ddl)
	if !strings.Contains(upper, "CREATE") || !strings.Contains(upper, "VIEW") {
		t.Fatalf("DDL = %q, want a CREATE VIEW statement", ddl)
	}
	if !strings.Contains(upper, "SALARIES") {
		t.Fatalf("DDL does not name the view: %q", ddl)
	}
}

// A view the server does not have is reported rather than shown as empty DDL.
func TestReadDDLUnknownObject(t *testing.T) {
	app, _, schema := ddlApp(t)
	app.tables[schema] = []tableInfo{{Name: "ghost", Type: "view"}}
	n := treeNode{label: "ghost", parent: schema, kind: nodeTable}
	if _, err := app.readDDL(context.Background(), app.db, n); err == nil {
		t.Fatal("a view the server does not have: want an error, got none")
	}
}

func TestTableType(t *testing.T) {
	app := &App{tables: map[string][]tableInfo{
		"public": {{Name: "people", Type: "table"}, {Name: "v", Type: "view"}},
	}}
	if got := app.tableType(treeNode{label: "v", parent: "public", kind: nodeTable}); got != "view" {
		t.Fatalf("tableType(view) = %q", got)
	}
	if got := app.tableType(treeNode{label: "people", parent: "public", kind: nodeTable}); got != "table" {
		t.Fatalf("tableType(table) = %q", got)
	}
	// An object the list does not name is taken to be a table, which is what the
	// driver's own DDL reader answers for it.
	if got := app.tableType(treeNode{label: "elsewhere", parent: "public", kind: nodeTable}); got != "table" {
		t.Fatalf("tableType(unknown) = %q", got)
	}
}

// The viewer's tree menu opens the modal and reads the statement into it: with no
// window in a test, the load fills the state directly.
func TestOpenDDL(t *testing.T) {
	app, _, schema := ddlApp(t)
	app.initModalEditors()

	n := treeNode{label: "employees", parent: schema, kind: nodeTable}
	app.openDDL(n)
	if !app.ddlOpen {
		t.Fatal("openDDL did not open the modal")
	}
	if app.ddlBusy {
		t.Fatal("loadDDL left the load marked as running")
	}
	if !strings.Contains(strings.ToUpper(app.ddlBuf.String()), "CREATE TABLE") {
		t.Fatalf("the viewer's buffer = %q", app.ddlBuf.String())
	}
	if !app.ddlState.ReadOnly {
		t.Fatal("the viewer's editor is not read-only")
	}
}

func TestOpenDDLWithoutConnection(t *testing.T) {
	app := &App{}
	app.initModalEditors()
	app.openDDL(treeNode{label: "employees", parent: "main", kind: nodeTable})
	if app.ddlOpen {
		t.Fatal("a modal opened without a connection")
	}
	if app.errMsg == "" {
		t.Fatal("opening the DDL without a connection says nothing")
	}
}

// A modal that is open draws over the view: its title and its buttons are on
// screen, and the editor inside it is the one whose buffer the read filled.
func TestDDLModalRenders(t *testing.T) {
	app := newViewApp(t, nil)
	app.ddlOpen = true
	app.ddlBusy = false
	app.ddlTable = "employees"
	app.ddlBuf.Set("CREATE TABLE employees (id INTEGER);")

	tt := ui.NewTester(app.view, 1200, 760)
	for _, want := range []string{"DDL", "employees", "Generate INSERTs", "Copy DDL", "Close"} {
		if !tt.HasText(want) {
			t.Errorf("the DDL modal does not show %q; texts = %v", want, tt.Texts())
		}
	}
}

// --- the cell value editor ----------------------------------------------------

// editApp is an App with a working copy of the seeded table loaded into the grid,
// the way a browse leaves it.
func editApp(t *testing.T) *App {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "cell.db")
	seed(t, path)
	db, err := OpenDB(ctx, Conn{Name: "cell", Kind: "sqlite", File: path})
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	cached := newCached(db)
	if err := cached.LoadSchemas(ctx); err != nil {
		t.Fatalf("LoadSchemas: %v", err)
	}
	schema := cached.SchemaNames()[0]
	if err := cached.setColumns(ctx, schema, "employees"); err != nil {
		t.Fatalf("setColumns: %v", err)
	}
	cols := cached.columnsOf(schema, "employees")

	res, err := db.SQL.Execute(ctx, "SELECT * FROM employees ORDER BY id")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	app := &App{db: db, conn: Conn{Kind: "sqlite"},
		cols: map[string][]dbcore.ColumnInfo{}, tables: map[string][]tableInfo{}}
	app.initModalEditors()
	app.cols[tableKey(schema, "employees")] = cols
	app.result = res
	app.startEdit(res, schema, "employees", func() {})
	return app
}

func TestOpenCellEditable(t *testing.T) {
	app := editApp(t)
	app.openCell(0, 2) // the name column of the first row
	if !app.cellOpen {
		t.Fatal("openCell did not open the editor")
	}
	if got := app.cellBuf.String(); got != "Ada" {
		t.Fatalf("the editor's text = %q, want Ada", got)
	}
	if app.cellState.ReadOnly {
		t.Fatal("a working copy's cell editor is read-only")
	}
	if app.cellRow != 0 || app.cellCol != 1 {
		t.Fatalf("cell = (%d, %d), want (0, 1)", app.cellRow, app.cellCol)
	}
}

func TestApplyCellEditsWorkingCopy(t *testing.T) {
	app := editApp(t)
	app.openCell(0, 2)
	app.cellBuf.Set("Ada Lovelace")
	app.applyCell()
	if app.cellOpen {
		t.Fatal("applyCell left the editor open")
	}
	if got := app.edit.rows[0].strs[1]; got != "Ada Lovelace" {
		t.Fatalf("the working copy's cell = %q, want Ada Lovelace", got)
	}
	if app.edit.rows[0].state != rowModified {
		t.Fatalf("the edited row is not marked modified: %v", app.edit.rows[0].state)
	}
	if !app.edit.dirty() {
		t.Fatal("an applied cell did not make the working copy dirty")
	}
}

// A NULL cell opens empty, and Apply leaves it empty: a NULL stays NULL.
func TestApplyCellNullStaysNull(t *testing.T) {
	app := editApp(t)
	// The second row's salary is NULL, which is its third column.
	app.edit.rows[1].strs[2] = ""
	app.edit.rows[1].origNull[2] = true
	app.openCell(1, 3)
	if got := app.cellBuf.String(); got != "" {
		t.Fatalf("a NULL cell opened as %q, want empty", got)
	}
	app.applyCell()
	if got := app.edit.rows[1].strs[2]; got != "" {
		t.Fatalf("a NULL cell applied as %q", got)
	}
}

func TestApplyCellBlobRejectsBadHex(t *testing.T) {
	app := editApp(t)
	// Make the id column a blob, so the cell it holds is hex-edited.
	app.edit.cols[0].Type = "BLOB"
	for _, r := range app.edit.rows {
		r.kinds[0] = dbcore.ValBlob
	}
	app.openCell(0, 1) // the id column
	if !app.cellBlob {
		t.Fatal("a blob cell did not open as a blob")
	}
	app.cellBuf.Set("not hex at all")
	app.applyCell()
	if !app.cellOpen {
		t.Fatal("applyCell closed the editor over invalid hex")
	}
	if app.cellErr == "" {
		t.Fatal("applyCell did not report invalid hex")
	}
}

func TestApplyCellBlobHex(t *testing.T) {
	app := editApp(t)
	app.edit.cols[0].Type = "BLOB"
	for _, r := range app.edit.rows {
		r.kinds[0] = dbcore.ValBlob
		r.strs[0] = "00ff10"
	}
	app.openCell(0, 1)
	if !app.cellBlob || !app.cellHex {
		t.Fatal("a blob cell did not open as hex")
	}
	if got := app.cellBuf.String(); got != "00ff10" {
		t.Fatalf("the blob's editor holds %q, want 00ff10", got)
	}
	// Editing it as hex writes the bytes it names back into the copy.
	app.cellBuf.Set("deadbeef")
	app.applyCell()
	if got := app.edit.rows[0].strs[0]; got != string([]byte{0xde, 0xad, 0xbe, 0xef}) {
		t.Fatalf("the applied blob = %q", got)
	}
}

func TestPasteCellText(t *testing.T) {
	app := editApp(t)
	app.pasteCellText(0, 2, "Grace Hopper")
	if got := app.edit.rows[0].strs[1]; got != "Grace Hopper" {
		t.Fatalf("the pasted cell = %q", got)
	}
	if app.edit.rows[0].state != rowModified {
		t.Fatal("a pasted cell did not mark its row")
	}
	if !app.edit.dirty() {
		t.Fatal("pasting did not make the working copy dirty")
	}
}

func TestPasteCellTextBlob(t *testing.T) {
	app := editApp(t)
	app.edit.cols[0].Type = "BLOB"
	for _, r := range app.edit.rows {
		r.kinds[0] = dbcore.ValBlob
	}
	// A hex text becomes the bytes it names.
	app.pasteCellText(0, 1, "deadbeef")
	if got := app.edit.rows[0].strs[0]; got != string([]byte{0xde, 0xad, 0xbe, 0xef}) {
		t.Fatalf("a hex pasted blob = %q", got)
	}
	// A text that is not hex is refused, and the cell is left alone.
	app.pasteCellText(0, 1, "not hex")
	if app.errMsg == "" {
		t.Fatal("a non-hex paste into a blob was not reported")
	}
}

func TestDeleteRowAt(t *testing.T) {
	app := editApp(t)
	app.deleteRowAt(0)
	if app.edit.rows[0].state != rowDeleted {
		t.Fatalf("the row's state = %v, want deleted", app.edit.rows[0].state)
	}
	if !app.edit.dirty() {
		t.Fatal("deleting a row did not make the copy dirty")
	}

	// A row inserted on the page is taken out of the copy rather than marked,
	// because it never existed in the table.
	app.edit.addRow([]string{"", "Grace", "150000"}, rowInserted)
	ins := len(app.edit.rows) - 1
	app.deleteRowAt(ins)
	if len(app.edit.rows) != 2 {
		t.Fatalf("an inserted row survived its own deletion: %d rows", len(app.edit.rows))
	}
	if app.edit.rows[ins-1].state == rowInserted {
		t.Fatal("an unrelated row is marked inserted")
	}
}

// The cell modal draws over the view, and its editor holds the cell's text in the
// buffer the read opened it with.
func TestCellModalRenders(t *testing.T) {
	app := newViewApp(t, nil)
	app.cellOpen = true
	app.cellState.ReadOnly = true
	app.cellBuf.Set("the whole value")

	tt := ui.NewTester(app.view, 1200, 760)
	for _, want := range []string{"Value of", "Copy", "Cancel", "Apply"} {
		if !tt.HasText(want) {
			t.Errorf("the cell modal does not show %q; texts = %v", want, tt.Texts())
		}
	}
}

// A value the grid shows as one line is held in full by the editor, and the
// grid's double click on a cell is what opens it.
func TestCellEditorForLongValue(t *testing.T) {
	app := editApp(t)
	// A very long name that one line would cut off.
	app.edit.rows[0].strs[1] = strings.Repeat("x", 300)
	app.edit.rows[0].state = rowModified
	app.openCell(0, 2)
	if got := app.cellBuf.String(); len(got) != 300 {
		t.Fatalf("the editor holds %d characters, want 300", len(got))
	}
}

// The read-only grid's cell editor shows the value, and an Apply on it has
// nothing to write, so it closes without changing the result.
func TestOpenCellReadOnly(t *testing.T) {
	app := &App{}
	app.initModalEditors()
	app.result = &dbcore.QueryResult{
		Columns: []dbcore.ColumnInfo{{Name: "n"}},
		Rows:    [][]dbcore.Value{{{Kind: dbcore.ValText, S: "hello"}}},
	}
	app.openCell(0, 1)
	if !app.cellOpen {
		t.Fatal("openCell did not open the editor on a read-only result")
	}
	if got := app.cellBuf.String(); got != "hello" {
		t.Fatalf("the editor's text = %q", got)
	}
	if !app.cellState.ReadOnly {
		t.Fatal("a read-only result's cell editor is editable")
	}
	app.cellBuf.Set("would not apply")
	app.applyCell()
	if app.cellOpen {
		t.Fatal("applyCell left a read-only editor open")
	}
	if got := app.result.Rows[0][0].String(); got != "hello" {
		t.Fatalf("an Apply changed the read-only result to %q", got)
	}
}

// The grid draws a read-only result's cells, so its double click is where the
// editor opens from.
func TestRenderTableReadOnlyDrawsCells(t *testing.T) {
	app := newViewApp(t, nil)
	app.result = &dbcore.QueryResult{
		Columns: []dbcore.ColumnInfo{{Name: "name"}},
		Rows:    [][]dbcore.Value{{{Kind: dbcore.ValText, S: "Ada"}}},
	}
	app.pager = newResultPager()
	tt := ui.NewTester(app.view, 1200, 760)
	if !tt.HasText("Ada") {
		t.Fatalf("the read-only grid does not show the cell; texts = %v", tt.Texts())
	}
}
