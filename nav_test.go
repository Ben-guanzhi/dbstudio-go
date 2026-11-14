package main

// Tests for editor navigation and hover documentation (#18): finding the name
// under an offset (and refusing one inside a literal or a comment), resolving it
// through the metadata cache (bare, schema-qualified, alias-qualified), the
// hover text it produces, and go-to-definition opening the right DDL.

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dbcore/dbcore"
	"github.com/egoist/mygo/ui/editor"
)

// navApp opens a seeded SQLite file and gives the App the metadata a connected
// window caches, so a name resolves the way it does at run time.
func navApp(t *testing.T) (*App, string) {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "nav.db")
	seed(t, path)
	db, err := OpenDB(ctx, Conn{Name: "nav", Kind: "sqlite", File: path})
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

	app := &App{db: db, conn: Conn{Kind: "sqlite"},
		schemas: []string{schema},
		tables:  map[string][]tableInfo{},
		cols:    map[string][]dbcore.ColumnInfo{},
		objects: objectCache{},
	}
	app.initModalEditors()
	for _, tab := range cached.tablesOf(schema) {
		app.tables[schema] = append(app.tables[schema],
			tableInfo{Name: tab.Name, Type: tab.Type, Comment: tab.Comment})
	}
	app.cols[tableKey(schema, "employees")] = cached.columnsOf(schema, "employees")
	app.edBuf = editor.NewBuffer("")
	app.edState = &editor.State{Buffer: app.edBuf}
	return app, schema
}

func TestIdentifierAtPlain(t *testing.T) {
	text := "SELECT id FROM employees WHERE id > 1"
	off := strings.Index(text, "employees") + 2
	parts := identifierAt(text, off)
	if len(parts) != 1 || parts[0].name != "employees" {
		t.Fatalf("parts = %+v, want [employees]", parts)
	}
	if got := text[parts[0].start:parts[0].end]; got != "employees" {
		t.Fatalf("range covers %q", got)
	}
}

func TestIdentifierAtDotted(t *testing.T) {
	text := "SELECT e.name FROM employees e"
	off := strings.Index(text, "name") + 1
	parts := identifierAt(text, off)
	if len(parts) != 2 || parts[0].name != "e" || parts[1].name != "name" {
		t.Fatalf("parts = %+v, want [e name]", parts)
	}
}

func TestIdentifierAtQuoted(t *testing.T) {
	cases := []struct {
		text string
		want string
	}{
		{`SELECT * FROM "my table"`, "my table"},
		{"SELECT * FROM `my table`", "my table"},
		{"SELECT * FROM [my table]", "my table"},
	}
	for _, c := range cases {
		off := strings.Index(c.text, "my table") + 2
		parts := identifierAt(c.text, off)
		if len(parts) != 1 || parts[0].name != c.want {
			t.Errorf("identifierAt(%q) = %+v, want %q", c.text, parts, c.want)
		}
	}
}

func TestIdentifierAtSkipsLiteralsAndComments(t *testing.T) {
	cases := []string{
		"SELECT 'employees' FROM t",
		"SELECT 1 -- employees",
		"SELECT 1 /* employees */ FROM t",
	}
	for _, text := range cases {
		off := strings.Index(text, "employees") + 2
		if parts := identifierAt(text, off); len(parts) != 0 {
			t.Errorf("identifierAt(%q) at the literal = %+v, want none", text, parts)
		}
	}
}

func TestIdentifierAtWhitespace(t *testing.T) {
	if parts := identifierAt("SELECT  id", 7); len(parts) != 0 {
		t.Fatalf("identifierAt on whitespace = %+v, want none", parts)
	}
}

func TestResolveTableName(t *testing.T) {
	app, schema := navApp(t)
	text := "SELECT * FROM employees"
	parts := identifierAt(text, strings.Index(text, "employees")+1)
	info, ok := app.resolveName(text, parts)
	if !ok {
		t.Fatal("employees did not resolve")
	}
	if !info.table || info.view || info.col != nil {
		t.Fatalf("info = %+v, want a table", info)
	}
	if info.ref.schema != schema || info.ref.table != "employees" {
		t.Fatalf("ref = %+v, want %s.employees", info.ref, schema)
	}
}

