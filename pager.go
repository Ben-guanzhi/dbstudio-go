package main

// The results pager.
//
// A table browse is paged on the server: the driver reads one window of rows at
// a time and tells the client the table's total, so First/Previous/Next/Last
// ask for the next window and the numbers are the database's. An arbitrary
// statement cannot be paged that way — there is no portable "give me page k" —
// so its pages are cut out of the rows the query already loaded, up to the
// driver's materialization cap.
//
// The grid draws one page: the rows it shows are the window [pageStart,
// pageEnd) of the working copy (edit mode) or of the result. A server-paged
// browse already holds exactly one page in its working copy, so its window is
// the whole copy, including rows the user inserted on it; a client-paged result
// cuts at most pageSize rows out of what was loaded.

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/Ben-guanzhi/dbcore"
)

// resultPager is the state of the results grid's pager.
type resultPager struct {
	page     int  // the page shown, 1-based
	pageSize int  // rows per page
	total    int  // the result set's rows, or -1 when unknown
	server   bool // true when a page is fetched from the database
	more     bool // the server gave no total and the page was full
}

const (
	defaultPageSize = 100
	maxPageSize     = 1000
	unknownTotal    = -1
	unboundedPages  = 1 << 30
)

// pageSizeChoices are the page sizes the selector offers.
var pageSizeChoices = []string{"50", "100", "200", "500", "1000"}

// newResultPager is the pager every result set starts from.
func newResultPager() resultPager {
	return resultPager{page: 1, pageSize: defaultPageSize, total: unknownTotal}
}

// reset starts a new result set on its first page.
func (p *resultPager) reset(server bool) {
	p.page = 1
	p.server = server
	p.total = unknownTotal
	p.more = false
}

// pages is how many pages the result set has. A known total gives the real
// count; no total with a full page means more may follow, so the pager is
// unbounded and every page beyond the first is reachable; otherwise one page.
func (p *resultPager) pages() int {
	if p.pageSize <= 0 {
		return 1
	}
	if p.total >= 0 {
		n := (p.total + p.pageSize - 1) / p.pageSize
		if n < 1 {
			return 1
		}
		return n
	}
	if p.more {
		return unboundedPages
	}
	return 1
}

// rowCount is how many rows the grid's full set holds: the working copy when
// the grid is editing, the result otherwise.
func (app *App) rowCount() int {
	if app.edit.active {
		return len(app.edit.rows)
	}
	if app.result == nil {
		return 0
	}
	return len(app.result.Rows)
}

// invalidate asks the window to redraw, when there is one. An App built for a
// test has no window.
//
// The window call goes through redraw, a variable, so a test can prove this
// reaches the window exactly once. That seam exists because of a real bug: this
// function once called *itself* (a bulk edit had rewritten `app.win.Invalidate()`
// into `app.invalidate()`), which overflowed the stack on the first click — and
// every headless test missed it, because a test App has no window and the
// guarded branch never ran.
func (app *App) invalidate() {
	if app.win != nil {
		redraw(app.win)
	}
}

// beginBusy and endBusy track which asynchronous layer is running. A single
// bool could not answer "who is busy": a browse or a load-more finishing first
// cleared the flag a script run still owned, which disabled Stop mid-run and
// made the status bar disagree with the work in flight. busy is the union of
// the owners, so each layer clears only its own mark.
func (app *App) beginBusy(owner string) {
	if app.busyOwners == nil {
		app.busyOwners = map[string]bool{}
	}
	app.busyOwners[owner] = true
	app.busy = true
}

func (app *App) endBusy(owner string) {
	delete(app.busyOwners, owner)
	app.busy = len(app.busyOwners) > 0
}

// clearBusy drops every owner's mark — the connection or the tab bar is going
// away, so no in-flight work it started can still claim the state.
func (app *App) clearBusy() {
	app.busyOwners = nil
	app.busy = false
}

// redraw is the one call invalidate makes on the window. Tests replace it.
var redraw = func(w *mygo.Window) { w.Invalidate() }

// post runs fn on the main thread: through the window's Update when there is a
// window, inline when there is none (an App a test built).
//
// It is the one way a goroutine reaches the view state, so "the fields the view
// reads change only on the main thread" is a structure rather than a
// convention: a load hands back through post, and nothing else assigns them
// from a goroutine.
func (app *App) post(fn func()) {
	if app.win == nil {
		fn()
		return
	}
	app.win.Update(fn)
}

// pageStart is the 0-based index, in the grid's full set, of the row the
// current page shows first. A server-paged browse already holds one page, so
// its start is 0; a client-paged result cuts its window out of the loaded rows.
func (app *App) pageStart() int {
	if app.pager.server || app.pager.page <= 1 {
		return 0
	}
	return (app.pager.page - 1) * app.pager.pageSize
}

