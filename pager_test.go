package main

// Tests for the results pager: how many pages a set has, which rows a page
// window holds, and how navigation and the page-size selector move it.

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Ben-guanzhi/dbcore"
	"github.com/Ben-guanzhi/dbcore/sqlite"
	"github.com/egoist/mygo/ui"
)

// pageApp builds an App with a result of n dummy rows and no window, the way a
// test draws one.
func pageApp(rows int) *App {
	app := &App{pager: newResultPager(), gridSel: -1}
	app.result = &dbcore.QueryResult{}
	for i := 0; i < rows; i++ {
		app.result.Rows = append(app.result.Rows, []dbcore.Value{{}})
	}
	app.pager.total = rows
	return app
}

func TestPagerPages(t *testing.T) {
	p := newResultPager()
	if p.page != 1 || p.pageSize != defaultPageSize || p.total != unknownTotal || p.server {
		t.Fatalf("newResultPager = %+v", p)
	}
	if p.pages() != 1 {
		t.Fatalf("unknown total: pages = %d, want 1", p.pages())
	}

	p.total = 250
	if p.pages() != 3 {
		t.Fatalf("250/100: pages = %d, want 3", p.pages())
	}
	p.pageSize = 200
	if p.pages() != 2 {
		t.Fatalf("250/200: pages = %d, want 2", p.pages())
	}
	p.total = 0
	if p.pages() != 1 {
		t.Fatalf("empty: pages = %d, want 1", p.pages())
	}
	p.pageSize = 0
	p.total = 500
	if p.pages() != 1 {
		t.Fatalf("page size 0: pages = %d, want 1", p.pages())
	}
}

func TestPagerWindow(t *testing.T) {
	app := pageApp(250)
	app.pager.pageSize = 100

	app.pager.page = 1
	if app.pageStart() != 0 || app.pageEnd() != 100 || app.rowsShown() != 100 {
		t.Fatalf("page 1 window = [%d,%d), shown %d", app.pageStart(), app.pageEnd(), app.rowsShown())
	}

	app.pager.page = 3
	if app.pageStart() != 200 || app.pageEnd() != 250 || app.rowsShown() != 50 {
		t.Fatalf("page 3 window = [%d,%d), shown %d", app.pageStart(), app.pageEnd(), app.rowsShown())
	}

	// Past the end: an empty window, never an out-of-range one.
	app.pager.page = 9
	if app.pageStart() != 800 || app.pageEnd() != 800 || app.rowsShown() != 0 {
		t.Fatalf("page 9 window = [%d,%d), shown %d", app.pageStart(), app.pageEnd(), app.rowsShown())
	}
}

func TestPagerServerWindowIsWholeCopy(t *testing.T) {
	app := pageApp(0)
	app.pager.server = true
	app.pager.page = 3
	app.edit.active = true
	for i := 0; i < 120; i++ {
		app.edit.rows = append(app.edit.rows, &editRow{})
	}
	if app.pageStart() != 0 {
		t.Fatalf("server pageStart = %d, want 0", app.pageStart())
	}
	if app.pageEnd() != 120 {
		t.Fatalf("server pageEnd = %d, want 120", app.pageEnd())
	}
}

func TestPagerClamp(t *testing.T) {
	app := pageApp(250)
	app.pager.pageSize = 100
	app.pager.page = 5
	if got := app.clampPage(); got != 3 {
		t.Fatalf("clampPage = %d, want 3", got)
	}
	app.pager.page = 0
	if got := app.clampPage(); got != 1 {
		t.Fatalf("clampPage(0) = %d, want 1", got)
	}
}

