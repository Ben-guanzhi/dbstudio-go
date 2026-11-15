package main

// The results grid's row filter bar.
//
// Each condition names a column, an operator and the text to compare the column
// against, and becomes one FilterExpr. A server-paged browse sends the
// expressions to the database, so the filter holds across pages and the total
// counts the rows it leaves; a result the driver loaded in full keeps only the
// rows they match, here in memory. The bar's draft is kept beside the applied
// filter so switching tabs shows the conditions as the user left them.
//
// A condition is asked for only when its user changes a choice or submits a
// value: a server-paged browse re-reads the table, which is not something to do
// for every keystroke, so its value field waits for Enter. A client-paged result
// is filtered here, so its value field answers as it is typed.

import (
	"strings"

	"github.com/Ben-guanzhi/dbcore"
	"github.com/egoist/mygo/ui"
)

// filterOp is one operator the filter bar offers.
type filterOp struct {
	label string
	op    dbcore.FilterOp
}

// filterOps are the operators the bar offers, in the order it draws them.
var filterOps = []filterOp{
	{label: "=", op: dbcore.FilterEq},
	{label: "!=", op: dbcore.FilterNeq},
	{label: ">", op: dbcore.FilterGt},
	{label: ">=", op: dbcore.FilterGte},
	{label: "<", op: dbcore.FilterLt},
	{label: "<=", op: dbcore.FilterLte},
	{label: "like", op: dbcore.FilterLike},
	{label: "is null", op: dbcore.FilterIsNull},
	{label: "is not null", op: dbcore.FilterIsNotNull},
}

// filterOpLabels are the operator labels, for the bar's selector.
var filterOpLabels = func() []string {
	out := make([]string, len(filterOps))
	for i, f := range filterOps {
		out[i] = f.label
	}
	return out
}()

// filterOpOf turns a bar label back into the op it names, or FilterEq when the
// label is not one the bar draws.
func filterOpOf(label string) dbcore.FilterOp {
	for _, f := range filterOps {
		if f.label == label {
			return f.op
		}
	}
	return dbcore.FilterEq
}

// filterOpLabel is the label an op draws as, or "" when the bar has no such op.
func filterOpLabel(op dbcore.FilterOp) string {
	for _, f := range filterOps {
		if f.op == op {
			return f.label
		}
	}
	return ""
}

// filterRow is one condition the filter bar edits.
type filterRow struct {
	column string // the column it names
	op     string // the operator's label
	text   string // the text it compares the column against
}

// applyGrid makes the tab on screen show the rows its header order and filter
// bar name, when that is not already what it shows. It is called after the view
// is built, so a header click or a bar change is answered outside the build that
// drew it. A server-paged browse asks the database for the order and the filter,
// in one read; a result the driver loaded in full is ordered and kept here.
func (app *App) applyGrid() {
	t := app.tab()
	res := app.tabResult()
	if t == nil || app.busy || res == nil {
		return
	}
	exprs := buildFilter(app.filterDraft, res.Columns)
	sortChanged := !sortOrderEqual(app.gridSortBy, t.sortBy)
	filterChanged := !filterExprsEqual(exprs, t.filter)
	if !sortChanged && !filterChanged {
		return
	}
	if app.edit.active && app.edit.dirty() {
		// A dirty working copy holds rows the grid is editing; re-reading the
		// page, ordering it or filtering it would throw the edits away, so the
		// header and the bar go back to what the rows are in.
		app.gridSortBy = append(app.gridSortBy[:0], t.sortBy...)
		if len(app.gridSortBy) > 0 {
			app.gridSort = app.gridSortBy[0]
		} else {
			app.gridSort = ui.SortOrder{}
		}
		app.filterDraft = cloneFilterRows(t.filterRows)
		// The revert is outside the build that drew the bar, so ask for the
		// frame that shows it again.
		app.invalidate()
		return
	}
	app.gridFilter = append(app.gridFilter[:0], exprs...)
	t.filter = append(t.filter[:0], exprs...)
	t.filterRows = cloneFilterRows(app.filterDraft)
	if app.pager.server {
		t.sortBy = append(t.sortBy[:0], app.gridSortBy...)
		app.pager.reset(true) // the order and the filter change which rows are first
		app.gridSel = -1
		app.fetchBrowse(t.node)
		return
	}
	t.sortBy = append(t.sortBy[:0], app.gridSortBy...)
	app.rebuildClient(t)
}

