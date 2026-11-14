package main

// The tests for the completion engine: the text is read back to the clause
// and the dot before the caret, and the metadata cache decides which names
// are offered. They build an App the way a connection does, without a
// server.

import (
	"strings"
	"testing"

	"github.com/dbcore/dbcore"
	"github.com/egoist/mygo/ui/editor"
)

// completionApp is an app with one schema of two tables, as a connection to
// the demo database has.
func completionApp() *App {
	app := &App{
		schemas: []string{"main"},
		tables: map[string][]tableInfo{
			"main": {{Name: "departments"}, {Name: "employees"}},
		},
		cols: map[string][]dbcore.ColumnInfo{
			tableKey("main", "employees"): {
				{Name: "id", Type: "INTEGER"},
				{Name: "name", Type: "TEXT"},
				{Name: "salary", Type: "REAL"},
			},
			tableKey("main", "departments"): {
				{Name: "id", Type: "INTEGER"},
				{Name: "name", Type: "TEXT"},
			},
		},
	}
	app.conn.Kind = "sqlite"
	return app
}

func labels(items []editor.Completion) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Label
	}
	return out
}

func hasLabel(items []editor.Completion, want string) bool {
	for _, it := range items {
		if it.Label == want {
			return true
		}
	}
	return false
}

func TestStripSQLLiterals(t *testing.T) {
	in := "SELECT * FROM t WHERE name = 'from x' -- from\n"
	out := stripSQLLiterals(in)
	if len(out) != len(in) {
		t.Fatalf("length = %d, want %d", len(out), len(in))
	}
	// The keyword before the literal survives, its own characters untouched.
	if out[9] != 'F' {
		t.Fatalf("FROM blanked: %q", out)
	}
	// What is inside the string and the comment does not.
	if strings.Contains(out[15:], "from") {
		t.Fatalf("literal or comment kept: %q", out[15:])
	}
	// A doubled quote inside a string keeps the length and blanks the span.
	in2 := `SELECT 'it''s from here'`
	out2 := stripSQLLiterals(in2)
	if len(out2) != len(in2) {
		t.Fatalf("doubled quote length = %d, want %d", len(out2), len(in2))
	}
	if strings.Contains(out2, "it") || strings.Contains(out2, "from") {
		t.Fatalf("doubled quote span kept: %q", out2)
	}
}

func TestReadBackWord(t *testing.T) {
	w, at := readBackWord("SELECT * FROM employees", len("SELECT * FROM employees"))
	if w != "employees" || at != len("SELECT * FROM ") {
		t.Fatalf("readBackWord = %q at %d, want employees at %d", w, at, len("SELECT * FROM "))
	}
	w, at = readBackWord("SELECT 1", len("SELECT 1"))
	if w != "1" || at != len("SELECT ") {
		t.Fatalf("number = %q at %d", w, at)
	}
	if w, _ := readBackWord("  ", 2); w != "" {
		t.Fatalf("spaces read %q", w)
	}
}

func TestCompletionsAfterFrom(t *testing.T) {
	app := completionApp()
	text := "SELECT * FROM emp"
	head := len(text)
	items := app.completionsAt(text, head, head-len("emp"))

	if !hasLabel(items, "employees") || !hasLabel(items, "departments") {
		t.Fatalf("tables missing from %v", labels(items))
	}
	// A table's columns are not offered where a table is asked for.
	if hasLabel(items, "salary") {
		t.Fatalf("column offered as a table: %v", labels(items))
	}
	// A keyword can still follow FROM, for a join or a subquery.
	if !hasLabel(items, "JOIN") && !hasLabel(items, "SELECT") {
		t.Fatalf("no keyword: %v", labels(items))
	}
}

func TestCompletionsAfterWhere(t *testing.T) {
	app := completionApp()
	text := "SELECT * FROM employees WHERE na"
	head := len(text)
	items := app.completionsAt(text, head, head-len("na"))

	if !hasLabel(items, "name") || !hasLabel(items, "salary") {
		t.Fatalf("columns missing from %v", labels(items))
	}
	if !hasLabel(items, "COUNT") {
		t.Fatalf("functions missing from %v", labels(items))
	}
}

func TestCompletionsQualified(t *testing.T) {
	app := completionApp()

	// A table's own columns after its name.
	text := "SELECT employees.sal"
	head := len(text)
	items := app.completionsAt(text, head, head-len("sal"))
	if !hasLabel(items, "salary") {
		t.Fatalf("table columns missing: %v", labels(items))
	}
	if hasLabel(items, "departments") {
		t.Fatalf("table offered after a dot: %v", labels(items))
	}

	// An alias names the same table, even though the caret is before it.
	text = "SELECT e.sal FROM employees e"
	head = len("SELECT e.sal")
	items = app.completionsAt(text, head, head-len("sal"))
	if !hasLabel(items, "salary") {
		t.Fatalf("alias columns missing: %v", labels(items))
	}

	// A schema names its tables.
	text = "SELECT main."
	head = len(text)
	items = app.completionsAt(text, head, head)
	if !hasLabel(items, "employees") || !hasLabel(items, "departments") {
		t.Fatalf("schema tables missing: %v", labels(items))
	}
}

