package main

// Hiding and reordering a result's columns.
//
// A wide result is unreadable in its natural order: an audit table's useful
// columns are not the first twelve, and a join's duplicated `id` columns get in
// the way. Every mature client lets a user hide what they are not looking at and
// put the rest in the order they think in — and remembers it while the result is
// open, so paging does not undo the arrangement.
//
// The arrangement is per result tab, not global: the same column name means
// different things in two tables, and hiding `id` in one must not hide it in the
// other. Within a tab it survives paging, sorting, filtering and editing (the
// tab keeps its own order and hidden set).
//
// Reordering rides the framework's own layout (ListState.Columns.Order), so a
// header drag and the Columns dialog write the same place; hiding is ours,
// because the framework draws every column it is given.

import (
	"sort"

	"github.com/dbcore/dbcore"
	"github.com/egoist/mygo/ui"
)

// visibleColumns is the result's columns the tab shows, as indexes into
// res.Columns, in the tab's order: the tab's order first (only the names that
// still exist), then everything else in the result's own order, minus the hidden
// ones.
func (app *App) visibleColumns(t *resTab, res *dbcore.QueryResult) []int {
	if res == nil {
		return nil
	}
	// A result can be drawn without a tab (a fixture, or a frame before the tab
	// is attached): no arrangement then, which is every column in its order.
	var order []string
	var hidden map[string]bool
	if t != nil {
		order, hidden = t.colOrder, t.colHidden
	}
	out := make([]int, 0, len(res.Columns))
	seen := make(map[int]bool, len(res.Columns))
	for _, name := range order {
		if i := indexOfColumn(res, name); i >= 0 && !seen[i] {
			seen[i] = true
			if !hidden[name] {
				out = append(out, i)
			}
		}
	}
	for i, col := range res.Columns {
		if seen[i] || hidden[col.Name] {
			continue
		}
		out = append(out, i)
	}
	return out
}

// indexOfColumn finds a column by name.
func indexOfColumn(res *dbcore.QueryResult, name string) int {
	for i, c := range res.Columns {
		if c.Name == name {
			return i
		}
	}
	return -1
}

// visibleResult is the result as the grid shows it: the tab's visible columns,
// in the tab's order. Exporting and copying go through it, so a file holds what
// the user is looking at rather than the columns they hid and the order they
// moved away from. When nothing is hidden or moved it answers the result itself,
// with no copy.
func (app *App) visibleResult(res *dbcore.QueryResult, t *resTab) *dbcore.QueryResult {
	if res == nil {
		return nil
	}
	visible := app.visibleColumns(t, res)
	same := len(visible) == len(res.Columns)
	if same {
		for i, v := range visible {
			if v != i {
				same = false
				break
			}
		}
	}
	if same {
		return res
	}
	out := &dbcore.QueryResult{
		Truncated:    res.Truncated,
		TotalRows:    res.TotalRows,
		AffectedRows: res.AffectedRows,
		LastInsertID: res.LastInsertID,
	}
	out.Columns = make([]dbcore.ColumnInfo, len(visible))
	for i, v := range visible {
		out.Columns[i] = res.Columns[v]
	}
	out.Rows = make([][]dbcore.Value, len(res.Rows))
	for r, row := range res.Rows {
		cells := make([]dbcore.Value, len(visible))
		for i, v := range visible {
			if v < len(row) {
				cells[i] = row[v]
			}
		}
		out.Rows[r] = cells
	}
	return out
}

// t2col maps a drawn column (0 is the row marker) to the result's column index,
// or -1 for the marker and for an out-of-range position.
func t2col(visible []int, col int) int {
	if col <= 0 || col-1 >= len(visible) {
		return -1
	}
	return visible[col-1]
}

// headerMenu is a result column's context menu: hiding, showing, and moving.
// It works on the result's column index.
func (app *App) headerMenu(m *ui.Menu, col int) {
	t := app.tab()
	res := app.tabResult()
	if t == nil || res == nil || col < 0 || col >= len(res.Columns) {
		return
	}
	name := res.Columns[col].Name
	if m.Item("Hide This Column").Chosen() {
		app.hideColumn(t, name, true)
	}
	if m.Item("Show All Columns").Chosen() {
		app.showAllColumns(t)
	}
	if m.Item("Move Left").Chosen() {
		app.moveColumn(t, res, name, -1)
	}
	if m.Item("Move Right").Chosen() {
		app.moveColumn(t, res, name, +1)
	}
	// Freezing: the visible columns up to this one stay first and locked.
	if pos := app.visiblePosition(t, res, col); pos >= 0 {
		if m.Item("Freeze Columns to Here").Chosen() {
			app.freezeTo(t, res, pos)
		}
	}
	if m.Item("Unfreeze Columns").Chosen() {
		app.unfreeze(t)
	}
	if m.Item("Columns…").Chosen() {
		app.openColumns()
	}
	if m.Item("Fit to Content").Chosen() {
		// Releasing the width lets the grid size the column from its cells, as
		// the pager's own double-click does.
		app.forgetWidth(name)
	}
}