// pageEnd is the index one past the page's last row. A server-paged browse
// shows its whole working copy, including rows the user inserted on it; a
// client-paged result shows at most pageSize rows.
func (app *App) pageEnd() int {
	n := app.rowCount()
	if app.pager.server {
		return n
	}
	start := app.pageStart()
	if start >= n {
		return start
	}
	if end := start + app.pager.pageSize; end < n {
		return end
	}
	return n
}

// pageAbsoluteStart is the 0-based index, in the whole result set, of the row
// the current page shows first. A server-paged browse is offset by its page
// number; a client-paged result by its window into the loaded rows.
func (app *App) pageAbsoluteStart() int {
	if app.pager.server {
		return (app.pager.page - 1) * app.pager.pageSize
	}
	return app.pageStart()
}

// pageRowNumber is the number the grid's row column shows for row idx, the
// position it holds in the whole result set. For a server-paged browse idx is
// the index within the page the working copy holds, so the page's offset is
// added; for a client-paged result idx is already absolute.
func (app *App) pageRowNumber(idx int) int {
	if app.pager.server {
		return (app.pager.page-1)*app.pager.pageSize + idx + 1
	}
	return idx + 1
}

// rowsShown is how many rows the grid draws for the current page.
func (app *App) rowsShown() int {
	return app.pageEnd() - app.pageStart()
}

// paged reports whether there is a result set to page through. A statement
// that changed rows (or came back empty) has nothing to page.
func (app *App) paged() bool {
	return app.result != nil && app.rowCount() > 0
}

// setPagerPage moves to p, clamped to the result set's pages. A server-paged
// browse re-reads the table's page; a client-paged result just re-renders.
// While the working copy is dirty a server-paged browse is left alone: paging
// re-reads the table and would throw the unsaved edits away.
func (app *App) setPagerPage(p int) {
	if app.result == nil || app.busy {
		return
	}
	if app.pager.server && app.edit.active && app.edit.dirty() {
		return
	}
	last := app.pager.pages()
	if p < 1 {
		p = 1
	}
	if p > last {
		p = last
	}
	if p == app.pager.page {
		return
	}
	app.pager.page = p
	app.gridSel = -1
	if app.pager.server && app.edit.active {
		app.edit.refresh()
		return
	}
	app.invalidate()
}

func (app *App) pagerFirst() { app.setPagerPage(1) }
func (app *App) pagerPrev()  { app.setPagerPage(app.pager.page - 1) }
func (app *App) pagerNext()  { app.setPagerPage(app.pager.page + 1) }
func (app *App) pagerLast()  { app.setPagerPage(app.pager.pages()) }

// clampPage keeps the shown page within the pages the result has, so a refresh
// that shrinks the set (a commit's deletes) does not leave the pager past the
// end. It returns the page it settled on.
func (app *App) clampPage() int {
	last := app.pager.pages()
	if app.pager.page > last {
		app.pager.page = last
	}
	if app.pager.page < 1 {
		app.pager.page = 1
	}
	return app.pager.page
}

// setPageSize applies the selector's label: the page size changes, so the
// window is recomputed from the first page.
func (app *App) setPageSize(label string) {
	n, err := strconv.Atoi(label)
	if err != nil || n < 1 {
		return
	}
	if n > maxPageSize {
		n = maxPageSize
	}
	if n == app.pager.pageSize {
		return
	}
	app.pager.pageSize = n
	app.pager.page = 1
	app.gridSel = -1
	if app.pager.server && app.edit.active {
		app.edit.refresh()
		return
	}
	app.invalidate()
}

// gotoRow puts the grid's selection on row idx of the full set, moving the
// pager to the page that holds it.
func (app *App) gotoRow(idx int) {
	if idx < 0 {
		idx = 0
	}
	if n := app.rowCount(); idx >= n {
		idx = n - 1
	}
	if !app.pager.server && app.pager.pageSize > 0 {
		app.pager.page = idx/app.pager.pageSize + 1
	}
	app.gridSel = idx - app.pageStart()
}

// pagerShortcuts answers the pager's keys: Alt+Left and Alt+Right walk to the
// previous and the next page, wherever the keyboard focus is (the editor on
// Windows and Linux leaves those keys to the window). It reports whether it
// moved, so the caller can decide what else to do with the frame.
func (app *App) pagerShortcuts(c *ui.Context) bool {
	if !app.paged() || app.busy {
		return false
	}
	moved := false
	if c.Shortcut(ui.Alt, ui.KeyLeft) {
		app.pagerPrev()
		moved = true
	}
	if c.Shortcut(ui.Alt, ui.KeyRight) {
		app.pagerNext()
		moved = true
	}
	return moved
}

