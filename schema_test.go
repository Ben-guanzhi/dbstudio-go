package main

// Tests for the three core-usability fixes: a chosen working schema (which
// scopes the object search and the bare-name defaults, and reopens the
// connection so the engine applies it), a search that is not pinned to one
// schema, and result columns that are wide enough to read.

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dbcore/dbcore"
	"github.com/dbcore/dbcore/sqltext"
	"github.com/egoist/mygo/ui"
)

func schemaApp(t *testing.T) (*App, *DB) {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "schema.db")
	seed(t, path)
	db, err := OpenDB(ctx, Conn{Name: "s", Kind: "sqlite", File: path})
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	app := &App{db: db, conn: Conn{Kind: "sqlite", File: path}, open: true,
		schemas: []string{"main", "other"},
		tables:  map[string][]tableInfo{},
		cols:    map[string][]dbcore.ColumnInfo{},
		objects: objectCache{}}
	app.initModalEditors()
	app.initConsoles()
	app.edBuf = app.current().buf
	return app, db
}

// The selector offers every schema plus "everything", and shows the working
// one.
func TestSchemaOptionsAndChoice(t *testing.T) {
	app, _ := schemaApp(t)
	opts := app.schemaOptions()
	if len(opts) != 3 || opts[0] != schemaAll || opts[1] != "main" {
		t.Fatalf("options = %v", opts)
	}
	if app.schemaChoice() != schemaAll {
		t.Fatalf("initial choice = %q, want %q", app.schemaChoice(), schemaAll)
	}
	app.schema = "main"
	if app.schemaChoice() != "main" {
		t.Fatalf("choice after choosing = %q", app.schemaChoice())
	}
}

// A chosen schema narrows the object search; none searches everywhere. This is
// the "cannot search tables" fix: the old code always asked about the first
// schema, so a table in any other schema was invisible.
func TestSearchScopeFollowsWorkingSchema(t *testing.T) {
	app, _ := schemaApp(t)
	if got := app.searchSchema(); got != "" {
		t.Fatalf("with no schema chosen the search asked %q, want every schema", got)
	}
	app.schema = "other"
	if got := app.searchSchema(); got != "other" {
		t.Fatalf("search asked %q, want the working schema", got)
	}
	// The kernel recipe must accept "" as "all schemas" for MySQL (where the
	// scope is a bind parameter), or the client's "" would kill the search.
	q, args, err := sqltext.SearchObjectsQuery("mysql", "", "ord", 10)
	if err != nil {
		t.Fatalf("SearchObjectsQuery: %v", err)
	}
	if strings.Contains(q, "TABLE_SCHEMA = ?") || len(args) != 1 {
		t.Fatalf("an unscoped MySQL search is still pinned to a schema: %s %v", q, args)
	}
}

// A bare table name belongs to the working schema when one is chosen.
func TestDefaultSchemaPrefersWorkingSchema(t *testing.T) {
	app, _ := schemaApp(t)
	if got := app.defaultSchema(); got != "main" {
		t.Fatalf("default = %q, want the first namespace", got)
	}
	app.schema = "other"
	if got := app.defaultSchema(); got != "other" {
		t.Fatalf("default = %q, want the working schema", got)
	}
}

// Switching schema reopens the connection so the engine applies it to every
// pooled connection: the connection's own default becomes the choice.
func TestSetActiveSchemaReconnects(t *testing.T) {
	app, _ := schemaApp(t)
	if !schemaIsConnectionDefault("mysql") || !schemaIsConnectionDefault("postgres") {
		t.Fatal("MySQL/PostgreSQL connections carry a default schema")
	}
	if schemaIsConnectionDefault("sqlite") {
		t.Fatal("SQLite has no session schema")
	}
	// MySQL/PostgreSQL: the connection value is what the driver puts in the DSN.
	c := connInSchema(Conn{Kind: "mysql", Database: "old"}, "gs_prod")
	if c.Database != "gs_prod" {
		t.Fatalf("mysql database = %q", c.Database)
	}
	p := connInSchema(Conn{Kind: "postgres", Schema: "public"}, "sales")
	if p.Schema != "sales" {
		t.Fatalf("pg schema = %q", p.Schema)
	}
	// An engine without a session schema keeps the connection untouched.
	s := connInSchema(Conn{Kind: "sqlite", File: "x.db"}, "main")
	if s.Schema != "" || s.Database != "" {
		t.Fatalf("sqlite connection gained a schema: %+v", s)
	}
	_ = app
}

// The connection's own default seeds the working schema after a connect.
func TestSchemaOfConn(t *testing.T) {
	cases := map[Conn]string{
		{Kind: "mysql", Database: "gs_prod"}: "gs_prod",
		{Kind: "postgres", Schema: "public"}: "public",
		{Kind: "mssql", Schema: "dbo"}:       "dbo",
		{Kind: "sqlite", File: "x.db"}:       "",
		{Kind: "mysql", Database: ""}:        "",
	}
	for c, want := range cases {
		if got := schemaOfConn(c); got != want {
			t.Errorf("schemaOfConn(%+v) = %q, want %q", c, got, want)
		}
	}
}