// visiblePosition is the drawn position of a result column, or -1 when it is
// hidden.
func (app *App) visiblePosition(t *resTab, res *dbcore.QueryResult, col int) int {
	for pos, i := range app.visibleColumns(t, res) {
		if i == col {
			return pos
		}
	}
	return -1
}

// layoutKey is a browse's table identity, "" when the tab is not a browse of a
// table (an arbitrary query has no stable name to remember it by).
func layoutKey(t *resTab) string {
	if t == nil || t.table == "" {
		return ""
	}
	return t.schema + "\x00" + t.table
}

// applyColumnLayout gives a new browse tab the arrangement its table was last
// left in.
func (app *App) applyColumnLayout(t *resTab) {
	key := layoutKey(t)
	if key == "" {
		return
	}
	if l, ok := app.colLayouts[key]; ok {
		t.colOrder = append([]string(nil), l.Order...)
		if len(l.Hidden) > 0 {
			t.colHidden = make(map[string]bool, len(l.Hidden))
			for _, name := range l.Hidden {
				t.colHidden[name] = true
			}
		}
		if len(l.Frozen) > 0 {
			t.colFrozen = make(map[string]bool, len(l.Frozen))
			for _, name := range l.Frozen {
				t.colFrozen[name] = true
			}
		}
	}
}

// saveColumnLayout records a browse tab's arrangement and writes the session, so
// the next visit to the table opens the way it was left. A tab without a table
// has nowhere to remember it by, and keeps its arrangement for its own lifetime.
func (app *App) saveColumnLayout(t *resTab) {
	key := layoutKey(t)
	if key == "" {
		return
	}
	l := colLayout{Order: append([]string(nil), t.colOrder...)}
	for name := range t.colHidden {
		l.Hidden = append(l.Hidden, name)
	}
	sort.Strings(l.Hidden)
	for name := range t.colFrozen {
		l.Frozen = append(l.Frozen, name)
	}
	sort.Strings(l.Frozen)
	// A drag changes the order many times in a row; writing only when the
	// arrangement actually differs keeps that to one write per real change.
	if old, ok := app.colLayouts[key]; ok && sameLayout(old, l) {
		return
	}
	if app.colLayouts == nil {
		app.colLayouts = map[string]colLayout{}
	}
	app.colLayouts[key] = l
	app.saveSession()
}

// sameLayout compares two arrangements.
func sameLayout(a, b colLayout) bool {
	return slicesEqual(a.Order, b.Order) && slicesEqual(a.Hidden, b.Hidden) &&
		slicesEqual(a.Frozen, b.Frozen)
}

// hideColumn hides or shows one column of the tab.
func (app *App) hideColumn(t *resTab, name string, hidden bool) {
	if t.colHidden == nil {
		t.colHidden = map[string]bool{}
	}
	if hidden {
		// The last visible column stays: a grid of nothing is not a grid.
		if len(app.visibleColumns(t, t.result)) <= 1 {
			app.status = "The last column cannot be hidden"
			app.invalidate()
			return
		}
		t.colHidden[name] = true
		app.status = "Hidden " + name
	} else {
		delete(t.colHidden, name)
		app.status = "Showing " + name
	}
	app.saveColumnLayout(t)
	app.invalidate()
}

// showAllColumns drops the tab's hidden set.
func (app *App) showAllColumns(t *resTab) {
	t.colHidden = nil
	app.status = "All columns shown"
	app.saveColumnLayout(t)
	app.invalidate()
}

// moveColumn moves a column one place in the tab's order.
func (app *App) moveColumn(t *resTab, res *dbcore.QueryResult, name string, delta int) {
	if t.frozen(name) {
		app.status = name + " is frozen: unfreeze the columns to move it"
		app.invalidate()
		return
	}
	order := app.columnOrder(t, res) // the full order, hidden ones included
	at := -1
	for i, n := range order {
		if n == name {
			at = i
			break
		}
	}
	if at < 0 {
		return
	}
	to := at + delta
	if to < 0 || to >= len(order) {
		return
	}
	order[at], order[to] = order[to], order[at]
	t.colOrder = order
	app.status = "Moved " + name
	app.saveColumnLayout(t)
	app.invalidate()
}