func TestPagerSetPage(t *testing.T) {
	app := pageApp(250)
	app.pager.pageSize = 100

	app.setPagerPage(2)
	if app.pager.page != 2 {
		t.Fatalf("page = %d, want 2", app.pager.page)
	}
	app.setPagerPage(99)
	if app.pager.page != 3 {
		t.Fatalf("page = %d, want 3 (clamped)", app.pager.page)
	}
	app.setPagerPage(0)
	if app.pager.page != 1 {
		t.Fatalf("page = %d, want 1", app.pager.page)
	}
	// A page that is already shown does not move.
	app.setPagerPage(1)
	if app.pager.page != 1 {
		t.Fatalf("page = %d, want 1", app.pager.page)
	}
}

func TestPagerServerDirtyIsLocked(t *testing.T) {
	app := pageApp(0)
	app.pager.server = true
	app.edit.active = true
	app.edit.cols = []dbcore.ColumnInfo{{}}
	app.edit.addRow(nil, rowInserted)
	app.edit.refresh = func() { t.Fatal("a dirty server-paged browse must not page") }

	app.setPagerPage(2)
	if app.pager.page != 1 {
		t.Fatalf("dirty page = %d, want 1", app.pager.page)
	}
}

func TestPagerServerCleanPages(t *testing.T) {
	app := pageApp(0)
	app.pager.server = true
	app.edit.active = true
	app.edit.cols = []dbcore.ColumnInfo{{}}
	app.edit.addRow(nil, rowInserted)
	// Rollback leaves the copy clean, so paging may re-read.
	app.rollback()
	if app.edit.dirty() {
		t.Fatal("copy still dirty")
	}
	called := false
	app.edit.refresh = func() { called = true }
	app.pager.pageSize = 50
	app.pager.total = 200
	app.setPagerPage(2)
	if !called {
		t.Fatal("a clean server-paged browse did not re-read on page")
	}
	if app.pager.page != 2 {
		t.Fatalf("page = %d, want 2", app.pager.page)
	}
}

func TestPagerGotoRow(t *testing.T) {
	app := pageApp(250)
	app.pager.pageSize = 100

	app.gotoRow(0)
	if app.pager.page != 1 || app.gridSel != 0 {
		t.Fatalf("gotoRow(0) = page %d sel %d", app.pager.page, app.gridSel)
	}
	app.gotoRow(150)
	if app.pager.page != 2 || app.gridSel != 50 {
		t.Fatalf("gotoRow(150) = page %d sel %d", app.pager.page, app.gridSel)
	}
	app.gotoRow(249)
	if app.pager.page != 3 || app.gridSel != 49 {
		t.Fatalf("gotoRow(249) = page %d sel %d", app.pager.page, app.gridSel)
	}
}

func TestPagerSetPageSize(t *testing.T) {
	app := pageApp(250)
	app.pager.pageSize = 100

	app.setPageSize("200")
	if app.pager.pageSize != 200 {
		t.Fatalf("pageSize = %d, want 200", app.pager.pageSize)
	}
	if app.pager.page != 1 {
		t.Fatalf("page = %d, want 1", app.pager.page)
	}
	if app.pageStart() != 0 || app.pageEnd() != 200 {
		t.Fatalf("page 1 window = [%d,%d)", app.pageStart(), app.pageEnd())
	}
	app.setPagerPage(2)
	if app.pageStart() != 200 || app.pageEnd() != 250 {
		t.Fatalf("page 2 window = [%d,%d), want [200,250)", app.pageStart(), app.pageEnd())
	}
	app.setPageSize("200")
	if app.pager.pageSize != 200 {
		t.Fatalf("same page size moved: %d", app.pager.pageSize)
	}
}

