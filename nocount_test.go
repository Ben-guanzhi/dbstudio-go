package main

// 懒计数（波 101 续十一）：浏览打开时不再为第一页付一次全表 COUNT(*)，
// 精确总数由 pager 的 Σ 按需索取。这里钉住"打开不数"与"按需才数"。

import (
	"context"
	"testing"
	"time"

	"github.com/Ben-guanzhi/dbcore"
)

// countingDriver wraps a SQLDriver and records how many counts ran through it.
type countingDriver struct {
	dbcore.SQLDriver
	counts int
	total  uint64
}

func (d *countingDriver) BrowseRows(_ context.Context, _ string, _ string, req *dbcore.TableRowsRequest) (*dbcore.TableRowsResult, error) {
	d.countsIfCounted(req)
	return &dbcore.TableRowsResult{
		Columns: []dbcore.ColumnInfo{{Name: "id", Type: "INTEGER"}},
		Rows:    [][]dbcore.Value{{{Kind: dbcore.ValInt, I: 1}}},
		Total:   d.total, TotalFound: !req.NoCount,
	}, nil
}

func (d *countingDriver) BrowseRowsCount(context.Context, string, string, *dbcore.TableRowsRequest) (uint64, error) {
	d.counts++
	return d.total, nil
}

// countsIfCounted records a count only when the browse was NOT lazy: the kernel
// skips the COUNT for a NoCount request, so a lazy browse must record nothing.
func (d *countingDriver) countsIfCounted(req *dbcore.TableRowsRequest) {
	if !req.NoCount {
		d.counts++
	}
}

// browseApp is an app whose connection speaks to a counting driver.
func browseApp(t *testing.T, total uint64) (*App, *countingDriver) {
	t.Helper()
	app, _ := refreshApp(t)
	d := &countingDriver{SQLDriver: app.db.SQL, total: total}
	app.db.SQL = d
	app.pager = newResultPager()
	app.pager.server = true
	return app, d
}

// settle waits for the browse/count goroutine to finish posting.
func settle(t *testing.T, done func() bool) {
	t.Helper()
	for i := 0; i < 500; i++ {
		if done() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("the async work did not finish")
}

func TestBrowseOpenDoesNotCount(t *testing.T) {
	app, d := browseApp(t, 1000)

	app.browseTable(treeNode{label: "employees", parent: "", kind: nodeTable})
	settle(t, func() bool { return !app.busy && app.result != nil && len(app.result.Rows) > 0 })

	if d.counts != 0 {
		t.Errorf("opening a table ran %d count(s); the first page must not count", d.counts)
	}
	if app.pager.total >= 0 {
		t.Errorf("pager total = %d, want unknown after a lazy open", app.pager.total)
	}
}

func TestExactCountOnDemandFillsTotal(t *testing.T) {
	app, d := browseApp(t, 1000)
	app.browseTable(treeNode{label: "employees", parent: "", kind: nodeTable})
	settle(t, func() bool { return !app.busy && app.result != nil && len(app.result.Rows) > 0 })

	before := d.counts
	app.pagerCountExact()
	settle(t, func() bool { return app.pager.total >= 0 })

	if got := d.counts - before; got != 1 {
		t.Errorf("the Σ button ran %d count(s), want exactly 1", got)
	}
	if app.pager.total != 1000 {
		t.Errorf("pager total = %d, want the counted 1000", app.pager.total)
	}
}
