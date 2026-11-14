package main

// Regression tests for the redraw path.
//
// These exist because of a real crash: `invalidate` had been rewritten by a bulk
// edit to call *itself*, so the first click with a window up overflowed the
// stack ("runtime: goroutine stack exceeds 1000000000-byte limit"). No headless
// test caught it, because a test App has no window and the guarded branch never
// ran — hence the `redraw` seam these tests pull on: it makes the window branch
// observable without a window.

import (
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// withRedraw replaces the redraw hook for the duration of a test, counting calls.
func withRedraw(t *testing.T) *int {
	t.Helper()
	n := 0
	old := redraw
	redraw = func(w *mygo.Window) { n++ }
	t.Cleanup(func() { redraw = old })
	return &n
}

// invalidate reaches the window exactly once per call — not zero times, and not
// recursively. The bug this guards against is a helper that calls itself.
func TestInvalidateReachesWindowOnce(t *testing.T) {
	n := withRedraw(t)
	app := &App{win: &mygo.Window{}}
	app.invalidate()
	if *n != 1 {
		t.Fatalf("invalidate redrew %d time(s), want exactly 1", *n)
	}
	app.invalidate()
	if *n != 2 {
		t.Fatalf("two invalidates redrew %d time(s), want 2", *n)
	}
}

// Without a window there is nothing to redraw, and nothing may panic.
func TestInvalidateWithoutWindowIsNoop(t *testing.T) {
	n := withRedraw(t)
	app := &App{}
	app.invalidate()
	if *n != 0 {
		t.Fatalf("invalidate without a window redrew %d time(s)", *n)
	}
}

// post is the one handoff to the main thread: inline without a window, through
// Update with one. Its body is the only place that calls app.win.Update.
func TestPostWithoutWindowRunsInline(t *testing.T) {
	app := &App{}
	ran := false
	app.post(func() { ran = true })
	if !ran {
		t.Fatal("post did not run fn inline without a window")
	}
}

// Opening the connection form and drawing a frame is the path that crashed:
// the form must draw, and its validation must not loop.
func TestOpenFormDraws(t *testing.T) {
	app := &App{pager: newResultPager(), gridSel: -1}
	app.initConsoles()
	app.initModalEditors()
	app.edBuf = app.current().buf
	app.edState = &editor.State{Buffer: app.edBuf}
	app.openForm()
	if !app.modal {
		t.Fatal("openForm did not open the modal")
	}
	tt := ui.NewTester(app.view, 1200, 800)
	for _, want := range []string{"Connection", "Name", "Kind", "Save", "Cancel"} {
		if !tt.HasText(want) {
			t.Errorf("the connection form does not show %q; texts = %v", want, tt.Texts())
		}
	}
}

// The form's Save refuses an empty name instead of storing a nameless
// connection, and drawing that error must not loop either.
func TestSaveFormRequiresName(t *testing.T) {
	app := &App{pager: newResultPager(), gridSel: -1}
	app.initModalEditors()
	app.edState = &editor.State{Buffer: editor.NewBuffer("")}
	app.openForm()
	app.form.Name = ""
	app.saveForm()
	if app.formErr == "" {
		t.Fatal("saveForm accepted a connection with no name")
	}
	if !app.modal {
		t.Fatal("a rejected save closed the form")
	}
	_ = ui.NewTester(app.view, 800, 600)
}
