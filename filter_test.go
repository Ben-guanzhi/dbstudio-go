package main

// Tests for the results grid's filter bar: the conditions the bar's draft
// becomes, the rows a client-paged result keeps, and the operators and patterns
// the rows are tested against.

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Ben-guanzhi/dbcore"
	"github.com/egoist/mygo/ui"
)

func filterCols() []dbcore.ColumnInfo {
	return []dbcore.ColumnInfo{
		{Name: "id", Type: "INTEGER"},
		{Name: "name", Type: "TEXT"},
		{Name: "salary", Type: "REAL"},
	}
}

func TestFilterOps(t *testing.T) {
	for _, f := range filterOps {
		if got := filterOpOf(f.label); got != f.op {
			t.Fatalf("filterOpOf(%q) = %q, want %q", f.label, got, f.op)
		}
	}
	if got := filterOpLabel(dbcore.FilterLike); got != "like" {
		t.Fatalf("label = %q, want like", got)
	}
	if got := filterOpLabel(dbcore.FilterIsNotNull); got != "is not null" {
		t.Fatalf("label = %q, want is not null", got)
	}
	if got := filterOpOf("not an operator"); got != dbcore.FilterEq {
		t.Fatalf("unknown label fell back to %q, want =", got)
	}
}

func TestBuildFilter(t *testing.T) {
	cols := filterCols()
	got := buildFilter([]filterRow{
		{column: "id", op: "=", text: "1"},
		{column: "name", op: "like", text: "%a%"},
		{column: "salary", op: ">", text: "100"},
		{column: "id", op: "is null"},
		{column: "id", op: "is not null"},
		// A condition with no text names nothing.
		{column: "name", op: "=", text: "  "},
		// A condition naming a column the result does not hold names nothing.
		{column: "missing", op: "=", text: "1"},
		// An unknown operator label falls back to "=".
		{column: "name", op: "??", text: "Ada"},
	}, cols)

	want := []dbcore.FilterExpr{
		{Column: "id", Op: dbcore.FilterEq, Value: dbcore.Value{Kind: dbcore.ValInt, I: 1}},
		{Column: "name", Op: dbcore.FilterLike, Value: dbcore.Value{Kind: dbcore.ValText, S: "%a%"}},
		{Column: "salary", Op: dbcore.FilterGt, Value: dbcore.Value{Kind: dbcore.ValFloat, F: 100}},
		{Column: "id", Op: dbcore.FilterIsNull},
		{Column: "id", Op: dbcore.FilterIsNotNull},
		{Column: "name", Op: dbcore.FilterEq, Value: dbcore.Value{Kind: dbcore.ValText, S: "Ada"}},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d conditions %v, want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i].Column != want[i].Column || got[i].Op != want[i].Op {
			t.Fatalf("condition %d = %v, want %v", i, got[i], want[i])
		}
		if compareValue(got[i].Value, want[i].Value) != 0 {
			t.Fatalf("condition %d value = %v, want %v", i, got[i].Value, want[i].Value)
		}
	}

	if got := buildFilter(nil, cols); len(got) != 0 {
		t.Fatalf("empty draft gave %v", got)
	}
}

func TestFilterExprsEqual(t *testing.T) {
	eq := dbcore.FilterExpr{Column: "id", Op: dbcore.FilterEq, Value: dbcore.Value{Kind: dbcore.ValInt, I: 1}}
	if !filterExprsEqual(nil, nil) {
		t.Fatal("two empty filters differ")
	}
	if !filterExprsEqual([]dbcore.FilterExpr{}, nil) {
		t.Fatal("empty and nil differ")
	}
	if !filterExprsEqual([]dbcore.FilterExpr{eq}, []dbcore.FilterExpr{eq}) {
		t.Fatal("the same condition differs")
	}
	other := eq
	other.Op = dbcore.FilterGt
	if filterExprsEqual([]dbcore.FilterExpr{eq}, []dbcore.FilterExpr{other}) {
		t.Fatal("different operators are equal")
	}
	other = eq
	other.Value = dbcore.Value{Kind: dbcore.ValInt, I: 2}
	if filterExprsEqual([]dbcore.FilterExpr{eq}, []dbcore.FilterExpr{other}) {
		t.Fatal("different values are equal")
	}
	// A null value has no text to compare, so two nulls are the one condition.
	nullA := dbcore.FilterExpr{Column: "id", Op: dbcore.FilterIsNull}
	nullB := dbcore.FilterExpr{Column: "id", Op: dbcore.FilterIsNull}
	if !filterExprsEqual([]dbcore.FilterExpr{nullA}, []dbcore.FilterExpr{nullB}) {
		t.Fatal("two null conditions differ")
	}
	if len([]dbcore.FilterExpr{nullA, eq}) == len([]dbcore.FilterExpr{nullA}) {
		t.Fatal("test is wrong")
	}
	if filterExprsEqual([]dbcore.FilterExpr{nullA, eq}, []dbcore.FilterExpr{nullA}) {
		t.Fatal("different lengths are equal")
	}
}