func TestCompletionsJoinAliases(t *testing.T) {
	app := completionApp()
	text := "SELECT d.na FROM employees e JOIN departments d ON e.id = d.id WHERE d."
	head := len(text)
	items := app.completionsAt(text, head, head)
	if !hasLabel(items, "name") {
		t.Fatalf("joined alias columns missing: %v", labels(items))
	}
}

func TestCompletionsUseBrowsedTable(t *testing.T) {
	app := completionApp()
	// No FROM clause names a table, so the columns of the table the editor is
	// browsing are offered.
	app.cached = &Cached{}
	app.cached.schema = "main"
	app.cached.table = "employees"

	text := "SELECT sal"
	head := len(text)
	items := app.completionsAt(text, head, head-len("sal"))
	if !hasLabel(items, "salary") {
		t.Fatalf("browsed columns missing: %v", labels(items))
	}
}

func TestCompletionsIgnoreLiteralsAndComments(t *testing.T) {
	app := completionApp()
	app.cached = &Cached{}
	app.cached.schema = "main"
	app.cached.table = "employees"

	// The WHERE in the comment and the one in the string are not clauses; the
	// one before the caret is.
	text := "SELECT * FROM employees -- WHERE nope\n  WHERE na"
	head := len(text)
	items := app.completionsAt(text, head, head-len("na"))
	if !hasLabel(items, "name") {
		t.Fatalf("comment broke the context: %v", labels(items))
	}

	text = "SELECT * FROM 'WHERE' WHERE na"
	head = len(text)
	items = app.completionsAt(text, head, head-len("na"))
	if !hasLabel(items, "name") {
		t.Fatalf("string broke the context: %v", labels(items))
	}
}

func TestCompletionsKeywordsAndTypes(t *testing.T) {
	app := completionApp()
	app.cached = &Cached{}
	app.cached.schema = "main"
	app.cached.table = "employees"

	text := "SELECT 1"
	head := len(text)
	items := app.completionsAt(text, head, head)

	for _, want := range []string{"SELECT", "COUNT", "INTEGER", "employees", "name"} {
		if !hasLabel(items, want) {
			t.Fatalf("%q missing from %v", want, labels(items))
		}
	}
	// A kind is offered for each, so the list shows what each word is.
	kinds := map[string]editor.CompletionKind{}
	for _, it := range items {
		kinds[it.Label] = it.Kind
	}
	if kinds["SELECT"] != editor.CompletionKeyword {
		t.Fatalf("SELECT kind = %v", kinds["SELECT"])
	}
	if kinds["COUNT"] != editor.CompletionFunction {
		t.Fatalf("COUNT kind = %v", kinds["COUNT"])
	}
	// A function inserts its open parenthesis, as it is a call.
	for _, it := range items {
		if it.Label == "COUNT" {
			if it.Insert != "COUNT(" {
				t.Fatalf("COUNT insert = %q", it.Insert)
			}
			break
		}
	}
	if kinds["INTEGER"] != editor.CompletionType {
		t.Fatalf("INTEGER kind = %v", kinds["INTEGER"])
	}
	if kinds["employees"] != editor.CompletionType {
		t.Fatalf("table kind = %v", kinds["employees"])
	}
	if kinds["name"] != editor.CompletionField {
		t.Fatalf("column kind = %v", kinds["name"])
	}
}

func TestAliasAs(t *testing.T) {
	app := completionApp()

	// AS names the alias; a word that merely starts with "as" is the alias.
	text := "SELECT employees.name FROM employees AS e"
	if ref, ok := app.aliasFor(text, "e"); !ok || ref.table != "employees" {
		t.Fatalf("AS alias = %+v %v", ref, ok)
	}

	text = "SELECT employees.name FROM employees assign"
	if ref, ok := app.aliasFor(text, "assign"); !ok || ref.table != "employees" {
		t.Fatalf("assign = %+v %v", ref, ok)
	}
	if _, ok := app.aliasFor(text, "sign"); ok {
		t.Fatal("assign split into as + sign")
	}
}

func TestFindTable(t *testing.T) {
	app := completionApp()
	ref, ok := app.findTable("employees")
	if !ok || ref.schema != "main" || ref.table != "employees" {
		t.Fatalf("bare name = %+v %v", ref, ok)
	}
	ref, ok = app.findTable("main.employees")
	if !ok || ref.schema != "main" || ref.table != "employees" {
		t.Fatalf("qualified name = %+v %v", ref, ok)
	}
	if _, ok := app.findTable("nosuch"); ok {
		t.Fatal("unknown table found")
	}
}
