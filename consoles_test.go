package main

// Tests for the editor consoles (#24): the tab switch swaps which buffer the
// editor reads without the consumers knowing, tabs are named after what they
// hold, the cap is honored, and the last tab cannot be closed.

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func consolesApp(t *testing.T) *App {
	t.Helper()
	app := newViewApp(t, nil)
	app.initConsoles()
	app.edBuf = app.current().buf
	return app
}

// Two consoles hold two buffers; switching swaps the editor's buffer and keeps
// each console's text where it was.
func TestConsoleSwitchKeepsTexts(t *testing.T) {
	app := consolesApp(t)
	app.edBuf.Set("SELECT 1;")
	first := app.edBuf

	if !app.newConsole() {
		t.Fatal("a second console did not open")
	}
	if app.edBuf == first {
		t.Fatal("the new console did not swap the editor's buffer")
	}
	app.edBuf.Set("SELECT 2;")
	second := app.edBuf

	// Back to the first tab: its text is intact.
	app.consolesel = 0
	app.selectConsole()
	if app.edBuf != first {
		t.Fatal("switching back did not restore the first buffer")
	}
	if got := app.edBuf.String(); got != "SELECT 1;" {
		t.Fatalf("first console holds %q, want SELECT 1;", got)
	}
	// And forward again.
	app.consolesel = 1
	app.selectConsole()
	if app.edBuf != second || app.edBuf.String() != "SELECT 2;" {
		t.Fatalf("second console holds %q", app.edBuf.String())
	}
}

// The editor state points at the shown tab's buffer, which is what completion
// and hover read.
func TestConsoleSwitchUpdatesState(t *testing.T) {
	app := consolesApp(t)
	app.edState = &editor.State{Buffer: app.edBuf}
	app.edBuf.Set("SELECT 1;")
	app.newConsole()
	if app.edState.Buffer != app.edBuf {
		t.Fatal("the editor state did not follow the swap")
	}
}

func TestConsoleLabelsAndRename(t *testing.T) {
	app := consolesApp(t)
	if labels := app.consoleLabels(); len(labels) != 1 || labels[0] != "Console 1" {
		t.Fatalf("labels = %v", labels)
	}
	app.edBuf.Set("SELECT id FROM employees WHERE id = 1")
	app.renameConsoleFromContent()
	if got := app.consoleLabels()[0]; !strings.Contains(got, "SELECT id FROM employees") {
		t.Fatalf("label = %q, want the statement's first line", got)
	}
	// An empty editor keeps its label.
	app.newConsole()
	app.renameConsoleFromContent()
	if got := app.consoleLabels()[1]; got != "Console 2" {
		t.Fatalf("empty console renamed to %q, want Console 2", got)
	}
	// A long first line is cut.
	app.edBuf.Set(strings.Repeat("x", 100))
	app.renameConsoleFromContent()
	got := app.consoleLabels()[1]
	if len([]rune(got)) > 25 || !strings.HasSuffix(got, "…") {
		t.Fatalf("long label = %q, want it cut with …", got)
	}
}

func TestConsoleClose(t *testing.T) {
	app := consolesApp(t)
	app.edBuf.Set("first")
	app.newConsole()
	app.edBuf.Set("second")
	app.closeConsole(1)
	if len(app.consoles) != 1 {
		t.Fatalf("consoles = %d, want 1", len(app.consoles))
	}
	if app.edBuf.String() != "first" {
		t.Fatalf("after closing, editor holds %q", app.edBuf.String())
	}
	// The last tab cannot be closed.
	app.closeConsole(0)
	if len(app.consoles) != 1 {
		t.Fatalf("the last console was closed: %d left", len(app.consoles))
	}
	// Closing the first of two moves the editor to what was the second.
	app.newConsole()
	app.edBuf.Set("second")
	app.closeConsole(0)
	if len(app.consoles) != 1 || app.edBuf.String() != "second" {
		t.Fatalf("after closing the first: %d consoles, editor %q",
			len(app.consoles), app.edBuf.String())
	}
}

// Labels stay unique: a tab named Console 2, closed, lets a new tab take the
// name again, but two live tabs never read alike.
func TestConsoleLabelsUnique(t *testing.T) {
	app := consolesApp(t)
	app.newConsole() // Console 2
	app.closeConsole(1)
	app.newConsole()
	if got := app.consoleLabels()[1]; got != "Console 2" {
		t.Fatalf("reused label = %q, want Console 2", got)
	}
	// A tab renamed to "Console 2" is respected by the next new tab.
	app.renameTo("Console 2", 1)
	app.newConsole()
	if got := app.consoleLabels()[2]; got != "Console 3" {
		t.Fatalf("label = %q, want Console 3 (skipping the taken name)", got)
	}
}

// Each console runs on its own editor state: undo, the hover cache and the
// completion context live in the state, so sharing one would carry them across
// tabs that look independent.
func TestConsoleHasOwnEditorState(t *testing.T) {
	app := consolesApp(t)
	app.initEditorState()
	first := app.edState
	if first == nil {
		t.Fatal("the first console has no editor state")
	}
	app.newConsole()
	second := app.edState
	if second == nil || second == first {
		t.Fatal("the new console shares the first console's editor state")
	}
	if second.Buffer != app.edBuf {
		t.Fatal("the new console's state does not point at its own buffer")
	}
	// Switching back restores the first console's own state, not a copy.
	app.consolesel = 0
	app.selectConsole()
	if app.edState != first {
		t.Fatal("switching back did not restore the first console's state")
	}
	// The hooks every console needs are wired on the state that is built.
	if app.edState.OnCompletion == nil || app.edState.OnHover == nil || app.edState.OnDefinition == nil {
		t.Fatal("a console's editor state is missing the completion/hover/definition hooks")
	}
}

func TestConsoleCap(t *testing.T) {
	app := consolesApp(t)
	for i := 0; i < consoleMax-1; i++ {
		if !app.newConsole() {
			t.Fatalf("console %d refused", i+2)
		}
	}
	if len(app.consoles) != consoleMax {
		t.Fatalf("consoles = %d, want the cap", len(app.consoles))
	}
	if app.newConsole() {
		t.Fatal("the cap was exceeded")
	}
}

// renameTo sets tab i's label directly, for tests that arrange the tab bar.
func (app *App) renameTo(label string, i int) {
	if i >= 0 && i < len(app.consoles) {
		app.consoles[i].label = label
	}
}

// The editor pane draws the consoles' tab bar with its + and × buttons, and the
// shown console's editor beneath it.
func TestConsolesViewRenders(t *testing.T) {
	app := consolesApp(t)
	app.edState = &editor.State{Buffer: app.edBuf}
	app.edBuf.Set("SELECT 1;")
	tt := ui.NewTester(app.editorPane, 1200, 400)
	// 波 99：单独的「Editor」标签行撤掉了——页签行 + 编辑器本体现在就是全部。
	for _, want := range []string{"Console 1", "+", "×", "1 / 8"} {
		if !tt.HasText(want) {
			t.Errorf("the editor pane does not show %q; texts = %v", want, tt.Texts())
		}
	}
}