func TestPagerAbsoluteRowNumbers(t *testing.T) {
	// Client-paged: the row column and summary count through the whole loaded set.
	app := pageApp(250)
	app.pager.pageSize = 100
	app.pager.page = 2
	if got := app.pageRowNumber(app.pageStart()); got != 101 {
		t.Fatalf("client page 2 first row number = %d, want 101", got)
	}
	if got := app.pageRowNumber(app.pageEnd() - 1); got != 200 {
		t.Fatalf("client page 2 last row number = %d, want 200", got)
	}
	if got := app.pageAbsoluteStart(); got != 100 {
		t.Fatalf("client page 2 absolute start = %d, want 100", got)
	}

	// Server-paged: the window is the whole page, so the numbers start past the
	// page offset and count from the table, not from 1.
	app.pager.server = true
	app.pager.page = 3
	app.edit.active = true
	app.edit.rows = make([]*editRow, 100)
	if got := app.pageRowNumber(0); got != 201 {
		t.Fatalf("server page 3 first row number = %d, want 201", got)
	}
	if got := app.pageRowNumber(99); got != 300 {
		t.Fatalf("server page 3 last row number = %d, want 300", got)
	}
	if got := app.pageAbsoluteStart(); got != 200 {
		t.Fatalf("server page 3 absolute start = %d, want 200", got)
	}
}

func TestPagerStatusText(t *testing.T) {
	if got := statusPageText("employees", 2, 250, 100); got != "employees: page 2, 100 of 250 row(s)" {
		t.Fatalf("known total = %q", got)
	}
	if got := statusPageText("employees", 1, -1, 100); got != "employees: 100 row(s)" {
		t.Fatalf("unknown total = %q", got)
	}
}

func TestPagerUnknownTotalPagesOnce(t *testing.T) {
	app := pageApp(0)
	app.result.Truncated = true
	app.pager.total = unknownTotal
	if app.paged() {
		t.Fatal("an empty truncated result is not paged")
	}
	app.result.Rows = append(app.result.Rows, []dbcore.Value{{}})
	if !app.paged() {
		t.Fatal("a one-row result is paged")
	}
	if app.pager.pages() != 1 {
		t.Fatalf("unknown total pages = %d, want 1", app.pager.pages())
	}
}

// TestPagerUnboundedWhenFullPageNoTotal covers the server that refuses to count
// the table: a full page is the only evidence that more rows exist, and the
// pager follows it.
func TestPagerUnboundedWhenFullPageNoTotal(t *testing.T) {
	app := pageApp(100)
	app.pager.pageSize = 100
	app.pager.server = true
	app.pager.total = unknownTotal
	app.pager.more = true
	app.result.Truncated = true

	if app.pager.pages() != unboundedPages {
		t.Fatalf("pages = %d, want unbounded", app.pager.pages())
	}
	// Navigation goes past one page; Last has no target.
	app.setPagerPage(5)
	if app.pager.page != 5 {
		t.Fatalf("page = %d, want 5", app.pager.page)
	}

	// A short page is the end of the set.
	app.pager.more = false
	app.setPagerPage(2)
	if app.pager.page != 1 {
		t.Fatalf("page = %d, want 1 (one page)", app.pager.page)
	}
}

