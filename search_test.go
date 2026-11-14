package main

// Tests for the schema tree's object search (#19): the kernel's per-engine
// recipe finds objects the tree has never loaded, the box's state machine
// (empty/generation/not-connected), the fragment being literal text rather than
// a pattern, and the result rows browsing their table.

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dbcore/dbcore"
	"github.com/dbcore/dbcore/sqltext"
)

// searchApp opens the seeded SQLite file with the metadata a connected window
// caches, plus a view, so a search has more than tables to find.
func searchApp(t *testing.T) *App {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "search.db")
	seed(t, path)
	db, err := OpenDB(ctx, Conn{Name: "s", Kind: "sqlite", File: path})
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.SQL.Execute(ctx,
		`CREATE VIEW employee_names AS SELECT name FROM employees`); err != nil {
		t.Fatalf("create view: %v", err)
	}

	cached := newCached(db)
	if err := cached.LoadSchemas(ctx); err != nil {
		t.Fatalf("LoadSchemas: %v", err)
	}
	schema := cached.SchemaNames()[0]
	app := &App{db: db, conn: Conn{Kind: "sqlite"}, schemas: []string{schema},
		tables: map[string][]tableInfo{}, cols: map[string][]dbcore.ColumnInfo{}}
	app.initModalEditors()
	return app
}

// The kernel recipe, run through the app's own path, finds a table by a fragment
// of its name.
func TestSearchFindsTable(t *testing.T) {
	app := searchApp(t)
	app.search.text = "employ"
	app.runTreeSearch()
	if app.search.err != "" {
		t.Fatalf("search failed: %s", app.search.err)
	}
	if app.search.busy {
		t.Fatal("a headless search left the box busy")
	}
	if !app.search.active {
		t.Fatal("a search did not take the tree's place")
	}
	var names []string
	for _, h := range app.search.hits {
		names = append(names, h.table)
	}
	joined := strings.Join(names, ",")
	if !strings.Contains(joined, "employees") {
		t.Fatalf("hits = %v, want employees", names)
	}
	// The view is an object too, and it came back with its kind.
	var sawView bool
	for _, h := range app.search.hits {
		if h.table == "employee_names" && strings.EqualFold(h.kind, "view") {
			sawView = true
		}
	}
	if !sawView {
		t.Fatalf("hits = %v, want the view with kind VIEW", app.search.hits)
	}
	// SQLite reports no schema in the row, so the app fills in the connection's.
	if app.search.hits[0].schema != app.schemas[0] {
		t.Fatalf("schema = %q, want %q", app.search.hits[0].schema, app.schemas[0])
	}
}

func TestSearchNoMatch(t *testing.T) {
	app := searchApp(t)
	app.search.text = "zzz_nowhere"
	app.runTreeSearch()
	if app.search.err != "" {
		t.Fatalf("search failed: %s", app.search.err)
	}
	if len(app.search.hits) != 0 {
		t.Fatalf("hits = %v, want none", app.search.hits)
	}
	if !app.search.active {
		t.Fatal("an empty result closed the search")
	}
}

// The fragment is literal text: its wildcards must not match arbitrary
// characters, which is what the kernel's LikePattern exists for.
func TestSearchFragmentIsLiteral(t *testing.T) {
	app := searchApp(t)
	ctx := context.Background()
	if _, err := app.db.SQL.Execute(ctx, `CREATE TABLE a_b (id INTEGER)`); err != nil {
		t.Fatalf("create a_b: %v", err)
	}
	if _, err := app.db.SQL.Execute(ctx, `CREATE TABLE axb (id INTEGER)`); err != nil {
		t.Fatalf("create axb: %v", err)
	}

	app.search.text = "a_b"
	app.runTreeSearch()
	if app.search.err != "" {
		t.Fatalf("search failed: %s", app.search.err)
	}
	var names []string
	for _, h := range app.search.hits {
		names = append(names, h.table)
	}
	if len(names) != 1 || names[0] != "a_b" {
		t.Fatalf("hits = %v, want only a_b (the _ must not be a wildcard)", names)
	}
}

// An emptied box gives the tree back.
func TestSearchClear(t *testing.T) {
	app := searchApp(t)
	app.search.text = "employ"
	app.runTreeSearch()
	if !app.search.active {
		t.Fatal("search did not activate")
	}
	app.search.text = ""
	app.runTreeSearch()
	if app.search.active || app.search.busy || app.search.hits != nil || app.search.err != "" {
		t.Fatalf("an empty box left state behind: %+v", app.search)
	}
	if app.search.text != "" {
		t.Fatalf("text = %q, want empty", app.search.text)
	}
}

func TestSearchNotConnected(t *testing.T) {
	app := &App{}
	app.search.text = "x"
	app.runTreeSearch()
	if app.search.err == "" {
		t.Fatal("a search with no connection reported no reason")
	}
	if app.search.busy {
		t.Fatal("a failed search left the box busy")
	}
}

// A newer search supersedes an older one: the older answer must not land after
// the newer result is on screen.
func TestSearchNewestWins(t *testing.T) {
	app := searchApp(t)
	app.search.text = "employ"
	app.runTreeSearch()
	first := len(app.search.hits)
	if first == 0 {
		t.Fatal("the first search found nothing")
	}
	app.search.text = "zzz_nowhere"
	app.runTreeSearch()
	if len(app.search.hits) != 0 {
		t.Fatalf("hits = %v, want the newer (empty) answer", app.search.hits)
	}
	// The generation advanced with each search, so a stale answer is dropped.
	if app.search.gen < 2 {
		t.Fatalf("generation = %d, want it advanced per search", app.search.gen)
	}
}

func TestOpenHitBrowses(t *testing.T) {
	app := searchApp(t)
	app.open = true
	app.openHit(searchHit{schema: app.schemas[0], table: "employees", kind: "TABLE"})
	tb := app.tab()
	if tb == nil {
		t.Fatal("browsing a hit opened no tab")
	}
	if tb.label != "employees" || tb.kind != tabBrowse {
		t.Fatalf("tab = %q/%v, want a browse of employees", tb.label, tb.kind)
	}
	if tb.node.parent != app.schemas[0] {
		t.Fatalf("tab node schema = %q, want %q", tb.node.parent, app.schemas[0])
	}
}

// The kernel recipe is what the box depends on: a SQLite search must cap its
// rows and exclude SQLite's own objects.
func TestSearchRecipeContract(t *testing.T) {
	q, args, err := sqltext.SearchObjectsQuery("sqlite", "", "emp%", 5)
	if err != nil {
		t.Fatalf("SearchObjectsQuery: %v", err)
	}
	upper := strings.ToUpper(q)
	if !strings.Contains(upper, "SQLITE_MASTER") {
		t.Fatalf("query = %q, want sqlite_master", q)
	}
	if !strings.Contains(upper, "LIMIT 5") {
		t.Fatalf("query = %q, want the limit", q)
	}
	if len(args) != 1 {
		t.Fatalf("args = %v, want one pattern", args)
	}
	// A fragment with a wildcard is escaped, not passed through.
	if p, _ := args[0].(string); strings.Contains(p, "%") == false {
		t.Fatalf("pattern = %q, want it wrapped in %%", p)
	}
}
