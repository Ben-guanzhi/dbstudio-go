package main

// Tests for the incremental browse (a segment is appended when the reader nears
// the end) and for the connection colour reaching the window itself.

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/dbcore/dbcore"
	"github.com/egoist/mygo/ui"
)

// browseApp is an App with a browse tab of a fake table, whose driver answers
// segments of `rows` rows each.
type segDriver struct {
	rows   int // rows per segment
	total  int // the table's size
	sent   int // rows served so far
	asked  []int
	failAt int // the page that fails (0 = never)
}

func (d *segDriver) Kind() dbcore.DatabaseKind       { return dbcore.KindSQLite }
func (d *segDriver) Ping(context.Context) error      { return nil }
func (d *segDriver) Close() error                    { return nil }
func (d *segDriver) AsSQL() (dbcore.SQLDriver, bool) { return nil, false }
func (d *segDriver) AsKV() (dbcore.KVDriver, bool)   { return nil, false }

// The fake is a full SQLDriver so app.db.SQL is the same object: the browse path
// calls it through that interface.
func (d *segDriver) Execute(context.Context, string, ...dbcore.Value) (*dbcore.QueryResult, error) {
	return &dbcore.QueryResult{}, nil
}
func (d *segDriver) ListSchemas(context.Context) ([]dbcore.SchemaInfo, error) {
	return []dbcore.SchemaInfo{{Name: "main"}}, nil
}
func (d *segDriver) ListTables(context.Context, string) ([]dbcore.TableInfo, error) {
	return []dbcore.TableInfo{{Schema: "main", Name: "big", Type: "table"}}, nil
}
func (d *segDriver) ListColumns(context.Context, string, string) ([]dbcore.ColumnInfo, error) {
	return []dbcore.ColumnInfo{{Name: "id", Type: "INTEGER"}}, nil
}
func (d *segDriver) ListIndexes(context.Context, string, string) ([]dbcore.IndexInfo, error) {
	return nil, nil
}
func (d *segDriver) ListForeignKeys(context.Context, string, string) ([]dbcore.ForeignKeyInfo, error) {
	return nil, nil
}
func (d *segDriver) CreateTableSQL(context.Context, string, string) (string, error) {
	return "CREATE TABLE big (id INTEGER)", nil
}
func (d *segDriver) BrowseRows(_ context.Context, schema, table string, req *dbcore.TableRowsRequest) (*dbcore.TableRowsResult, error) {
	d.asked = append(d.asked, req.Page)
	if d.failAt > 0 && req.Page == d.failAt {
		return nil, errSeg("segment refused")
	}
	start := (req.Page - 1) * req.PageSize
	n := d.rows
	if start >= d.total {
		n = 0
	} else if start+n > d.total {
		n = d.total - start
	}
	res := &dbcore.TableRowsResult{
		Columns: []dbcore.ColumnInfo{{Name: "id"}},
		Rows:    make([][]dbcore.Value, n),
	}
	for i := 0; i < n; i++ {
		res.Rows[i] = []dbcore.Value{{Kind: dbcore.ValInt, I: int64(start + i + 1)}}
	}
	d.sent += n
	return res, nil
}

func (d *segDriver) BrowseRowsCount(context.Context, string, string, *dbcore.TableRowsRequest) (uint64, error) {
	return uint64(d.total), nil
}

type errSeg string

func (e errSeg) Error() string { return string(e) }

// browseTabApp builds a browse tab in the state fetchBrowse leaves it in.
func browseTabApp(t *testing.T, d *segDriver, fetchSize int) (*App, *resTab) {
	t.Helper()
	app := &App{db: &DB{Driver: d, SQL: d, Kind: "sqlite"}, conn: Conn{Kind: "sqlite"}, open: true,
		pager:   newResultPager(),
		gridSel: -1,
		tables:  map[string][]tableInfo{},
		cols:    map[string][]dbcore.ColumnInfo{},
		objects: objectCache{}}
	app.initModalEditors()
	app.initConsoles()
	app.initEditorState()
	app.autocommit = true

	tab := newTab("big", tabBrowse)
	tab.node = treeNode{label: "big", parent: "main", kind: nodeTable}
	tab.schema, tab.table = "main", "big"
	tab.pager.reset(true)
	tab.pager.pageSize = fetchSize
	tab.fetchSize = fetchSize
	app.tabs = []*resTab{tab}
	app.tabSel = 0
	app.loadTab(tab)
	app.pager = tab.pager
	return app, tab
}

