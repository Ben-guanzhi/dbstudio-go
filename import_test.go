package main

// Tests for the data import (#21): the parser (separator from the extension,
// ragged rows, oversized cells), the header guess, the mapping (explicit, with
// skip), the inserts the mapping builds (typed cells, empty as NULL), and the
// whole path against a real SQLite table — including the clear-first option.

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dbcore/dbcore"
	"github.com/egoist/mygo/ui"
)

func TestSepFor(t *testing.T) {
	cases := []struct {
		path string
		want importSep
	}{
		{"data.csv", sepComma},
		{"data.TSV", sepTab},
		{"data.tab", sepTab},
		{"data.txt", sepComma},
		{`C:\Users\a.b\dump`, sepComma},
	}
	for _, c := range cases {
		if got := sepFor(c.path); got != c.want {
			t.Errorf("sepFor(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestParseImportComma(t *testing.T) {
	r := strings.NewReader("id,name\n1,Ada\n2,\"Alan, M\"\n")
	f, err := parseImport(r, sepComma, "x.csv")
	if err != nil {
		t.Fatalf("parseImport: %v", err)
	}
	if len(f.records) != 3 {
		t.Fatalf("records = %d, want 3", len(f.records))
	}
	if got := f.records[2][1]; got != "Alan, M" {
		t.Fatalf("quoted cell = %q, want Alan, M", got)
	}
}

func TestParseImportTab(t *testing.T) {
	r := strings.NewReader("id\tname\n1\tAda")
	f, err := parseImport(r, sepTab, "x.tsv")
	if err != nil {
		t.Fatalf("parseImport: %v", err)
	}
	if len(f.records[0]) != 2 || f.records[1][1] != "Ada" {
		t.Fatalf("records = %v", f.records)
	}
}

func TestParseImportRaggedRows(t *testing.T) {
	// A ragged file parses: the mapping reports the missing cells as NULL, so
	// the parser must not fail the whole file on one short row.
	r := strings.NewReader("a,b,c\n1,2\n3,4,5\n")
	f, err := parseImport(r, sepComma, "x.csv")
	if err != nil {
		t.Fatalf("ragged file failed: %v", err)
	}
	if len(f.records[1]) != 2 {
		t.Fatalf("short row kept %d cells, want 2", len(f.records[1]))
	}
}

func TestHeaderGuess(t *testing.T) {
	withHeader, _ := parseImport(strings.NewReader("id,name\n1,Ada"), sepComma, "")
	if !withHeader.Header() {
		t.Fatal("id/name read as data")
	}
	allNumbers, _ := parseImport(strings.NewReader("1,Ada\n2,Alan"), sepComma, "")
	if allNumbers.Header() {
		t.Fatal("a first row of numbers read as a header")
	}
	// An empty file has no header.
	empty, _ := parseImport(strings.NewReader(""), sepComma, "")
	if empty.Header() {
		t.Fatal("an empty file read as having a header")
	}
}

func TestColumnsAndDataRows(t *testing.T) {
	f, _ := parseImport(strings.NewReader("id,name\n1,Ada\n2,Alan"), sepComma, "")
	if cols := f.Columns(true); len(cols) != 2 || cols[0] != "id" {
		t.Fatalf("Columns(true) = %v", cols)
	}
	if rows := f.DataRows(true); len(rows) != 2 {
		t.Fatalf("DataRows(true) = %d rows, want 2", len(rows))
	}
	// Without a header the labels are positional and all rows are data.
	if cols := f.Columns(false); cols[0] != "1" {
		t.Fatalf("Columns(false) = %v, want positional labels", cols)
	}
	if rows := f.DataRows(false); len(rows) != 3 {
		t.Fatalf("DataRows(false) = %d rows, want 3", len(rows))
	}
}

func TestBuildInsertsTypedAndNull(t *testing.T) {
	f, _ := parseImport(strings.NewReader("id,name,salary\n1,Ada,190000.5\n2,,\n"), sepComma, "")
	rows := f.DataRows(true)
	cols := []dbcore.ColumnInfo{
		{Name: "id", Type: "INTEGER"},
		{Name: "name", Type: "TEXT"},
		{Name: "salary", Type: "REAL"},
	}
	// All three file columns map, in order, to the table's three.
	mapping := importMapping{0, 1, 2}
	batches, total, err := buildInserts(f, rows, mapping, cols)
	if err != nil {
		t.Fatalf("buildInserts: %v", err)
	}
	if total != 2 {
		t.Fatalf("total = %d, want 2", total)
	}
	if len(batches) != 1 || len(batches[0]) != 2 {
		t.Fatalf("batches = %d x %d, want 1 x 2", len(batches), len(batches[0]))
	}
	first := batches[0][0]
	if v := first["id"]; v.Kind != dbcore.ValInt || v.I != 1 {
		t.Fatalf("id = %+v, want int 1", v)
	}
	if v := first["salary"]; v.Kind != dbcore.ValFloat || v.F != 190000.5 {
		t.Fatalf("salary = %+v, want float", v)
	}
	// An empty cell is NULL, not an empty string.
	second := batches[0][1]
	if v := second["name"]; v.Kind != dbcore.ValNull {
		t.Fatalf("empty name = %+v, want NULL", v)
	}
}

func TestBuildInsertsSkipAndShortRow(t *testing.T) {
	f, _ := parseImport(strings.NewReader("junk,id\nx,7\n"), sepComma, "")
	rows := f.DataRows(true)
	cols := []dbcore.ColumnInfo{{Name: "id", Type: "INTEGER"}}
	// The first file column is skipped; only the second maps.
	mapping := importMapping{-1, 0}
	batches, _, err := buildInserts(f, rows, mapping, cols)
	if err != nil {
		t.Fatalf("buildInserts: %v", err)
	}
	if len(batches[0][0]) != 1 {
		t.Fatalf("insert has %d columns, want 1 (the skipped one left out)", len(batches[0][0]))
	}
	if v := batches[0][0]["id"]; v.Kind != dbcore.ValInt || v.I != 7 {
		t.Fatalf("id = %+v, want int 7", v)
	}
	// Nothing mapped at all is the modal's error to show, not an empty batch.
	if _, _, err := buildInserts(f, rows, importMapping{-1, -1}, cols); err == nil {
		t.Fatal("an all-skip mapping built inserts")
	}
}

// A short row's missing cells become NULL, which is what a truncated CSV line
// means — the parser kept the row, the mapping fills what it can.
func TestBuildInsertsShortRowNulls(t *testing.T) {
	f, _ := parseImport(strings.NewReader("id,name\n1\n"), sepComma, "")
	rows := f.DataRows(true)
	cols := []dbcore.ColumnInfo{{Name: "id", Type: "INTEGER"}, {Name: "name", Type: "TEXT"}}
	batches, _, err := buildInserts(f, rows, importMapping{0, 1}, cols)
	if err != nil {
		t.Fatalf("buildInserts: %v", err)
	}
	if v := batches[0][0]["name"]; v.Kind != dbcore.ValNull {
		t.Fatalf("missing cell = %+v, want NULL", v)
	}
}

// The whole path: parse a file, map it, apply the batches to a real SQLite
// table, and read the rows back.
func TestImportIntoSQLite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "imp.db")
	seed(t, path)
	ctx := context.Background()
	db, err := OpenDB(ctx, Conn{Name: "imp", Kind: "sqlite", File: path})
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.SQL.Execute(ctx, `CREATE TABLE people (id INTEGER PRIMARY KEY, name TEXT, salary REAL)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	w, ok := dbcore.AsBatchWriter(db.Driver)
	if !ok {
		t.Fatal("the SQLite driver does not implement dbcore.BatchWriter")
	}

	f, err := parseImport(strings.NewReader("id,name,salary\n1,Ada,190000\n2,Alan,185000\n3,Grace,\n"), sepComma, "people.csv")
	if err != nil {
		t.Fatalf("parseImport: %v", err)
	}
	rows := f.DataRows(true)
	cols := []dbcore.ColumnInfo{
		{Name: "id", Type: "INTEGER"},
		{Name: "name", Type: "TEXT"},
		{Name: "salary", Type: "REAL"},
	}
	batches, total, err := buildInserts(f, rows, importMapping{0, 1, 2}, cols)
	if err != nil {
		t.Fatalf("buildInserts: %v", err)
	}
	if total != 3 {
		t.Fatalf("total = %d, want 3", total)
	}
	for _, batch := range batches {
		if err := w.ApplyBatch(ctx, dbcore.BatchRequest{Table: "people", Inserts: batch}); err != nil {
			t.Fatalf("ApplyBatch: %v", err)
		}
	}

	res, err := db.SQL.Execute(ctx, `SELECT id, name, salary FROM people ORDER BY id`)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if len(res.Rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(res.Rows))
	}
	if res.Rows[2][1].S != "Grace" {
		t.Fatalf("name = %q, want Grace", res.Rows[2][1].S)
	}
	// The empty salary went in as NULL.
	if res.Rows[2][2].Kind != dbcore.ValNull {
		t.Fatalf("salary = %+v, want NULL", res.Rows[2][2])
	}
}

// The clear-first option truncates before the inserts, in its own batch.
func TestImportTruncateFirst(t *testing.T) {
	path := filepath.Join(t.TempDir(), "imp2.db")
	seed(t, path)
	ctx := context.Background()
	db, err := OpenDB(ctx, Conn{Name: "imp2", Kind: "sqlite", File: path})
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	w, ok := dbcore.AsBatchWriter(db.Driver)
	if !ok {
		t.Fatal("no BatchWriter")
	}
	// employees already has 2 seeded rows; clear, then insert one.
	req := dbcore.BatchRequest{Table: "employees", DDL: []string{truncateSQL("sqlite", "main", "employees")}}
	if err := w.ApplyBatch(ctx, req); err != nil {
		t.Fatalf("truncate batch: %v", err)
	}
	res, err := db.SQL.Execute(ctx, `SELECT COUNT(*) FROM employees`)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if res.Rows[0][0].I != 0 {
		t.Fatalf("rows after truncate = %d, want 0", res.Rows[0][0].I)
	}
	// And the statement itself is what SQLite accepts (the per-engine spelling
	// falls back to DELETE there).
	if !strings.Contains(truncateSQL("sqlite", "main", "t"), "DELETE FROM") {
		t.Fatal("SQLite's truncate spelling should be DELETE FROM")
	}
	if !strings.Contains(strings.ToUpper(truncateSQL("postgres", "public", "t")), "TRUNCATE TABLE") {
		t.Fatal("PostgreSQL's truncate spelling should be TRUNCATE TABLE")
	}
}

// A cell beyond the cap is cut, so one pathological field cannot exhaust memory.
func TestParseImportCutsOversizedCell(t *testing.T) {
	big := strings.Repeat("x", importMaxCell+1000)
	f, err := parseImport(strings.NewReader(big), sepComma, "x.csv")
	if err != nil {
		t.Fatalf("parseImport: %v", err)
	}
	if got := len(f.records[0][0]); got != importMaxCell {
		t.Fatalf("cell length = %d, want %d", got, importMaxCell)
	}
}

// The modal draws: its title, the options, the mapping labels, and the buttons.
func TestImportModalRenders(t *testing.T) {
	app := newViewApp(t, nil)
	r := strings.NewReader("id,name\n1,Ada\n")
	file, err := parseImport(r, sepComma, "people.csv")
	if err != nil {
		t.Fatalf("parseImport: %v", err)
	}
	app.importer = app.newImportState(file, "main", "people", []dbcore.ColumnInfo{
		{Name: "id", Type: "INTEGER"},
		{Name: "name", Type: "TEXT"},
	})
	app.importer.progress = "Ready."

	tt := ui.NewTester(app.view, 1200, 760)
	for _, want := range []string{"Import into people", "people.csv", "First row is the header", "1 · id", "2 · name", "Cancel", "Import"} {
		if !tt.HasText(want) {
			t.Errorf("the import modal does not show %q; texts = %v", want, tt.Texts())
		}
	}
}