// openJump opens the modal that asks which page to show. The pager bar's page
// counter opens it, and it starts from the page shown.
func (app *App) openJump() {
	if !app.paged() || app.busy {
		return
	}
	app.jump = true
	app.jumpErr = ""
	app.jumpNum = float64(app.pager.page)
	app.invalidate()
}

// jumpToPage applies the modal's page number: it clamps nothing, but reports
// why a number is not usable instead of silently moving somewhere else.
func (app *App) jumpToPage(p int) {
	if p < 1 {
		app.jumpErr = "The first page is 1"
		return
	}
	last := app.pager.pages()
	if p > last {
		app.jumpErr = fmt.Sprintf("There are only %d page(s)", last)
		return
	}
	app.setPagerPage(p)
	app.jump = false
}

// jumpView draws the pager's jump-to-page modal, which the page counter opens.
func (app *App) jumpView(c *ui.Context) {
	if !app.jump {
		return
	}
	t := c.Theme()
	ui.Modal(c, &app.jump, func() {
		ui.Column(c).Gap(12).Padding(16).Width(320).
			Background(t.Surface).Radius(10).Border(1, t.Border).
			Children(func() {
				ui.Text(c, "Go to page").FontSize(15).Bold()
				if app.jumpErr != "" {
					ui.Text(c, app.jumpErr).TextColor(t.Danger).Padding(4, 0)
				}
				ui.Form(c, func() {
					ui.Field(c, "Page", func() {
						ui.NumberInput(c, &app.jumpNum, 1, float64(app.pager.pages()), 1)
					})
				})
				ui.Row(c).Gap(8).Children(func() {
					ui.Spacer(c)
					if ui.Button(c, "Cancel").Clicked() {
						app.jump = false
					}
					if ui.PrimaryButton(c, "Go").Clicked() {
						app.jumpToPage(int(app.jumpNum))
					}
				})
			})
	})
}

// pagerBar draws the results pager: the page navigation, the page counter and
// the rows-per-page selector.
func (app *App) pagerBar(c *ui.Context) {
	t := c.Theme()
	if !app.paged() {
		return
	}
	page := app.pager.page
	last := app.pager.pages()
	// No total and a full page: the pager has no last page to jump to.
	unbounded := app.pager.total < 0 && app.pager.more
	// A browse loads segments as it is scrolled: its pager is a "loaded" counter
	// and a Load more, not a page window. A query's result is the window.
	if app.pager.server && app.tab() != nil && app.tab().kind == tabBrowse {
		app.incrementalBar(c, t)
		return
	}
	locked := app.busy || (app.pager.server && app.edit.active && app.edit.dirty())

	ui.Row(c).Gap(4).Padding(4, 8).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
		// 波 98（DataGrip 惯例）：分页控件紧凑化——符号按钮 + 一个页码文本，
		// 不再是五个带长标签的按钮。Page x/y 本身是按钮（点击跳页）。
		if ui.Button(c, "⏮").FontSize(10).Disabled(locked || page <= 1).
			Tooltip("First page (Alt+Left 回退一页)").Clicked() {
			app.pagerFirst()
		}
		if ui.Button(c, "◀").FontSize(10).Disabled(locked || page <= 1).
			Tooltip("Previous page (Alt+Left)").Clicked() {
			app.pagerPrev()
		}
		// The page counter is a button: clicking it asks which page to show.
		pageLabel := fmt.Sprintf("Page %d / %d", page, last)
		if unbounded {
			pageLabel = fmt.Sprintf("Page %d / ?", page)
		}
		if ui.Button(c, pageLabel).Disabled(locked).
			Tooltip("Click to jump to a page").Clicked() {
			app.openJump()
		}
		if ui.Button(c, "▶").FontSize(10).Disabled(locked || page >= last).
			Tooltip("Next page (Alt+Right)").Clicked() {
			app.pagerNext()
		}
		if ui.Button(c, "⏭").FontSize(10).Disabled(locked || unbounded || page >= last).
			Tooltip("Last page").Clicked() {
			app.pagerLast()
		}
		// 波 101 续十一：精确计数按需。浏览打开时不再跑 COUNT(*)，所以这里平时是
		// 未知态；点一下才去数，数完把真实总数交给 pager（页数随之变成确定的）。
		if app.pager.total < 0 && app.db != nil && app.db.SQL != nil {
			if ui.Button(c, "Σ").FontSize(10).Disabled(locked).
				Tooltip("Count the rows exactly (the estimate above costs nothing)").Clicked() {
				app.pagerCountExact()
			}
		}
		// A table too large to count still says how large it is: the catalog's
		// estimate, when the pager has no total to show.
		if app.pager.total < 0 {
			if label := app.estimateLabel(app.tab()); label != "" {
				ui.Text(c, label).TextColor(t.TextMuted).FontSize(10).
					Tooltip("Estimate from the catalog's statistics, not a count").
					SingleLine()
			}
		}
		ui.Spacer(c)
		app.pagerSummary(c, t)
		if ui.Select(c, &app.pageSizeLabel, pageSizeChoices).Width(72).Label("Rows/Page").Changed() {
			app.setPageSize(app.pageSizeLabel)
		}
	})
}