func TestResolveAliasColumn(t *testing.T) {
	app, _ := navApp(t)
	text := "SELECT e.name FROM employees e"
	parts := identifierAt(text, strings.Index(text, "name")+1)
	info, ok := app.resolveName(text, parts)
	if !ok {
		t.Fatal("e.name did not resolve")
	}
	if info.col == nil || info.col.Name != "name" {
		t.Fatalf("info = %+v, want column name", info)
	}
	if info.ref.table != "employees" {
		t.Fatalf("alias resolved to %q, want employees", info.ref.table)
	}
}

func TestResolveBareColumn(t *testing.T) {
	app, _ := navApp(t)
	text := "SELECT salary FROM employees"
	parts := identifierAt(text, strings.Index(text, "salary")+1)
	info, ok := app.resolveName(text, parts)
	if !ok {
		t.Fatal("salary did not resolve")
	}
	if info.col == nil || info.col.Name != "salary" {
		t.Fatalf("info = %+v, want column salary", info)
	}
}

func TestResolveUnknownName(t *testing.T) {
	app, _ := navApp(t)
	text := "SELECT nope FROM nothing_here"
	parts := identifierAt(text, strings.Index(text, "nope")+1)
	if _, ok := app.resolveName(text, parts); ok {
		t.Fatal("an unknown name resolved")
	}
}

func TestHoverTextColumn(t *testing.T) {
	app, _ := navApp(t)
	text := "SELECT e.name FROM employees e"
	parts := identifierAt(text, strings.Index(text, "name")+1)
	info, ok := app.resolveName(text, parts)
	if !ok {
		t.Fatal("e.name did not resolve")
	}
	got := app.hoverText(info)
	for _, want := range []string{"column", "employees.name", "name", "TEXT"} {
		if !strings.Contains(got, want) {
			t.Errorf("hover = %q, want %q", got, want)
		}
	}
}

func TestHoverTextTable(t *testing.T) {
	app, schema := navApp(t)
	text := "SELECT * FROM employees"
	parts := identifierAt(text, strings.Index(text, "employees")+1)
	info, _ := app.resolveName(text, parts)
	got := app.hoverText(info)
	if !strings.Contains(got, "table "+schema+".employees") {
		t.Errorf("hover = %q, want the table's qualified name", got)
	}
	if !strings.Contains(got, "column(s)") {
		t.Errorf("hover = %q, want the column count", got)
	}
}

func TestHoverIdentifierShows(t *testing.T) {
	app, _ := navApp(t)
	text := "SELECT e.name FROM employees e"
	app.edBuf.Set(text)
	off := strings.Index(text, "name") + 1
	app.hoverIdentifier(editor.HoverRequest{Offset: off})
	// A hover shown needs the editor's internal state; what the app controls is
	// that asking for one does not panic and the name resolves.
	info, ok := app.resolveName(text, identifierAt(text, off))
	if !ok || info.col == nil {
		t.Fatalf("hover path did not resolve: %+v", info)
	}
}

func TestGoToDefinitionOpensDDL(t *testing.T) {
	app, schema := navApp(t)
	text := "SELECT e.name FROM employees e"
	app.edBuf.Set(text)
	app.goToDefinition(strings.Index(text, "name") + 1)
	if !app.ddlOpen {
		t.Fatal("goToDefinition did not open the DDL viewer")
	}
	if app.ddlTable != "employees" || app.ddlSchema != schema {
		t.Fatalf("DDL opened for %s.%s, want %s.employees",
			app.ddlSchema, app.ddlTable, schema)
	}
}

func TestGoToDefinitionUnknownDoesNothing(t *testing.T) {
	app, _ := navApp(t)
	text := "SELECT nope FROM nothing_here"
	app.edBuf.Set(text)
	app.goToDefinition(strings.Index(text, "nope") + 1)
	if app.ddlOpen {
		t.Fatal("goToDefinition opened DDL for an unknown name")
	}
}
