package main

// Tests for the extended object tree (P1 #8): the tree layers the extra
// objects — indexes, foreign keys, CHECK constraints, partitions under a table;
// sequences, functions and enums under a schema — that the base tree did not
// show, and the definitions it composes for each. Driven against a real SQLite
// file, which exercises the supported sections (views, indexes, foreign keys)
// and the hidden ones (SQLite has no sequences, routines, enums, partitions or
// CHECK-constraint catalog).

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ben-guanzhi/dbcore"
)

// objApp opens a SQLite file with a second table referencing employees, an
// index, and a view, and gives the App the tables a connected window loaded.
func objApp(t *testing.T) (*App, *DB, string) {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "obj.db")
	seed(t, path)
	db, err := OpenDB(ctx, Conn{Name: "obj", Kind: "sqlite", File: path})
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if _, err := db.SQL.Execute(ctx, `CREATE INDEX idx_emp_salary ON employees(salary)`); err != nil {
		t.Fatalf("create index: %v", err)
	}
	// SQLite has no ALTER TABLE ... ADD CONSTRAINT, so the foreign key is
	// declared where the referencing table is created.
	if _, err := db.SQL.Execute(ctx, `
		PRAGMA foreign_keys = ON;
		CREATE TABLE project_emp (
			project_id INTEGER PRIMARY KEY,
			employee_id INTEGER,
			CONSTRAINT fk_emp_project FOREIGN KEY (employee_id) REFERENCES employees(id)
		)`); err != nil {
		t.Fatalf("create referencing table: %v", err)
	}
	if _, err := db.SQL.Execute(ctx,
		`CREATE VIEW top_salaries AS SELECT id, salary FROM employees WHERE salary > 150000`); err != nil {
		t.Fatalf("create view: %v", err)
	}

	cached := newCached(db)
	if err := cached.LoadSchemas(ctx); err != nil {
		t.Fatalf("LoadSchemas: %v", err)
	}
	schema := cached.SchemaNames()[0]
	if err := cached.setTables(ctx, schema); err != nil {
		t.Fatalf("setTables: %v", err)
	}
	app := &App{db: db, conn: Conn{Kind: "sqlite"},
		tables:  map[string][]tableInfo{},
		cols:    map[string][]dbcore.ColumnInfo{},
		objects: objectCache{},
	}
	for _, tab := range cached.tablesOf(schema) {
		app.tables[schema] = append(app.tables[schema],
			tableInfo{Name: tab.Name, Type: tab.Type, Comment: tab.Comment})
	}
	// A connected window loads a table's columns when it opens; do the same so
	// the table branch shows its sections.
	for _, tab := range app.tables[schema] {
		_ = cached.setColumns(ctx, schema, tab.Name)
		app.cols[tableKey(schema, tab.Name)] = cached.columnsOf(schema, tab.Name)
	}
	return app, db, schema
}

func TestTreeSchemaChildrenIncludeObjectSections(t *testing.T) {
	app, _, schema := objApp(t)
	children := app.treeChildren(treeNode{label: schema, kind: nodeSchema})
	var sawTable, sawSection bool
	for _, c := range children {
		switch c.kind {
		case nodeTable:
			sawTable = true
		case nodeSection:
			sawSection = true
		}
	}
	if !sawTable {
		t.Fatal("schema children have no tables")
	}
	if !sawSection {
		t.Fatal("schema children have no object sections")
	}
}

func TestTreeTableChildrenIncludeObjectSections(t *testing.T) {
	app, _, schema := objApp(t)
	children := app.treeChildren(treeNode{label: "employees", kind: nodeTable, parent: schema})
	var sawColumn, sawSection bool
	for _, c := range children {
		switch c.kind {
		case nodeColumn:
			sawColumn = true
		case nodeSection:
			sawSection = true
		}
	}
	if !sawColumn {
		t.Fatal("table children have no columns")
	}
	if !sawSection {
		t.Fatal("table children have no object sections")
	}
}

// The indexes section of a table loads its leaves from ListIndexes.
func TestIndexSectionLoadsLeaves(t *testing.T) {
	app, _, schema := objApp(t)
	tableKey := schema + "\x00" + "employees"
	_ = tableKey
	app.objects = objectCache{}

	names, _, _ := app.objects.leaves(objIndex, schema, "employees")
	if len(names) != 0 {
		t.Fatalf("an unasked section has leaves: %v", names)
	}
	app.loadObjectSection(schema, "employees", sectionOf(objIndex))

	names, loaded, _ := app.objects.leaves(objIndex, schema, "employees")
	if !loaded {
		t.Fatal("the section did not load")
	}
	found := false
	for _, n := range names {
		if n == "idx_emp_salary" {
			found = true
		}
	}
	if !found {
		t.Fatalf("index section = %v, want idx_emp_salary", names)
	}
}

