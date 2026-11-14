package main

// Tests for the polish batch: the destructive-statement question, the session
// that survives a restart, numeric columns lining up, and the form's Test
// button.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// --- the destructive-statement question -------------------------------------

// The guard questions exactly the statements that cannot be undone, and nothing
// else: a guard that nags is a guard that gets clicked through.
func TestRiskyStatement(t *testing.T) {
	cases := []struct {
		sql   string
		risky bool
	}{
		{"DROP TABLE employees", true},
		{"drop table if exists t", true},
		{"/* tidy up */ DROP VIEW v", true},
		{"TRUNCATE TABLE logs", true},
		{"DELETE FROM logs", true},
		{"delete from logs", true},
		{"DELETE FROM logs -- all of them", true},
		{"UPDATE accounts SET bal = 0", true},
		{"DELETE FROM logs WHERE id = 1", false},
		{"DELETE FROM logs where created < now()", false},
		{"UPDATE accounts SET bal = 0 WHERE id = 1", false},
		{"SELECT * FROM employees", false},
		{"INSERT INTO logs VALUES (1)", false},
		{"CREATE TABLE t (id int)", false},
		{"-- DELETE FROM logs", false},                   // a comment, not a statement
		{"DELETE FROM logs WHERE note = 'where'", false}, // the WHERE is real
		{"DELETE FROM logs WHERE 1 = 1", false},          // odd, but it says WHERE
	}
	for _, c := range cases {
		got := riskyStatement([]string{c.sql})
		if got != "" && !c.risky {
			t.Errorf("%q was questioned: %s", c.sql, got)
		}
		if got == "" && c.risky {
			t.Errorf("%q was not questioned", c.sql)
		}
	}
}

// One risky statement in a script is enough: the run is one unit.
func TestRiskyStatementAcrossScript(t *testing.T) {
	if riskyStatement([]string{"SELECT 1", "DROP TABLE t"}) == "" {
		t.Fatal("a script ending in a DROP was not questioned")
	}
	if riskyStatement([]string{"SELECT 1", "SELECT 2"}) != "" {
		t.Fatal("a read-only script was questioned")
	}
}

// A "where" inside a string literal does not count: that DELETE still deletes
// every row.
func TestHasWhereIgnoresLiterals(t *testing.T) {
	if hasWhere(`DELETE FROM t WHERE note = 'where'`) != true {
		t.Fatal("a real WHERE was not found")
	}
	if hasWhere(`DELETE FROM t -- where id = 1`) {
		t.Fatal("a WHERE in a comment counted")
	}
	if hasWhere(`UPDATE t SET note = 'somewhere'`) {
		t.Fatal("a WHERE inside a word counted")
	}
}

// The run path asks once: the question opens instead of running, and the
// confirmation runs the same script with the guard skipped.
func TestGuardAsksBeforeRunning(t *testing.T) {
	app, d := multiApp(t, setOf([]string{"id"}))
	app.runSQL("DROP TABLE employees")
	waitRun(t, app)
	if d.calls != 0 {
		t.Fatalf("the drop ran without asking (%d call(s))", d.calls)
	}
	if !app.confirm.open {
		t.Fatal("no question was asked")
	}
	if !strings.Contains(app.confirm.sql, "DROP TABLE employees") {
		t.Errorf("the question does not show the statement: %q", app.confirm.sql)
	}
	if !strings.Contains(app.confirm.why, "drops") {
		t.Errorf("the question does not say why: %q", app.confirm.why)
	}

	// The confirmation runs it, and only then does the driver see a statement.
	again := app.confirm.again
	again()
	waitRun(t, app)
	if d.calls != 1 {
		t.Fatalf("the confirmed drop ran %d time(s), want 1", d.calls)
	}
}

// A statement that cannot destroy anything runs without a question.
func TestGuardLetsSafeStatementsThrough(t *testing.T) {
	app, d := multiApp(t, setOf([]string{"id"}))
	app.runSQL("SELECT * FROM employees")
	waitRun(t, app)
	if app.confirm.open {
		t.Fatal("a SELECT was questioned")
	}
	if d.calls != 1 {
		t.Fatalf("the select ran %d time(s), want 1", d.calls)
	}
}

// --- numeric columns line up ------------------------------------------------

func TestAlignOf(t *testing.T) {
	right := []string{"INTEGER", "BIGINT", "int", "DECIMAL(10,2)", "NUMERIC", "REAL", "DOUBLE", "FLOAT", "BOOLEAN"}
	for _, ty := range right {
		if got := alignOf(ty); got != ui.End {
			t.Errorf("alignOf(%q) = %v, want right", ty, got)
		}
	}
	left := []string{"TEXT", "VARCHAR(50)", "DATE", "TIMESTAMP", "BLOB", "JSON", "UUID", ""}
	for _, ty := range left {
		if got := alignOf(ty); got != ui.Start {
			t.Errorf("alignOf(%q) = %v, want left", ty, got)
		}
	}
}