// The first segment fills the view, and reaching its end appends the next one.
func TestIncrementalBrowseAppends(t *testing.T) {
	d := &segDriver{rows: 50, total: 300}
	app, tab := browseTabApp(t, d, 50)

	// The first segment, as fetchBrowse would leave it.
	res, err := app.db.SQL.BrowseRows(context.Background(), "", "big", &dbcore.TableRowsRequest{Page: 1, PageSize: 50})
	if err != nil {
		t.Fatalf("BrowseRows: %v", err)
	}
	tab.result = &dbcore.QueryResult{Columns: res.Columns, Rows: res.Rows}
	tab.pagesLoaded = 1
	tab.moreRows = true
	app.result = tab.result

	// The reader is at the top: nothing is fetched.
	app.lastDrawnRow = 0
	app.maybeLoadMore()
	if tab.pagesLoaded != 1 {
		t.Fatalf("a segment was fetched at the top of the grid (%d pages)", tab.pagesLoaded)
	}

	// Near the end of the loaded rows, the next segment is asked for.
	app.lastDrawnRow = 40
	app.maybeLoadMore()
	waitBrowse(t, tab)
	if tab.pagesLoaded != 2 {
		t.Fatalf("pages loaded = %d, want 2", tab.pagesLoaded)
	}
	if len(tab.result.Rows) != 100 {
		t.Fatalf("rows = %d, want 100", len(tab.result.Rows))
	}
	// The appended rows continue where the first segment stopped.
	if got := tab.result.Rows[99][0].I; got != 100 {
		t.Fatalf("last row = %d, want 100", got)
	}
}

// The end of the table stops the loading, and a short segment proves it.
func TestIncrementalBrowseStopsAtEnd(t *testing.T) {
	d := &segDriver{rows: 40, total: 60}
	app, tab := browseTabApp(t, d, 40)
	res, _ := app.db.SQL.BrowseRows(context.Background(), "", "big", &dbcore.TableRowsRequest{Page: 1, PageSize: 40})
	tab.result = &dbcore.QueryResult{Columns: res.Columns, Rows: res.Rows}
	tab.pagesLoaded = 1
	tab.moreRows = true
	app.result = tab.result

	app.lastDrawnRow = 39
	app.maybeLoadMore()
	waitBrowse(t, tab)
	if len(tab.result.Rows) != 60 {
		t.Fatalf("rows = %d, want 60", len(tab.result.Rows))
	}
	if tab.moreRows {
		t.Fatal("a short segment said there was more")
	}
	// And another ask does nothing.
	asked := len(d.asked)
	app.maybeLoadMore()
	waitBrowse(t, tab)
	if len(d.asked) != asked {
		t.Fatalf("the exhausted table was asked again (%d asks)", len(d.asked))
	}
}

// A dirty working copy stops the loading: appending re-reads the rows and would
// take the edits with them.
func TestIncrementalBrowseStopsOnDirtyCopy(t *testing.T) {
	d := &segDriver{rows: 50, total: 500}
	app, tab := browseTabApp(t, d, 50)
	res, _ := app.db.SQL.BrowseRows(context.Background(), "", "big", &dbcore.TableRowsRequest{Page: 1, PageSize: 50})
	tab.result = &dbcore.QueryResult{Columns: res.Columns, Rows: res.Rows}
	tab.pagesLoaded = 1
	tab.moreRows = true
	app.result = tab.result
	// A working copy with a real edit in it: dirty() compares the text, so the
	// cell must actually differ from what was read.
	app.edit.init(tab.result, "main", "big", []dbcore.ColumnInfo{{Name: "id"}}, func() {})
	app.edit.rows[0].strs[0] = "999"
	app.edit.markModified(0)
	if !app.edit.dirty() {
		t.Fatal("the fixture is not dirty")
	}

	app.lastDrawnRow = 49
	app.maybeLoadMore()
	if tab.pagesLoaded != 1 {
		t.Fatalf("a segment was fetched while the copy was dirty (%d pages)", tab.pagesLoaded)
	}
	if tab.moreRows {
		t.Fatal("loading was not stopped for the dirty copy")
	}
}

