package main

// Tests for the grid's "copy as SQL / CSV" paths. The statement builders are
// pure, so they are tested directly; copyText is the one that needs the
// working copy to name a table, so its App is set up by hand.

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ben-guanzhi/dbcore"
	"github.com/Ben-guanzhi/dbcore/sqltext"
)

// peopleCols is a small table's columns, the way the catalog reports them: id
// is the key, and the rest carry no flag.
func peopleCols() []dbcore.ColumnInfo {
	return []dbcore.ColumnInfo{
		{Name: "id", Type: "INTEGER", IsPrimaryKey: true},
		{Name: "name", Type: "TEXT"},
		{Name: "salary", Type: "REAL"},
	}
}

func TestSQLLiteralByKind(t *testing.T) {
	tm := dbcore.Value{Kind: dbcore.ValTime, T: time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)}
	for _, c := range []struct {
		name string
		v    dbcore.Value
		want string
	}{
		{"null", dbcore.Value{Kind: dbcore.ValNull}, "NULL"},
		{"int", dbcore.Value{Kind: dbcore.ValInt, I: -17}, "-17"},
		{"float", dbcore.Value{Kind: dbcore.ValFloat, F: 1.5}, "1.5"},
		{"decimal", dbcore.Value{Kind: dbcore.ValDecimal, S: "12345678901234567890"}, "12345678901234567890"},
		{"bool", dbcore.Value{Kind: dbcore.ValBool, Bool: true}, "TRUE"},
		{"bool false", dbcore.Value{Kind: dbcore.ValBool, Bool: false}, "FALSE"},
		{"blob", dbcore.Value{Kind: dbcore.ValBlob, B: []byte{0xde, 0xad}}, "X'dead'"},
		{"text", dbcore.Value{Kind: dbcore.ValText, S: "Ada"}, "'Ada'"},
		{"quote", dbcore.Value{Kind: dbcore.ValText, S: "O'Brien"}, "'O''Brien'"},
		{"time", tm, sqltext.QuotedLiteral("sqlite", tm.String())},
	} {
		if got := sqlLiteral("sqlite", c.v); got != c.want {
			t.Errorf("%s: sqlLiteral = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestInsertStatement(t *testing.T) {
	cols := peopleCols()
	row := []dbcore.Value{
		{Kind: dbcore.ValInt, I: 1},
		{Kind: dbcore.ValText, S: "Ada"},
		{Kind: dbcore.ValFloat, F: 190000},
	}
	got, err := insertStatement("sqlite", "", "employees", cols, row)
	if err != nil {
		t.Fatalf("insertStatement: %v", err)
	}
	want := `INSERT INTO "employees" ("id", "name", "salary") VALUES (1, 'Ada', 190000);`
	if got != want {
		t.Fatalf("insertStatement =\n%s\nwant\n%s", got, want)
	}
}

func TestInsertStatementNullAndQuote(t *testing.T) {
	cols := []dbcore.ColumnInfo{{Name: "a"}, {Name: `we"rd`}}
	row := []dbcore.Value{{Kind: dbcore.ValNull}, {Kind: dbcore.ValText, S: "x"}}
	got, err := insertStatement("sqlite", "", "t", cols, row)
	if err != nil {
		t.Fatalf("insertStatement: %v", err)
	}
	want := `INSERT INTO "t" ("a", "we""rd") VALUES (NULL, 'x');`
	if got != want {
		t.Fatalf("insertStatement =\n%s\nwant\n%s", got, want)
	}
}

func TestUpdateStatement(t *testing.T) {
	cols := peopleCols()
	row := []dbcore.Value{
		{Kind: dbcore.ValInt, I: 7},
		{Kind: dbcore.ValText, S: "Grace"},
		{Kind: dbcore.ValNull},
	}
	got, err := updateStatement("sqlite", "", "employees", cols, row)
	if err != nil {
		t.Fatalf("updateStatement: %v", err)
	}
	want := `UPDATE "employees" SET "name" = 'Grace', "salary" = NULL WHERE "id" = 7;`
	if got != want {
		t.Fatalf("updateStatement =\n%s\nwant\n%s", got, want)
	}
}

func TestUpdateStatementCompositeKey(t *testing.T) {
	cols := []dbcore.ColumnInfo{
		{Name: "a", IsPrimaryKey: true},
		{Name: "b", IsPrimaryKey: true},
		{Name: "v"},
	}
	row := []dbcore.Value{{Kind: dbcore.ValInt, I: 1}, {Kind: dbcore.ValInt, I: 2}, {Kind: dbcore.ValText, S: "x"}}
	got, err := updateStatement("sqlite", "", "t", cols, row)
	if err != nil {
		t.Fatalf("updateStatement: %v", err)
	}
	want := `UPDATE "t" SET "v" = 'x' WHERE "a" = 1 AND "b" = 2;`
	if got != want {
		t.Fatalf("updateStatement =\n%s\nwant\n%s", got, want)
	}
}

func TestUpdateAndDeleteWithoutKey(t *testing.T) {
	cols := []dbcore.ColumnInfo{{Name: "a"}, {Name: "b"}}
	row := []dbcore.Value{{Kind: dbcore.ValInt, I: 1}, {Kind: dbcore.ValInt, I: 2}}
	if _, err := updateStatement("sqlite", "", "t", cols, row); err == nil {
		t.Error("updateStatement without a key: want an error, got none")
	}
	if _, err := deleteStatement("sqlite", "", "t", cols, row); err == nil {
		t.Error("deleteStatement without a key: want an error, got none")
	}
}

func TestDeleteStatement(t *testing.T) {
	cols := peopleCols()
	row := []dbcore.Value{
		{Kind: dbcore.ValInt, I: 3},
		{Kind: dbcore.ValText, S: "Alan"},
		{Kind: dbcore.ValFloat, F: 1},
	}
	got, err := deleteStatement("sqlite", "", "employees", cols, row)
	if err != nil {
		t.Fatalf("deleteStatement: %v", err)
	}
	want := `DELETE FROM "employees" WHERE "id" = 3;`
	if got != want {
		t.Fatalf("deleteStatement =\n%s\nwant\n%s", got, want)
	}
}

func TestIdentifiersByDialect(t *testing.T) {
	cols := []dbcore.ColumnInfo{{Name: "a b", IsPrimaryKey: true}, {Name: "c"}}
	row := []dbcore.Value{{Kind: dbcore.ValInt, I: 1}, {Kind: dbcore.ValInt, I: 2}}
	for _, c := range []struct {
		kind   string
		schema string
		want   string
	}{
		{"mysql", "", "INSERT INTO `t` (`a b`, `c`) VALUES (1, 2);"},
		{"sqlserver", "dbo", "INSERT INTO [dbo].[t] ([a b], [c]) VALUES (1, 2);"},
		{"postgresql", "public", `INSERT INTO "public"."t" ("a b", "c") VALUES (1, 2);`},
	} {
		got, err := insertStatement(c.kind, c.schema, "t", cols, row)
		if err != nil {
			t.Fatalf("insertStatement(%s): %v", c.kind, err)
		}
		if got != c.want {
			t.Errorf("%s: insertStatement =\n%s\nwant\n%s", c.kind, got, c.want)
		}
	}
}

func TestCSVText(t *testing.T) {
	res := &dbcore.QueryResult{
		Columns: []dbcore.ColumnInfo{{Name: "id"}, {Name: "name"}, {Name: "note"}},
		Rows: [][]dbcore.Value{
			{
				{Kind: dbcore.ValInt, I: 1},
				{Kind: dbcore.ValText, S: "Ada"},
				{Kind: dbcore.ValNull},
			},
			{
				{Kind: dbcore.ValInt, I: 2},
				{Kind: dbcore.ValText, S: "a, b"},
				{Kind: dbcore.ValText, S: `say "hi"`},
			},
		},
	}
	got, err := csvText(res)
	if err != nil {
		t.Fatalf("csvText: %v", err)
	}
	want := "id,name,note\n1,Ada,\n2,\"a, b\",\"say \"\"hi\"\"\"\n"
	if got != want {
		t.Fatalf("csvText =\n%q\nwant\n%q", got, want)
	}
}

func TestCopyTextTable(t *testing.T) {
	app := &App{}
	app.edit.active = true
	app.edit.schema = "public"
	app.edit.table = "people"
	app.edit.cols = peopleCols()
	app.conn.Kind = "postgresql"

	res := &dbcore.QueryResult{
		Columns: peopleCols(),
		Rows: [][]dbcore.Value{
			{
				{Kind: dbcore.ValInt, I: 1},
				{Kind: dbcore.ValText, S: "Ada"},
				{Kind: dbcore.ValFloat, F: 190000},
			},
			{
				{Kind: dbcore.ValInt, I: 2},
				{Kind: dbcore.ValText, S: "Alan"},
				{Kind: dbcore.ValNull},
			},
		},
	}

	got, err := app.copyText(copyInsert, res, nil)
	if err != nil {
		t.Fatalf("copyText INSERT: %v", err)
	}
	want := `INSERT INTO "public"."people" ("id", "name", "salary") VALUES (1, 'Ada', 190000);
INSERT INTO "public"."people" ("id", "name", "salary") VALUES (2, 'Alan', NULL);`
	if got != want {
		t.Fatalf("copyText INSERT =\n%s\nwant\n%s", got, want)
	}

	got, err = app.copyText(copyUpdate, res, []int{0})
	if err != nil {
		t.Fatalf("copyText UPDATE: %v", err)
	}
	want = `UPDATE "public"."people" SET "name" = 'Ada', "salary" = 190000 WHERE "id" = 1;`
	if got != want {
		t.Fatalf("copyText UPDATE =\n%s\nwant\n%s", got, want)
	}

	got, err = app.copyText(copyDelete, res, []int{1})
	if err != nil {
		t.Fatalf("copyText DELETE: %v", err)
	}
	want = `DELETE FROM "public"."people" WHERE "id" = 2;`
	if got != want {
		t.Fatalf("copyText DELETE =\n%s\nwant\n%s", got, want)
	}

	got, err = app.copyText(copyCSV, res, nil)
	if err != nil {
		t.Fatalf("copyText CSV: %v", err)
	}
	if !strings.HasPrefix(got, "id,name,salary\n") {
		t.Fatalf("copyText CSV header = %q", got)
	}
}

func TestCopyTextWithoutTable(t *testing.T) {
	// A read-only result 鈥?an aggregate, say 鈥?has no table to address rows
	// with, so the statements refuse and CSV still works.
	app := &App{}
	res := &dbcore.QueryResult{
		Columns: []dbcore.ColumnInfo{{Name: "n"}},
		Rows:    [][]dbcore.Value{{{Kind: dbcore.ValInt, I: 3}}},
	}
	for _, k := range []copyKind{copyInsert, copyUpdate, copyDelete} {
		if _, err := app.copyText(k, res, nil); err == nil {
			t.Errorf("copyText %s without a table: want an error, got none", k.label())
		}
	}
	if got, err := app.copyText(copyCSV, res, nil); err != nil || got != "n\n3\n" {
		t.Fatalf("copyText CSV without a table = %q, %v; want \"n\\n3\\n\"", got, err)
	}
}

func TestCopyTextOutOfRangeRow(t *testing.T) {
	app := &App{}
	app.edit.active = true
	app.edit.table = "t"
	app.edit.cols = []dbcore.ColumnInfo{{Name: "a", IsPrimaryKey: true}}
	app.conn.Kind = "sqlite"
	res := &dbcore.QueryResult{
		Columns: []dbcore.ColumnInfo{{Name: "a", IsPrimaryKey: true}},
		Rows:    [][]dbcore.Value{{{Kind: dbcore.ValInt, I: 1}}},
	}
	got, err := app.copyText(copyInsert, res, []int{9})
	if err != nil {
		t.Fatalf("copyText: %v", err)
	}
	if got != "" {
		t.Fatalf("copyText out-of-range row = %q, want empty", got)
	}
}

func TestCopyTableRejectsReadOnly(t *testing.T) {
	// Without an active working copy there is no table to name, whatever the
	// grid holds.
	app := &App{}
	if _, _, _, _, ok := app.copyTable(); ok {
		t.Fatal("copyTable on a read-only grid: want ok=false")
	}
}

// TestCopyRoundTrip drives the whole path against a real database: the working
// copy names the table, the statements copyText renders are executed, and the
// rows they address are the rows that change.
func TestCopyRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "copy.db")
	seed(t, path)

	ctx := context.Background()
	db, err := OpenDB(ctx, Conn{Name: "copy", Kind: "sqlite", File: path})
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()

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
	cols := cached.columnsOf(schema, "employees")

	res, err := db.SQL.Execute(ctx, "SELECT * FROM employees ORDER BY id")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	app := &App{db: db}
	app.edit.init(res, schema, "employees", cols, nil)

	// The INSERT of every row is a statement the engine accepts, and the rows
	// it names are the rows that land back in the table.
	ins, err := app.copyText(copyInsert, res, nil)
	if err != nil {
		t.Fatalf("copyText INSERT: %v", err)
	}
	if _, err := db.SQL.Execute(ctx, "DELETE FROM employees"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, err := db.SQL.Execute(ctx, ins); err != nil {
		t.Fatalf("execute copied INSERT: %v\n%s", err, ins)
	}
	back, err := db.SQL.Execute(ctx, "SELECT name FROM employees ORDER BY name")
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if len(back.Rows) != 2 {
		t.Fatalf("rows after INSERT = %d, want 2", len(back.Rows))
	}

	// The UPDATE's key keeps the row it was generated for and changes its text.
	res.Rows[0][1] = dbcore.Value{Kind: dbcore.ValText, S: "Ada Lovelace"}
	upd, err := app.copyText(copyUpdate, res, []int{0})
	if err != nil {
		t.Fatalf("copyText UPDATE: %v", err)
	}
	if _, err := db.SQL.Execute(ctx, upd); err != nil {
		t.Fatalf("execute copied UPDATE: %v\n%s", err, upd)
	}
	changed, err := db.SQL.Execute(ctx, "SELECT name FROM employees WHERE name = 'Ada Lovelace'")
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if len(changed.Rows) != 1 {
		t.Fatalf("rows updated = %d, want 1", len(changed.Rows))
	}

	// The DELETE's key takes exactly its own row out.
	del, err := app.copyText(copyDelete, res, []int{1})
	if err != nil {
		t.Fatalf("copyText DELETE: %v", err)
	}
	if _, err := db.SQL.Execute(ctx, del); err != nil {
		t.Fatalf("execute copied DELETE: %v\n%s", err, del)
	}
	final, err := db.SQL.Execute(ctx, "SELECT name FROM employees ORDER BY name")
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if len(final.Rows) != 1 || final.Rows[0][0].S != "Ada Lovelace" {
		t.Fatalf("rows after DELETE = %+v, want Ada Lovelace alone", final.Rows)
	}
}
