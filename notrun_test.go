package main

// 多语句脚本的失败/取消语义（§335）：失败语句之后的 tab 从未执行——
// "Query done" 会让人以为它们跑过了。测试锁住 "Not run" 状态。

import (
	"errors"
	"strings"
	"testing"

	"github.com/Ben-guanzhi/dbcore"
)

func TestApplyRunMarksUnrunTabs(t *testing.T) {
	app := &App{pager: newResultPager(), gridSel: -1}
	app.initModalEditors()
	app.runGen = 1 // the run this applyRun reports for
	tab := func() *resTab { return newTab("t", tabRun) }
	tabs := []*resTab{tab(), tab(), tab()}
	stmts := []string{"SELECT 1", "SELECT bad", "SELECT 3"}
	out := []runOut{
		{sets: []*dbcore.QueryResult{{Columns: []dbcore.ColumnInfo{{Name: "a"}}}}},
		{err: errors.New("statement 2: boom")},
		{notRun: true},
	}
	app.applyRun(1, tabs, stmts, out, 0, false, false)
	if tabs[0].status == "Not run" {
		t.Fatal("statement 1 ran and must not be marked Not run")
	}
	if !strings.Contains(tabs[1].errMsg, "statement 2") {
		t.Fatalf("failed tab lost its error: %q", tabs[1].errMsg)
	}
	if tabs[2].status != "Not run" {
		t.Fatalf("statement 3 never ran: status = %q", tabs[2].status)
	}
}

func TestApplyRunCancelledTabsNotRun(t *testing.T) {
	app := &App{pager: newResultPager(), gridSel: -1}
	app.initModalEditors()
	app.runGen = 1 // the run this applyRun reports for
	tab := func() *resTab { return newTab("t", tabRun) }
	tabs := []*resTab{tab(), tab(), tab()}
	stmts := []string{"SELECT 1", "SELECT 2", "SELECT 3"}
	// 取消落在第 2 条：它没产出（被中断）、第 3 条从未运行。
	// run goroutine 在取消时把「被中断 + 未运行」的语句都打 notRun
	// （query.go 的取消分支）；这里直接构造它的输出形状。
	out := []runOut{
		{sets: []*dbcore.QueryResult{{Columns: []dbcore.ColumnInfo{{Name: "a"}}}}},
		{notRun: true},
		{notRun: true},
	}
	app.applyRun(1, tabs, stmts, out, 0, false, true)
	// 取消脚本：最后一个 tab 报 Cancelled（运行到此为止——既有语义），
	// 中间未运行的 tab 不被 Cancelled 覆盖成"done"。
	if tabs[2].status != "Cancelled" {
		t.Fatalf("the run's last tab: %q", tabs[2].status)
	}
	if tabs[1].status == "Query done" {
		t.Fatal("a cancelled script's pending statement must not read as done")
	}
}
