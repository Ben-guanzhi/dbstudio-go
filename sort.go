package main

// The results grid's header sorting.
//
// A click on a column's header orders the rows by it, a second click reverses
// them, and a Cmd-click or Shift-click adds the column after the ones already
// there. The framework's table keeps the order in the grid's SortBy and draws
// the arrows; the data is ordered to match, in one of two ways. A server-paged
// browse asks the database for the order, so it survives paging and the total
// stays the table's. A result the driver loaded in full is ordered here, in
// memory, which is the only place it can be ordered at all.

import (
	"math/big"
	"sort"
	"strings"

	"github.com/Ben-guanzhi/dbcore"
	"github.com/egoist/mygo/ui"
)

// sortLocally orders a result the driver loaded in full, in place, and the
// working copy that mirrors it when the grid is editing.
func (app *App) sortLocally(t *resTab) {
	res := t.result
	if res == nil || len(app.gridSortBy) == 0 {
		return
	}
	keys := sortKeys(app.gridSortBy)

	perm := permByRows(len(res.Rows), res.Columns, keys, func(i int) []dbcore.Value {
		return res.Rows[i]
	})
	applyPerm(res.Rows, perm)

	if e := &app.edit; e.active {
		if len(e.rows) == len(res.Rows) {
			// The working copy holds the same rows in the same order, so one
			// order moves both: a commit keeps reading the cells it edited.
			applyPerm(e.rows, perm)
			applyPerm(e.orig, perm)
			return
		}
		// Inserted rows made the working copy longer than the result, so it is
		// ordered by its own cells, which are what the commit reads back.
		eperm := permByRows(len(e.rows), e.cols, keys, func(i int) []dbcore.Value {
			return editRowValues(e.rows[i], e.cols)
		})
		applyPerm(e.rows, eperm)
		applyPerm(e.orig, eperm)
	}
}

// editRowValues turns a working copy's row back into the values its cells hold,
// so the same comparison orders a copy the user has been editing.
func editRowValues(r *editRow, cols []dbcore.ColumnInfo) []dbcore.Value {
	out := make([]dbcore.Value, len(cols))
	for d := range out {
		var v dbcore.Value
		if d < len(r.strs) {
			kind := dbcore.ValText
			if d < len(r.kinds) {
				kind = r.kinds[d]
			} else if d < len(cols) {
				kind = kindOf(cols[d].Type)
			}
			v = parseCell(kind, r.strs[d])
		}
		out[d] = v
	}
	return out
}

// sortKeys turns the grid's header order into the keys a request carries.
func sortKeys(by []ui.SortOrder) []dbcore.SortKey {
	out := make([]dbcore.SortKey, 0, len(by))
	for _, o := range by {
		out = append(out, dbcore.SortKey{Column: o.Column, Desc: o.Descending})
	}
	return out
}

// sortOrderEqual reports whether two header orders name the same columns in the
// same directions.
func sortOrderEqual(a, b []ui.SortOrder) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Column != b[i].Column || a[i].Descending != b[i].Descending {
			return false
		}
	}
	return true
}

// permByRows orders the rows the getter returns and returns the order as a
// permutation, the index each row goes to. It returns nil when there is nothing
// to order.
func permByRows(n int, cols []dbcore.ColumnInfo, keys []dbcore.SortKey, get func(i int) []dbcore.Value) []int {
	if len(keys) == 0 || n <= 1 {
		return nil
	}
	pos := make(map[string]int, len(cols))
	for d, c := range cols {
		pos[c.Name] = d
	}
	perm := make([]int, n)
	for i := range perm {
		perm[i] = i
	}
	sort.SliceStable(perm, func(a, b int) bool {
		ra, rb := get(perm[a]), get(perm[b])
		for _, k := range keys {
			d, ok := pos[k.Column]
			if !ok {
				continue
			}
			c := compareValue(cellAt(ra, d), cellAt(rb, d))
			if c == 0 {
				continue
			}
			if k.Desc {
				return c > 0
			}
			return c < 0
		}
		return false
	})
	return perm
}

// applyPerm moves each element to the place the permutation names.
func applyPerm[T any](rows []T, perm []int) {
	if len(perm) != len(rows) {
		return
	}
	out := make([]T, len(rows))
	for i, p := range perm {
		out[i] = rows[p]
	}
	copy(rows, out)
}

// cellAt reads a row's cell, or the zero value when the row is short of it.
func cellAt(row []dbcore.Value, d int) dbcore.Value {
	if d >= 0 && d < len(row) {
		return row[d]
	}
	return dbcore.Value{}
}

// compareValue orders two cells: NULL last, numbers by their exact value, and
// anything else by its text. A column may hold different kinds across its rows
// (SQLite is dynamically typed), so a number beside text is ordered by the
// number's value, which is what the database's own comparison would do.
func compareValue(a, b dbcore.Value) int {
	switch {
	case a.Kind == dbcore.ValNull || b.Kind == dbcore.ValNull:
		switch {
		case a.Kind == b.Kind:
			return 0
		case a.Kind == dbcore.ValNull:
			return 1
		default:
			return -1
		}
	case isNumericKind(a.Kind) && isNumericKind(b.Kind):
		ra, oka := ratOf(a)
		rb, okb := ratOf(b)
		if oka && okb {
			return ra.Cmp(rb)
		}
	}
	if a.Kind == dbcore.ValTime && b.Kind == dbcore.ValTime {
		return a.T.Compare(b.T)
	}
	if a.Kind == dbcore.ValBool && b.Kind == dbcore.ValBool {
		switch {
		case a.Bool == b.Bool:
			return 0
		case a.Bool:
			return 1
		default:
			return -1
		}
	}
	return strings.Compare(a.String(), b.String())
}

func isNumericKind(k dbcore.ValueKind) bool {
	switch k {
	case dbcore.ValInt, dbcore.ValFloat, dbcore.ValDecimal:
		return true
	}
	return false
}

// ratOf turns a value into an exact rational, so an int64 that float64 cannot
// hold, or a decimal with more digits than a double, still orders correctly.
func ratOf(v dbcore.Value) (*big.Rat, bool) {
	switch v.Kind {
	case dbcore.ValInt:
		return new(big.Rat).SetInt64(v.I), true
	case dbcore.ValFloat:
		r := new(big.Rat).SetFloat64(v.F) // nil for NaN and the infinities
		return r, r != nil
	case dbcore.ValDecimal:
		return new(big.Rat).SetString(strings.TrimSpace(v.S))
	case dbcore.ValText:
		return new(big.Rat).SetString(strings.TrimSpace(v.S))
	case dbcore.ValBlob:
		return new(big.Rat).SetString(strings.TrimSpace(string(v.B)))
	}
	return nil, false
}