func TestMatchFilter(t *testing.T) {
	int := func(x int64) dbcore.Value { return dbcore.Value{Kind: dbcore.ValInt, I: x} }
	null := dbcore.Value{Kind: dbcore.ValNull}
	for _, c := range []struct {
		name string
		cell dbcore.Value
		f    dbcore.FilterExpr
		want bool
	}{
		{"eq", int(2), dbcore.FilterExpr{Column: "n", Op: dbcore.FilterEq, Value: int(2)}, true},
		{"eq no", int(2), dbcore.FilterExpr{Column: "n", Op: dbcore.FilterEq, Value: int(3)}, false},
		{"neq", int(2), dbcore.FilterExpr{Column: "n", Op: dbcore.FilterNeq, Value: int(3)}, true},
		{"gt", int(2), dbcore.FilterExpr{Column: "n", Op: dbcore.FilterGt, Value: int(1)}, true},
		{"gt no", int(2), dbcore.FilterExpr{Column: "n", Op: dbcore.FilterGt, Value: int(2)}, false},
		{"gte", int(2), dbcore.FilterExpr{Column: "n", Op: dbcore.FilterGte, Value: int(2)}, true},
		{"lt", int(2), dbcore.FilterExpr{Column: "n", Op: dbcore.FilterLt, Value: int(3)}, true},
		{"lte", int(2), dbcore.FilterExpr{Column: "n", Op: dbcore.FilterLte, Value: int(2)}, true},
		{"is null", null, dbcore.FilterExpr{Column: "n", Op: dbcore.FilterIsNull}, true},
		{"is null no", int(1), dbcore.FilterExpr{Column: "n", Op: dbcore.FilterIsNull}, false},
		{"is not null", int(1), dbcore.FilterExpr{Column: "n", Op: dbcore.FilterIsNotNull}, true},
		{"is not null no", null, dbcore.FilterExpr{Column: "n", Op: dbcore.FilterIsNotNull}, false},
		// NULL is unknown beside every comparison, so the row is not in the result.
		{"null eq", null, dbcore.FilterExpr{Column: "n", Op: dbcore.FilterEq, Value: int(1)}, false},
		{"null neq", null, dbcore.FilterExpr{Column: "n", Op: dbcore.FilterNeq, Value: int(1)}, false},
		{"null gt", null, dbcore.FilterExpr{Column: "n", Op: dbcore.FilterGt, Value: int(1)}, false},
		{"like", dbcore.Value{Kind: dbcore.ValText, S: "Ada Lovelace"}, dbcore.FilterExpr{Column: "n", Op: dbcore.FilterLike, Value: dbcore.Value{Kind: dbcore.ValText, S: "a%"}}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := matchFilter(c.cell, c.f); got != c.want {
				t.Fatalf("matchFilter(%v, %v) = %v, want %v", c.cell, c.f, got, c.want)
			}
		})
	}
}

