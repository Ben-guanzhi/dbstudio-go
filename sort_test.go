package main

// Tests for the results grid's header sorting: the order two cells compare in,
// the permutation that orders a set of rows, and the in-place reorder of a
// result and its working copy.

import (
	"testing"
	"time"

	"github.com/dbcore/dbcore"
	"github.com/egoist/mygo/ui"
)

func vals(t *testing.T, rows ...[]dbcore.Value) [][]dbcore.Value {
	t.Helper()
	return rows
}

func TestCompareValue(t *testing.T) {
	when1 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	when2 := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		name string
		a, b dbcore.Value
		want int
	}{
		{"ints", dbcore.Value{Kind: dbcore.ValInt, I: 1}, dbcore.Value{Kind: dbcore.ValInt, I: 2}, -1},
		{"int equal", dbcore.Value{Kind: dbcore.ValInt, I: 2}, dbcore.Value{Kind: dbcore.ValInt, I: 2}, 0},
		{"float less", dbcore.Value{Kind: dbcore.ValFloat, F: 1.5}, dbcore.Value{Kind: dbcore.ValFloat, F: 2}, -1},
		// A float and a decimal of the same value are one value, exactly.
		{"float decimal equal", dbcore.Value{Kind: dbcore.ValFloat, F: 1.5}, dbcore.DecimalValue("1.5"), 0},
		// An int64 too big for a float64, against the float that rounds to it.
		{"big int vs float", dbcore.Value{Kind: dbcore.ValInt, I: 1 << 62}, dbcore.Value{Kind: dbcore.ValFloat, F: 4611686018427387904}, 0},
		{"big int vs smaller float", dbcore.Value{Kind: dbcore.ValInt, I: 1 << 62}, dbcore.Value{Kind: dbcore.ValFloat, F: 4.6e18}, 1},
		{"text", dbcore.Value{Kind: dbcore.ValText, S: "a"}, dbcore.Value{Kind: dbcore.ValText, S: "b"}, -1},
		{"null last", dbcore.Value{Kind: dbcore.ValInt, I: 1}, dbcore.Value{Kind: dbcore.ValNull}, -1},
		{"null after text", dbcore.Value{Kind: dbcore.ValText, S: "z"}, dbcore.Value{Kind: dbcore.ValNull}, -1},
		{"two nulls", dbcore.Value{Kind: dbcore.ValNull}, dbcore.Value{Kind: dbcore.ValNull}, 0},
		{"times", dbcore.Value{Kind: dbcore.ValTime, T: when1}, dbcore.Value{Kind: dbcore.ValTime, T: when2}, -1},
		{"bools", dbcore.Value{Kind: dbcore.ValBool, Bool: false}, dbcore.Value{Kind: dbcore.ValBool, Bool: true}, -1},
		// SQLite puts text and numbers in one column: a number beside text is
		// ordered by the number's value, not the width of its string.
		{"number before text", dbcore.Value{Kind: dbcore.ValInt, I: 1}, dbcore.Value{Kind: dbcore.ValText, S: "10"}, -1},
		// Two texts order by their strings, as a TEXT column does.
		{"text is lexicographic", dbcore.Value{Kind: dbcore.ValText, S: "2"}, dbcore.Value{Kind: dbcore.ValText, S: "10"}, 1},
		{"unparseable text", dbcore.Value{Kind: dbcore.ValText, S: "abc"}, dbcore.Value{Kind: dbcore.ValText, S: "abd"}, -1},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := compareValue(c.a, c.b); got != c.want {
				t.Fatalf("compareValue(%v, %v) = %d, want %d", c.a, c.b, got, c.want)
			}
			// The order is symmetric in sign.
			if got := compareValue(c.b, c.a); got != -c.want {
				t.Fatalf("reverse = %d, want %d", got, -c.want)
			}
		})
	}
}