// columnOrder is the tab's full column order (hidden names included), starting
// from whatever the tab has and filling in the result's own order.
func (app *App) columnOrder(t *resTab, res *dbcore.QueryResult) []string {
	if res == nil {
		return nil
	}
	var order []string
	if t != nil {
		order = t.colOrder
	}
	out := make([]string, 0, len(res.Columns))
	seen := make(map[string]bool, len(res.Columns))
	for _, name := range order {
		if indexOfColumn(res, name) >= 0 && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	for _, c := range res.Columns {
		if !seen[c.Name] {
			seen[c.Name] = true
			out = append(out, c.Name)
		}
	}
	return out
}

// frozen reports whether a column is held at the left.
func (t *resTab) frozen(name string) bool { return t != nil && t.colFrozen[name] }

// freezeTo holds the visible columns up to and including pos (a drawn position,
// 0-based over the visible columns) at the left, in the order they are in now.
func (app *App) freezeTo(t *resTab, res *dbcore.QueryResult, pos int) {
	visible := app.visibleColumns(t, res)
	if pos < 0 || pos >= len(visible) {
		return
	}
	// The frozen columns are first in the order, so the lock is visible: a
	// frozen column that sat in the middle could not be dragged anyway, and the
	// order would not say why.
	frozen := make([]string, 0, pos+1)
	for i := 0; i <= pos; i++ {
		frozen = append(frozen, res.Columns[visible[i]].Name)
	}
	rest := make([]string, 0, len(res.Columns))
	for _, name := range app.columnOrder(t, res) {
		if !containsString(frozen, name) {
			rest = append(rest, name)
		}
	}
	t.colOrder = append(frozen, rest...)
	if t.colFrozen == nil {
		t.colFrozen = map[string]bool{}
	}
	for _, name := range frozen {
		t.colFrozen[name] = true
	}
	app.status = "Frozen " + itoa(len(frozen)) + " column(s) at the left"
	app.saveColumnLayout(t)
	app.invalidate()
}

// unfreeze drops every frozen column.
func (app *App) unfreeze(t *resTab) {
	t.colFrozen = nil
	app.status = "Columns unfrozen"
	app.saveColumnLayout(t)
	app.invalidate()
}

// containsString reports whether a slice holds a value.
func containsString(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// forgetWidth drops a column's remembered width, so it sizes to its content
// again. The framework owns the width table; clearing the entry is enough.
func (app *App) forgetWidth(name string) {
	if app.grid.Columns.Widths == nil {
		return
	}
	delete(app.grid.Columns.Widths, name)
	app.status = "Fitted " + name
	app.invalidate()
}

// columnsModal is the Columns dialog's state: the tab it edits, and its open
// flag.
type columnsModal struct {
	open bool
}

// openColumns shows the Columns dialog for the tab on screen.
func (app *App) openColumns() {
	if app.tabResult() == nil {
		app.status = "Nothing to arrange"
		app.invalidate()
		return
	}
	app.columns = columnsModal{open: true}
	app.invalidate()
}

// columnsView draws the Columns dialog: every column of the result in its
// order, a checkbox for whether it shows, and buttons to move or reset. The
// dialog is how a column hidden by a stray click comes back, and how a wide
// result is arranged without hunting for each header.
func (app *App) columnsView(c *ui.Context) {
	cm := &app.columns
	if !cm.open {
		return
	}
	t := c.Theme()
	tab := app.tab()
	res := app.tabResult()
	if tab == nil || res == nil {
		cm.open = false
		return
	}
	order := app.columnOrder(tab, res)
	ui.Modal(c, &cm.open, func() {
		ui.Column(c).Gap(8).Padding(12).Width(520).Height(520).
			Background(t.Surface).Radius(10).Border(1, t.Border).
			Children(func() {
				ui.Text(c, "Columns").FontSize(13).Bold()
				ui.Scroll(c).Grow(1).FillWidth().Children(func() {
					ui.Column(c).Gap(3).Children(func() {
						for _, name := range order {
							name := name
							shown := !tab.colHidden[name]
							ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
								if ui.Checkbox(c, &shown, name).Changed() {
									app.hideColumn(tab, name, !shown)
								}
								ui.Spacer(c)
								if ui.Button(c, "↑").FontSize(10).Tooltip("Move up").Clicked() {
									app.moveColumn(tab, res, name, -1)
								}
								if ui.Button(c, "↓").FontSize(10).Tooltip("Move down").Clicked() {
									app.moveColumn(tab, res, name, +1)
								}
							})
						}
					})
				})
				ui.Row(c).Gap(8).Children(func() {
					if ui.Button(c, "Show All").Clicked() {
						app.showAllColumns(tab)
					}
					if ui.Button(c, "Reset Order").Clicked() {
						tab.colOrder = nil
						app.status = "Column order reset"
						app.saveColumnLayout(tab)
						app.invalidate()
					}
					ui.Spacer(c)
					if ui.PrimaryButton(c, "Close").Clicked() {
						cm.open = false
					}
				})
			})
	})
}