// TestBrowsePagesAreDistinct drives the server-side paging the pager relies on:
// a later page of a table browse is a later window of the same ordered rows.
func TestBrowsePagesAreDistinct(t *testing.T) {
	path := filepath.Join(t.TempDir(), "page.db")
	d, err := sqlite.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	sql, ok := d.AsSQL()
	if !ok {
		t.Fatal("no SQL")
	}
	ctx := context.Background()
	for _, stmt := range []string{
		`DROP TABLE IF EXISTS employees`,
		`CREATE TABLE employees (id INTEGER PRIMARY KEY, name TEXT NOT NULL)`,
		`INSERT INTO employees (id, name) VALUES (1,'Ada'),(2,'Alan'),(3,'Grace'),(4,'Edsger'),(5,'Barbara')`,
	} {
		if _, err := sql.Execute(ctx, stmt); err != nil {
			t.Fatalf("execute %q: %v", stmt, err)
		}
	}
	if err := d.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	db, err := OpenDB(ctx, Conn{Name: "page", Kind: "sqlite", File: path})
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()

	cached := newCached(db)
	if err := cached.LoadSchemas(ctx); err != nil {
		t.Fatalf("LoadSchemas: %v", err)
	}
	schema := cached.SchemaNames()[0]

	first, err := db.SQL.BrowseRows(ctx, schema, "employees", &dbcore.TableRowsRequest{Page: 1, PageSize: 2})
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if len(first.Rows) != 2 || !first.TotalFound || first.Total != 5 {
		t.Fatalf("page 1 = %d rows, total %d (%v), want 2 and 5", len(first.Rows), first.Total, first.TotalFound)
	}
	if first.Rows[0][1].String() != "Ada" || first.Rows[1][1].String() != "Alan" {
		t.Fatalf("page 1 names = %q, %q", first.Rows[0][1].String(), first.Rows[1][1].String())
	}

	second, err := db.SQL.BrowseRows(ctx, schema, "employees", &dbcore.TableRowsRequest{Page: 2, PageSize: 2})
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if len(second.Rows) != 2 {
		t.Fatalf("page 2 = %d rows, want 2", len(second.Rows))
	}
	if second.Rows[0][1].String() != "Grace" || second.Rows[1][1].String() != "Edsger" {
		t.Fatalf("page 2 names = %q, %q", second.Rows[0][1].String(), second.Rows[1][1].String())
	}

	third, err := db.SQL.BrowseRows(ctx, schema, "employees", &dbcore.TableRowsRequest{Page: 3, PageSize: 2})
	if err != nil {
		t.Fatalf("page 3: %v", err)
	}
	if len(third.Rows) != 1 || third.Rows[0][1].String() != "Barbara" {
		t.Fatalf("page 3 = %d rows %v, want [Barbara]", len(third.Rows), third.Rows)
	}
}

// TestPagerBarRenders drives the results pane, and so the pager bar, through
// the real view code: a widget that cannot build panics a test, where an app
// only logs it.
func TestPagerBarRenders(t *testing.T) {
	cols := []dbcore.ColumnInfo{{Name: "id"}, {Name: "name"}}
	render := func(app *App) {
		app.grid.Selected = &app.gridSel
		ui.NewTester(func(c *ui.Context) { app.resultsPane(c) }, 960, 420)
	}

	// Client-paged: a plain statement's pages are cut out of the loaded rows.
	client := &App{pager: newResultPager(), gridSel: -1}
	client.pager.pageSize = 100
	client.pager.page = 2
	client.pager.total = 250
	client.result = &dbcore.QueryResult{Columns: cols, Truncated: true}
	for i := 0; i < 250; i++ {
		client.result.Rows = append(client.result.Rows, []dbcore.Value{{}, {}})
	}
	render(client)

	// Server-paged, known total: one page's working copy plus the pager.
	server := &App{pager: newResultPager(), gridSel: -1}
	server.pager.pageSize = 100
	server.pager.server = true
	server.pager.page = 3
	server.pager.total = 250
	server.result = &dbcore.QueryResult{Columns: cols}
	server.edit.active = true
	server.edit.cols = cols
	server.grid.CellSelect = true
	for i := 0; i < 100; i++ {
		server.edit.rows = append(server.edit.rows, &editRow{
			strs:     []string{"1", "a"},
			origNull: []bool{false, false},
		})
	}
	render(server)

	// Server-paged, no total and a full page: the pager is unbounded.
	nototal := &App{pager: newResultPager(), gridSel: -1}
	nototal.pager.pageSize = 100
	nototal.pager.server = true
	nototal.pager.page = 1
	nototal.pager.more = true
	nototal.result = &dbcore.QueryResult{Columns: cols, Truncated: true}
	for i := 0; i < 100; i++ {
		nototal.result.Rows = append(nototal.result.Rows, []dbcore.Value{{}, {}})
	}
	render(nototal)
}