func TestLikeMatch(t *testing.T) {
	for _, c := range []struct {
		s, pattern string
		want       bool
	}{
		{"Ada", "Ada", true},
		{"Ada", "Ad", false},
		{"Ada", "a%", true},
		{"Ada", "%a", true},
		{"Ada", "%", true},
		{"Ada", "%%", true},
		{"Ada", "_da", true},
		{"Ada", "_d", false},
		{"Ada", "___", true},
		{"Ada", "____", false},
		{"Ada Lovelace", "%Ada%", true},
		{"Ada Lovelace", "%love%", true},
		{"Ada Lovelace", "%Love%", true}, // ASCII case folding, as SQLite's LIKE
		{"Ada Lovelace", "%Lov%", true},
		{"Ada", "a%a", true},
		{"Ada", "a%b", false},
		{"", "%", true},
		{"", "_", false},
		{"", "", true},
		// A rune outside BMP, matched as one rune and folded to nothing.
		{"café", "%CAF%", true},
		{"café", "caf_", true},
		{"café", "cafe", false},
	} {
		t.Run(c.s+"~"+c.pattern, func(t *testing.T) {
			if got := likeMatch(c.s, c.pattern); got != c.want {
				t.Fatalf("likeMatch(%q, %q) = %v, want %v", c.s, c.pattern, got, c.want)
			}
		})
	}
}

func TestFilterRows(t *testing.T) {
	cols := filterCols()
	rows := vals(t,
		[]dbcore.Value{{Kind: dbcore.ValInt, I: 1}, {Kind: dbcore.ValText, S: "Ada"}, {Kind: dbcore.ValFloat, F: 190000}},
		[]dbcore.Value{{Kind: dbcore.ValInt, I: 2}, {Kind: dbcore.ValText, S: "Alan"}, {Kind: dbcore.ValFloat, F: 185000}},
		[]dbcore.Value{{Kind: dbcore.ValInt, I: 3}, {Kind: dbcore.ValText, S: "Grace"}, {Kind: dbcore.ValNull}},
	)
	// salary > 186000 keeps Ada only.
	got := filterRows(rows, cols, []dbcore.FilterExpr{
		{Column: "salary", Op: dbcore.FilterGt, Value: dbcore.Value{Kind: dbcore.ValFloat, F: 186000}},
	})
	if len(got) != 1 || got[0][1].S != "Ada" {
		t.Fatalf("got %v, want Ada only", got)
	}
	// A NULL salary is unknown, so it is not in a range.
	got = filterRows(rows, cols, []dbcore.FilterExpr{
		{Column: "salary", Op: dbcore.FilterLt, Value: dbcore.Value{Kind: dbcore.ValFloat, F: 188000}},
	})
	if len(got) != 1 || got[0][1].S != "Alan" {
		t.Fatalf("got %v, want Alan only", got)
	}
	// name like '%a%' folds case, so Ada, Alan and Grace match.
	got = filterRows(rows, cols, []dbcore.FilterExpr{
		{Column: "name", Op: dbcore.FilterLike, Value: dbcore.Value{Kind: dbcore.ValText, S: "%a%"}},
	})
	if len(got) != 3 {
		t.Fatalf("got %v, want all three names", got)
	}
	// Every condition must hold.
	got = filterRows(rows, cols, []dbcore.FilterExpr{
		{Column: "name", Op: dbcore.FilterLike, Value: dbcore.Value{Kind: dbcore.ValText, S: "%a%"}},
		{Column: "salary", Op: dbcore.FilterGt, Value: dbcore.Value{Kind: dbcore.ValFloat, F: 186000}},
	})
	if len(got) != 1 || got[0][1].S != "Ada" {
		t.Fatalf("got %v, want Ada only", got)
	}
	// A condition naming a column not in the result keeps nothing.
	got = filterRows(rows, cols, []dbcore.FilterExpr{
		{Column: "missing", Op: dbcore.FilterEq, Value: dbcore.Value{Kind: dbcore.ValInt, I: 1}},
	})
	if len(got) != 0 {
		t.Fatalf("got %v, want nothing", got)
	}
}

func TestCloneFilterRows(t *testing.T) {
	in := []filterRow{{column: "a", op: "=", text: "1"}}
	out := cloneFilterRows(in)
	out[0].column = "b"
	if in[0].column != "a" {
		t.Fatal("the clone shares the slice with its source")
	}
}

func TestFinishFilterBar(t *testing.T) {
	app := &App{filterDraft: []filterRow{{column: "a"}, {column: "b"}, {column: "c"}}, filterRemove: -1}
	app.finishFilterBar()
	if len(app.filterDraft) != 3 {
		t.Fatalf("nothing asked to drop, draft = %v", app.filterDraft)
	}
	app.filterRemove = 1
	app.finishFilterBar()
	if len(app.filterDraft) != 2 || app.filterDraft[0].column != "a" || app.filterDraft[1].column != "c" {
		t.Fatalf("draft = %v, want a and c", app.filterDraft)
	}
	if app.filterRemove != -1 {
		t.Fatal("the drop is not cleared")
	}
	app.filterRemove = 7 // out of range, nothing to drop
	app.finishFilterBar()
	if len(app.filterDraft) != 2 {
		t.Fatalf("out-of-range drop changed the draft: %v", app.filterDraft)
	}
	if app.filterRemove != -1 {
		t.Fatal("an out-of-range drop is not cleared")
	}
}

