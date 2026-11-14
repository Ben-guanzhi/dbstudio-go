package main

// The catalog: the metadata a connection's schema tree and its consumers read.
//
// Three facts it owns, so no caller has to re-derive them:
//
//   1. The key of a table's columns is the schema and the table joined with a
//      byte no identifier can contain. That encoding is an implementation
//      detail; callers ask by (schema, table).
//   2. "Not loaded yet" and "loaded and empty" are different answers. A tree
//      branch that has never been asked must load; a table that has no columns
//      must not load forever. The Loaded/OK pair below says which is which.
//   3. The schemas are the connection's namespaces, in the order the server
//      reports them.
//
// The struct is embedded in App, so its fields stay reachable where the tests
// build an App by hand; the selectors are what production code reads through,
// which keeps the encoding and the nil semantics on one side of a barrier
// instead of copied at 19 call sites.

import (
	"strings"

	"github.com/dbcore/dbcore"
)

// catalog is the metadata cache of the open connection.
type catalog struct {
	schemas []string
	tables  map[string][]tableInfo
	cols    map[string][]dbcore.ColumnInfo
}

// tableKey is the cache key of a table's columns. The separator is a byte an
// identifier cannot contain, so ("a", "b") and ("a.b") cannot collide.
func (c *catalog) tableKey(schema, table string) string {
	return schema + "\x00" + table
}

// SchemaNames returns the connection's schemas in report order.
func (c *catalog) SchemaNames() []string { return c.schemas }

// FirstSchema is the namespace a single-schema engine addresses without a name
// (SQLite's main), or the first one reported. It answers "" before any load.
func (c *catalog) FirstSchema() string {
	if len(c.schemas) == 0 {
		return ""
	}
	return c.schemas[0]
}

// TableState says what a schema's table list is.
type TableState int

const (
	TablesNotLoaded TableState = iota // never asked; the caller should load
	TablesEmpty                       // asked; the schema has no tables
	TablesLoaded                      // asked; the list is the answer
)

// TablesOf returns a schema's tables and its state. ok is false only for
// TablesNotLoaded, so a caller can write `tabs, st := ...; if st ==
// TablesNotLoaded { load }` without a second lookup.
func (c *catalog) TablesOf(schema string) ([]tableInfo, TableState) {
	tabs, ok := c.tables[schema]
	if !ok {
		return nil, TablesNotLoaded
	}
	if tabs == nil {
		return nil, TablesEmpty
	}
	return tabs, TablesLoaded
}

// TableType is the kind of object a tree node names, as the table list calls it
// ("table" / "view" / "materialized_view"). An object the list does not know is
// taken to be a table, which is what the driver's own DDL reader answers.
func (c *catalog) TableType(schema, table string) string {
	tabs, st := c.TablesOf(schema)
	if st == TablesNotLoaded {
		return "table"
	}
	for _, t := range tabs {
		if t.Name == table {
			return t.Type
		}
	}
	return "table"
}

// ColumnState says what a table's column list is.
type ColumnState int

const (
	ColsNotLoaded ColumnState = iota // never asked; the caller should load
	ColsEmpty                        // asked; the table has no columns
	ColsLoaded                       // asked; the list is the answer
)

// ColumnsOf returns a table's columns and its state, keyed by (schema, table).
func (c *catalog) ColumnsOf(schema, table string) ([]dbcore.ColumnInfo, ColumnState) {
	cols, ok := c.cols[c.tableKey(schema, table)]
	if !ok {
		return nil, ColsNotLoaded
	}
	if cols == nil {
		return nil, ColsEmpty
	}
	return cols, ColsLoaded
}

// HasTables reports whether a schema's table list has been loaded (with or
// without tables), which is what completion asks before offering its names.
func (c *catalog) HasTables(schema string) bool {
	_, ok := c.tables[schema]
	return ok
}

// HasColumns reports whether a table's column list has been loaded.
func (c *catalog) HasColumns(schema, table string) bool {
	_, ok := c.cols[c.tableKey(schema, table)]
	return ok
}

// PutTables stores a schema's table list. A nil list means "loaded, none".
func (c *catalog) PutTables(schema string, tabs []tableInfo) {
	if c.tables == nil {
		c.tables = map[string][]tableInfo{}
	}
	c.tables[schema] = tabs
}

// PutColumns stores a table's column list.
func (c *catalog) PutColumns(schema, table string, cols []dbcore.ColumnInfo) {
	if c.cols == nil {
		c.cols = map[string][]dbcore.ColumnInfo{}
	}
	c.cols[c.tableKey(schema, table)] = cols
}

// FindTableByName looks a bare table name up across every loaded schema, in
// report order, and answers the first of its name. It is the completion
// engine's and the navigation's "which table did the user mean" lookup.
func (c *catalog) FindTableByName(name string) (tableRef, bool) {
	for _, schema := range c.schemas {
		tabs, st := c.TablesOf(schema)
		if st == TablesNotLoaded {
			continue
		}
		for _, t := range tabs {
			if strings.EqualFold(t.Name, name) {
				return tableRef{schema: schema, table: t.Name}, true
			}
		}
	}
	return tableRef{}, false
}

// SetSchemas replaces the connection's namespaces, clearing everything the old
// connection cached: a schema's tables do not survive a disconnect.
func (c *catalog) SetSchemas(names []string) {
	c.schemas = names
	c.tables = map[string][]tableInfo{}
	c.cols = map[string][]dbcore.ColumnInfo{}
}

// Reset empties the whole cache, as disconnecting does.
func (c *catalog) Reset() {
	c.schemas = nil
	c.tables = map[string][]tableInfo{}
	c.cols = map[string][]dbcore.ColumnInfo{}
}
