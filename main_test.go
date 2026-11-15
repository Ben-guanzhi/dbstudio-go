package main

// A smoke test for the non-UI half of the app: it opens a real SQLite file
// through OpenDB and walks the same metadata and query paths the UI uses.

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Ben-guanzhi/dbcore"
	"github.com/Ben-guanzhi/dbcore/sqlite"
)

func seed(t *testing.T, path string) {
	t.Helper()
	d, err := sqlite.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	sql, ok := d.AsSQL()
	if !ok {
		t.Fatal("no SQL")
	}
	ctx := context.Background()
	for _, stmt := range []string{
		`DROP TABLE IF EXISTS employees`,
		`CREATE TABLE employees (id INTEGER PRIMARY KEY, name TEXT NOT NULL, salary REAL)`,
		`INSERT INTO employees (name, salary) VALUES ('Ada', 190000), ('Alan', 185000)`,
	} {
		if _, err := sql.Execute(ctx, stmt); err != nil {
			t.Fatalf("execute %q: %v", stmt, err)
		}
	}
	if err := d.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestOpenDBListAndQuery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	seed(t, path)

	ctx := context.Background()
	db, err := OpenDB(ctx, Conn{Name: "test", Kind: "sqlite", File: path})
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()

	if db.Kind != dbcore.KindSQLite {
		t.Fatalf("kind = %v, want sqlite", db.Kind)
	}
	if db.SQL == nil {
		t.Fatal("SQL is nil")
	}

	cached := newCached(db)
	if err := cached.LoadSchemas(ctx); err != nil {
		t.Fatalf("LoadSchemas: %v", err)
	}
	schemas := cached.SchemaNames()
	if len(schemas) == 0 {
		t.Fatal("no schemas")
	}
	schema := schemas[0]
	t.Logf("schema = %q", schema)

	if err := cached.setTables(ctx, schema); err != nil {
		t.Fatalf("setTables: %v", err)
	}
	tables := cached.TableNames(schema)
	if len(tables) != 1 || tables[0] != "employees" {
		t.Fatalf("tables = %v, want [employees]", tables)
	}

	if err := cached.setColumns(ctx, schema, "employees"); err != nil {
		t.Fatalf("setColumns: %v", err)
	}
	cols := cached.columnsOf(schema, "employees")
	if len(cols) != 3 {
		t.Fatalf("columns = %v, want 3", cols)
	}
	// Ordinal sorting: id, name, salary.
	if cols[0].Name != "id" || cols[1].Name != "name" || cols[2].Name != "salary" {
		t.Fatalf("columns out of ordinal order: %v", cols)
	}

	res, err := db.SQL.Execute(ctx, "SELECT name, salary FROM employees ORDER BY name")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(res.Columns) != 2 {
		t.Fatalf("columns = %v", res.Columns)
	}
	if len(res.Rows) != 2 || res.Rows[0][0].String() != "Ada" {
		t.Fatalf("rows = %v", res.Rows)
	}

	rows, err := db.SQL.BrowseRows(ctx, schema, "employees", &dbcore.TableRowsRequest{Page: 0, PageSize: 200})
	if err != nil {
		t.Fatalf("BrowseRows: %v", err)
	}
	if len(rows.Rows) != 2 {
		t.Fatalf("browse rows = %d, want 2", len(rows.Rows))
	}
	if !rows.TotalFound || rows.Total != 2 {
		t.Fatalf("browse total = %d (found %v), want 2", rows.Total, rows.TotalFound)
	}
}