// rebuildClient makes a result the driver loaded in full show the rows its filter
// bar keeps, ordered by the header. It is the only place such a result can be
// filtered or ordered, and it is asked for only while the working copy is clean,
// so the copy is rebuilt from the rows as they now stand.
func (app *App) rebuildClient(t *resTab) {
	res := t.raw
	if res == nil {
		res = t.result
	}
	if res == nil {
		return
	}
	rows := res.Rows
	if len(t.filter) > 0 {
		rows = filterRows(rows, res.Columns, t.filter)
	}
	out := &dbcore.QueryResult{
		Columns:   res.Columns,
		Rows:      rows,
		Truncated: res.Truncated,
		TotalRows: res.TotalRows,
	}
	t.result = out
	app.result = out
	t.pager.total = len(out.Rows)
	app.pager = t.pager
	app.gridSel = -1
	if app.edit.active {
		// The copy mirrors the rows as they now stand; it was clean, so
		// rebuilding it loses nothing.
		app.edit.init(out, app.edit.schema, app.edit.table, app.edit.cols, app.edit.refresh)
	}
	app.sortLocally(t)
	app.invalidate()
}

// buildFilter turns the bar's conditions into the expressions a request carries
// or a row is tested against. A condition with no text, or one naming a column
// the result does not hold, names nothing and is left out: a filter that names
// nothing would hide every row.
func buildFilter(rows []filterRow, cols []dbcore.ColumnInfo) []dbcore.FilterExpr {
	out := make([]dbcore.FilterExpr, 0, len(rows))
	pos := make(map[string]int, len(cols))
	for i, c := range cols {
		pos[c.Name] = i
	}
	for _, r := range rows {
		op := filterOpOf(r.op)
		if op == dbcore.FilterIsNull || op == dbcore.FilterIsNotNull {
			out = append(out, dbcore.FilterExpr{Column: r.column, Op: op})
			continue
		}
		if strings.TrimSpace(r.text) == "" {
			continue
		}
		d, ok := pos[r.column]
		if !ok {
			continue
		}
		out = append(out, dbcore.FilterExpr{
			Column: r.column,
			Op:     op,
			Value:  parseCell(kindOf(cols[d].Type), r.text),
		})
	}
	return out
}

// filterExprsEqual reports whether two filters name the same conditions. A Value
// holds a []byte, so the expressions are compared field by field.
func filterExprsEqual(a, b []dbcore.FilterExpr) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Column != b[i].Column || a[i].Op != b[i].Op {
			return false
		}
		if a[i].Value.Kind != b[i].Value.Kind {
			return false
		}
		if a[i].Value.Kind != dbcore.ValNull && a[i].Value.String() != b[i].Value.String() {
			return false
		}
	}
	return true
}

// filterRows keeps the rows matching every expression, in place.
func filterRows(rows [][]dbcore.Value, cols []dbcore.ColumnInfo, exprs []dbcore.FilterExpr) [][]dbcore.Value {
	out := make([][]dbcore.Value, 0, len(rows))
	for _, row := range rows {
		if rowMatches(row, cols, exprs) {
			out = append(out, row)
		}
	}
	return out
}

// rowMatches reports whether a row satisfies every expression.
func rowMatches(row []dbcore.Value, cols []dbcore.ColumnInfo, exprs []dbcore.FilterExpr) bool {
	for _, f := range exprs {
		d := columnIndex(cols, f.Column)
		if d < 0 || d >= len(row) {
			return false
		}
		if !matchFilter(row[d], f) {
			return false
		}
	}
	return true
}

// matchFilter applies one operator to a cell. A NULL cell matches only the
// null operators: SQL's three-valued logic makes every comparison with it
// unknown, so the row is not in the result.
func matchFilter(v dbcore.Value, f dbcore.FilterExpr) bool {
	switch f.Op {
	case dbcore.FilterIsNull:
		return v.Kind == dbcore.ValNull
	case dbcore.FilterIsNotNull:
		return v.Kind != dbcore.ValNull
	case dbcore.FilterLike:
		return likeMatch(v.String(), f.Value.String())
	}
	if v.Kind == dbcore.ValNull {
		return false
	}
	c := compareValue(v, f.Value)
	switch f.Op {
	case dbcore.FilterEq:
		return c == 0
	case dbcore.FilterNeq:
		return c != 0
	case dbcore.FilterGt:
		return c > 0
	case dbcore.FilterGte:
		return c >= 0
	case dbcore.FilterLt:
		return c < 0
	case dbcore.FilterLte:
		return c <= 0
	}
	return false
}