// TestApplyGridClientFilter is the whole client path: the bar's draft names a
// condition, the result the driver loaded keeps the rows it matches, and the
// tab remembers the filter so switching back to it shows the same rows.
func TestApplyGridClientFilter(t *testing.T) {
	cols := filterCols()
	raw := &dbcore.QueryResult{
		Columns: cols,
		Rows: vals(t,
			[]dbcore.Value{{Kind: dbcore.ValInt, I: 1}, {Kind: dbcore.ValText, S: "Ada"}, {Kind: dbcore.ValFloat, F: 190000}},
			[]dbcore.Value{{Kind: dbcore.ValInt, I: 2}, {Kind: dbcore.ValText, S: "Alan"}, {Kind: dbcore.ValFloat, F: 185000}},
			[]dbcore.Value{{Kind: dbcore.ValInt, I: 3}, {Kind: dbcore.ValText, S: "Grace"}, {Kind: dbcore.ValNull}},
		),
	}
	tb := newTab("q", tabRun)
	tb.result = raw
	tb.raw = raw

	app := &App{result: raw}
	app.tabs = []*resTab{tb}
	app.tabSel = 0
	app.pager = newResultPager()
	app.filterDraft = []filterRow{{column: "salary", op: ">", text: "186000"}}

	app.applyGrid()

	if len(app.result.Rows) != 1 || app.result.Rows[0][1].S != "Ada" {
		t.Fatalf("the result was not filtered: %v", app.result.Rows)
	}
	if len(tb.filter) != 1 || tb.filter[0].Column != "salary" {
		t.Fatalf("the tab forgot the filter: %v", tb.filter)
	}
	if len(app.gridFilter) != 1 || len(tb.filterRows) != 1 || tb.filterRows[0].text != "186000" {
		t.Fatalf("the bar's draft was not kept: %v / %v", app.gridFilter, tb.filterRows)
	}
	// A second frame, unchanged, must not filter again: the rows stay as they
	// are, not rebuilt from raw.
	first := app.result
	app.applyGrid()
	if app.result != first || len(app.result.Rows) != 1 {
		t.Fatalf("an unchanged bar rebuilt the result: %v", app.result.Rows)
	}
}

// TestApplyGridClientRevert is the guard on the client path: a dirty working
// copy keeps its rows, so the bar goes back to the filter they are in.
func TestApplyGridClientRevert(t *testing.T) {
	cols := filterCols()
	raw := &dbcore.QueryResult{
		Columns: cols,
		Rows: vals(t,
			[]dbcore.Value{{Kind: dbcore.ValInt, I: 1}, {Kind: dbcore.ValText, S: "Ada"}},
			[]dbcore.Value{{Kind: dbcore.ValInt, I: 2}, {Kind: dbcore.ValText, S: "Alan"}},
		),
	}
	tb := newTab("q", tabRun)
	tb.result = raw
	tb.raw = raw
	app := &App{result: raw}
	app.tabs = []*resTab{tb}
	app.tabSel = 0
	app.pager = newResultPager()
	app.edit.active = true
	app.edit.addRow([]string{"3", "Grace"}, rowInserted)
	// The bar names a filter the inserted row would be dropped by.
	app.filterDraft = []filterRow{{column: "id", op: "<", text: "2"}}
	tb.filterRows = []filterRow{{column: "id", op: "=", text: "1"}}

	app.applyGrid()

	if len(app.result.Rows) != 2 {
		t.Fatalf("a dirty working copy was filtered: %v", app.result.Rows)
	}
	if len(app.filterDraft) != 1 || app.filterDraft[0].text != "1" {
		t.Fatalf("the bar did not go back to the filter the rows are in: %v", app.filterDraft)
	}
	if len(tb.filter) != 0 {
		t.Fatalf("the tab took the filter: %v", tb.filter)
	}
}