// TestPagerKeyboardPrevNext drives Alt+Left and Alt+Right through a real
// Tester, as the view does.
func TestPagerKeyboardPrevNext(t *testing.T) {
	app := pageApp(250)
	app.pager.pageSize = 100
	app.grid.Selected = &app.gridSel
	tt := ui.NewTester(func(c *ui.Context) { app.pagerShortcuts(c) }, 640, 120)

	tt.Key(ui.Alt, ui.KeyRight)
	if app.pager.page != 2 {
		t.Fatalf("Alt+Right: page = %d, want 2", app.pager.page)
	}
	tt.Key(ui.Alt, ui.KeyRight)
	if app.pager.page != 3 {
		t.Fatalf("Alt+Right: page = %d, want 3", app.pager.page)
	}
	tt.Key(ui.Alt, ui.KeyRight) // past the end stays on the last
	if app.pager.page != 3 {
		t.Fatalf("Alt+Right past the end: page = %d, want 3", app.pager.page)
	}
	tt.Key(ui.Alt, ui.KeyLeft)
	if app.pager.page != 2 {
		t.Fatalf("Alt+Left: page = %d, want 2", app.pager.page)
	}
	tt.Key(ui.Alt, ui.KeyLeft)
	if app.pager.page != 1 {
		t.Fatalf("Alt+Left: page = %d, want 1", app.pager.page)
	}
	tt.Key(ui.Alt, ui.KeyLeft) // before the first stays on the first
	if app.pager.page != 1 {
		t.Fatalf("Alt+Left past the start: page = %d, want 1", app.pager.page)
	}
}

func TestPagerKeyboardIgnored(t *testing.T) {
	for _, tc := range []struct {
		name string
		busy bool
		rows int
	}{
		{"busy", true, 250},
		{"no rows", false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := pageApp(tc.rows)
			app.pager.pageSize = 100
			app.busy = tc.busy
			app.grid.Selected = &app.gridSel
			tt := ui.NewTester(func(c *ui.Context) { app.pagerShortcuts(c) }, 640, 120)
			tt.Key(ui.Alt, ui.KeyRight)
			if app.pager.page != 1 {
				t.Fatalf("page = %d, want the first left alone", app.pager.page)
			}
		})
	}
}

func TestJumpToPage(t *testing.T) {
	app := pageApp(250)
	app.pager.pageSize = 100
	app.grid.Selected = &app.gridSel
	if last := app.pager.pages(); last != 3 {
		t.Fatalf("pages = %d, want 3", last)
	}

	// Before the first and past the end are refused, not clamped.
	app.jumpToPage(0)
	if app.jumpErr == "" || app.pager.page != 1 {
		t.Fatalf("page 0: err = %q page = %d", app.jumpErr, app.pager.page)
	}
	app.jumpErr = ""
	app.jumpToPage(4)
	if app.jumpErr == "" || app.pager.page != 1 {
		t.Fatalf("page 4: err = %q page = %d", app.jumpErr, app.pager.page)
	}

	// A page in range moves, and closes the modal.
	app.jump = true
	app.jumpToPage(3)
	if app.jump || app.pager.page != 3 {
		t.Fatalf("page 3: jump = %v page = %d", app.jump, app.pager.page)
	}
}

func TestOpenJumpIgnoredWhenNotPaged(t *testing.T) {
	app := pageApp(0)
	app.openJump()
	if app.jump {
		t.Fatal("an empty result has no page to jump to")
	}
}

// TestJumpModalRenders draws the jump modal as the view does, so a widget that
// cannot build panics the test.
func TestJumpModalRenders(t *testing.T) {
	app := pageApp(250)
	app.pager.pageSize = 100
	app.grid.Selected = &app.gridSel
	app.jump = true
	app.jumpNum = 2
	ui.NewTester(func(c *ui.Context) { app.jumpView(c) }, 400, 260)

	app.jumpErr = "too far"
	ui.NewTester(func(c *ui.Context) { app.jumpView(c) }, 400, 300)
}
