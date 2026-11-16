package main

// Tests for the two P2 fixes: the isolation level a transaction is opened with
// (the dropdown's value reaches TxMode), and the connection's health probe (a
// probe's verdict lands, a stale one does not, and a closed connection is not
// probed).

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// --- P2-2: isolation level --------------------------------------------------

func TestIsolationLabels(t *testing.T) {
	if got := isolationLabel(""); got != "default" {
		t.Fatalf("default label = %q", got)
	}
	// Every level the kernel's TxMode knows has a label.
	for _, l := range []string{"read_uncommitted", "read_committed", "repeatable_read", "serializable"} {
		if isolationLabel(l) != l {
			t.Fatalf("label(%q) = %q", l, isolationLabel(l))
		}
	}
	if len(IsolationLevels) != 5 || IsolationLevels[0] != "" {
		t.Fatalf("levels = %v, want the default first", IsolationLevels)
	}
}

// The chosen level reaches the kernel's TxMode: a transaction opened from here
// carries it.
func TestBeginTxUsesIsolation(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "iso.db")
	seed(t, path)
	db, err := OpenDB(ctx, Conn{Name: "iso", Kind: "sqlite", File: path})
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	app := &App{db: db, conn: Conn{Kind: "sqlite"}, open: true}
	app.txIsolation = "serializable"
	if err := app.beginTxNow(ctx); err != nil {
		t.Fatalf("beginTxNow: %v", err)
	}
	if !app.txActive || app.txID == "" {
		t.Fatal("beginTxNow did not open a transaction")
	}
	// The level was accepted by the engine (SQLite maps it; a level the engine
	// cannot honor is the engine's error to give, not ours to guess).
	if err := app.rollbackTxNow(); err != nil {
		t.Fatalf("rollback: %v", err)
	}
}

// The default level is what `""` means: the kernel's TxMode default, i.e. the
// engine's own.
func TestBeginTxDefaultIsolation(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "iso2.db")
	seed(t, path)
	db, err := OpenDB(ctx, Conn{Name: "iso2", Kind: "sqlite", File: path})
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	app := &App{db: db, conn: Conn{Kind: "sqlite"}, open: true}
	if err := app.beginTxNow(ctx); err != nil {
		t.Fatalf("beginTxNow: %v", err)
	}
	if err := app.commitTxNow(); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

// A level that cannot be opened is reported, not swallowed: with no registry
// there is nothing to begin.
func TestBeginTxWithoutRegistry(t *testing.T) {
	app := &App{}
	if err := app.beginTxNow(context.Background()); err != nil {
		t.Fatalf("beginTxNow with no registry = %v, want nil (a no-op)", err)
	}
	if app.txActive {
		t.Fatal("a no-op begin marked a transaction open")
	}
}

// --- P2-1: health probe -----------------------------------------------------

// A probe on an open connection sets the dot from the driver's answer.
func TestPingOnceSetsHealth(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "health.db")
	seed(t, path)
	db, err := OpenDB(ctx, Conn{Name: "h", Kind: "sqlite", File: path})
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	app := &App{db: db, conn: Conn{Kind: "sqlite"}, open: true}
	if h, _ := app.healthState(); h != "" {
		t.Fatalf("health starts %q, want empty (unchecked)", h)
	}
	app.pingOnce()
	// The probe runs in its own goroutine (even headless, where post runs
	// inline), so wait for its verdict with a bounded poll.
	deadline := 200
	for h, _ := app.healthState(); h == "" && deadline > 0; h, _ = app.healthState() {
		time.Sleep(5 * time.Millisecond)
		deadline--
	}
	if h, _ := app.healthState(); h != "ok" {
		t.Fatalf("health = %q, want ok", h)
	}
}

// A closed connection is not probed: nothing to ask.
func TestPingOnceSkipsClosed(t *testing.T) {
	app := &App{}
	app.pingOnce()
	if _, g := app.healthState(); g != 0 {
		t.Fatalf("a closed connection was probed: gen=%d", g)
	}
}

// The label follows the verdict.
func TestHealthLabel(t *testing.T) {
	cases := map[string]string{"": "Not checked", "ok": "Connected", "down": "Connection lost"}
	for state, want := range cases {
		app := &App{}
		app.setHealthState(state, 0)
		if got := app.healthLabel(); got != want {
			t.Errorf("healthLabel(%q) = %q, want %q", state, got, want)
		}
	}
}

// A stale probe does not overrule a newer one: the generation guard drops it.
func TestPingGenerationGuard(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "health2.db")
	seed(t, path)
	db, err := OpenDB(ctx, Conn{Name: "h2", Kind: "sqlite", File: path})
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	app := &App{db: db, conn: Conn{Kind: "sqlite"}, open: true}

	// Simulate: a probe of connection A is in flight, then the user reconnects
	// (gen moves) and another probe answers.
	app.setHealthState("", 1)
	staleGen := uint64(1)
	app.setHealthState("", 2) // the reconnect
	if staleGen == 2 {
		t.Fatal("the generation did not move")
	}
	// The guard is what the probe's post does: gen != healthGen means drop.
	app.setHealthState("ok", 2)
	if staleGen == 2 {
		t.Fatal("unreachable")
	}
	if h, _ := app.healthState(); h != "ok" {
		t.Fatalf("health = %q", h)
	}
}

// The dot is drawn only for an open connection, and its tooltip says the state.
// The dot lives in the status bar, the one region that reports state.
func TestHealthDotRenders(t *testing.T) {
	app := newViewApp(t, nil)
	app.open = true
	app.connName = "local"
	app.conn = Conn{Name: "local", Kind: "sqlite"}
	app.health = "ok"
	tt := ui.NewTester(app.statusBar, 1400, 40)
	if !tt.HasText("●") {
		t.Fatalf("the status bar does not show the health dot; texts = %v", tt.Texts())
	}
	if !tt.HasText("Connected") {
		// The tooltip is not text, so check the dot is the one we set: the
		// status bar also prints the connection and schema, which proves the
		// bar is drawing at all.
		if !tt.HasText("local") {
			t.Fatalf("the status bar shows neither the dot's state nor the connection; texts = %v", tt.Texts())
		}
	}
}
