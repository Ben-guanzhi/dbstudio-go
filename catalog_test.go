package main

// Tests for the catalog barrier itself (#21): the selectors' contract — "not
// loaded" vs "loaded empty", the key encoding staying inside, and the reset —
// is what 19 former call sites now rely on.

import (
	"testing"

	"github.com/Ben-guanzhi/dbcore"
)

func TestCatalogTablesStates(t *testing.T) {
	var c catalog
	// Never asked: NotLoaded, and the caller is expected to load.
	if tabs, st := c.TablesOf("public"); st != TablesNotLoaded || tabs != nil {
		t.Fatalf("fresh catalog: state=%v tabs=%v, want NotLoaded/nil", st, tabs)
	}
	// Loaded empty is a different answer from not loaded.
	c.PutTables("public", nil)
	if tabs, st := c.TablesOf("public"); st != TablesEmpty || tabs != nil {
		t.Fatalf("empty schema: state=%v tabs=%v, want Empty/nil", st, tabs)
	}
	// Loaded with rows.
	c.PutTables("public", []tableInfo{{Name: "users", Type: "table"}})
	tabs, st := c.TablesOf("public")
	if st != TablesLoaded || len(tabs) != 1 || tabs[0].Name != "users" {
		t.Fatalf("loaded schema: state=%v tabs=%v", st, tabs)
	}
}

func TestCatalogColumnsStates(t *testing.T) {
	var c catalog
	if cols, st := c.ColumnsOf("public", "users"); st != ColsNotLoaded || cols != nil {
		t.Fatalf("fresh catalog: state=%v cols=%v, want NotLoaded/nil", st, cols)
	}
	// The key encoding stays inside: the caller never passes a joined key.
	c.PutColumns("public", "users", []dbcore.ColumnInfo{{Name: "id", Type: "INTEGER"}})
	cols, st := c.ColumnsOf("public", "users")
	if st != ColsLoaded || len(cols) != 1 || cols[0].Name != "id" {
		t.Fatalf("loaded table: state=%v cols=%v", st, cols)
	}
	// A key that would collide as a joined string stays separate here.
	c.PutColumns("a.b", "c", []dbcore.ColumnInfo{{Name: "x"}})
	if cols, _ := c.ColumnsOf("a", "b.c"); len(cols) != 0 {
		t.Fatalf("ColumnsOf(a, b.c) found %v; the two tables must not collide", cols)
	}
	if _, st := c.ColumnsOf("a.b", "c"); st != ColsLoaded {
		t.Fatalf("ColumnsOf(a.b, c) state=%v, want Loaded", st)
	}
}

func TestCatalogHasAndType(t *testing.T) {
	var c catalog
	if c.HasTables("public") {
		t.Fatal("HasTables true before any load")
	}
	if c.HasColumns("public", "users") {
		t.Fatal("HasColumns true before any load")
	}
	c.PutTables("public", nil) // loaded, none
	if !c.HasTables("public") {
		t.Fatal("HasTables false after a load")
	}
	// An unknown object is a table, which is what the DDL reader answers.
	if got := c.TableType("public", "users"); got != "table" {
		t.Fatalf("TableType of an unknown object = %q, want table", got)
	}
	c.PutTables("public", []tableInfo{{Name: "salaries", Type: "view"}})
	if got := c.TableType("public", "salaries"); got != "view" {
		t.Fatalf("TableType = %q, want view", got)
	}
	if got := c.TableType("other", "salaries"); got != "table" {
		t.Fatalf("TableType in a not-loaded schema = %q, want table", got)
	}
}

func TestCatalogFindTableByName(t *testing.T) {
	var c catalog
	if _, ok := c.FindTableByName("users"); ok {
		t.Fatal("an empty catalog found a table")
	}
	c.SetSchemas([]string{"public", "sales"})
	// A not-loaded schema contributes nothing.
	if _, ok := c.FindTableByName("orders"); ok {
		t.Fatal("a not-loaded schema answered a lookup")
	}
	c.PutTables("public", []tableInfo{{Name: "users", Type: "table"}})
	c.PutTables("sales", []tableInfo{{Name: "users", Type: "table"}, {Name: "orders", Type: "table"}})
	// The first schema in report order wins.
	ref, ok := c.FindTableByName("users")
	if !ok || ref.schema != "public" {
		t.Fatalf("ref = %+v, want public.users (first in report order)", ref)
	}
	// Case-insensitive, answering the catalog's own spelling.
	ref, ok = c.FindTableByName("ORDERS")
	if !ok || ref.table != "orders" {
		t.Fatalf("ref = %+v, want orders", ref)
	}
}

func TestCatalogSetSchemasAndReset(t *testing.T) {
	var c catalog
	c.SetSchemas([]string{"public"})
	c.PutTables("public", []tableInfo{{Name: "users"}})
	c.PutColumns("public", "users", []dbcore.ColumnInfo{{Name: "id"}})
	// A new connection's namespaces wipe everything the old one cached.
	c.SetSchemas([]string{"main"})
	if _, st := c.TablesOf("public"); st != TablesNotLoaded {
		t.Fatalf("old schema survived SetSchemas: state=%v", st)
	}
	if _, st := c.ColumnsOf("public", "users"); st != ColsNotLoaded {
		t.Fatalf("old columns survived SetSchemas: state=%v", st)
	}
	if c.FirstSchema() != "main" {
		t.Fatalf("FirstSchema = %q, want main", c.FirstSchema())
	}
	// Reset empties even the namespaces.
	c.Reset()
	if c.FirstSchema() != "" || c.HasTables("main") {
		t.Fatalf("Reset left state behind: first=%q tables=%v", c.FirstSchema(), c.tables)
	}
}