// pagerCountExact asks the server for the exact row count the pager is showing as
// unknown. It is the on-demand half of the interactive-analytics rule: browsing
// never pays a COUNT(*), and a reader who wants the precise figure pays for one
// query, once.
func (app *App) pagerCountExact() {
	if app.db == nil || app.db.SQL == nil {
		return
	}
	tab := app.tab()
	if tab == nil || tab.kind != tabBrowse {
		return
	}
	schema, table := tab.schema, tab.table
	if table == "" {
		return
	}
	db := app.db
	req := &dbcore.TableRowsRequest{
		Page:     1,
		PageSize: app.pager.pageSize,
		Sort:     sortKeys(tab.sortBy),
		Filter:   tab.filter,
	}
	app.beginBusy("count")
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		n, err := db.SQL.BrowseRowsCount(ctx, schema, table, req)
		app.post(func() {
			app.endBusy("count")
			if err != nil {
				app.errMsg = "Count failed: " + err.Error()
				app.status = "Count failed"
				return
			}
			app.pager.total = int(n)
			// A known total makes the estimate redundant.
			app.putEstimate(estimateKey(schema, table), rowEstimate{rows: int64(n), known: true})
			app.status = "Counted " + itoa(int(n)) + " rows"
		})
	}()
}

// incrementalBar is the browse's pager: how much of the table is loaded, and a
// button for the next segment when the reader would rather not scroll.
func (app *App) incrementalBar(c *ui.Context, t *ui.Theme) {
	tab := app.tab()
	loaded := len(app.result.Rows)
	label := "Loaded " + itoa(loaded)
	if app.pager.total >= 0 {
		label = itoa(loaded) + " of " + itoa(app.pager.total) + " rows"
	} else if est := app.estimateLabel(tab); est != "" {
		label = itoa(loaded) + " of " + est
	}
	ui.Row(c).Gap(6).Padding(4, 8).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
		ui.Text(c, label).FontSize(10).TextColor(t.TextMuted).SingleLine()
		ui.Spacer(c)
		if tab != nil && (tab.loadingMore || app.busy) {
			ui.Spinner(c)
		}
		more := tab != nil && tab.moreRows
		if ui.Button(c, "Load More").Disabled(!more || tab != nil && tab.loadingMore || app.busy).
			Tooltip("Fetch the next segment of rows (also loads when you scroll to the end)").
			Clicked() {
			app.loadMoreRows()
		}
		if ui.Select(c, &app.pageSizeLabel, pageSizeChoices).Width(88).Label("Rows/Fetch").Changed() {
			app.setPageSize(app.pageSizeLabel)
			if tab != nil {
				tab.fetchSize = app.pager.pageSize
			}
		}
	})
}

// pagerSummary is the right-hand end of the pager bar: the rows the page shows
// and, when the size is known, how many there are in all.
func (app *App) pagerSummary(c *ui.Context, t *ui.Theme) {
	start, end := app.pageStart(), app.pageEnd()
	abs := app.pageAbsoluteStart()
	count := end - start
	switch {
	case count == 0:
		ui.Text(c, "No rows").TextColor(t.TextMuted).SingleLine()
	case app.pager.total >= 0:
		ui.Textf(c, "Rows %d–%d of %d", abs+1, abs+count, app.pager.total).TextColor(t.TextMuted).SingleLine()
	default:
		ui.Textf(c, "%s", pluralRows(count)).TextColor(t.TextMuted).SingleLine()
	}
	if app.result.Truncated {
		ui.Text(c, " (truncated)").TextColor(t.TextMuted).SingleLine()
	}
}

// statusPageText is the short line the results header shows for a page.
func statusPageText(table string, page, total, shown int) string {
	if total >= 0 {
		return fmt.Sprintf("%s: page %d, %d of %s", table, page, shown, pluralRows(total))
	}
	return fmt.Sprintf("%s: %s", table, pluralRows(shown))
}
