package main

// Tests for the trigger section of the object tree (#20): the kernel's
// TriggerLister capability is what the tree asks, SQLite's triggers load as a
// table section, and a trigger's definition opens in the DDL viewer.

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ben-guanzhi/dbcore"
)

// triggerApp opens the seeded SQLite file with a table that has a trigger, and
// gives the App the metadata a connected window caches.
func triggerApp(t *testing.T) (*App, string) {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "trigger.db")
	seed(t, path)
	db, err := OpenDB(ctx, Conn{Name: "trg", Kind: "sqlite", File: path})
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.SQL.Execute(ctx, `CREATE TABLE audit_log (id INTEGER PRIMARY KEY, note TEXT)`); err != nil {
		t.Fatalf("create audit table: %v", err)
	}
	if _, err := db.SQL.Execute(ctx, `CREATE TRIGGER trg_employees_audit AFTER INSERT ON employees
		BEGIN
			INSERT INTO audit_log (note) VALUES ('inserted');
		END`); err != nil {
		t.Fatalf("create trigger: %v", err)
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

func TestTriggerSectionLoadsLeaves(t *testing.T) {
	app, schema := triggerApp(t)
	app.loadObjectSection(schema, "employees", sectionOf(objTrigger))
	names, loaded, notSupported := app.objects.leaves(objTrigger, schema, "employees")
	if notSupported {
		t.Fatal("SQLite has triggers, so the section must not be hidden")
	}
	if !loaded {
		t.Fatal("the trigger section did not load")
	}
	if len(names) != 1 || names[0] != "trg_employees_audit" {
		t.Fatalf("leaves = %v, want trg_employees_audit", names)
	}
}

// A table without triggers answers an empty section rather than hiding it: the
// concept exists, it simply has nothing inside.
func TestTriggerSectionEmptyForPlainTable(t *testing.T) {
	app, schema := triggerApp(t)
	app.loadObjectSection(schema, "audit_log", sectionOf(objTrigger))
	names, loaded, notSupported := app.objects.leaves(objTrigger, schema, "audit_log")
	if notSupported {
		t.Fatal("a plain table's trigger section was reported unsupported")
	}
	if !loaded {
		t.Fatal("the trigger section did not load")
	}
	if len(names) != 0 {
		t.Fatalf("leaves = %v, want none", names)
	}
}

func TestTriggerDefinition(t *testing.T) {
	app, schema := triggerApp(t)
	n := treeNode{label: "trg_employees_audit", kind: nodeObject, group: objTrigger,
		parent: schema + "\x00" + "employees"}
	def, err := app.objectDDL(context.Background(), n)
	if err != nil {
		t.Fatalf("objectDDL: %v", err)
	}
	upper := strings.ToUpper(def)
	if !strings.Contains(upper, "CREATE TRIGGER") {
		t.Fatalf("definition = %q, want CREATE TRIGGER", def)
	}
	if !strings.Contains(upper, "AUDIT_LOG") {
		t.Fatalf("definition = %q, want the trigger body", def)
	}
}

// The tree shows a Triggers section under a table, after the other sections.
func TestTriggerSectionAppearsInTree(t *testing.T) {
	app, schema := triggerApp(t)
	// Columns have to be loaded before a table's children include its sections.
	app.cols[tableKey(schema, "employees")] = []dbcore.ColumnInfo{{Name: "id", Type: "INTEGER"}}
	children := app.treeChildren(treeNode{label: "employees", kind: nodeTable, parent: schema})
	var labels []string
	sawTrigger := false
	for _, c := range children {
		if c.kind == nodeSection {
			labels = append(labels, c.label)
			if c.group == objTrigger {
				sawTrigger = true
			}
		}
	}
	if !sawTrigger {
		t.Fatalf("sections = %v, want a Triggers section", labels)
	}
}

// Triggers are table-level: their section node's parent is schema \x00 table, so
// the target decodes back to the table.
func TestTriggerSectionTarget(t *testing.T) {
	n := treeNode{label: "Triggers", kind: nodeSection, parent: "public\x00employees", group: objTrigger}
	schema, table := sectionTarget(n)
	if schema != "public" || table != "employees" {
		t.Fatalf("target = %q/%q, want public/employees", schema, table)
	}
}
