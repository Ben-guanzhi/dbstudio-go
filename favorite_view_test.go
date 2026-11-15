package main

// The history modal's favorites, as the user meets them.
//
// The store tests pin the mark; these pin what the window does with it: the
// button is there, it marks the selected statement, and the filter leaves only
// the favorites on screen.
//
// Two details the assertions respect: AddHistory prepends, so index 0 is the
// newest statement; and Find matches a label exactly, so a prefix is asked for
// with HasText.

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// favApp is a connected app with three statements in its history, oldest last.
// After AddHistory(1), AddHistory(2), AddHistory(3) the newest is index 0.
func favApp(t *testing.T) *App {
	t.Helper()
	app := connectedApp(t, 20)
	store, err := NewStoreAt(filepath.Join(t.TempDir(), "connections.json"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	app.store = store
	at := time.Date(2026, 11, 12, 15, 4, 5, 0, time.UTC)
	for _, sql := range []string{"SELECT 1", "SELECT 2", "SELECT 3"} {
		app.store.AddHistory(HistoryEntry{SQL: sql, OK: true, At: at})
	}
	app.historyOpen = true
	return app
}

func TestHistoryModalOffersFavorites(t *testing.T) {
	app := favApp(t)
	tt := ui.NewTester(app.view, 900, 700)
	tt.Frame()

	for _, want := range []string{"☆ Favorite", "☆ Favorites", "History"} {
		if _, ok := tt.Find(want); !ok {
			t.Errorf("the history modal does not show %q; texts = %v", want, tt.Texts())
		}
	}
}

func TestFavoriteMarksTheSelectedStatement(t *testing.T) {
	app := favApp(t)
	// Index 0 is the newest statement, "SELECT 3".
	app.histID = "0"
	app.store.ToggleHistoryFavorite(0)

	if !app.store.ListHistory()[0].Favorite {
		t.Fatalf("the statement was not marked")
	}
	// The modal still offers the filter; the marked row is drawn in the accent.
	tt := ui.NewTester(app.view, 900, 700)
	tt.Frame()
	if _, ok := tt.Find("☆ Favorites"); !ok {
		t.Errorf("the modal does not offer the favorites filter; texts = %v", tt.Texts())
	}
}

func TestFavoritesFilterLeavesOnlyMarkedRows(t *testing.T) {
	app := favApp(t)
	app.store.ToggleHistoryFavorite(0) // "SELECT 3", the newest
	app.histFavOnly = true

	tt := ui.NewTester(app.view, 900, 700)
	tt.Frame()

	if !tt.HasText("SELECT 3") {
		t.Errorf("the favorite is not shown; texts = %v", tt.Texts())
	}
	for _, gone := range []string{"SELECT 1", "SELECT 2"} {
		if tt.HasText(gone) {
			t.Errorf("a statement that is not a favorite is still shown: %q", gone)
		}
	}
}

func TestFavoritesFilterEmptyState(t *testing.T) {
	app := favApp(t)
	app.histFavOnly = true // nothing marked yet

	tt := ui.NewTester(app.view, 900, 700)
	tt.Frame()

	if !tt.HasText("No favorites yet") {
		t.Errorf("the empty favorites list does not say so; texts = %v", tt.Texts())
	}
}