func TestPermByRows(t *testing.T) {
	cols := []dbcore.ColumnInfo{{Name: "n"}, {Name: "s"}}
	ints := func(xs ...int64) [][]dbcore.Value {
		out := make([][]dbcore.Value, len(xs))
		for i, x := range xs {
			out[i] = []dbcore.Value{{Kind: dbcore.ValInt, I: x}}
		}
		return out
	}
	for _, c := range []struct {
		name string
		rows [][]dbcore.Value
		keys []dbcore.SortKey
		want [][]dbcore.Value
	}{
		{
			name: "ascending",
			rows: ints(3, 1, 2),
			keys: []dbcore.SortKey{{Column: "n"}},
			want: ints(1, 2, 3),
		},
		{
			name: "descending",
			rows: ints(3, 1, 2),
			keys: []dbcore.SortKey{{Column: "n", Desc: true}},
			want: ints(3, 2, 1),
		},
		{
			name: "nulls last",
			rows: vals(t,
				[]dbcore.Value{{Kind: dbcore.ValInt, I: 1}},
				[]dbcore.Value{{Kind: dbcore.ValNull}},
				[]dbcore.Value{{Kind: dbcore.ValInt, I: 2}}),
			keys: []dbcore.SortKey{{Column: "n"}},
			want: vals(t,
				[]dbcore.Value{{Kind: dbcore.ValInt, I: 1}},
				[]dbcore.Value{{Kind: dbcore.ValInt, I: 2}},
				[]dbcore.Value{{Kind: dbcore.ValNull}}),
		},
		{
			name: "two keys",
			rows: vals(t,
				[]dbcore.Value{{Kind: dbcore.ValInt, I: 1}, {Kind: dbcore.ValText, S: "b"}},
				[]dbcore.Value{{Kind: dbcore.ValInt, I: 1}, {Kind: dbcore.ValText, S: "a"}},
				[]dbcore.Value{{Kind: dbcore.ValInt, I: 0}, {Kind: dbcore.ValText, S: "z"}}),
			keys: []dbcore.SortKey{{Column: "n"}, {Column: "s"}},
			want: vals(t,
				[]dbcore.Value{{Kind: dbcore.ValInt, I: 0}, {Kind: dbcore.ValText, S: "z"}},
				[]dbcore.Value{{Kind: dbcore.ValInt, I: 1}, {Kind: dbcore.ValText, S: "a"}},
				[]dbcore.Value{{Kind: dbcore.ValInt, I: 1}, {Kind: dbcore.ValText, S: "b"}}),
		},
		{
			name: "first key reverses",
			rows: vals(t,
				[]dbcore.Value{{Kind: dbcore.ValInt, I: 1}, {Kind: dbcore.ValText, S: "a"}},
				[]dbcore.Value{{Kind: dbcore.ValInt, I: 1}, {Kind: dbcore.ValText, S: "b"}}),
			keys: []dbcore.SortKey{{Column: "n", Desc: true}, {Column: "s"}},
			want: vals(t,
				[]dbcore.Value{{Kind: dbcore.ValInt, I: 1}, {Kind: dbcore.ValText, S: "b"}},
				[]dbcore.Value{{Kind: dbcore.ValInt, I: 1}, {Kind: dbcore.ValText, S: "a"}}),
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			perm := permByRows(len(c.rows), cols, c.keys, func(i int) []dbcore.Value { return c.rows[i] })
			applyPerm(c.rows, perm)
			for i := range c.rows {
				if compareValue(c.rows[i][0], c.want[i][0]) != 0 {
					t.Fatalf("row %d = %v, want %v", i, c.rows[i][0], c.want[i][0])
				}
			}
		})
	}
}

func TestSortLocally(t *testing.T) {
	cols := []dbcore.ColumnInfo{{Name: "n"}, {Name: "s"}}
	res := &dbcore.QueryResult{
		Columns: cols,
		Rows: vals(t,
			[]dbcore.Value{{Kind: dbcore.ValInt, I: 3}, {Kind: dbcore.ValText, S: "c"}},
			[]dbcore.Value{{Kind: dbcore.ValInt, I: 1}, {Kind: dbcore.ValText, S: "a"}},
			[]dbcore.Value{{Kind: dbcore.ValInt, I: 2}, {Kind: dbcore.ValText, S: "b"}}),
	}
	tb := newTab("q", tabRun)
	tb.result = res

	app := &App{}
	app.tabs = []*resTab{tb}
	app.tabSel = 0
	app.gridSortBy = []ui.SortOrder{{Column: "n"}}
	app.sortLocally(tb)

	want := []int64{1, 2, 3}
	for i, row := range res.Rows {
		if row[0].I != want[i] {
			t.Fatalf("row %d = %d, want %d", i, row[0].I, want[i])
		}
	}
}

