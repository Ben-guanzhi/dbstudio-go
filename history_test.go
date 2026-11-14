package main

// Tests for the query history: the store's order, cap and persistence, the
// label the sidebar shows, and loading a statement back into the editor.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui/editor"
)

func TestHistoryLabel(t *testing.T) {
	at := time.Date(2026, 11, 12, 15, 4, 5, 0, time.UTC)
	e := HistoryEntry{SQL: "SELECT * FROM employees", OK: true, At: at}
	if got, want := historyLabel(e), "15:04:05  SELECT * FROM employees"; got != want {
		t.Fatalf("label = %q, want %q", got, want)
	}
	// A multi-line statement shows its first line only.
	e.SQL = "SELECT 1\nFROM t"
	if got, want := historyLabel(e), "15:04:05  SELECT 1"; got != want {
		t.Fatalf("label = %q, want %q", got, want)
	}
	// A failed statement is marked, so it can be found again.
	e.SQL = "SELECT 1"
	e.OK = false
	if got, want := historyLabel(e), "15:04:05  SELECT 1  !"; got != want {
		t.Fatalf("label = %q, want %q", got, want)
	}
	// A statement wider than the sidebar is cut, not wrapped.
	e.OK = true
	e.SQL = strings.Repeat("x", historyWidth+10)
	want := "15:04:05  " + strings.Repeat("x", historyWidth) + "…"
	if got := historyLabel(e); got != want {
		t.Fatalf("label = %q, want %q", got, want)
	}
}

func TestHistoryEntry(t *testing.T) {
	t0 := time.Date(2026, 11, 12, 15, 4, 5, 0, time.UTC)
	e := historyEntry("SELECT 1", nil, t0)
	if !e.OK || e.SQL != "SELECT 1" || !e.At.Equal(t0) || e.Elapsed == "" {
		t.Fatalf("entry = %+v", e)
	}
	e = historyEntry("SELECT 1", errors.New("boom"), t0)
	if e.OK {
		t.Fatal("a statement that failed is recorded as one that worked")
	}
}

func TestHistoryStore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "connections.json")
	s, err := NewStoreAt(path)
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	at := time.Date(2026, 11, 12, 15, 4, 5, 0, time.UTC)
	s.AddHistory(HistoryEntry{SQL: "SELECT 1", OK: true, At: at})
	s.AddHistory(HistoryEntry{SQL: "SELECT 2", OK: false, At: at})

	got := s.ListHistory()
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	// Newest first.
	if got[0].SQL != "SELECT 2" || got[1].SQL != "SELECT 1" {
		t.Fatalf("order = %q, %q; want newest first", got[0].SQL, got[1].SQL)
	}
	if got[0].OK || !got[1].OK {
		t.Fatalf("OK flags = %v, %v; want false, true", got[0].OK, got[1].OK)
	}

	// A store on the same path reads the history back, so it comes back on the
	// next launch.
	s2, _ := NewStoreAt(path)
	s2.loadHistory()
	got2 := s2.ListHistory()
	if len(got2) != 2 || got2[0].SQL != "SELECT 2" || got2[1].SQL != "SELECT 1" {
		t.Fatalf("reloaded = %+v", got2)
	}

	// The history lives in a file of its own, beside the connections file.
	histFile := strings.TrimSuffix(path, filepath.Ext(path)) + ".history.json"
	hist, err := os.ReadFile(histFile)
	if err != nil {
		t.Fatalf("history file: %v", err)
	}
	if !strings.Contains(string(hist), "SELECT 1") {
		t.Fatal("the history file does not hold the statement")
	}
	if raw, err := os.ReadFile(path); err == nil && strings.Contains(string(raw), "SELECT 1") {
		t.Fatal("the history was written into connections.json")
	}

	// Clearing drops it, and the file says so.
	if err := s.ClearHistory(); err != nil {
		t.Fatalf("ClearHistory: %v", err)
	}
	if len(s.ListHistory()) != 0 {
		t.Fatal("history not cleared")
	}
	s3, _ := NewStoreAt(path)
	s3.loadHistory()
	if len(s3.ListHistory()) != 0 {
		t.Fatal("history came back after clearing")
	}
}

func TestHistoryStoreCaps(t *testing.T) {
	s, _ := NewStoreAt(filepath.Join(t.TempDir(), "connections.json"))
	at := time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)
	for i := 0; i < histMax+25; i++ {
		s.AddHistory(HistoryEntry{SQL: fmt.Sprintf("SELECT %d", i), At: at})
	}
	got := s.ListHistory()
	if len(got) != histMax {
		t.Fatalf("len = %d, want %d", len(got), histMax)
	}
	if got[0].SQL != fmt.Sprintf("SELECT %d", histMax+24) {
		t.Fatalf("newest = %q", got[0].SQL)
	}
	if got[histMax-1].SQL != fmt.Sprintf("SELECT %d", 25) {
		t.Fatalf("oldest kept = %q, want SELECT 25", got[histMax-1].SQL)
	}
}

func TestHistoryStoreSkipsBlank(t *testing.T) {
	s, _ := NewStoreAt(filepath.Join(t.TempDir(), "connections.json"))
	at := time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)
	s.AddHistory(HistoryEntry{SQL: "   ", At: at})
	if got := s.ListHistory(); len(got) != 0 {
		t.Fatalf("len = %d, want none for a blank statement", len(got))
	}
}

func TestLoadHistory(t *testing.T) {
	app := &App{edBuf: editor.NewBuffer("")}
	app.loadHistory(HistoryEntry{SQL: "SELECT 42;"})
	if got := app.edBuf.String(); got != "SELECT 42;" {
		t.Fatalf("editor = %q, want the statement", got)
	}
	if app.status != "Loaded from history" {
		t.Fatalf("status = %q", app.status)
	}
}

func TestClearHistoryState(t *testing.T) {
	s, _ := NewStoreAt(filepath.Join(t.TempDir(), "connections.json"))
	s.AddHistory(HistoryEntry{SQL: "SELECT 1", At: time.Now()})
	app := &App{store: s, histID: "0"}
	app.clearHistory()
	if len(s.ListHistory()) != 0 {
		t.Fatal("history not cleared")
	}
	if app.histID != "" {
		t.Fatalf("histID = %q, want none", app.histID)
	}
	if app.errMsg != "" {
		t.Fatalf("errMsg = %q, want none", app.errMsg)
	}
}