func TestOpenDBInMemory(t *testing.T) {
	ctx := context.Background()
	db, err := OpenDB(ctx, Conn{Name: "mem", Kind: "sqlite"})
	if err != nil {
		t.Fatalf("OpenDB :memory:: %v", err)
	}
	defer db.Close()
	if _, err := db.SQL.Execute(ctx, "CREATE TABLE t (x INTEGER)"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := db.SQL.Execute(ctx, "INSERT INTO t VALUES (42)"); err != nil {
		t.Fatalf("insert: %v", err)
	}
}

func TestSplitSQL(t *testing.T) {
	got := splitSQL("SELECT 1; -- trailing\nSELECT 2;")
	if len(got) != 2 {
		t.Fatalf("got %v, want 2 statements", got)
	}
	if got[0] != "SELECT 1" {
		t.Fatalf("first = %q", got[0])
	}
	if got[1] != "SELECT 2" {
		t.Fatalf("second = %q", got[1])
	}

	// A semicolon inside a string and a comment must not split.
	got = splitSQL("INSERT INTO t VALUES ('a;b'); SELECT 1 -- ;")
	if len(got) != 2 {
		t.Fatalf("got %v, want 2", got)
	}
	if got[0] != "INSERT INTO t VALUES ('a;b')" {
		t.Fatalf("first = %q", got[0])
	}
	if got[1] != "SELECT 1" {
		t.Fatalf("second = %q", got[1])
	}

	if got := splitSQL(""); len(got) != 0 {
		t.Fatalf("empty got %v", got)
	}
}

func TestNormalizeAndLabel(t *testing.T) {
	var sqlite Conn
	sqlite.Kind = "sqlite"
	hostDefaults(&sqlite)
	if sqlite.Port != 0 || sqlite.Host != "" {
		t.Fatalf("sqlite defaults = %+v", sqlite)
	}

	var pg Conn
	pg.Kind = "postgres"
	hostDefaults(&pg)
	if pg.Host != "127.0.0.1" || pg.Port != 5432 || pg.Schema != "public" {
		t.Fatalf("postgres defaults = %+v", pg)
	}

	var ms Conn
	ms.Kind = "mssql"
	hostDefaults(&ms)
	if ms.Host != "127.0.0.1" || ms.Port != 1433 || ms.Schema != "dbo" {
		t.Fatalf("mssql defaults = %+v", ms)
	}

	if sqlite.label() == "" || pg.label() == "" || ms.label() == "" {
		t.Fatal("empty label")
	}
}

func TestStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStoreAt(filepath.Join(dir, "connections.json"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	c := Conn{Name: " local ", Kind: " SQLite ", File: ":memory:"}
	if err := s.Add(c); err != nil {
		t.Fatalf("Add: %v", err)
	}
	got, ok := s.Get("local")
	if !ok {
		t.Fatal("Get failed")
	}
	if got.Kind != "sqlite" {
		t.Fatalf("kind = %q, want sqlite (normalized)", got.Kind)
	}
	if err := s.Upsert(Conn{Name: "local", Kind: "mysql", Host: "h", Port: 3306, Database: "d"}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if got, _ := s.Get("local"); got.Kind != "mysql" {
		t.Fatalf("kind = %q, want mysql", got.Kind)
	}
	if n := len(s.List()); n != 1 {
		t.Fatalf("List = %d, want 1", n)
	}
	if err := s.Remove("local"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if len(s.List()) != 0 {
		t.Fatal("not removed")
	}
}

func TestBrowseSQL(t *testing.T) {
	if got := browseSQL("sqlite", "main", "t"); got != "SELECT * FROM t LIMIT 100;\n" {
		t.Fatalf("sqlite = %q", got)
	}
	if got := browseSQL("postgres", "public", "t"); got != "SELECT * FROM t LIMIT 100;\n" {
		t.Fatalf("postgres = %q", got)
	}
	if got := browseSQL("postgres", "sales", "t"); got != "SELECT * FROM sales.t LIMIT 100;\n" {
		t.Fatalf("postgres schema = %q", got)
	}
	if got := browseSQL("mssql", "dbo", "t"); got != "SELECT TOP 100 * FROM t;\n" {
		t.Fatalf("mssql = %q", got)
	}
	if got := browseSQL("oracle", "t_user", "t"); got != "SELECT * FROM t_user.t FETCH FIRST 100 ROWS ONLY;\n" {
		t.Fatalf("oracle = %q", got)
	}
}