// The primary key is not a separate secondary index, so it does not appear.
func TestIndexSectionSkipsPrimaryKey(t *testing.T) {
	app, _, schema := objApp(t)
	app.objects = objectCache{}
	app.loadObjectSection(schema, "employees", sectionOf(objIndex))
	names, _, _ := app.objects.leaves(objIndex, schema, "employees")
	for _, n := range names {
		if n == "pk_employees" || n == "sqlite_autoindex_employees_1" {
			t.Fatalf("primary key leaked into the index section: %v", names)
		}
	}
}

// SQLite has no sequences, routines, enums, partitions or CHECK catalog, so
// those sections resolve to "not supported" and are hidden.
func TestUnsupportedSectionsAreHidden(t *testing.T) {
	app, _, schema := objApp(t)
	app.objects = objectCache{}

	cases := []struct {
		kind  int
		table string
	}{
		{objSequence, ""},
		{objRoutine, ""},
		{objEnum, ""},
		{objCheck, "employees"},
		{objPartition, "employees"},
	}
	for _, c := range cases {
		app.loadObjectSection(schema, c.table, sectionOf(c.kind))
		_, _, notSupported := app.objects.leaves(c.kind, schema, c.table)
		if !notSupported {
			t.Errorf("section kind %d was not reported unsupported on SQLite", c.kind)
		}
	}
	// And the schema's treeChildren drops a not-supported section from view.
	for _, s := range schemaSections {
		if app.sectionUsable(schema, "", s) {
			t.Errorf("schema section %q shown but SQLite has none", s.label)
		}
	}
}

func TestViewDefinitionComposed(t *testing.T) {
	app, _, schema := objApp(t)
	n := treeNode{label: "top_salaries", parent: schema, kind: nodeTable}
	ddl, err := app.readDDL(context.Background(), app.db, n)
	if err != nil {
		t.Fatalf("readDDL: %v", err)
	}
	upper := strings.ToUpper(ddl)
	if !strings.Contains(upper, "CREATE VIEW") || !strings.Contains(upper, "TOP_SALARIES") {
		t.Fatalf("view DDL = %q, want a CREATE VIEW top_salaries", ddl)
	}
}

func TestObjectDefinitionRoute(t *testing.T) {
	app, _, schema := objApp(t)

	t.Run("index", func(t *testing.T) {
		n := treeNode{label: "idx_emp_salary", kind: nodeObject, group: objIndex,
			parent: schema + "\x00" + "employees"}
		def, err := app.objectDDL(context.Background(), n)
		if err != nil {
			t.Fatalf("objectDDL index: %v", err)
		}
		upper := strings.ToUpper(def)
		if !strings.Contains(upper, "CREATE") || !strings.Contains(upper, "INDEX") ||
			!strings.Contains(upper, "IDX_EMP_SALARY") {
			t.Fatalf("index DDL = %q", def)
		}
	})

	t.Run("foreign key", func(t *testing.T) {
		// SQLite names its foreign keys synthetically (fk_<id>) rather than by
		// the declared constraint name, so discover what the driver reports.
		fks, err := app.db.SQL.ListForeignKeys(context.Background(), schema, "project_emp")
		if err != nil {
			t.Fatalf("ListForeignKeys: %v", err)
		}
		if len(fks) == 0 {
			t.Fatal("project_emp has no reported foreign key")
		}
		name := fks[0].Name
		n := treeNode{label: name, kind: nodeObject, group: objFK,
			parent: schema + "\x00" + "project_emp"}
		def, err := app.objectDDL(context.Background(), n)
		if err != nil {
			t.Fatalf("objectDDL FK: %v", err)
		}
		upper := strings.ToUpper(def)
		if !strings.Contains(upper, "FOREIGN KEY") || !strings.Contains(upper, "PROJECT_EMP") {
			t.Fatalf("FK DDL = %q", def)
		}
	})

	t.Run("unsupported routine", func(t *testing.T) {
		n := treeNode{label: "nope", kind: nodeObject, group: objRoutine, parent: schema}
		if _, err := app.objectDDL(context.Background(), n); !isNotSupported(err) {
			t.Fatalf("routine DDL on SQLite = %v, want unsupported", err)
		}
	})
}

// isNotSupported matches the kernel's not-supported sentinel.
func isNotSupported(err error) bool {
	return err == dbcore.ErrNotSupported
}

// The section cache ignores object loads for kinds a driver does not have, and
// a table node's children only show sections that are still usable.
func TestDroppedSectionsStayDropped(t *testing.T) {
	app, _, schema := objApp(t)
	app.objects = objectCache{}
	// Mark partitions unsupported, then ask for the table's children: the
	// partitions section is gone, indexes/foreign keys remain.
	app.objects.putNotSupported(objPartition, schema, "employees")

	children := app.treeChildren(treeNode{label: "employees", kind: nodeTable, parent: schema})
	for _, c := range children {
		if c.kind == nodeSection && c.group == objPartition {
			t.Fatal("a not-supported section still appears in the tree")
		}
	}
}