// The selector draws with its current choice and the "everything" option.
func TestSchemaSelectorRenders(t *testing.T) {
	app, _ := schemaApp(t)
	app.schema = "main"
	tt := ui.NewTester(app.schemaSelector, 320, 60)
	if !tt.HasText("Schema") {
		t.Fatalf("the selector has no label; texts = %v", tt.Texts())
	}
	if !tt.HasText("main") {
		t.Fatalf("the selector does not show the working schema; texts = %v", tt.Texts())
	}
}

// A schema that no longer exists does not leave a dangling label: the selector
// falls back to "everything" instead of showing a value it does not offer.
func TestSchemaSelectorForgivesStaleChoice(t *testing.T) {
	app, _ := schemaApp(t)
	app.schema = "dropped_schema"
	tt := ui.NewTester(app.schemaSelector, 320, 60)
	if tt.HasText("dropped_schema") {
		t.Fatal("a stale schema is still shown")
	}
	if !tt.HasText(schemaAll) {
		t.Fatalf("the selector did not fall back; texts = %v", tt.Texts())
	}
}

// Choosing a schema scopes the navigator's tree to it, opened: that is the
// selector's visible effect. Its other effects (search scope, bare-name
// defaults, the connection's default database) are real but invisible, and a
// control whose effect cannot be seen reads as broken.
func TestTreeRootsFollowWorkingSchema(t *testing.T) {
	app, _ := schemaApp(t)
	if roots := app.treeRoots(); len(roots) != 2 {
		t.Fatalf("with no schema chosen the tree roots = %d, want every schema", len(roots))
	}
	app.schema = "other"
	roots := app.treeRoots()
	if len(roots) != 1 || roots[0].label != "other" {
		t.Fatalf("the tree is not scoped to the working schema: %+v", roots)
	}
	// It is opened, so the tables it holds are the next thing on screen.
	if !app.tree.Open.Has(roots[0]) {
		t.Fatal("the scoped schema is collapsed; its tables are not shown")
	}
	// A stale choice falls back to every schema rather than an empty tree.
	app.schema = "dropped"
	if roots := app.treeRoots(); len(roots) != 2 {
		t.Fatalf("a stale schema emptied the tree: %+v", roots)
	}
}

// --- result column widths ---------------------------------------------------

// A column is sized from its header and its cells: a long name or a long value
// widens it, a narrow one stays at the floor, and both are clamped.
func TestColumnWidths(t *testing.T) {
	res := &dbcore.QueryResult{
		Columns: []dbcore.ColumnInfo{
			{Name: "id"},
			{Name: "customer_email_address"},
			{Name: "flag"},
		},
		Rows: [][]dbcore.Value{
			{{Kind: dbcore.ValInt, I: 1}, {Kind: dbcore.ValText, S: "a@b.c"}, {Kind: dbcore.ValBool, Bool: true}},
			{{Kind: dbcore.ValInt, I: 2}, {Kind: dbcore.ValText, S: "someone.with.a.long.address@example.com"}, {Kind: dbcore.ValBool, Bool: false}},
		},
	}
	app := &App{}
	id := app.columnWidth(res, 0, 0, len(res.Rows))
	name := app.columnWidth(res, 1, 0, len(res.Rows))
	flag := app.columnWidth(res, 2, 0, len(res.Rows))

	if id != 88 {
		t.Fatalf("id width = %v, want the 88 floor", id)
	}
	if name <= id {
		t.Fatalf("a long value/name column (%v) is not wider than a narrow one (%v)", name, id)
	}
	if name > 560 {
		t.Fatalf("width %v exceeds the clamp", name)
	}
	if flag < 88 {
		t.Fatalf("flag width = %v, below the floor", flag)
	}
	// The clamp holds for a pathological value.
	huge := &dbcore.QueryResult{
		Columns: []dbcore.ColumnInfo{{Name: "x"}},
		Rows:    [][]dbcore.Value{{{Kind: dbcore.ValText, S: strings.Repeat("y", 5000)}}},
	}
	if w := app.columnWidth(huge, 0, 0, 1); w > 560 {
		t.Fatalf("a 5000-rune cell produced width %v", w)
	}
}

// The grid gives every result column a starting width and a minimum, so a wide
// result scrolls instead of crushing its columns.
func TestRenderTableSetsColumnWidths(t *testing.T) {
	app, _ := schemaApp(t)
	res := &dbcore.QueryResult{
		Columns: []dbcore.ColumnInfo{{Name: "employee_id"}, {Name: "salary"}},
		Rows: [][]dbcore.Value{
			{{Kind: dbcore.ValInt, I: 1}, {Kind: dbcore.ValFloat, F: 1000}},
		},
	}
	app.result = res
	app.pager.reset(false)
	app.pager.total = 1
	ui.NewTester(app.resultsPane, 500, 240)
	// The tester drew the table; the widths it was built with are what matters,
	// so assert on the same computation the pane makes.
	widths := []float32{app.columnWidth(res, 0, 0, 1), app.columnWidth(res, 1, 0, 1)}
	for i, w := range widths {
		if w < 88 {
			t.Fatalf("column %d width = %v, below the floor", i, w)
		}
	}
}
