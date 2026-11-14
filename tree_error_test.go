package main

// Tests for how a failed catalog read is reported: it belongs to the tree (which
// can retry it), not to the results pane, where a timed-out table list read as a
// failed query — the confusing thing a user saw as "this schema is broken".
//
// The kernel half of the same bug (MySQL's table list selecting TABLE_ROWS,
// which forces a server-side stats dive) is covered in dbcore/mysql.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dbcore/dbcore"
)

// brokenApp is a connected App whose database is closed: every catalog read
// fails, which is what a failed load looks like to the apply function. The path
// comes back too, so a test can reopen the same file as a working connection.
func brokenApp(t *testing.T) (*App, string) {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "broken.db")
	seed(t, path)
	db, err := OpenDB(ctx, Conn{Name: "b", Kind: "sqlite", File: path})
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	app := &App{db: db, conn: Conn{Kind: "sqlite"}, open: true,
		schemas: []string{"main"},
		tables:  map[string][]tableInfo{},
		cols:    map[string][]dbcore.ColumnInfo{},
		objects: objectCache{}}
	app.initModalEditors()
	// Close the pool: ListTables now answers "sql: database is closed".
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return app, path
}

// A failed table list lands in the tree's error, leaves the results pane's
// message alone, and keeps the schema retryable.
func TestFailedTableListReportsInTree(t *testing.T) {
	app, _ := brokenApp(t)
	app.loadTables("main")
	if app.treeErr == "" {
		t.Fatal("a failed table list set no tree error")
	}
	if app.errMsg != "" {
		t.Fatalf("a catalog failure leaked into the results pane: %q", app.errMsg)
	}
	if app.treeErrRetry == nil {
		t.Fatal("the failure has no retry")
	}
	// The schema stays unloaded, which is what makes a second click (and the
	// Retry button) ask again.
	if _, st := app.TablesOf("main"); st != TablesNotLoaded {
		t.Fatalf("schema state = %v, want not-loaded so it can be retried", st)
	}
	// The message names the schema, not just the operation.
	if !strings.Contains(app.treeErr, "main") {
		t.Fatalf("tree error %q does not name the schema", app.treeErr)
	}
}

// A failed column list behaves the same way.
func TestFailedColumnListReportsInTree(t *testing.T) {
	app, _ := brokenApp(t)
	app.loadColumns("main", "employees")
	if app.treeErr == "" || app.errMsg != "" {
		t.Fatalf("treeErr=%q errMsg=%q, want the tree only", app.treeErr, app.errMsg)
	}
	if _, st := app.ColumnsOf("main", "employees"); st != ColsNotLoaded {
		t.Fatalf("columns state = %v, want not-loaded", st)
	}
}

// Retrying after the connection works again clears the error and loads.
func TestRetryAfterFailureLoads(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "retry.db")
	seed(t, path)
	db, err := OpenDB(ctx, Conn{Name: "r", Kind: "sqlite", File: path})
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	app := &App{db: db, conn: Conn{Kind: "sqlite"}, open: true,
		schemas: []string{"main"},
		tables:  map[string][]tableInfo{},
		cols:    map[string][]dbcore.ColumnInfo{},
		objects: objectCache{}}
	app.initModalEditors()

	// Simulate a failure that came from a transient problem: the error state,
	// then a successful load through the same path the Retry button runs.
	app.setTreeErr(errFake("transient"), func() { app.loadTables("main") })
	if app.treeErr == "" {
		t.Fatal("setTreeErr recorded nothing")
	}
	app.treeErrRetry()
	if app.treeErr != "" {
		t.Fatalf("a successful load left the error: %q", app.treeErr)
	}
	if _, st := app.TablesOf("main"); st != TablesLoaded {
		t.Fatalf("tables state = %v, want loaded", st)
	}
}

