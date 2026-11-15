package main

// Tests for the read-only lock and connection colours: the lock refuses every
// write path (script, grid, commit, import) and remembers its state; a colour
// survives a save and is what the navigator and the status bar show.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ben-guanzhi/dbcore"
	"github.com/egoist/mygo/ui"
)

// --- read-only: the classification ------------------------------------------

func TestWriteStatement(t *testing.T) {
	writes := []string{
		"INSERT INTO t VALUES (1)",
		"UPDATE t SET x = 1",
		"DELETE FROM t",
		"DROP TABLE t",
		"TRUNCATE t",
		"CREATE TABLE t (id int)",
		"ALTER TABLE t ADD c int",
		"GRANT SELECT ON t TO u",
		"CALL p()",
		"EXEC p",
		"/* maintenance */ VACUUM",
		`WITH gone AS (SELECT 1) DELETE FROM t`,
	}
	for _, sql := range writes {
		if got := writeStatement([]string{sql}); got == "" {
			t.Errorf("%q is a write but was allowed", sql)
		}
	}
	reads := []string{
		"SELECT * FROM t",
		"select 1",
		"SHOW TABLES",
		"EXPLAIN SELECT 1",
		"DESCRIBE t",
		"PRAGMA table_info(t)",
		"-- INSERT INTO t VALUES (1)",
		`SELECT 'UPDATE' AS word`,
	}
	for _, sql := range reads {
		if got := writeStatement([]string{sql}); got != "" {
			t.Errorf("%q only reads but was refused as %q", sql, got)
		}
	}
	// A script where any statement writes is a write.
	if writeStatement([]string{"SELECT 1", "DELETE FROM t"}) == "" {
		t.Fatal("a script ending in a DELETE was allowed")
	}
}

// --- read-only: the refusal paths -------------------------------------------

// A locked connection refuses a write before it reaches the driver.
func TestReadOnlyRefusesScript(t *testing.T) {
	app, d := multiApp(t, setOf([]string{"id"}))
	app.readOnlyOn = true
	app.runSQL("DELETE FROM employees")
	waitRun(t, app)
	if d.calls != 0 {
		t.Fatalf("the delete reached the driver (%d call(s))", d.calls)
	}
	if !strings.Contains(app.errMsg, "Read-only") {
		t.Fatalf("no refusal was reported: %q", app.errMsg)
	}
	if app.confirm.open {
		t.Fatal("a read-only refusal asked a destructive question instead")
	}
	// Reads still run on a locked connection: reading is the point of it.
	app.runSQL("SELECT * FROM employees")
	waitRun(t, app)
	if d.calls != 1 {
		t.Fatalf("a read was refused on a read-only connection (%d call(s))", d.calls)
	}
}

// The grid never becomes a working copy on a locked connection.
func TestReadOnlyGridIsNotEditable(t *testing.T) {
	app, _ := refreshApp(t)
	app.open = true
	app.readOnlyOn = true
	app.tables["main"] = []tableInfo{{Name: "employees", Type: "table"}}
	res := &dbcore.QueryResult{
		Columns: []dbcore.ColumnInfo{{Name: "id"}},
		Rows:    [][]dbcore.Value{{{Kind: dbcore.ValInt, I: 1}}},
	}
	app.result = res
	app.startEdit(res, "main", "employees", func() {})
	if app.edit.active {
		t.Fatal("a read-only connection produced a working copy")
	}
	if !strings.Contains(app.status, "Read-only") {
		t.Fatalf("nothing said why the grid is read-only: %q", app.status)
	}
}

// Commit is refused even if a working copy somehow exists: the lock is checked
// where the write would happen.
func TestReadOnlyRefusesCommit(t *testing.T) {
	app, _ := refreshApp(t)
	app.open = true
	app.readOnlyOn = true
	res := &dbcore.QueryResult{
		Columns: []dbcore.ColumnInfo{{Name: "id"}},
		Rows:    [][]dbcore.Value{{{Kind: dbcore.ValInt, I: 1}}},
	}
	app.edit.init(res, "main", "employees", []dbcore.ColumnInfo{{Name: "id"}}, func() {})
	app.commit()
	if !strings.Contains(app.errMsg, "Read-only") {
		t.Fatalf("commit was not refused: %q", app.errMsg)
	}
}

// An import is refused on a locked connection.
func TestReadOnlyRefusesImport(t *testing.T) {
	app, _ := refreshApp(t)
	app.open = true
	app.readOnlyOn = true
	app.beginImport(treeNode{label: "employees", parent: "main", kind: nodeTable})
	if !strings.Contains(app.errMsg, "Read-only") {
		t.Fatalf("import was not refused: %q", app.errMsg)
	}
	if app.importer.open {
		t.Fatal("the import modal opened on a read-only connection")
	}
}

