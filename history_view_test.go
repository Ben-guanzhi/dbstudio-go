package main

// A headless render of the real view: the sidebar is a view, so drawing it
// through the framework's Tester is what proves the history panel is actually
// on screen, not just that its state functions are right.

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ben-guanzhi/dbcore"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// newViewApp builds an App ready to draw: a store, the splits main sets, and
// the editor the view needs (the editor pane is always drawn, whether a
// connection is open or not).
func newViewApp(t *testing.T, store *Store) *App {
	t.Helper()
	app := &App{store: store, splitSide: 260, splitEditor: 300, treeSel: -1, gridSel: -1,
		// Every test that draws the app may read these; an empty map is what a
		// connected app has, and a nil one is a trap.
		tables: map[string][]tableInfo{},
		cols:   map[string][]dbcore.ColumnInfo{},
	}
	app.grid.Selected = &app.gridSel
	app.edBuf = editor.NewBuffer("")
	app.edBuf.SetLanguage(editor.LanguageFor("query.sql"))
	app.edState = &editor.State{
		Buffer:       app.edBuf,
		Indent:       editor.IndentSpaces,
		TabSize:      2,
		LineNumbers:  editor.LineNumbersOn,
		Placeholder:  "-- Write SQL here.\n",
		OnCompletion: app.onCompletion,
	}
	// The modals draw editors too, so a headless render needs their buffers.
	app.initModalEditors()
	return app
}

func newSidebarStore(t *testing.T, entries ...HistoryEntry) *Store {
	t.Helper()
	store, err := NewStoreAt(filepath.Join(t.TempDir(), "connections.json"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	for _, e := range entries {
		store.AddHistory(e)
	}
	return store
}

func TestSidebarRendersHistory(t *testing.T) {
	at := time.Now()
	app := newViewApp(t, newSidebarStore(t,
		HistoryEntry{SQL: "SELECT * FROM employees", OK: true, At: at},
		HistoryEntry{SQL: "UPDATE accounts SET bal = 0", OK: true, At: at},
		HistoryEntry{SQL: "DELETE FROM logs", OK: false, At: at},
	))
	// The history is a tool window now: opened from the toolbar.
	app.openHistory()

	tt := ui.NewTester(app.view, 1200, 760)
	frame := tt.Image()
	if frame == nil || frame.Rect.Dx() <= 0 || frame.Rect.Dy() <= 0 {
		t.Fatal("the view drew nothing")
	}

	// The modal is on screen: its header, the navigator behind it, and each
	// statement's first line are all text nodes the screen reader would see.
	for _, want := range []string{"History", "Objects", "SELECT * FROM employees", "DELETE FROM logs"} {
		if !tt.HasText(want) {
			t.Errorf("the view does not show %q; texts = %v", want, tt.Texts())
		}
	}
	// The failed statement is marked, so it can be found again.
	if !markedFailed(tt.Texts()) {
		t.Errorf("the failed statement is not marked; texts = %v", tt.Texts())
	}
}

func TestSidebarRendersEmptyHistory(t *testing.T) {
	app := newViewApp(t, newSidebarStore(t))
	app.openHistory()
	tt := ui.NewTester(app.view, 1200, 760)
	if !tt.HasText("History") {
		t.Fatalf("the empty history panel is not shown; texts = %v", tt.Texts())
	}
	if !tt.HasText("No statements yet") {
		t.Errorf("an empty history says nothing; texts = %v", tt.Texts())
	}
}

func TestSidebarRendersNoStore(t *testing.T) {
	// A store that is not there yet: the panel still draws its header, but it
	// has nothing to list and does not pretend it does.
	app := newViewApp(t, nil)
	app.openHistory()
	tt := ui.NewTester(app.view, 1200, 760)
	if !tt.HasText("History") {
		t.Fatalf("the history panel is missing entirely; texts = %v", tt.Texts())
	}
	if tt.HasText("No statements yet") {
		t.Errorf("a missing store claims it is empty; texts = %v", tt.Texts())
	}
}

func markedFailed(texts []string) bool {
	for _, s := range texts {
		if strings.Contains(s, "DELETE FROM logs") && strings.Contains(s, "!") {
			return true
		}
	}
	return false
}