// TestSortLocallyEditCopy checks that ordering a result the grid is editing
// moves the working copy with the result, so a commit still reads the cells the
// user edited.
func TestSortLocallyEditCopy(t *testing.T) {
	cols := []dbcore.ColumnInfo{{Name: "n", Type: "INTEGER"}, {Name: "s", Type: "TEXT"}}
	res := &dbcore.QueryResult{
		Columns: cols,
		Rows: vals(t,
			[]dbcore.Value{{Kind: dbcore.ValInt, I: 3}, {Kind: dbcore.ValText, S: "c"}},
			[]dbcore.Value{{Kind: dbcore.ValInt, I: 1}, {Kind: dbcore.ValText, S: "a"}},
			[]dbcore.Value{{Kind: dbcore.ValInt, I: 2}, {Kind: dbcore.ValText, S: "b"}}),
	}
	tb := newTab("q", tabRun)
	tb.result = res
	tb.edit.init(res, "", "t", nil, nil)
	// One cell the user edited, so the row is dirty after the reorder.
	tb.edit.rows[0].strs[1] = "edited"
	tb.edit.rows[0].state = rowModified

	app := &App{edit: tb.edit}
	app.tabs = []*resTab{tb}
	app.tabSel = 0
	app.gridSortBy = []ui.SortOrder{{Column: "n"}}
	app.sortLocally(tb)

	if res.Rows[0][0].I != 1 || res.Rows[2][0].I != 3 {
		t.Fatalf("result not ordered: %d..%d", res.Rows[0][0].I, res.Rows[2][0].I)
	}
	// The edited row (n = 3) moved to the end with its result row, and is
	// still the same edit.
	if tb.edit.rows[2].strs[0] != "3" || tb.edit.rows[2].strs[1] != "edited" {
		t.Fatalf("working copy not ordered with the result: %+v", tb.edit.rows[2].strs)
	}
	if tb.edit.rows[2].state != rowModified {
		t.Fatal("the edit was lost in the reorder")
	}
}

// TestSortLocallyInsertedRows covers the case where the working copy is longer
// than the result, as after a row insert: the copy orders by its own cells.
func TestSortLocallyInsertedRows(t *testing.T) {
	cols := []dbcore.ColumnInfo{{Name: "n", Type: "INTEGER"}}
	res := &dbcore.QueryResult{
		Columns: cols,
		Rows: vals(t,
			[]dbcore.Value{{Kind: dbcore.ValInt, I: 3}},
			[]dbcore.Value{{Kind: dbcore.ValInt, I: 1}}),
	}
	tb := newTab("q", tabRun)
	tb.result = res
	tb.edit.init(res, "", "t", nil, nil)
	tb.edit.addRow([]string{"0"}, rowInserted)

	app := &App{edit: tb.edit}
	app.tabs = []*resTab{tb}
	app.tabSel = 0
	app.gridSortBy = []ui.SortOrder{{Column: "n"}}
	app.sortLocally(tb)

	// The inserted row sorts first, and the result is ordered as well.
	if tb.edit.rows[0].strs[0] != "0" || tb.edit.rows[2].strs[0] != "3" {
		t.Fatalf("inserted row not ordered with the rest: %+v", [][]string{
			tb.edit.rows[0].strs, tb.edit.rows[1].strs, tb.edit.rows[2].strs})
	}
	if res.Rows[0][0].I != 1 || res.Rows[1][0].I != 3 {
		t.Fatalf("result rows not ordered: %v", res.Rows)
	}
}

func TestSortOrderEqual(t *testing.T) {
	o := func(c string, d bool) ui.SortOrder { return ui.SortOrder{Column: c, Descending: d} }
	if !sortOrderEqual([]ui.SortOrder{o("a", false)}, []ui.SortOrder{o("a", false)}) {
		t.Fatal("same order not equal")
	}
	if sortOrderEqual([]ui.SortOrder{o("a", false)}, []ui.SortOrder{o("a", true)}) {
		t.Fatal("different direction is equal")
	}
	if sortOrderEqual([]ui.SortOrder{o("a", false)}, []ui.SortOrder{o("b", false)}) {
		t.Fatal("different column is equal")
	}
	if sortOrderEqual([]ui.SortOrder{o("a", false)}, []ui.SortOrder{o("a", false), o("b", false)}) {
		t.Fatal("different length is equal")
	}
	if !sortOrderEqual(nil, nil) {
		t.Fatal("two empty orders are equal")
	}
}
