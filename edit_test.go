package main

// Tests for the editable working copy: the parsing of edited cells, the
// batch a changed copy asks for, and a round trip through a real SQLite
// table. The grid itself is drawn by the UI framework; what is tested here is
// the half that decides what the database is sent.

import (
	"context"
	"testing"

	"github.com/Ben-guanzhi/dbcore"
)

func TestTextCell(t *testing.T) {
	if got := textCell(dbcore.Value{Kind: dbcore.ValNull}); got != "" {
		t.Fatalf("NULL text = %q, want empty", got)
	}
	if got := textCell(dbcore.Value{Kind: dbcore.ValInt, I: 7}); got != "7" {
		t.Fatalf("int text = %q", got)
	}
	if got := textCell(dbcore.Value{Kind: dbcore.ValText, S: "Ada"}); got != "Ada" {
		t.Fatalf("text = %q", got)
	}
}

func TestParseCell(t *testing.T) {
	// A blank cell is NULL whatever the column is.
	if v := parseCell(dbcore.ValInt, ""); v.Kind != dbcore.ValNull {
		t.Fatalf("blank kind = %v", v.Kind)
	}
	if v := parseCell(dbcore.ValText, ""); v.Kind != dbcore.ValNull {
		t.Fatalf("blank text kind = %v", v.Kind)
	}

	// A number of the column's kind parses as one; a number that does not
	// keeps its text rather than being turned into something else.
	if v := parseCell(dbcore.ValInt, "42"); v.Kind != dbcore.ValInt || v.I != 42 {
		t.Fatalf("int = %+v", v)
	}
	if v := parseCell(dbcore.ValInt, "not a number"); v.Kind != dbcore.ValText || v.S != "not a number" {
		t.Fatalf("bad int = %+v", v)
	}
	if v := parseCell(dbcore.ValFloat, "1.5"); v.Kind != dbcore.ValFloat || v.F != 1.5 {
		t.Fatalf("float = %+v", v)
	}
	if v := parseCell(dbcore.ValFloat, "1,5"); v.Kind != dbcore.ValText {
		t.Fatalf("bad float = %+v", v)
	}

	// Exact decimals keep their digits.
	if v := parseCell(dbcore.ValDecimal, "1.2300"); v.Kind != dbcore.ValDecimal || v.S != "1.2300" {
		t.Fatalf("decimal = %+v", v)
	}
	if v := parseCell(dbcore.ValDecimal, "not a number"); v.Kind != dbcore.ValText {
		t.Fatalf("bad decimal = %+v", v)
	}

	// Booleans answer the spellings a user may type.
	for _, s := range []string{"true", "TRUE", "1", "yes"} {
		if v := parseCell(dbcore.ValBool, s); !v.Bool || v.Kind != dbcore.ValBool {
			t.Fatalf("bool %q = %+v", s, v)
		}
	}
	for _, s := range []string{"false", "FALSE", "0", "no"} {
		if v := parseCell(dbcore.ValBool, s); v.Bool || v.Kind != dbcore.ValBool {
			t.Fatalf("bool %q = %+v", s, v)
		}
	}

	// Dates and times accept the layouts the drivers hand back.
	for _, s := range []string{"2024-01-02", "2024-01-02 03:04:05", "2024-01-02T03:04:05Z"} {
		if v := parseCell(dbcore.ValTime, s); v.Kind != dbcore.ValTime {
			t.Fatalf("time %q = %+v", s, v)
		}
	}

	// A blob is edited as text.
	if v := parseCell(dbcore.ValBlob, "abc"); v.Kind != dbcore.ValText || v.S != "abc" {
		t.Fatalf("blob = %+v", v)
	}
}

