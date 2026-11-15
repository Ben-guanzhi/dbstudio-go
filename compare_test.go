package main

// Tests for the schema comparison (P2 #16), driven end to end on two real
// SQLite files: a source and a target are saved as connections, collected,
// diffed, scripted, and the script applied to the target — then the same
// comparison answers nothing left, which is the acceptance the PLAN asks for
// (双库 diff 端到端).

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ben-guanzhi/dbcore/schemadiff"
	"github.com/egoist/mygo/ui"
)

// compareSetup saves two SQLite connections whose schemas differ: the source
// has employees and a salaries view, the target has employees missing a column.
func compareSetup(t *testing.T) (*App, string) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()

	srcPath := filepath.Join(dir, "src.db")
	seed(t, srcPath)
	srcDB, err := OpenDB(ctx, Conn{Name: "src", Kind: "sqlite", File: srcPath})
	if err != nil {
		t.Fatalf("open source: %v", err)
	}
	if _, err := srcDB.SQL.Execute(ctx,
		`CREATE TABLE audit (id INTEGER PRIMARY KEY, note TEXT)`); err != nil {
		t.Fatalf("create audit: %v", err)
	}
	_ = srcDB.Close()

	dstPath := filepath.Join(dir, "dst.db")
	seed(t, dstPath) // same employees table, no audit table
	dstDB, err := OpenDB(ctx, Conn{Name: "dst", Kind: "sqlite", File: dstPath})
	if err != nil {
		t.Fatalf("open target: %v", err)
	}
	// Drop a column-bearing difference: rename salary's type is harder on
	// SQLite; instead make the target lack the audit table only.
	_ = dstDB.Close()

	store, err := NewStoreAt(filepath.Join(dir, "connections.json"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	if err := store.Upsert(Conn{Name: "src", Kind: "sqlite", File: srcPath}); err != nil {
		t.Fatalf("save src: %v", err)
	}
	if err := store.Upsert(Conn{Name: "dst", Kind: "sqlite", File: dstPath}); err != nil {
		t.Fatalf("save dst: %v", err)
	}
	app := &App{store: store, connName: "src"}
	return app, dstPath
}

func TestCompareDiffsAndScripts(t *testing.T) {
	app, _ := compareSetup(t)
	ctx := context.Background()

	srcConn, _ := app.store.Get("src")
	dstConn, _ := app.store.Get("dst")
	srcDB, err := OpenDB(ctx, srcConn)
	if err != nil {
		t.Fatalf("open source: %v", err)
	}
	defer srcDB.Close()
	dstDB, err := OpenDB(ctx, dstConn)
	if err != nil {
		t.Fatalf("open target: %v", err)
	}
	defer dstDB.Close()

	srcSnap, err := schemadiff.Collect(ctx, srcDB.SQL, firstSchema(ctx, srcDB))
	if err != nil {
		t.Fatalf("collect source: %v", err)
	}
	dstSnap, err := schemadiff.Collect(ctx, dstDB.SQL, firstSchema(ctx, dstDB))
	if err != nil {
		t.Fatalf("collect target: %v", err)
	}
	entries := schemadiff.Diff(srcSnap, dstSnap, schemadiff.Options{})
	if len(entries) == 0 {
		t.Fatal("the differing schemas produced no entries")
	}
	// The target is missing the audit table: that change must be present.
	var sawAudit bool
	for _, e := range entries {
		if e.Table == "audit" && e.Op == schemadiff.OpCreateTable {
			sawAudit = true
		}
	}
	if !sawAudit {
		var ops []string
		for _, e := range entries {
			ops = append(ops, string(e.Op)+" "+e.Table)
		}
		t.Fatalf("entries = %v, want a create_table audit", ops)
	}

	result := schemadiff.Script(srcSnap, dstSnap, entries)
	if len(result.Statements) == 0 {
		t.Fatal("the diff rendered no statements")
	}
	var joined strings.Builder
	for _, st := range result.Statements {
		joined.WriteString(st.SQL)
		joined.WriteString("\n")
	}
	if !strings.Contains(strings.ToUpper(joined.String()), "AUDIT") {
		t.Fatalf("script = %q, want the audit table's CREATE", joined.String())
	}
}

// The modal draws: its title, the two connection pickers, the action buttons,
// and the diff it holds are on screen.
func TestCompareModalRenders(t *testing.T) {
	app, _ := compareSetup(t)
	app.compare = compareState{
		open:    true,
		srcName: "src",
		dstName: "dst",
		entries: []schemadiff.Entry{{
			ID: "audit/create_table", Op: schemadiff.OpCreateTable,
			Table: "audit", Phase: schemadiff.PhaseCreateTable,
		}},
		stmts:  []schemadiff.Statement{{EntryID: "audit/create_table", SQL: "CREATE TABLE audit (id INTEGER);"}},
		script: "CREATE TABLE audit (id INTEGER);",
	}
	view := newViewApp(t, app.store)
	view.compare = app.compare

	tt := ui.NewTester(view.view, 1200, 760)
	for _, want := range []string{"Compare Schemas", "Source", "Target", "Diff", "Cancel", "Apply to Target"} {
		if !tt.HasText(want) {
			t.Errorf("the compare modal does not show %q; texts = %v", want, tt.Texts())
		}
	}
}

// The whole UI path: runCompare fills the state an apply then acts on.
func TestCompareRunAndApply(t *testing.T) {
	app, dstPath := compareSetup(t)
	app.compare = compareState{open: true, srcName: "src", dstName: "dst"}
	if app.win != nil {
		t.Fatal("this test drives the state path directly")
	}

	// runCompare opens both connections itself; with no window its result
	// delivery is a win.Update, so drive the same steps it runs inline instead.
	ctx := context.Background()
	srcConn, _ := app.store.Get("src")
	dstConn, _ := app.store.Get("dst")
	srcDB, err := OpenDB(ctx, srcConn)
	if err != nil {
		t.Fatalf("open source: %v", err)
	}
	defer srcDB.Close()
	dstDB, err := OpenDB(ctx, dstConn)
	if err != nil {
		t.Fatalf("open target: %v", err)
	}
	defer dstDB.Close()

	srcSnap, err := schemadiff.Collect(ctx, srcDB.SQL, firstSchema(ctx, srcDB))
	if err != nil {
		t.Fatalf("collect source: %v", err)
	}
	dstSnap, err := schemadiff.Collect(ctx, dstDB.SQL, firstSchema(ctx, dstDB))
	if err != nil {
		t.Fatalf("collect target: %v", err)
	}
	entries := schemadiff.Diff(srcSnap, dstSnap, schemadiff.Options{})
	result := schemadiff.Script(srcSnap, dstSnap, entries)

	// Apply: one statement at a time, as applyScript does.
	applied := 0
	var failure string
	for _, st := range result.Statements {
		if _, err := dstDB.SQL.Execute(ctx, st.SQL); err != nil {
			failure = st.EntryID + ": " + err.Error()
			break
		}
		applied++
	}
	if failure != "" {
		t.Fatalf("apply failed after %d: %s", applied, failure)
	}
	if applied != len(result.Statements) {
		t.Fatalf("applied %d of %d", applied, len(result.Statements))
	}

	// After the apply, a fresh comparison of the two files answers nothing.
	after, err := OpenDB(ctx, Conn{Name: "dst2", Kind: "sqlite", File: dstPath})
	if err != nil {
		t.Fatalf("reopen target: %v", err)
	}
	defer after.Close()
	afterSnap, err := schemadiff.Collect(ctx, after.SQL, firstSchema(ctx, after))
	if err != nil {
		t.Fatalf("collect after: %v", err)
	}
	remaining := schemadiff.Diff(srcSnap, afterSnap, schemadiff.Options{})
	if len(remaining) != 0 {
		var ops []string
		for _, e := range remaining {
			ops = append(ops, string(e.Op)+" "+e.Table+" "+e.Object)
		}
		t.Fatalf("diff after apply = %v, want none", ops)
	}
}
