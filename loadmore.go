package main

// Loading a table as it is scrolled, instead of a page at a time.
//
// A browse used to fetch one page and stop: reaching the hundredth row meant
// clicking Next, which replaced the grid and threw away the position. Mature
// clients load the next segment when the reader reaches the end, so a table reads
// like a document — that is what this does.
//
// The grid already draws every row the result holds for a server-paged browse
// (pageEnd answers with all of them), so appending is all it takes: the fetch
// asks for the next segment and the rows are added to the same result. The pager
// keeps the segment size and the count loaded; its page controls are for a
// query's client-side window, which is a different question and stays as it was.
//
// Three rules keep it honest:
//
//   - the end is only fetched when the reader is near it (a segment ahead), so a
//     look at the first rows costs one request;
//   - a dirty working copy stops the loading: appending re-reads the rows, and
//     the user's uncommitted edits would go with them;
//   - there is a cap (rowsLoadedMax), because "scroll forever" over a hundred
//     million rows is a memory leak with a progress bar. Past it the grid says so
//     and the reader uses the query editor for the rest.

import (
	"context"
	"time"

	"github.com/Ben-guanzhi/dbcore"
)

// rowsLoadedMax bounds how much of a table the incremental browse holds.
const rowsLoadedMax = 50_000

// loadAhead is how close to the last loaded row the reader must get before the
// next segment is asked for: about a screenful.
const loadAhead = 25

// loadMoreRows appends the next segment of a browse's table. It is a no-op when
// the tab is not a browse, when the table is exhausted, when a fetch is already
// running, or when the grid holds uncommitted edits.
func (app *App) loadMoreRows() {
	t := app.tab()
	if t == nil || t.kind != tabBrowse || t.node.label == "" || t.loadingMore {
		return
	}
	if !app.open || app.db == nil || app.db.SQL == nil || app.busy {
		return
	}
	if !t.moreRows {
		return
	}
	if app.edit.active && app.edit.dirty() {
		// The working copy would lose its edits: stop at what is loaded and let
		// the user commit or roll back.
		t.moreRows = false
		app.status = "Further rows are not loaded while the table has unsaved changes"
		app.invalidate()
		return
	}
	if len(t.result.Rows) >= rowsLoadedMax {
		t.moreRows = false
		app.status = "Stopped at 50,000 rows — use the query editor for the rest"
		app.invalidate()
		return
	}

	t.loadingMore = true
	app.beginBusy("loadmore")
	app.invalidate()

	table := t.node.label
	schema := t.node.parent
	query := schema
	if app.conn.Kind == "sqlite" {
		query = ""
	}
	next := t.pagesLoaded + 1
	size := t.fetchSize
	if size <= 0 {
		size = defaultPageSize
	}
	db := app.db
	// The order and filter the view is in: a segment appended in another order
	// would interleave.
	sortBy := append([]dbcore.SortKey(nil), sortKeys(t.sortBy)...)
	filter := append([]dbcore.FilterExpr(nil), t.filter...)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		res, err := db.SQL.BrowseRows(ctx, query, table, &dbcore.TableRowsRequest{
			Page:     next,
			PageSize: size,
			Sort:     sortBy,
			Filter:   filter,
		})
		app.post(func() {
			t.loadingMore = false
			app.endBusy("loadmore")
			if app.tab() != t {
				return // the reader moved on
			}
			if err != nil {
				t.moreRows = false
				app.errMsg = "Loading more rows failed: " + err.Error()
				app.invalidate()
				return
			}
			app.appendBrowseRows(t, res, size)
		})
	}()
}

// appendBrowseRows adds a fetched segment to the browse tab and keeps the pager
// and the working copy in step with it.
func (app *App) appendBrowseRows(t *resTab, res *dbcore.TableRowsResult, size int) {
	if res == nil || t.result == nil {
		return
	}
	if len(res.Rows) == 0 {
		t.moreRows = false // the table ended between one segment and the next
		app.status = "No more rows"
		app.invalidate()
		return
	}
	t.result.Rows = append(t.result.Rows, res.Rows...)
	t.pagesLoaded++
	t.pager.more = len(res.Rows) >= size && !res.TotalFound
	if res.TotalFound {
		t.pager.total = int(res.Total)
		t.moreRows = len(t.result.Rows) < int(res.Total)
	} else {
		t.moreRows = len(res.Rows) >= size
	}
	if len(t.result.Rows) >= rowsLoadedMax {
		t.moreRows = false
		app.status = "Stopped at 50,000 rows — use the query editor for the rest"
	}
	if app.pager.total >= 0 {
		app.status = statusPageText(t.table, 1, app.pager.total, len(t.result.Rows))
	} else {
		app.status = "Loaded " + itoa(len(t.result.Rows)) + " rows"
	}
	// The working copy holds a copy of the rows: extend it, or it would show the
	// first segment only. A clean copy is re-read, which is cheap and keeps the
	// grid editable.
	if app.edit.active && len(app.edit.rows) > 0 {
		res2 := &dbcore.QueryResult{Columns: t.result.Columns, Rows: t.result.Rows}
		app.edit.init(res2, t.schema, t.table, app.edit.cols, func() { app.browseTable(t.node) })
	}
	app.result = t.result
	app.invalidate()
}

// maybeLoadMore asks for the next segment when the grid has drawn close to the
// end of what is loaded. The drawn rows are the reader's position: a table only
// builds the rows in view, so the largest one seen is how far down the page is.
func (app *App) maybeLoadMore() {
	t := app.tab()
	if t == nil || t.kind != tabBrowse || !t.moreRows || t.loadingMore {
		return
	}
	if app.result == nil {
		return
	}
	if app.lastDrawnRow+loadAhead < len(app.result.Rows) {
		return
	}
	app.loadMoreRows()
}