func TestKindOf(t *testing.T) {
	cases := map[string]dbcore.ValueKind{
		"INTEGER":       dbcore.ValInt,
		"INT":           dbcore.ValInt,
		"BIGINT":        dbcore.ValInt,
		"SERIAL":        dbcore.ValInt,
		"REAL":          dbcore.ValFloat,
		"DOUBLE":        dbcore.ValFloat,
		"FLOAT":         dbcore.ValFloat,
		"NUMERIC":       dbcore.ValDecimal,
		"DECIMAL(10,2)": dbcore.ValDecimal,
		"NUMBER":        dbcore.ValDecimal,
		"BOOL":          dbcore.ValBool,
		"BOOLEAN":       dbcore.ValBool,
		"DATE":          dbcore.ValTime,
		"TIMESTAMP":     dbcore.ValTime,
		"BLOB":          dbcore.ValBlob,
		"BYTEA":         dbcore.ValBlob,
		"VARCHAR(64)":   dbcore.ValText,
		"TEXT":          dbcore.ValText,
		"":              dbcore.ValText,
	}
	for in, want := range cases {
		if got := kindOf(in); got != want {
			t.Errorf("kindOf(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestMergeColumns(t *testing.T) {
	res := []dbcore.ColumnInfo{
		{Name: "id", Type: "INTEGER"},
		{Name: "name", Type: "TEXT"},
	}
	cached := []dbcore.ColumnInfo{
		{Name: "id", Type: "INTEGER", IsPrimaryKey: true, IsAutoIncrement: true, Table: "employees"},
		{Name: "name", Type: "TEXT", Table: "employees"},
	}
	got := mergeColumns(res, cached)
	if len(got) != 2 {
		t.Fatalf("merged %d columns, want 2", len(got))
	}
	if !got[0].IsPrimaryKey || !got[0].IsAutoIncrement || got[0].Table != "employees" {
		t.Fatalf("id lost its flags: %+v", got[0])
	}
	if got[1].Table != "employees" || got[1].IsPrimaryKey {
		t.Fatalf("name = %+v", got[1])
	}

	// A result with no columns of its own takes the cached ones whole.
	got = mergeColumns(nil, cached)
	if len(got) != 2 || !got[0].IsPrimaryKey {
		t.Fatalf("empty result merge = %+v", got)
	}
}

// sampleRows is an edit state over two employees, one of them with a NULL
// salary, for the tests below.
func sampleRows() *editState {
	e := &editState{
		active: true,
		schema: "main",
		table:  "employees",
		cols: []dbcore.ColumnInfo{
			{Name: "id", Type: "INTEGER", IsPrimaryKey: true},
			{Name: "name", Type: "TEXT"},
			{Name: "salary", Type: "REAL"},
		},
	}
	kinds := []dbcore.ValueKind{dbcore.ValInt, dbcore.ValText, dbcore.ValFloat}
	e.rows = []*editRow{
		{strs: []string{"1", "Ada", "190000"}, origNull: []bool{false, false, false}, kinds: kinds},
		{strs: []string{"2", "Alan", ""}, origNull: []bool{false, false, true}, kinds: kinds},
	}
	for _, r := range e.rows {
		r.origStrs = append([]string(nil), r.strs...)
	}
	e.orig = cloneRows(e.rows)
	return e
}

func TestEditRequest(t *testing.T) {
	e := sampleRows()
	e.rows[0].strs[1] = "Ada Lovelace"
	e.rows[0].state = rowModified
	e.rows[1].state = rowDeleted
	e.addRow([]string{"", "Grace", "150000"}, rowInserted)

	if !e.dirty() {
		t.Fatal("the copy should be dirty")
	}
	req, why := e.request()
	if why != "" {
		t.Fatalf("request = %q", why)
	}
	if req.Table != "employees" {
		t.Fatalf("table = %q", req.Table)
	}
	if len(req.Updates) != 1 {
		t.Fatalf("updates = %d, want 1", len(req.Updates))
	}
	u := req.Updates[0]
	if u.PK["id"].I != 1 {
		t.Fatalf("update PK = %+v", u.PK)
	}
	if u.Changes["name"].S != "Ada Lovelace" {
		t.Fatalf("update changes = %+v", u.Changes)
	}
	if len(u.Changes) != 1 {
		t.Fatalf("update should carry only the name, got %+v", u.Changes)
	}
	if len(req.Deletes) != 1 || req.Deletes[0]["id"].I != 2 {
		t.Fatalf("deletes = %+v", req.Deletes)
	}
	if len(req.Inserts) != 1 {
		t.Fatalf("inserts = %d, want 1", len(req.Inserts))
	}
	in := req.Inserts[0]
	if in["name"].S != "Grace" {
		t.Fatalf("insert name = %+v", in)
	}
	if in["salary"].Kind != dbcore.ValFloat || in["salary"].F != 150000 {
		t.Fatalf("insert salary = %+v", in["salary"])
	}
	if _, ok := in["id"]; ok {
		t.Fatalf("an empty identity column should not be sent: %+v", in)
	}
}

func TestEditRollback(t *testing.T) {
	e := sampleRows()
	e.rows[0].strs[1] = "Ada Lovelace"
	e.rows[0].state = rowModified
	e.rows[1].state = rowDeleted
	e.addRow([]string{"", "Grace", "150000"}, rowInserted)

	e.rows = cloneRows(e.orig)
	if len(e.rows) != 2 {
		t.Fatalf("rollback left %d rows", len(e.rows))
	}
	if e.rows[0].strs[1] != "Ada" {
		t.Fatalf("an edit survived rollback: %q", e.rows[0].strs[1])
	}
	if e.rows[1].state != rowUntouched {
		t.Fatalf("a deletion survived rollback: %v", e.rows[1].state)
	}
	if e.dirty() {
		t.Fatal("a rolled back copy is clean")
	}
	req, why := e.request()
	if why != "" || len(req.Updates)+len(req.Deletes)+len(req.Inserts) != 0 {
		t.Fatalf("rolled back copy asks for %+v (%q)", req, why)
	}
}

func TestSyncState(t *testing.T) {
	e := sampleRows()
	// A change the grid took without a Changed report is still a change.
	e.rows[0].strs[1] = "Ada Lovelace"
	e.syncState()
	if e.rows[0].state != rowModified {
		t.Fatalf("state = %v, want modified", e.rows[0].state)
	}
	if !e.dirty() {
		t.Fatal("a synced change is dirty")
	}
}

func TestRequestNeedsPrimaryKey(t *testing.T) {
	e := sampleRows()
	e.cols[0].IsPrimaryKey = false // a table without a usable key
	e.rows[0].strs[1] = "Ada Lovelace"
	e.rows[0].state = rowModified
	e.syncState()
	if _, why := e.request(); why == "" {
		t.Fatal("a row without a primary key must not be addressable")
	}

	// And a NULL key column is not a key either.
	e = sampleRows()
	e.rows[0].strs[0] = ""
	e.rows[0].strs[1] = "Ada Lovelace"
	e.rows[0].state = rowModified
	e.syncState()
	if _, why := e.request(); why == "" {
		t.Fatal("a row whose key is NULL must not be addressable")
	}
}

func TestTableOf(t *testing.T) {
	app := &App{
		conn:    Conn{Kind: "sqlite"},
		schemas: []string{"main"},
		cols: map[string][]dbcore.ColumnInfo{
			tableKey("main", "employees"): {{Name: "id", IsPrimaryKey: true}},
		},
	}

	editable := []string{
		"SELECT * FROM employees",
		"select * from employees limit 100",
		"SELECT * FROM employees;",
		"SELECT * FROM main.employees",
		"SELECT * FROM `main`.`employees`",
		"SELECT DISTINCT * FROM employees",
		"SELECT TOP 100 * FROM employees",
		"SELECT * FROM employees FETCH FIRST 10 ROWS ONLY",
	}
	for _, sql := range editable {
		schema, table, ok := app.tableOf(sql)
		if !ok {
			t.Errorf("tableOf(%q) not editable", sql)
			continue
		}
		if schema != "main" || table != "employees" {
			t.Errorf("tableOf(%q) = %q.%q, want main.employees", sql, schema, table)
		}
	}

	readOnly := []string{
		"SELECT id FROM employees",                 // a projection
		"SELECT * FROM employees WHERE id > 1",     // not the whole table
		"SELECT * FROM employees JOIN departments", // a join
		"SELECT * FROM nope",                       // unknown to the catalog
		"SELECT * FROM a.b.c",                      // three parts
		"SELECT count(*) FROM employees",           // an aggregate
		"UPDATE employees SET name = 'x'",          // not a select
		"",                                         // empty
	}
	for _, sql := range readOnly {
		if _, _, ok := app.tableOf(sql); ok {
			t.Errorf("tableOf(%q) should be read-only", sql)
		}
	}
}

func TestDefaultSchema(t *testing.T) {
	app := &App{schemas: []string{"sales", "public"}}
	if got := app.defaultSchema(); got != "sales" {
		t.Fatalf("defaultSchema = %q", got)
	}
	app = &App{}
	if got := app.defaultSchema(); got != "" {
		t.Fatalf("empty schemas defaultSchema = %q", got)
	}
}

// TestCommitRoundTrip runs a whole commit against a real SQLite table: two rows
// read, one edited, one deleted and one inserted, applied as one batch, and the
// table read back.
func TestCommitRoundTrip(t *testing.T) {
	ctx := context.Background()
	db, err := OpenDB(ctx, Conn{Name: "edit", Kind: "sqlite"})
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()

	for _, stmt := range []string{
		`CREATE TABLE employees (id INTEGER PRIMARY KEY, name TEXT NOT NULL, salary REAL)`,
		`INSERT INTO employees (name, salary) VALUES ('Ada', 190000), ('Alan', 185000)`,
	} {
		if _, err := db.SQL.Execute(ctx, stmt); err != nil {
			t.Fatalf("execute %q: %v", stmt, err)
		}
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
	if err := cached.setTables(ctx, schema); err != nil {
		t.Fatalf("setTables: %v", err)
	}
	if err := cached.setColumns(ctx, schema, "employees"); err != nil {
		t.Fatalf("setColumns: %v", err)
	}
	cols := cached.columnsOf(schema, "employees")
	if len(cols) != 3 {
		t.Fatalf("columns = %+v", cols)
	}
	if !cols[0].IsPrimaryKey {
		t.Fatalf("the id column is not marked a primary key: %+v", cols[0])
	}

	res, err := db.SQL.Execute(ctx, "SELECT * FROM employees ORDER BY id")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var e editState
	e.init(res, schema, "employees", cols, func() {})
	if len(e.rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(e.rows))
	}

	// Edit Ada, delete Alan, insert Grace.
	e.rows[0].strs[1] = "Ada Lovelace"
	e.rows[0].state = rowModified
	e.rows[1].state = rowDeleted
	e.addRow([]string{"", "Grace", "150000"}, rowInserted)

	req, why := e.request()
	if why != "" {
		t.Fatalf("request = %q", why)
	}
	w, ok := dbcore.AsBatchWriter(db.Driver)
	if !ok {
		t.Fatal("the sqlite driver cannot commit a batch")
	}
	if err := w.ApplyBatch(ctx, req); err != nil {
		t.Fatalf("ApplyBatch: %v", err)
	}

	after, err := db.SQL.Execute(ctx, "SELECT name, salary FROM employees ORDER BY name")
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if len(after.Rows) != 2 {
		t.Fatalf("rows after commit = %d, want 2", len(after.Rows))
	}
	if after.Rows[0][0].S != "Ada Lovelace" {
		t.Fatalf("first row = %+v, want Ada Lovelace", after.Rows[0])
	}
	if after.Rows[1][0].S != "Grace" {
		t.Fatalf("second row = %+v, want Grace", after.Rows[1])
	}
	if after.Rows[1][1].Kind != dbcore.ValFloat || after.Rows[1][1].F != 150000 {
		t.Fatalf("Grace's salary = %+v, want 150000", after.Rows[1][1])
	}
}
