package main

// The results pane's tabs.
//
// One result set is not always the only one: a script of several statements
// brings back several, and a table browse is a result set of its own. Each one
// gets a tab, and the pane shows the tab chosen. A tab holds what its result
// needs to be shown again exactly as it was left: the rows and columns, the
// pager's window, its error, and, for a table browse, the working copy the
// cells edit.
//
// The App's result fields (app.result, app.pager, app.edit, …) stay the live
// view of the tab on screen. They are copied into a tab when it is left and
// back out of it when it is chosen, through saveTab and loadTab. Since the
// working copy's rows are edited in place, the copy shares them, so the edits a
// user makes show in the tab they belong to without a second pass.

import (
	"fmt"
	"strconv"
	"time"

	"github.com/Ben-guanzhi/dbcore"
	"github.com/egoist/mygo/ui"
)

// tabKind is what a tab holds.
type tabKind int

const (
	tabRun    tabKind = iota // a statement's result
	tabBrowse                // a table's rows, as a working copy
)

// resTab is one result set the results pane can show.
type resTab struct {
	label string
	kind  tabKind

	result *dbcore.QueryResult
	errMsg string

	// pager is the tab's paging window; pageSizeLabel is the rows-per-page
	// selector's choice.
	pager         resultPager
	pageSizeLabel string

	edit editState

	// elapsed is how long the result took to read; status is the one-line
	// report of the statement, for the message panel. sql is the statement the
	// tab ran, and node is the tree node a browse was opened from.
	elapsed time.Duration
	status  string
	sql     string
	node    treeNode

	// pagesLoaded, fetchSize and moreRows are the incremental browse: how many
	// segments of the table have been read, how big a segment is, and whether
	// the table has more. loadingMore is a segment in flight.
	pagesLoaded int
	fetchSize   int
	moreRows    bool
	loadingMore bool

	// colOrder and colHidden are the tab's arrangement of its columns: the order
	// the user put them in, and the ones hidden from view. They are per tab
	// because a column name means different things in two tables.
	colOrder  []string
	colHidden map[string]bool
	// colFrozen are the columns held at the left: the toolkit cannot scroll them
	// in place (its table is one row of cells scrolling together), so freezing
	// locks what it can — the columns stay first and cannot be dragged away or
	// resized, which is what keeps a wide table's key columns where the eye
	// looks for them.
	colFrozen map[string]bool

	// schema and table are the table a browse shows, when the tab is one: the
	// pager asks the catalog for its row estimate by that name.
	schema, table string

	// sortBy is the order the tab's rows are in, as the grid's header knows it:
	// the columns it sorted by, in place. A server-paged browse asks the
	// database for that order; a result the driver loaded in full is ordered
	// here, so switching back to the tab shows the same order.
	sortBy []ui.SortOrder

	// filter is the filter the tab's rows are kept by, as the bar's conditions
	// ask for it: the FilterExpr a server-paged browse sends, or the rows a
	// client-paged result keeps. filterRows is the bar's own draft, kept so
	// switching back shows the conditions as the user left them. raw is the
	// result as the driver returned it, before the filter kept its rows: a
	// client-paged result is filtered and ordered from it again.
	filter     []dbcore.FilterExpr
	filterRows []filterRow
	raw        *dbcore.QueryResult
}

// newTab is a tab at its starting state, before it holds a result.
func newTab(label string, kind tabKind) *resTab {
	return &resTab{
		label:         label,
		kind:          kind,
		pager:         newResultPager(),
		pageSizeLabel: strconv.Itoa(defaultPageSize),
	}
}

// tab returns the tab the pane shows, or nil when there is none.
func (app *App) tab() *resTab {
	if app.tabSel < 0 || app.tabSel >= len(app.tabs) {
		return nil
	}
	return app.tabs[app.tabSel]
}

// tabResult is the result the pane shows, or the tab's own when the view has no
// live copy of it: a page is being read, so the grid and its filter bar stay on
// the rows that were last loaded until the new ones arrive.
func (app *App) tabResult() *dbcore.QueryResult {
	if app.result != nil {
		return app.result
	}
	if t := app.tab(); t != nil {
		return t.result
	}
	return nil
}

// indexOf returns the position of a tab in the list, or -1.
func (app *App) indexOf(t *resTab) int {
	for i, e := range app.tabs {
		if e == t {
			return i
		}
	}
	return -1
}

// findBrowseTab finds the tab already holding a browse of a node.
func (app *App) findBrowseTab(n treeNode) *resTab {
	for _, t := range app.tabs {
		if t.kind == tabBrowse && t.node.kind == nodeTable &&
			t.node.label == n.label && t.node.parent == n.parent {
			return t
		}
	}
	return nil
}

// saveTab copies the live result view into its tab. It is called before the
// view leaves a tab and before a new result replaces one.
func (app *App) saveTab() {
	t := app.tab()
	if t == nil {
		return
	}
	t.result = app.result
	t.errMsg = app.errMsg
	t.pager = app.pager
	t.pageSizeLabel = app.pageSizeLabel
	t.edit = app.edit
	t.sortBy = append(t.sortBy[:0], app.gridSortBy...)
	t.filter = append(t.filter[:0], app.gridFilter...)
	t.filterRows = cloneFilterRows(app.filterDraft)
}

