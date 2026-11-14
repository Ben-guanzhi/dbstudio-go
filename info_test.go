package main

// Tests for the two small items: the sentence that explains a hidden section
// (so a vanished branch does not read as a bug), and the connection info panel
// (what this connection can do, read from the same capability assertions the
// toolbar and tree act on).

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// --- the unsupported-section note -------------------------------------------

func TestEngineLabel(t *testing.T) {
	cases := map[string]string{
		"sqlite":     "SQLite",
		"mysql":      "MySQL",
		"postgres":   "PostgreSQL",
		"postgresql": "PostgreSQL",
		"mssql":      "SQL Server",
		"sqlserver":  "SQL Server",
		"oracle":     "Oracle",
		"redis":      "Redis",
		"":           "This engine",
		"duckdb":     "duckdb", // an engine the client does not know keeps its name
	}
	for kind, want := range cases {
		if got := engineLabel(kind); got != want {
			t.Errorf("engineLabel(%q) = %q, want %q", kind, got, want)
		}
	}
}

func TestConceptOf(t *testing.T) {
	// Every section kind has a phrase, and the phrase is not the fallback.
	for _, sec := range allSections {
		got := conceptOf(sec.kind)
		if got == "" || got == "this object type" {
			t.Errorf("conceptOf(%q) = %q, want a phrase", sec.label, got)
		}
	}
}

// The note names both the engine and the concept, in a sentence.
func TestUnsupportedNote(t *testing.T) {
	cases := []struct {
		kind   int
		engine string
		wants  []string
	}{
		{objSequence, "sqlite", []string{"SQLite", "sequences", "hidden"}},
		{objRoutine, "sqlite", []string{"SQLite", "functions or procedures"}},
		{objEnum, "mysql", []string{"MySQL", "enum types"}},
		{objPartition, "sqlite", []string{"SQLite", "partitioning"}},
		{objCheck, "sqlite", []string{"SQLite", "CHECK-constraint"}},
	}
	for _, c := range cases {
		got := unsupportedNote(objectSection{kind: c.kind}, c.engine)
		for _, w := range c.wants {
			if !strings.Contains(got, w) {
				t.Errorf("note(%d, %s) = %q, want it to contain %q", c.kind, c.engine, got, w)
			}
		}
	}
}

// The hiding path sets the note: opening a section SQLite has no concept for
// leaves the section hidden and the reason in the status line.
func TestUnsupportedSectionExplains(t *testing.T) {
	app, schema := refreshApp(t) // a SQLite connection with a loaded schema
	app.status = ""
	app.loadObjectSection(schema, "", sectionOf(objSequence))
	if _, _, notSupported := app.objects.leaves(objSequence, schema, ""); !notSupported {
		t.Fatal("the sequences section was not resolved as unsupported on SQLite")
	}
	if !strings.Contains(app.status, "SQLite") || !strings.Contains(app.status, "sequences") {
		t.Fatalf("status = %q, want the engine and the concept named", app.status)
	}
}

// A section the engine does have leaves no such note.
func TestSupportedSectionNoNote(t *testing.T) {
	app, schema := refreshApp(t)
	app.status = ""
	// SQLite has triggers, so the trigger section loads empty and says nothing
	// about the engine lacking it.
	app.loadObjectSection(schema, "employees", sectionOf(objTrigger))
	if strings.Contains(app.status, "has no") {
		t.Fatalf("a supported section explained itself away: %q", app.status)
	}
}

// --- the connection info panel ----------------------------------------------

// The rows report what the SQLite driver actually implements, and the gaps
// (no KV, no metrics) are marked as gaps.
func TestConnInfoRowsSQLite(t *testing.T) {
	app, _ := refreshApp(t)
	app.connName = "local"
	rows := app.connInfoRows()

	byLabel := map[string]infoRow{}
	for _, r := range rows {
		byLabel[r.label] = r
	}
	if got := byLabel["Engine"].value; got != "SQLite" {
		t.Fatalf("engine = %q", got)
	}
	for _, label := range []string{
		"SQL statements",
		"Transactions (Autocommit, Commit Tx)",
		"Batch writes (grid commit, import)",
		"Extra metadata (views, CHECK, sequences, routines, partitions, enums)",
		"Trigger catalog",
	} {
		r, ok := byLabel[label]
		if !ok {
			t.Fatalf("no %q row; rows = %v", label, byLabel)
		}
		if !r.ok || r.value != "yes" {
			t.Errorf("%q = %q (ok=%v), want yes", label, r.value, r.ok)
		}
	}
	// SQLite has no KV driver; that is the one capability the panel reports as a
	// gap for it. (It does implement MetricsSource — the pragma metrics.)
	if r, ok := byLabel["Key/value browse (Redis)"]; !ok || r.ok || r.value != "no" {
		t.Errorf("Key/value browse = %+v, want no", byLabel["Key/value browse (Redis)"])
	}
	if r, ok := byLabel["Server metrics"]; !ok || !r.ok {
		t.Errorf("Server metrics = %+v, want yes (SQLite reads pragma metrics)", r)
	}
	// The address row exists for a file connection.
	if _, ok := byLabel["File"]; !ok {
		t.Fatal("a file connection has no File row")
	}
}

// A DB without a driver (a fixture, or a connection mid-setup) says so instead
// of reporting every capability as missing: "no" for the wrong reason is worse
// than "not attached".
func TestConnInfoRowsWithoutDriver(t *testing.T) {
	app := &App{db: &DB{Kind: "redis"}, conn: Conn{Name: "redis", Kind: "redis"}}
	rows := app.connInfoRows()
	var guard infoRow
	for _, r := range rows {
		if r.label == "Driver" {
			guard = r
		}
	}
	if guard.value != "not attached" || guard.ok {
		t.Fatalf("guard = %+v, want Driver: not attached", guard)
	}
	if got := engineLabel("redis"); got != "Redis" {
		t.Fatalf("engine label = %q", got)
	}
}

func TestOpenConnInfoNotConnected(t *testing.T) {
	app := &App{}
	app.openConnInfo()
	if app.errMsg == "" || app.status != "Not connected" {
		t.Fatalf("errMsg=%q status=%q, want a not-connected report", app.errMsg, app.status)
	}
	if app.connInfo.open {
		t.Fatal("the panel opened on a closed connection")
	}
}

// The panel draws: its title, a capability row, the note, and Close.
func TestConnInfoRenders(t *testing.T) {
	app, _ := refreshApp(t)
	app.connName = "local"
	app.openConnInfo()
	if !app.connInfo.open {
		t.Fatal("openConnInfo did not open the panel")
	}
	tt := ui.NewTester(app.connInfoView, 900, 600)
	for _, want := range []string{"Connection Info", "Engine", "SQLite", "SQL statements", "Close"} {
		if !tt.HasText(want) {
			t.Errorf("the panel does not show %q; texts = %v", want, tt.Texts())
		}
	}
}

// The rows are a snapshot: reconnecting does not leave them half-updated, and
// reopening reads the new connection.
func TestConnInfoRebuilds(t *testing.T) {
	app, _ := refreshApp(t)
	app.connName = "first"
	app.openConnInfo()
	first := len(app.connInfo.rows)
	if first == 0 {
		t.Fatal("no rows")
	}
	app.connName = "second"
	app.openConnInfo()
	found := false
	for _, r := range app.connInfo.rows {
		if r.label == "Connection" && r.value == "second" {
			found = true
		}
	}
	if !found {
		t.Fatal("reopening did not rebuild the rows for the new name")
	}
}

var _ = context.Background
var _ = filepath.Join