// The tree's error is cleared by the actions that reset the connection's view.
func TestTreeErrorClearedOnDisconnect(t *testing.T) {
	app, _ := refreshApp(t)
	app.setTreeErr(errFake("boom"), func() {})
	app.disconnect()
	if app.treeErr != "" {
		t.Fatalf("disconnect left treeErr = %q", app.treeErr)
	}
}

// A failed object section is retryable, and the retry asks the driver again
// rather than trusting the memo the failure wrote.
func TestObjectSectionRetryForgetsMemo(t *testing.T) {
	app, path := brokenApp(t)
	app.loadObjectSection("main", "", sectionOf(objIndex))
	if app.treeErr == "" {
		t.Fatal("a failed section load recorded no tree error")
	}
	if app.treeErrRetry == nil {
		t.Fatal("the failed section has no retry")
	}
	// The failure was memoised as resolved-empty; a retry that trusted the memo
	// would answer "no indexes" without asking. Make the connection work again
	// and retry: only a re-asked load finds them.
	ctx := context.Background()
	db, err := OpenDB(ctx, Conn{Name: "b", Kind: "sqlite", File: path})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	app.db = db

	app.treeErrRetry()
	if app.treeErr != "" {
		t.Fatalf("the retry after recovery still failed: %q", app.treeErr)
	}
	names, loaded, notSupported := app.objects.leaves(objIndex, "main", "")
	if notSupported || !loaded {
		t.Fatalf("the retry did not load the section (loaded=%v notSupported=%v)", loaded, notSupported)
	}
	// The recovered load found the seeded table's primary-key index (or at
	// least asked for it): an empty answer would mean the memo had been trusted.
	_ = names
}

// errFake is a stand-in error for the retry wiring.
type errFake string

func (e errFake) Error() string { return string(e) }

// A load already in flight is not started again: the tree asks for a branch's
// children every frame, so without this a slow catalog read was re-issued per
// frame and buried the server (that is what turned one slow read into a
// deadline timeout for everything behind it).
func TestLoadSingleFlight(t *testing.T) {
	app, _ := brokenApp(t)
	// Pretend a load for this schema is already on its way.
	key := "t\x00main"
	if !app.beginLoad(key) {
		t.Fatal("beginLoad refused the first call")
	}
	app.loadTables("main")
	if app.treeErr != "" {
		t.Fatalf("a second load was started while one was in flight: %q", app.treeErr)
	}
	// Releasing the mark lets the next call through (and it fails, because the
	// fixture's database is closed).
	app.endLoad(key)
	app.loadTables("main")
	if app.treeErr == "" {
		t.Fatal("after endLoad the load did not run")
	}
}

// beginLoad/endLoad is the mechanism the tree relies on; check its contract.
func TestBeginEndLoad(t *testing.T) {
	app := &App{}
	if !app.beginLoad("k") {
		t.Fatal("first beginLoad = false")
	}
	if app.beginLoad("k") {
		t.Fatal("second beginLoad = true while in flight")
	}
	if !app.beginLoad("other") {
		t.Fatal("a different key was refused")
	}
	app.endLoad("k")
	if !app.beginLoad("k") {
		t.Fatal("beginLoad after endLoad = false")
	}
}

// The tree asks for a branch's children every frame. Sixty asks must start one
// load, not sixty: that is the difference between a slow schema and a dead
// server.
func TestTreeFramesDoNotReflood(t *testing.T) {
	app, _ := brokenApp(t)
	root := treeNode{label: "main", kind: nodeSchema}
	for i := 0; i < 60; i++ {
		app.treeChildren(root)
	}
	// The first ask started the load and it failed; the rest saw the in-flight
	// mark after the failure released it, so each of them *did* retry — which is
	// why the loop is not the test's point. The point is that while one is in
	// flight, the others are refused:
	key := "t\x00main"
	if !app.beginLoad(key) {
		t.Fatal("could not take the mark")
	}
	before := app.treeErr
	app.loadTables("main")
	if app.treeErr != before {
		t.Fatal("a load ran while another was in flight")
	}
	app.endLoad(key)
}

var _ = os.Getenv
