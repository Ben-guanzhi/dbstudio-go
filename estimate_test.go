package main

// Tests for the pager's row estimate and for column widths that survive a
// restart.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Ben-guanzhi/dbcore"
	"github.com/egoist/mygo/ui"
)

// --- row estimates ----------------------------------------------------------

// estDriver answers the estimate the test wants, and counts the questions.
type estDriver struct {
	rows  int64
	known bool
	asked int
}

func (d *estDriver) Kind() dbcore.DatabaseKind       { return dbcore.KindSQLite }
func (d *estDriver) Ping(context.Context) error      { return nil }
func (d *estDriver) Close() error                    { return nil }
func (d *estDriver) AsSQL() (dbcore.SQLDriver, bool) { return nil, false }
func (d *estDriver) AsKV() (dbcore.KVDriver, bool)   { return nil, false }
func (d *estDriver) EstimatedRows(context.Context, string, string) (int64, bool, bool, error) {
	d.asked++
	return d.rows, true, d.known, nil
}

// A browse of a table the catalog has an estimate for shows it, and asks the
// driver only when the table list did not carry one.
func TestEstimateFromTableList(t *testing.T) {
	app, _ := schemaApp(t)
	app.tables["main"] = []tableInfo{{Name: "big", Type: "table", Rows: 1_200_000}}
	app.requestEstimate("main", "big")
	if rows, ok := app.estimateOf("main", "big"); !ok || rows != 1_200_000 {
		t.Fatalf("estimate = %d (ok=%v), want the list's 1.2M", rows, ok)
	}
	// The tab's label is what the pager draws.
	tab := newTab("big", tabBrowse)
	tab.schema, tab.table = "main", "big"
	if got := app.estimateLabel(tab); got != "~1.2M rows" {
		t.Fatalf("label = %q", got)
	}
}

// A driver that can estimate is asked, off the thread, and the answer is cached.
func TestEstimateFromDriver(t *testing.T) {
	d := &estDriver{rows: 4_500_000, known: true}
	app := &App{db: &DB{Driver: d, Kind: "sqlite"}, open: true,
		tables: map[string][]tableInfo{}, cols: map[string][]dbcore.ColumnInfo{}}
	app.requestEstimate("main", "huge")
	// The estimate is fetched off the view thread; wait for the answer.
	deadline := 400
	for deadline > 0 {
		if _, ok := app.estimateOf("main", "huge"); ok {
			break
		}
		time.Sleep(5 * time.Millisecond)
		deadline--
	}
	if d.asked != 1 {
		t.Fatalf("the driver was asked %d time(s), want 1", d.asked)
	}
	rows, ok := app.estimateOf("main", "huge")
	if !ok || rows != 4_500_000 {
		t.Fatalf("estimate = %d (ok=%v)", rows, ok)
	}
	// Asking again does not ask the driver again.
	app.requestEstimate("main", "huge")
	if d.asked != 1 {
		t.Fatalf("the cached estimate was asked for again (%d asks)", d.asked)
	}
}

// A driver with no estimate leaves the pager's "?" alone: a miss is remembered
// so it is not asked for on every frame.
func TestEstimateUnknownStaysUnknown(t *testing.T) {
	d := &estDriver{known: false}
	app := &App{db: &DB{Driver: d, Kind: "sqlite"}, open: true,
		tables: map[string][]tableInfo{}, cols: map[string][]dbcore.ColumnInfo{}}
	app.requestEstimate("main", "view")
	time.Sleep(20 * time.Millisecond) // let the one fetch finish
	for i := 0; i < 50; i++ {
		app.requestEstimate("main", "view")
	}
	if d.asked > 1 {
		t.Fatalf("an unknown estimate was asked %d times", d.asked)
	}
	tab := newTab("view", tabBrowse)
	tab.schema, tab.table = "main", "view"
	if got := app.estimateLabel(tab); got != "" {
		t.Fatalf("an unknown estimate produced %q", got)
	}
}

// The pager draws the estimate when it has no total.
func TestPagerShowsEstimate(t *testing.T) {
	app, _ := schemaApp(t)
	app.tables["main"] = []tableInfo{{Name: "big", Type: "table", Rows: 1_200_000}}
	app.requestEstimate("main", "big")

	res := &dbcore.QueryResult{
		Columns: []dbcore.ColumnInfo{{Name: "id"}},
		Rows:    [][]dbcore.Value{{{Kind: dbcore.ValInt, I: 1}}},
	}
	tab := newTab("big", tabBrowse)
	tab.schema, tab.table = "main", "big"
	tab.result = res
	tab.pager.reset(true)
	tab.pager.more = true // a full page with no total: the unbounded case
	app.tabs = []*resTab{tab}
	app.loadTab(tab)
	app.pager = tab.pager

	tt := ui.NewTester(app.pagerBar, 1100, 40)
	// A browse loads segments, so its bar is a loaded-counter with the estimate,
	// not a page window.
	if !tt.HasText("1 of ~1.2M rows") {
		t.Errorf("the browse bar does not show the loaded count and estimate; texts = %v", tt.Texts())
	}
	if !tt.HasText("Load More") {
		t.Errorf("the browse bar has no Load More; texts = %v", tt.Texts())
	}

	// A query's result keeps the page window: it is fetched whole and paged
	// client-side, which is a different question from a table's segments.
	q := newTab("Result 1", tabRun)
	q.result = res
	q.pager.reset(false)
	q.pager.total = -1
	q.pager.more = true
	q.pager.page = 1
	app.tabs = []*resTab{q}
	app.loadTab(q)
	qp := ui.NewTester(app.pagerBar, 1100, 40)
	if !qp.HasText("Page 1 / ?") {
		t.Errorf("a query's pager lost its page window; texts = %v", qp.Texts())
	}
}

// --- column widths ----------------------------------------------------------

// A dragged column width is written to the session and restored on the next
// launch, and the first frame after a restore does not write it again.
func TestColumnWidthsPersist(t *testing.T) {
	app, _ := refreshApp(t)
	store, err := NewStoreAt(filepath.Join(t.TempDir(), "connections.json"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	app.store = store
	app.initConsoles()
	app.initEditorState()

	app.grid.Columns.Widths = map[string]float32{"id": 140, "name": 320}
	app.noteColumnWidths()
	// The session file now holds them.
	got := loadSession(app.sessionPath())
	if got.Widths["id"] != 140 || got.Widths["name"] != 320 {
		t.Fatalf("widths = %v", got.Widths)
	}
	// A second call with the same widths writes nothing new (the file's mtime
	// would change); a changed width does write.
	before, err := os.Stat(app.sessionPath())
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	app.noteColumnWidths()
	again, _ := os.Stat(app.sessionPath())
	if !again.ModTime().Equal(before.ModTime()) {
		t.Fatal("an unchanged width wrote the session again")
	}

	// Restoring puts them in the grid and remembers them as the current state.
	restored := &App{grid: app.grid}
	restored.applySession(sessionState{Widths: map[string]float32{"id": 99}})
	if restored.grid.Columns.Widths["id"] != 99 {
		t.Fatalf("restored width = %v", restored.grid.Columns.Widths["id"])
	}
	if restored.savedWidths["id"] != 99 {
		t.Fatalf("the restored width was not recorded as current: %v", restored.savedWidths)
	}
	// Restoring a session with no widths leaves the grid alone.
	fresh := &App{}
	fresh.applySession(sessionState{})
	if len(fresh.grid.Columns.Widths) != 0 {
		t.Fatalf("an empty session produced widths: %v", fresh.grid.Columns.Widths)
	}
}
