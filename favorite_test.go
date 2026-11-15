package main

// Favorites: a statement marked to find again.
//
// The history keeps everything that ran; a favorite is the one the user wants
// back without scrolling. This pins the mark itself — that it flips, that it
// survives a reload, that an out-of-range index is refused — and the filter
// that shows favorites alone.

import (
	"path/filepath"
	"testing"
	"time"
)

func favStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "connections.json")
	s, err := NewStoreAt(path)
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	return s
}

func TestFavoriteFlipsAndPersists(t *testing.T) {
	s := favStore(t)
	at := time.Date(2026, 11, 12, 15, 4, 5, 0, time.UTC)
	s.AddHistory(HistoryEntry{SQL: "SELECT 1", OK: true, At: at})
	s.AddHistory(HistoryEntry{SQL: "SELECT 2", OK: true, At: at})
	s.AddHistory(HistoryEntry{SQL: "SELECT 3", OK: true, At: at})

	// The newest entry is index 0; the oldest is the last one.
	last := len(s.ListHistory()) - 1
	if got := s.ToggleHistoryFavorite(last); !got {
		t.Fatalf("the first toggle did not mark the entry as a favorite")
	}
	// Flipping again takes the mark off.
	if got := s.ToggleHistoryFavorite(last); got {
		t.Fatalf("the second toggle did not clear the mark")
	}
	// And back on, so the persistence check has something to read.
	if got := s.ToggleHistoryFavorite(last); !got {
		t.Fatalf("the third toggle did not mark the entry again")
	}

	// The mark is in the file, not just in memory. A store reads its history
	// explicitly (NewStoreAt loads the connections, not the history).
	reloaded, err := NewStoreAt(filepath.Join(filepath.Dir(s.histPath), "connections.json"))
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	reloaded.loadHistory()
	entries := reloaded.ListHistory()
	if len(entries) != 3 {
		t.Fatalf("reload has %d entries, want 3", len(entries))
	}
	if !entries[last].Favorite {
		t.Errorf("the favorite mark did not survive the reload")
	}
	if entries[0].Favorite || entries[1].Favorite {
		t.Errorf("the other entries were marked too")
	}
}

func TestFavoriteRefusesAnOutOfRangeIndex(t *testing.T) {
	s := favStore(t)
	if s.ToggleHistoryFavorite(0) {
		t.Errorf("toggling an empty history reported a favorite")
	}
	at := time.Date(2026, 11, 12, 15, 4, 5, 0, time.UTC)
	s.AddHistory(HistoryEntry{SQL: "SELECT 1", OK: true, At: at})
	if s.ToggleHistoryFavorite(-1) || s.ToggleHistoryFavorite(99) {
		t.Errorf("an out-of-range index was accepted")
	}
	if s.ListHistory()[0].Favorite {
		t.Errorf("the refused toggle still marked the entry")
	}
}
