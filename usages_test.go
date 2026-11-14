package main

// Tests for find usages (#23): the walk over the loaded tables' foreign keys
// finds the tables that reference the target (and skips the target itself),
// reports the walk's coverage honestly, and lands the findings in a results tab.

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dbcore/dbcore"
)

// usagesApp opens the seeded SQLite file with a referencing table, and gives
// the App the loaded schema a connected window caches.
func usagesApp(t *testing.T) (*App, string) {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "usage.db")
	seed(t, path)
	db, err := OpenDB(ctx, Conn{Name: "u", Kind: "sqlite", File: path})
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.SQL.Execute(ctx, `CREATE TABLE project_emp (
		id INTEGER PRIMARY KEY,
		employee_id INTEGER,
		CONSTRAINT fk_emp FOREIGN KEY (employee_id) REFERENCES employees(id))`); err != nil {
		t.Fatalf("create referencing table: %v", err)
	}

	cached := newCached(db)
	if err := cached.LoadSchemas(ctx); err != nil {
		t.Fatalf("LoadSchemas: %v", err)
	}
	schema := cached.SchemaNames()[0]
	if err := cached.setTables(ctx, schema); err != nil {
		t.Fatalf("setTables: %v", err)
	}
	app := &App{db: db, conn: Conn{Kind: "sqlite"}, schemas: []string{schema},
		tables:  map[string][]tableInfo{},
		cols:    map[string][]dbcore.ColumnInfo{},
		objects: objectCache{}}
	app.initModalEditors()
	for _, tab := range cached.tablesOf(schema) {
		app.tables[schema] = append(app.tables[schema],
			tableInfo{Name: tab.Name, Type: tab.Type, Comment: tab.Comment})
	}
	return app, schema
}

// The walk is what the goroutine runs; drive it directly and apply its answer.
func runUsagesWalk(app *App, target treeNode, column string) []usageRow {
	var rows []usageRow
	seen := 0
	schema := target.parent
	tabs, st := app.TablesOf(schema)
	if st == TablesNotLoaded {
		return nil
	}
	for _, tab := range tabs {
		if strings.EqualFold(tab.Name, target.label) && strings.EqualFold(schema, target.parent) {
			continue
		}
		fks, err := app.db.SQL.ListForeignKeys(context.Background(), schema, tab.Name)
		if err != nil {
			continue
		}
		seen++
		for _, fk := range fks {
			if !strings.EqualFold(fk.RefTable, target.label) {
				continue
			}
			if column != "" && !refColumnsContain(fk.RefColumns, column) {
				continue
			}
			rows = append(rows, usageRow{
				schema:    schema,
				table:     tab.Name,
				key:       fk.Name,
				columns:   strings.Join(fk.RefColumns, ", "),
				ourColumn: strings.Join(fk.Columns, ", "),
			})
		}
	}
	_ = seen
	return rows
}

func TestFindUsagesTable(t *testing.T) {
	app, schema := usagesApp(t)
	rows := runUsagesWalk(app, treeNode{label: "employees", parent: schema}, "")
	if len(rows) != 1 {
		t.Fatalf("rows = %+v, want one (project_emp)", rows)
	}
	if rows[0].table != "project_emp" {
		t.Fatalf("usage table = %q, want project_emp", rows[0].table)
	}
	if rows[0].columns != "id" {
		t.Fatalf("referenced columns = %q, want id", rows[0].columns)
	}
	// The referencing table's own column is reported too.
	if rows[0].ourColumn != "employee_id" {
		t.Fatalf("own column = %q, want employee_id", rows[0].ourColumn)
	}
}

// A table nothing references answers none — and a table with no FKs cached
// elsewhere does not stop the walk.
func TestFindUsagesNone(t *testing.T) {
	app, schema := usagesApp(t)
	rows := runUsagesWalk(app, treeNode{label: "project_emp", parent: schema}, "")
	if len(rows) != 0 {
		t.Fatalf("rows = %+v, want none", rows)
	}
}

// refColumnsContain is the column filter: case-insensitive, any of the key's
// referenced columns matching.
func TestRefColumnsContain(t *testing.T) {
	cols := []string{"ID", "Name"}
	if !refColumnsContain(cols, "id") {
		t.Fatal("id did not match ID")
	}
	if !refColumnsContain(cols, "NAME") {
		t.Fatal("NAME did not match Name")
	}
	if refColumnsContain(cols, "salary") {
		t.Fatal("salary matched")
	}
	if refColumnsContain(nil, "id") {
		t.Fatal("an empty key matched")
	}
}

// showUsages lands the findings in a titled tab with the result's columns and
// the coverage in its status.
func TestShowUsagesTab(t *testing.T) {
	app, schema := usagesApp(t)
	app.open = true
	n := treeNode{label: "employees", parent: schema, kind: nodeTable}
	rows := runUsagesWalk(app, n, "")
	app.showUsages(n, "", rows, 3, nil)

	tb := app.tab()
	if tb == nil {
		t.Fatal("no tab opened")
	}
	if tb.label != "Usages of employees" {
		t.Fatalf("tab label = %q", tb.label)
	}
	if tb.result == nil || len(tb.result.Rows) != 1 {
		t.Fatalf("result rows = %v", tb.result)
	}
	if !strings.Contains(tb.status, "1 usage(s) across 3 table(s)") {
		t.Fatalf("status = %q", tb.status)
	}
	// The columns are the ones a reader sorts and filters by.
	names := make([]string, len(tb.result.Columns))
	for i, c := range tb.result.Columns {
		names[i] = c.Name
	}
	if got := strings.Join(names, ","); got != "schema,table,foreign key,references,columns" {
		t.Fatalf("columns = %q", got)
	}
}

// An empty finding still opens the tab, with its "none" status: the answer
// "nothing references it" is a result, not a silence.
func TestShowUsagesEmpty(t *testing.T) {
	app, schema := usagesApp(t)
	app.open = true
	n := treeNode{label: "project_emp", parent: schema, kind: nodeTable}
	app.showUsages(n, "", nil, 3, nil)
	tb := app.tab()
	if tb == nil {
		t.Fatal("no tab opened")
	}
	if len(tb.result.Rows) != 0 {
		t.Fatalf("rows = %v, want none", tb.result.Rows)
	}
	if app.status != "No usages found for project_emp" {
		t.Fatalf("status = %q", app.status)
	}
}