// columnIndex is the position of a column name in a result's columns, or -1.
func columnIndex(cols []dbcore.ColumnInfo, name string) int {
	for i, c := range cols {
		if c.Name == name {
			return i
		}
	}
	return -1
}

// likeMatch reports whether s matches a SQL LIKE pattern: '%' any run of
// characters, '_' exactly one, everything else equal. ASCII letters compare
// case-insensitively, as SQLite's LIKE does by default.
func likeMatch(s, pattern string) bool {
	r := []rune(s)
	p := []rune(pattern)
	i, j := 0, 0
	star := -1 // the pattern position of the last '%'
	match := 0 // the text position it was last tried at
	for i < len(r) {
		if j < len(p) && (p[j] == '_' || lowerRune(p[j]) == lowerRune(r[i])) {
			i++
			j++
			continue
		}
		if j < len(p) && p[j] == '%' {
			star = j
			match = i
			j++
			continue
		}
		if star >= 0 {
			j = star + 1
			match++
			i = match
			continue
		}
		return false
	}
	for j < len(p) && p[j] == '%' {
		j++
	}
	return j == len(p)
}

// lowerRune folds an ASCII letter to lower case, leaving every other rune as it
// is.
func lowerRune(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r + ('a' - 'A')
	}
	return r
}

// cloneFilterRows copies the bar's conditions, so the tab's copy is not the
// view's slice.
func cloneFilterRows(in []filterRow) []filterRow {
	return append([]filterRow(nil), in...)
}

// filterBar draws the grid's filter bar: one row per condition, a column
// selector, an operator selector and the value to compare against, with a
// button to add a condition and one to clear them all.
func (app *App) filterBar(c *ui.Context, t *ui.Theme) {
	res := app.tabResult()
	if res == nil || len(res.Columns) == 0 {
		return
	}
	cols := make([]string, len(res.Columns))
	for i, col := range res.Columns {
		cols[i] = col.Name
	}
	// A dirty working copy is not re-read to filter, so its bar is locked until
	// it is committed or rolled back, as its sortable headers are.
	locked := app.busy || (app.pager.server && app.edit.active && app.edit.dirty())

	ui.Column(c).Gap(4).Padding(4, 8).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
		for i := range app.filterDraft {
			app.filterRow(c, t, i, cols, locked)
		}
		ui.Row(c).Gap(4).Children(func() {
			if ui.Button(c, "+ Add filter").Disabled(locked).Clicked() {
				app.filterDraft = append(app.filterDraft, filterRow{column: cols[0], op: filterOpLabels[0]})
			}
			if len(app.filterDraft) > 0 && ui.Button(c, "Clear").Disabled(locked).Clicked() {
				app.filterDraft = nil
			}
		})
	})
}

// filterRow draws one condition and answers its choices.
func (app *App) filterRow(c *ui.Context, t *ui.Theme, i int, cols []string, locked bool) {
	r := &app.filterDraft[i]
	local := !app.pager.server
	ui.Row(c).Gap(4).Children(func() {
		ui.Select(c, &r.column, cols).Width(150).Label("Column").Disabled(locked).Changed()
		ui.Select(c, &r.op, filterOpLabels).Width(96).Label("Op").Disabled(locked).Changed()
		if r.op != filterOpLabel(dbcore.FilterIsNull) && r.op != filterOpLabel(dbcore.FilterIsNotNull) {
			if local {
				// A client-paged result is filtered here, so the value answers
				// as it is typed.
				ui.TextInput(c, &r.text).Width(180).Label("Value").Disabled(locked).Changed()
			} else {
				// A server-paged browse re-reads the table, so the value waits
				// for Enter rather than re-reading per keystroke.
				ui.TextInput(c, &r.text).Width(180).Label("Value").Disabled(locked).Submitted()
			}
		}
		if ui.Button(c, "×").Disabled(locked).Clicked() {
			// The rows are being ranged over, so the drop is applied after the
			// build that drew the button.
			app.filterRemove = i
		}
	})
}

// finishFilterBar drops the condition the bar asked to remove. It runs after the
// view is built, because the build that drew the button ranges over the
// conditions and cannot take one out of its own slice.
func (app *App) finishFilterBar() {
	if app.filterRemove < 0 || app.filterRemove >= len(app.filterDraft) {
		app.filterRemove = -1
		return
	}
	app.filterDraft = append(app.filterDraft[:app.filterRemove], app.filterDraft[app.filterRemove+1:]...)
	app.filterRemove = -1
	// The row is gone after the build that drew it, so ask for the frame that
	// shows the bar without it.
	app.invalidate()
}