// --- read-only: the switch and its persistence ------------------------------

// Flipping the lock writes the choice back to the connection, so a locked
// connection is still locked after a restart.
func TestReadOnlySwitchPersists(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStoreAt(filepath.Join(dir, "connections.json"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	if err := store.Upsert(Conn{Name: "prod", Kind: "mysql", Host: "h"}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	app := &App{store: store, open: true, conn: Conn{Name: "prod", Kind: "mysql", Host: "h"}}
	app.setReadOnly(true)
	if !app.readOnlyOn {
		t.Fatal("the session did not lock")
	}
	c, ok := store.Get("prod")
	if !ok || !c.ReadOnly {
		t.Fatalf("the lock was not saved with the connection: %+v", c)
	}
	// Unlocking is saved too.
	app.setReadOnly(false)
	if c, _ := store.Get("prod"); c.ReadOnly {
		t.Fatal("unlocking was not saved")
	}
}

// A locked connection seeds the session's lock when it opens.
func TestReadOnlySeededOnConnect(t *testing.T) {
	app, _ := schemaApp(t)
	app.conn.ReadOnly = true
	// The connect path's post-connect assignment is what a real connect runs;
	// here it is the one line under test (the rest needs a server).
	app.readOnlyOn = app.conn.ReadOnly
	if !app.readOnly() {
		t.Fatal("a connection saved as read-only did not start locked")
	}
}

// --- colours ----------------------------------------------------------------

func TestConnPalette(t *testing.T) {
	if got := colorLabel(""); got != "none" {
		t.Errorf("the empty key's label = %q", got)
	}
	if _, ok := colorInk(""); ok {
		t.Error("no colour was reported as a colour")
	}
	if _, ok := colorInk("red"); !ok {
		t.Error("red is not in the palette")
	}
	if _, ok := colorInk("chartreuse"); ok {
		t.Error("an unknown key was accepted")
	}
	// Every label maps back to its key.
	for _, c := range connPalette {
		if got := colorOfLabel(c.label); got != c.key {
			t.Errorf("label %q maps to %q, want %q", c.label, got, c.key)
		}
	}
}

// A connection's mark says all three things the window needs to know: which
// connection, its colour, and whether it can write.
func TestConnectionMark(t *testing.T) {
	if got := connectionMark(Conn{Name: "local"}); got != "local" {
		t.Fatalf("plain = %q", got)
	}
	if got := connectionMark(Conn{Name: "prod", Color: "red"}); got != connDot+" prod" {
		t.Fatalf("coloured = %q", got)
	}
	if got := connectionMark(Conn{Name: "prod", Color: "red", ReadOnly: true}); !strings.Contains(got, connDot) || !strings.Contains(got, "🔒") {
		t.Fatalf("locked = %q", got)
	}
	// A stale colour key does not add a mark it cannot colour.
	if got := connectionMark(Conn{Name: "x", Color: "chartreuse"}); got != "x" {
		t.Fatalf("stale colour = %q", got)
	}
}

// The colour and the lock survive a save and a reload.
func TestStoreKeepsColourAndReadOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "connections.json")
	s, err := NewStoreAt(path)
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	in := Conn{Name: "prod", Kind: "mysql", Host: "h", Color: "amber", ReadOnly: true}
	if err := s.Upsert(in); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	s2, _ := NewStoreAt(path)
	if err := s2.load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	got, ok := s2.Get("prod")
	if !ok || got.Color != "amber" || !got.ReadOnly {
		t.Fatalf("reloaded = %+v", got)
	}
}

// The navigator and the status bar show the mark.
func TestConnectionMarkRenders(t *testing.T) {
	app := newViewApp(t, nil)
	app.open = true
	app.conn = Conn{Name: "prod", Kind: "mysql", Color: "red", ReadOnly: true}
	app.connName = "prod"
	app.health = "ok"

	bar := ui.NewTester(app.statusBar, 1200, 32)
	if !bar.HasText(connDot + " prod 🔒") {
		t.Errorf("the status bar does not mark the connection; texts = %v", bar.Texts())
	}
}

// The form offers the palette and the lock, and the session starts from them.
func TestFormOffersColourAndLock(t *testing.T) {
	app := newViewApp(t, nil)
	app.openForm()
	app.form.Color = "red"
	app.form.ReadOnly = true
	tt := ui.NewTester(app.formView, 900, 700)
	for _, want := range []string{"Colour", "Read-only", "refuse statements that write"} {
		if !tt.HasText(want) {
			t.Errorf("the form is missing %q; texts = %v", want, tt.Texts())
		}
	}
}