// --- the session ------------------------------------------------------------

// A session round-trips through its file: consoles with their SQL, the chosen
// console, the working schema, the splits and the window's size.
func TestSessionRoundTrip(t *testing.T) {
	app, _ := refreshApp(t)
	app.initConsoles()
	app.initEditorState()
	app.edBuf.Set("SELECT 1;")
	app.consoles[0].label = "First"
	app.newConsole()
	app.edBuf.Set("SELECT 2;")
	app.consolesel = 1
	app.schema = "main"
	app.splitSide = 300
	app.splitEditor = 250

	path := filepath.Join(t.TempDir(), sessionFile)
	saved := app.sessionNow()
	saved.Width, saved.Height = 1200, 700
	writeSession(t, path, saved)

	// 波 100：钳制依赖屏幕事实，测试没有 running app——注入一个假的。
	origArea := workAreaOf
	workAreaOf = func() (int, int) { return 0, 0 }
	t.Cleanup(func() { workAreaOf = origArea })

	got := loadSession(path)
	if len(got.Consoles) != 2 {
		t.Fatalf("consoles = %d, want 2", len(got.Consoles))
	}
	if got.Consoles[0].Label != "First" || got.Consoles[0].SQL != "SELECT 1;" {
		t.Fatalf("first console = %+v", got.Consoles[0])
	}
	if got.ConsoleSel != 1 || got.Schema != "main" {
		t.Fatalf("selection/schema = %d/%q", got.ConsoleSel, got.Schema)
	}
	if got.SplitSide != 300 || got.SplitEdit != 250 {
		t.Fatalf("splits = %v/%v", got.SplitSide, got.SplitEdit)
	}
	if w, h := windowSize(got); w != 1200 || h != 700 {
		t.Fatalf("window = %dx%d, want 1200x700", w, h)
	}
}

// 波 100：会话里的大于屏幕的窗口尺寸被钳到工作区里——换过显示器的会话
// 不能把任务栏和半张桌面顶出屏幕。
func TestWindowSizeClampsToWorkArea(t *testing.T) {
	origArea := workAreaOf
	t.Cleanup(func() { workAreaOf = origArea })

	workAreaOf = func() (int, int) { return 1920, 1040 }
	if w, h := windowSize(sessionState{Width: 2560, Height: 1400}); w != 1904 || h != 1024 {
		t.Errorf("oversized session = %dx%d, want clamped 1904x1024", w, h)
	}
	// A session that fits passes through untouched.
	if w, h := windowSize(sessionState{Width: 1200, Height: 700}); w != 1200 || h != 700 {
		t.Errorf("fitting session = %dx%d, want 1200x700", w, h)
	}
	// A tiny work area clamps to the window's own minimum, never below it.
	workAreaOf = func() (int, int) { return 800, 400 }
	if w, h := windowSize(sessionState{Width: 2560, Height: 1400}); w != 860 || h != 520 {
		t.Errorf("tiny work area = %dx%d, want minimum 860x520", w, h)
	}
	// No screen facts (a test, a headless run): the session or default stands.
	workAreaOf = func() (int, int) { return 0, 0 }
	if w, h := windowSize(sessionState{Width: 2560, Height: 1400}); w != 2560 || h != 1400 {
		t.Errorf("no work area = %dx%d, want the session's own", w, h)
	}
}

// Applying a session rebuilds the consoles (each with its own editor state),
// the choice, the schema and the splits.
func TestApplySession(t *testing.T) {
	app, _ := refreshApp(t)
	app.initConsoles()
	app.initEditorState()
	app.applySession(sessionState{
		Schema:     "main",
		Consoles:   []sessionConsole{{Label: "A", SQL: "SELECT 1;"}, {Label: "B", SQL: "SELECT 2;"}},
		ConsoleSel: 1,
		SplitSide:  111,
		SplitEdit:  222,
	})
	if len(app.consoles) != 2 {
		t.Fatalf("consoles = %d, want 2", len(app.consoles))
	}
	if app.edBuf.String() != "SELECT 2;" {
		t.Fatalf("the shown console holds %q", app.edBuf.String())
	}
	if app.consoles[0].state == app.consoles[1].state {
		t.Fatal("restored consoles share one editor state")
	}
	if app.schema != "main" || app.splitSide != 111 || app.splitEditor != 222 {
		t.Fatalf("state = %q %v %v", app.schema, app.splitSide, app.splitEditor)
	}
	// A saved choice past the end is clamped rather than left dangling.
	app.applySession(sessionState{
		Consoles:   []sessionConsole{{Label: "only", SQL: ""}},
		ConsoleSel: 7,
	})
	if app.consolesel != 0 {
		t.Fatalf("consoleSel = %d, want 0", app.consolesel)
	}
}

