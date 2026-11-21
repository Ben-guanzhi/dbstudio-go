package main

// Tests for the parameterized query panel (P2 #15): placeholder extraction
// (positional ?, $n, named :name — skipping strings, comments and ::casts),
// the rewrite into the target engine's own markers, and the bound execution
// against a real SQLite file, which is the whole path the panel's Run takes.

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ben-guanzhi/dbcore"
	"github.com/egoist/mygo/ui"
)

func TestExtractParamsPositional(t *testing.T) {
	refs := extractParams("SELECT * FROM t WHERE a = ? AND b = ?")
	if len(refs) != 2 {
		t.Fatalf("refs = %d, want 2", len(refs))
	}
	if refs[0].Index != 0 || refs[1].Index != 1 {
		t.Fatalf("indices = %d, %d, want 0, 1", refs[0].Index, refs[1].Index)
	}
	if refs[0].Start != 26 || refs[0].End != 27 {
		t.Fatalf("first range = [%d, %d), want [26, 27)", refs[0].Start, refs[0].End)
	}
}

func TestExtractParamsDollarNumbered(t *testing.T) {
	// $n is self-indexing: $2 binds the second value even written first.
	refs := extractParams("SELECT $2, $1")
	if len(refs) != 2 {
		t.Fatalf("refs = %d, want 2", len(refs))
	}
	if refs[0].Index != 1 || refs[1].Index != 0 {
		t.Fatalf("indices = %d, %d, want 1, 0 (self-indexing)", refs[0].Index, refs[1].Index)
	}
}

func TestExtractParamsNamed(t *testing.T) {
	refs := extractParams("SELECT * FROM t WHERE a = :low AND b = :high AND c = :low")
	if len(refs) != 3 {
		t.Fatalf("refs = %d, want 3", len(refs))
	}
	// Reusing a name reuses its binding position.
	if refs[0].Name != "low" || refs[0].Index != 0 {
		t.Fatalf("first = %+v, want low@0", refs[0])
	}
	if refs[2].Name != "low" || refs[2].Index != 0 {
		t.Fatalf("third = %+v, want low@0 again", refs[2])
	}
	if refs[1].Name != "high" || refs[1].Index != 1 {
		t.Fatalf("second = %+v, want high@1", refs[1])
	}
}

func TestExtractParamsSkipsStringsCommentsCasts(t *testing.T) {
	cases := []struct {
		sql  string
		want int
	}{
		{`SELECT 'a?b' FROM t`, 0},
		{`SELECT "a?b" FROM t`, 0},
		{`SELECT 1 -- comment ? :x $1
`, 0},
		{`SELECT 1 /* block ? :x */ FROM t`, 0},
		{`SELECT x::text FROM t`, 0},
		{`SELECT ? FROM t WHERE x::text = ?`, 2},
	}
	for _, c := range cases {
		if got := len(extractParams(c.sql)); got != c.want {
			t.Errorf("extract(%q) = %d refs, want %d", c.sql, got, c.want)
		}
	}
}

func TestRewriteParamsPerEngine(t *testing.T) {
	sql := "SELECT * FROM t WHERE a = :low AND b = ?"
	refs := extractParams(sql)

	my, err := rewriteParams(sql, refs, "mysql")
	if err != nil {
		t.Fatalf("mysql rewrite: %v", err)
	}
	if my != "SELECT * FROM t WHERE a = ? AND b = ?" {
		t.Fatalf("mysql rewrite = %q", my)
	}

	pg, err := rewriteParams(sql, refs, "postgres")
	if err != nil {
		t.Fatalf("pg rewrite: %v", err)
	}
	if pg != "SELECT * FROM t WHERE a = $1 AND b = $2" {
		t.Fatalf("pg rewrite = %q", pg)
	}

	ora, err := rewriteParams(sql, refs, "oracle")
	if err != nil {
		t.Fatalf("oracle rewrite: %v", err)
	}
	if ora != "SELECT * FROM t WHERE a = :1 AND b = :2" {
		t.Fatalf("oracle rewrite = %q", ora)
	}
}

func TestRewriteParamsNoRefsUnchanged(t *testing.T) {
	sql := "SELECT 1"
	if got, err := rewriteParams(sql, nil, "postgres"); err != nil || got != sql {
		t.Fatalf("rewrite of a parameterless statement = %q, %v", got, err)
	}
}

func TestParamValue(t *testing.T) {
	cases := []struct {
		in   string
		want interface{}
	}{
		{"", nil},
		{"  ", nil},
		{"42", int64(42)},
		{"-7", int64(-7)},
		{"3.5", float64(3.5)},
		{"Ada", "Ada"},
		{"007", "007"}, // a leading-zero text stays text, not a number
	}
	for _, c := range cases {
		got := paramValue(c.in)
		if got != c.want {
			t.Errorf("paramValue(%q) = %#v, want %#v", c.in, got, c.want)
		}
	}
}

func TestParamLabel(t *testing.T) {
	sql := "SELECT :low, ?"
	refs := extractParams(sql)
	if got := paramLabel(sql, refs[0]); got != "low" {
		t.Fatalf("named label = %q, want low", got)
	}
	if got := paramLabel(sql, refs[1]); got != "2" {
		t.Fatalf("positional label = %q, want 2", got)
	}
}

// The panel draws: its title, one field per parameter, and its buttons are on
// screen — so a nil state field cannot hide until Run is pressed.
func TestParamsModalRenders(t *testing.T) {
	app := newViewApp(t, nil)
	app.open = true
	sql := "SELECT * FROM t WHERE a = :low AND b = ?"
	refs := extractParams(sql)
	app.params = paramPanel{
		open:    true,
		sql:     sql,
		refs:    refs,
		labels:  []string{"low", "2"},
		values:  []string{"", ""},
		rewrite: "SELECT * FROM t WHERE a = ? AND b = ?",
	}

	tt := ui.NewTester(app.view, 1200, 760)
	for _, want := range []string{"Parameters", "low", "2", "Cancel", "Run"} {
		if !tt.HasText(want) {
			t.Errorf("the parameter panel does not show %q; texts = %v", want, tt.Texts())
		}
	}
}

// The end-to-end path: extract, rewrite for the engine, bind the values the
// panel collected, and run on a real SQLite file.
func TestBoundExecutionOnSQLite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "params.db")
	seed(t, path)
	ctx := context.Background()
	db, err := OpenDB(ctx, Conn{Name: "params", Kind: "sqlite", File: path})
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	sql := "SELECT name FROM employees WHERE salary > :low AND id < ?"
	refs := extractParams(sql)
	rewritten, err := rewriteParams(sql, refs, "sqlite")
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if rewritten != "SELECT name FROM employees WHERE salary > ? AND id < ?" {
		t.Fatalf("rewritten = %q", rewritten)
	}

	// low binds position 1, the plain ? position 2: Ada(190000) and Alan(185000)
	// both clear 100000, and both ids are below 100.
	res, err := db.SQL.Execute(ctx, rewritten,
		dbcore.ValueOf(int64(100000)), dbcore.ValueOf(int64(100)))
	if err != nil {
		t.Fatalf("bound execute: %v", err)
	}
	if len(res.Rows) != 2 {
		t.Fatalf("rows = %d, want 2 (Ada, Alan)", len(res.Rows))
	}
	var names []string
	for _, r := range res.Rows {
		names = append(names, r[0].S)
	}
	if strings.Join(names, ",") != "Ada,Alan" {
		t.Fatalf("names = %v", names)
	}
}