// A failed segment is reported and stops further automatic loading.
func TestIncrementalBrowseFailure(t *testing.T) {
	d := &segDriver{rows: 50, total: 500, failAt: 2}
	app, tab := browseTabApp(t, d, 50)
	res, _ := app.db.SQL.BrowseRows(context.Background(), "", "big", &dbcore.TableRowsRequest{Page: 1, PageSize: 50})
	tab.result = &dbcore.QueryResult{Columns: res.Columns, Rows: res.Rows}
	tab.pagesLoaded = 1
	tab.moreRows = true
	app.result = tab.result

	app.lastDrawnRow = 49
	app.maybeLoadMore()
	waitBrowse(t, tab)
	if tab.moreRows {
		t.Fatal("a failed segment left loading on")
	}
	if app.errMsg == "" {
		t.Fatal("a failed segment was not reported")
	}
	if len(tab.result.Rows) != 50 {
		t.Fatalf("rows = %d, want the first segment only", len(tab.result.Rows))
	}
}

// The cap stops the growth, with a message that says where to go.
func TestIncrementalBrowseCap(t *testing.T) {
	d := &segDriver{rows: 1000, total: rowsLoadedMax + 5000}
	app, tab := browseTabApp(t, d, 1000)
	res, _ := app.db.SQL.BrowseRows(context.Background(), "", "big", &dbcore.TableRowsRequest{Page: 1, PageSize: 1000})
	tab.result = &dbcore.QueryResult{Columns: res.Columns, Rows: res.Rows}
	tab.pagesLoaded = 1
	tab.moreRows = true
	app.result = tab.result
	// Pretend the cap is nearly reached.
	tab.result.Rows = append(tab.result.Rows, make([][]dbcore.Value, rowsLoadedMax-len(tab.result.Rows))...)

	app.lastDrawnRow = len(tab.result.Rows) - 1
	app.maybeLoadMore()
	if tab.moreRows {
		t.Fatal("loading continued past the cap")
	}
	if app.status == "" {
		t.Fatal("the cap was not explained")
	}
}

// waitBrowse waits for the segment fetch to land.
func waitBrowse(t *testing.T, tab *resTab) {
	t.Helper()
	for i := 0; i < 400; i++ {
		if !tab.loadingMore {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("the segment fetch did not finish")
}

// --- the colour on the window -----------------------------------------------

// A coloured connection puts a band across the top and names itself in the
// window title; a plain one does neither.
func TestWindowColourAndTitle(t *testing.T) {
	app := newViewApp(t, nil)
	app.open = true
	app.conn = Conn{Name: "prod", Kind: "mysql", Color: "red", ReadOnly: true}
	app.connName = "prod"

	if got := app.windowTitle(); got != "DB Studio — ■ prod 🔒" {
		t.Fatalf("title = %q", got)
	}
	// The band draws (the tester draws the view, which includes it).
	ui.NewTester(app.colorBand, 400, 10)

	// A connection with no colour has no band but still names itself.
	app.conn = Conn{Name: "local", Kind: "sqlite"}
	if got := app.windowTitle(); got != "DB Studio — local" {
		t.Fatalf("title = %q", got)
	}
	// A closed window is the app alone.
	app.open = false
	if got := app.windowTitle(); got != "DB Studio" {
		t.Fatalf("title = %q", got)
	}
}

// A session with a per-table layout still restores widths: the two memories are
// independent.
func TestSessionKeepsLayoutsAndWidths(t *testing.T) {
	app, _ := refreshApp(t)
	store, err := NewStoreAt(filepath.Join(t.TempDir(), "connections.json"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	app.store = store
	app.initConsoles()
	app.initEditorState()
	app.colLayouts = map[string]colLayout{"main\x00t": {Order: []string{"b", "a"}, Hidden: []string{"c"}}}
	app.grid.Columns.Widths = map[string]float32{"a": 100}
	app.saveSession()

	got := loadSession(app.sessionPath())
	if len(got.ColLayouts) != 1 || !strsEqual(got.ColLayouts["main\x00t"].Order, []string{"b", "a"}) {
		t.Fatalf("layouts = %v", got.ColLayouts)
	}
	if got.Widths["a"] != 100 {
		t.Fatalf("widths = %v", got.Widths)
	}
}