// loadTab makes the live result view show a tab. It is the other half of
// saveTab: together they let the pane move between result sets without the
// grid or the pager knowing a tab is a tab. The grid's header order comes back
// with it, so a tab's arrows show again as they were.
func (app *App) loadTab(t *resTab) {
	app.result = t.result
	app.errMsg = t.errMsg
	app.pager = t.pager
	app.pageSizeLabel = t.pageSizeLabel
	app.edit = t.edit
	app.gridSortBy = append(app.gridSortBy[:0], t.sortBy...)
	if len(app.gridSortBy) > 0 {
		app.gridSort = app.gridSortBy[0] // syncSort keeps this, set when there is one
	} else {
		app.gridSort = ui.SortOrder{}
	}
	app.gridFilter = append(app.gridFilter[:0], t.filter...)
	app.filterDraft = cloneFilterRows(t.filterRows)
}

// showTab loads a tab and makes the grid show it as the app wants: a working
// copy when the tab edits, a plain result otherwise.
//
// It also moves the tab bar's selection: the pane is showing t, so the bar has
// to say so, or a run whose last answer is a later result set would leave the
// bar pointing at the statement's first set while the grid shows the last.
func (app *App) showTab(t *resTab) {
	for i, other := range app.tabs {
		if other == t {
			app.tabSel = i
			break
		}
	}
	app.loadTab(t)
	app.beginEditMode()
	app.invalidate()
}

// beginEditMode makes the grid's cell cursor follow the result on screen: a
// table browse takes the keys of a spreadsheet, a read-only result lets a row
// be chosen.
func (app *App) beginEditMode() {
	if app.edit.active {
		app.grid.CellSelect = true
		app.grid.Selection = &app.gridSelSet
		if app.gridSel < 0 {
			app.gridSel = 0
		}
	} else {
		app.grid.CellSelect = false
		app.grid.Selection = nil
	}
}

// setTab chooses the tab at i, saving the one the view is on first.
func (app *App) setTab(i int) {
	if i < 0 || i >= len(app.tabs) || i == app.tabSel {
		return
	}
	app.saveTab()
	app.tabSel = i
	app.showTab(app.tabs[i])
}

// startRunSQL opens one tab per statement of a script, and chooses the last.
func (app *App) startRunSQL(stmts []string) []*resTab {
	app.saveTab()
	app.dropRuns()
	tabs := make([]*resTab, len(stmts))
	for i, s := range stmts {
		t := newTab("Result "+strconv.Itoa(i+1), tabRun)
		t.sql = s
		app.tabs = append(app.tabs, t)
		tabs[i] = t
	}
	app.tabSel = len(app.tabs) - 1
	app.loadTab(tabs[len(tabs)-1])
	app.beginEditMode()
	app.invalidate()
	return tabs
}

// dropRuns removes the tabs a previous run opened, keeping the browses.
func (app *App) dropRuns() {
	keep := app.tabs[:0]
	for _, t := range app.tabs {
		if t.kind != tabRun {
			keep = append(keep, t)
		}
	}
	app.tabs = keep
	if app.tabSel >= len(app.tabs) {
		if len(app.tabs) > 0 {
			app.tabSel = len(app.tabs) - 1
		} else {
			app.tabSel = -1
		}
	}
}

// clearTabs drops every result set, as disconnecting or opening a connection
// does: the old result cannot belong to the new one.
func (app *App) clearTabs() {
	app.tabs = nil
	app.tabSel = -1
	app.result = nil
	app.errMsg = ""
	app.clearBusy()
	app.pager = newResultPager()
	app.pageSizeLabel = strconv.Itoa(defaultPageSize)
	app.edit.clear()
	app.gridSortBy = app.gridSortBy[:0]
	app.gridSort = ui.SortOrder{}
	app.gridFilter = app.gridFilter[:0]
	app.filterDraft = app.filterDraft[:0]
	app.filterRemove = -1
}

// tabLabels are the labels the tab bar shows. 波 99（DBeaver 惯例，与
// TableProGo/db-dev 同款）：结果页签带行数——切页签前就知道上次查询的规模；
// 写语句显示影响行数，截断带 +。
func (app *App) tabLabels() []string {
	out := make([]string, len(app.tabs))
	for i, t := range app.tabs {
		label := t.label
		if r := t.result; r != nil {
			switch {
			case r.AffectedRows > 0:
				label += fmt.Sprintf(" · %d affected", r.AffectedRows)
			case len(r.Rows) > 0:
				n := len(r.Rows)
				if t.moreRows {
					// A browse's loaded segment is not the whole table.
					label += fmt.Sprintf(" · %d+", n)
				} else if r.Truncated {
					label += fmt.Sprintf(" · %d+", n)
				} else {
					label += fmt.Sprintf(" · %d", n)
				}
			}
		}
		out[i] = label
	}
	return out
}