// TestApplyGridClientOrderAndFilter is the two together: the rows the filter
// keeps are ordered by the header, so a client-paged result reads the same way
// a server-paged one does.
func TestApplyGridClientOrderAndFilter(t *testing.T) {
	cols := filterCols()
	raw := &dbcore.QueryResult{
		Columns: cols,
		Rows: vals(t,
			[]dbcore.Value{{Kind: dbcore.ValInt, I: 3}, {Kind: dbcore.ValText, S: "Grace"}},
			[]dbcore.Value{{Kind: dbcore.ValInt, I: 1}, {Kind: dbcore.ValText, S: "Ada"}},
			[]dbcore.Value{{Kind: dbcore.ValInt, I: 2}, {Kind: dbcore.ValText, S: "Alan"}},
		),
	}
	tb := newTab("q", tabRun)
	tb.result = raw
	tb.raw = raw
	app := &App{result: raw}
	app.tabs = []*resTab{tb}
	app.tabSel = 0
	app.pager = newResultPager()
	app.gridSort = ui.SortOrder{Column: "id"}
	app.gridSortBy = []ui.SortOrder{{Column: "id"}}
	app.filterDraft = []filterRow{{column: "id", op: ">", text: "1"}}

	app.applyGrid()

	if len(app.result.Rows) != 2 {
		t.Fatalf("filtered to %v, want 2 rows", app.result.Rows)
	}
	if app.result.Rows[0][0].I != 2 || app.result.Rows[1][0].I != 3 {
		t.Fatalf("rows = %v, want 2 then 3", app.result.Rows)
	}
}

// TestBrowseRowsFilter is the database half of the bar: the FilterExpr a
// server-paged browse sends must reach the query and keep the rows it names, so
// the filter holds across pages.
func TestBrowseRowsFilter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "filter.db")
	seed(t, path)
	db, err := OpenDB(context.Background(), Conn{Name: "filter", Kind: "sqlite", File: path})
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	// The seed's employees table holds Ada (190000), Alan (185000) and, after
	// the inserts below, a row with no salary.
	if _, err := db.SQL.Execute(ctx, `INSERT INTO employees (name) VALUES ('Grace')`); err != nil {
		t.Fatalf("insert: %v", err)
	}

	browse := func(f ...dbcore.FilterExpr) *dbcore.TableRowsResult {
		t.Helper()
		res, err := db.SQL.BrowseRows(ctx, "", "employees", &dbcore.TableRowsRequest{
			Page: 0, PageSize: 100, Filter: f,
		})
		if err != nil {
			t.Fatalf("BrowseRows: %v", err)
		}
		return res
	}

	eq := browse(dbcore.FilterExpr{Column: "name", Op: dbcore.FilterEq, Value: dbcore.Value{Kind: dbcore.ValText, S: "Ada"}})
	if len(eq.Rows) != 1 || eq.Rows[0][1].String() != "Ada" || !eq.TotalFound || eq.Total != 1 {
		t.Fatalf("eq = %v (total %d/%v)", eq.Rows, eq.Total, eq.TotalFound)
	}

	gt := browse(dbcore.FilterExpr{Column: "salary", Op: dbcore.FilterGt, Value: dbcore.Value{Kind: dbcore.ValFloat, F: 186000}})
	if len(gt.Rows) != 1 || gt.Rows[0][1].String() != "Ada" {
		t.Fatalf("gt = %v, want Ada only", gt.Rows)
	}

	like := browse(dbcore.FilterExpr{Column: "name", Op: dbcore.FilterLike, Value: dbcore.Value{Kind: dbcore.ValText, S: "a%"}})
	if len(like.Rows) != 2 {
		t.Fatalf("like = %v, want Ada and Alan", like.Rows)
	}

	isNull := browse(dbcore.FilterExpr{Column: "salary", Op: dbcore.FilterIsNull})
	if len(isNull.Rows) != 1 || isNull.Rows[0][1].String() != "Grace" {
		t.Fatalf("is null = %v, want Grace only", isNull.Rows)
	}
	isNotNull := browse(dbcore.FilterExpr{Column: "salary", Op: dbcore.FilterIsNotNull})
	if len(isNotNull.Rows) != 2 {
		t.Fatalf("is not null = %v, want Ada and Alan", isNotNull.Rows)
	}
}