// A missing or broken session is a fresh start, not an error.
func TestSessionMissingOrBroken(t *testing.T) {
	// 波 100：钳制依赖屏幕事实，测试没有 running app——注入"无屏幕"。
	origArea := workAreaOf
	workAreaOf = func() (int, int) { return 0, 0 }
	t.Cleanup(func() { workAreaOf = origArea })

	dir := t.TempDir()
	if got := loadSession(filepath.Join(dir, "nope.json")); len(got.Consoles) != 0 {
		t.Fatal("a missing session produced a state")
	}
	path := filepath.Join(dir, sessionFile)
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := loadSession(path); len(got.Consoles) != 0 {
		t.Fatal("a broken session produced a state")
	}
	// The window's fallback size is used when the session has none.
	if w, h := windowSize(sessionState{}); w != 1320 || h != 860 {
		t.Fatalf("default window = %dx%d", w, h)
	}
	// A session too small for the window minimum is ignored.
	if w, h := windowSize(sessionState{Width: 100, Height: 100}); w != 1320 {
		t.Fatalf("a too-small session was used: %dx%d", w, h)
	}
}

// writeSession is the test's writer (the app's saveSession needs a window).
func writeSession(t *testing.T, path string, s sessionState) {
	t.Helper()
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// --- the form's Test button -------------------------------------------------

// A test against a real SQLite file reports success and the engine, without
// saving the connection.
func TestFormTestSucceeds(t *testing.T) {
	app, _ := refreshApp(t)
	app.initConsoles()
	app.initEditorState()
	path := filepath.Join(t.TempDir(), "t.db")
	seed(t, path)
	app.openForm()
	app.form.Kind = "sqlite"
	app.form.File = path
	app.testForm()
	deadline := 400
	for app.formTesting && deadline > 0 {
		time.Sleep(5 * time.Millisecond)
		deadline--
	}
	if app.formTesting {
		t.Fatal("the test did not finish")
	}
	if !app.formTestOK {
		t.Fatalf("the test failed: %q", app.formTest)
	}
	if !strings.Contains(app.formTest, "SQLite") || !strings.Contains(app.formTest, "in ") {
		t.Fatalf("the verdict does not name the engine and the time: %q", app.formTest)
	}
}

// A test that cannot connect says why, in the test's own line (not the form's
// save error).
func TestFormTestFails(t *testing.T) {
	app, _ := refreshApp(t)
	app.openForm()
	app.form.Kind = "mysql"
	app.form.Host = "127.0.0.1"
	app.form.Port = 1
	app.form.User = "x"
	app.form.Name = "bad"
	app.testForm()
	// The connect has its own 15s budget; the test waits for the verdict rather
	// than guessing, because a loaded machine dials slower than an idle one.
	for i := 0; i < 3000 && app.formTesting; i++ {
		time.Sleep(5 * time.Millisecond)
	}
	if app.formTesting {
		t.Fatal("the test never finished")
	}
	if app.formTestOK {
		t.Fatal("a connection to a dead port succeeded")
	}
	if app.formTest == "" {
		t.Fatal("a failed test said nothing")
	}
	if app.formErr != "" {
		t.Fatalf("the test wrote into the save error: %q", app.formErr)
	}
}

// --- the session file and the console keys ---------------------------------

// Changing consoles writes the session: a crash costs the typing since the last
// run, not the set of consoles.
func TestConsoleChangeSavesSession(t *testing.T) {
	app, _ := refreshApp(t)
	// A store gives the app a directory to keep the session in.
	store, err := NewStoreAt(filepath.Join(t.TempDir(), "connections.json"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	app.store = store
	app.initConsoles()
	app.initEditorState()
	app.edBuf.Set("SELECT 42;")
	app.newConsole()
	path := app.sessionPath()
	if path == "" {
		t.Fatal("no session path")
	}
	got := loadSession(path)
	if len(got.Consoles) != 2 {
		t.Fatalf("the session holds %d consoles, want 2", len(got.Consoles))
	}
	if !strings.Contains(got.Consoles[0].SQL, "SELECT 42;") {
		t.Fatalf("the first console's SQL was not saved: %+v", got.Consoles[0])
	}
}

// selectConsoleAt cycles and clamps.
func TestSelectConsoleAt(t *testing.T) {
	app := consolesApp(t)
	app.newConsole() // two consoles, second selected
	if app.consolesel != 1 {
		t.Fatalf("selection = %d", app.consolesel)
	}
	app.selectConsoleAt(0)
	if app.consolesel != 0 {
		t.Fatalf("selection = %d, want 0", app.consolesel)
	}
	app.selectConsoleAt(9) // out of range: nothing happens
	if app.consolesel != 0 {
		t.Fatalf("an out-of-range choice moved the selection: %d", app.consolesel)
	}
}
