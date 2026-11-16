package main

// Tests for the results pane's report: the lines the message panel shows, the
// report clearing, and the split reset. The panel is a view, so only the state
// it reads and writes is exercised.

import (
	"strings"
	"testing"

	"github.com/Ben-guanzhi/dbcore"
)

func TestMessageLines(t *testing.T) {
	res := &dbcore.QueryResult{
		Columns:      []dbcore.ColumnInfo{{Name: "id"}, {Name: "name"}},
		Rows:         [][]dbcore.Value{{{Kind: dbcore.ValInt, I: 1}, {Kind: dbcore.ValText, S: "Ada"}}},
		AffectedRows: 3,
		LastInsertID: 17,
	}
	app := &App{}
	tb := &resTab{sql: "INSERT INTO employees (name) VALUES ('Ada')", status: "3 rows affected, 2ms", result: res}

	lines := app.messageLines(tb)
	if len(lines) == 0 {
		t.Fatal("no message lines for a completed statement")
	}
	if got := lines[0].text; !strings.Contains(got, tb.sql) {
		t.Errorf("statement line = %q, want it to carry %q", got, tb.sql)
	}
	if lines[0].kind != msgStatement {
		t.Errorf("first line kind = %v, want the statement", lines[0].kind)
	}
	// The status line carries the one-line report.
	found := false
	for _, l := range lines {
		if l.text == tb.status {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("status %q missing from the lines", tb.status)
	}
	// The rows the statement changed, and the id it inserted, are named.
	if !hasDetail(lines, "3 rows affected") || !hasDetail(lines, "Last insert id: 17") {
		t.Errorf("lines = %+v, want the affected count and last insert id", lines)
	}
}

func TestMessageLinesTruncated(t *testing.T) {
	total := 250
	res := &dbcore.QueryResult{
		Rows:      [][]dbcore.Value{{{Kind: dbcore.ValInt, I: 1}}},
		Truncated: true,
		TotalRows: &total,
	}
	lines := (&App{}).messageLines(&resTab{sql: "SELECT * FROM big", result: res})
	if !hasDetail(lines, "1 row of an estimated 250, truncated") {
		t.Errorf("lines = %+v, want the estimated total", lines)
	}
}

func TestMessageLinesError(t *testing.T) {
	lines := (&App{}).messageLines(&resTab{errMsg: "statement 2: no such table: t"})
	if len(lines) != 1 || lines[0].kind != msgError {
		t.Fatalf("lines = %+v, want the error alone", lines)
	}
	// A failure keeps its full text, not just the first line.
	lines = (&App{}).messageLines(&resTab{errMsg: "one\ntwo"})
	if len(lines) != 1 || lines[0].text != "one\ntwo" {
		t.Fatalf("error text = %+v, want it whole", lines)
	}
}

func TestMessageLinesNoTabFallsBackToApp(t *testing.T) {
	// A connection failure has no tab to read, so the app's message stands in.
	app := &App{errMsg: "Connect failed: dial tcp"}
	lines := app.messageLines(nil)
	if len(lines) != 1 || lines[0].text != "Connect failed: dial tcp" || lines[0].kind != msgError {
		t.Fatalf("lines = %+v, want the app's error", lines)
	}
	// Nothing at all means nothing to say.
	if lines := (&App{}).messageLines(nil); len(lines) != 0 {
		t.Fatalf("lines = %+v, want none", lines)
	}
}

func TestClearMessages(t *testing.T) {
	app := &App{status: "Query done"}
	tb := &resTab{status: "3 rows", errMsg: "statement 1: boom"}
	app.tabs = []*resTab{tb}
	app.tabSel = 0
	app.clearMessages()
	if tb.status != "" || tb.errMsg != "" {
		t.Errorf("tab report after clear = %q / %q, want both empty", tb.status, tb.errMsg)
	}
	if app.errMsg != "" {
		t.Errorf("app message after clear = %q, want empty", app.errMsg)
	}
}

func TestResetSplit(t *testing.T) {
	app := &App{splitSide: 10, splitEditor: 10}
	app.resetSplit()
	if app.splitSide != defaultSplitSide || app.splitEditor != defaultSplitEditor {
		t.Fatalf("split after reset = %v / %v, want %v / %v",
			app.splitSide, app.splitEditor, defaultSplitSide, defaultSplitEditor)
	}
}

func hasDetail(lines []messageLine, want string) bool {
	for _, l := range lines {
		if l.kind == msgDetail && l.text == want {
			return true
		}
	}
	return false
}
