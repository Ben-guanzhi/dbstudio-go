package main

// 执行计划的树形缩进。
//
// dbcore 的 EXPLAIN QUERY PLAN 已经带树（id/parent），界面却把它平铺。这些
// 测试钉住转换本身：深度算得对、树形前缀画得对（├─ / └─ / │）、以及结果里
// 确实出现了缩进后的文本。

import (
	"testing"

	"github.com/Ben-guanzhi/dbcore"
	"github.com/egoist/mygo/ui"
)

// planResult 造一个 SQLite 形状的 EXPLAIN QUERY PLAN 结果。
//
// id 1 是根；2、3 是它的孩子（3 是末子）；4 是 2 的孩子。
func planResult() *dbcore.QueryResult {
	return &dbcore.QueryResult{
		Columns: []dbcore.ColumnInfo{
			{Name: "id"}, {Name: "parent"}, {Name: "notused"}, {Name: "detail"},
		},
		Rows: [][]dbcore.Value{
			{{Kind: dbcore.ValInt, I: 1}, {Kind: dbcore.ValInt, I: 0}, {Kind: dbcore.ValInt, I: 0}, {Kind: dbcore.ValText, S: "root"}},
			{{Kind: dbcore.ValInt, I: 2}, {Kind: dbcore.ValInt, I: 1}, {Kind: dbcore.ValInt, I: 0}, {Kind: dbcore.ValText, S: "child A"}},
			{{Kind: dbcore.ValInt, I: 3}, {Kind: dbcore.ValInt, I: 1}, {Kind: dbcore.ValInt, I: 0}, {Kind: dbcore.ValText, S: "child B"}},
			{{Kind: dbcore.ValInt, I: 4}, {Kind: dbcore.ValInt, I: 2}, {Kind: dbcore.ValInt, I: 0}, {Kind: dbcore.ValText, S: "grandchild"}},
		},
	}
}

func TestPlanDepths(t *testing.T) {
	tree := buildPlanTree(planResult())
	if tree == nil {
		t.Fatal("buildPlanTree 没认出计划结果")
	}
	want := []int{0, 1, 1, 2}
	for i, w := range want {
		if tree.depth[i] != w {
			t.Errorf("行 %d 的深度 = %d, want %d", i, tree.depth[i], w)
		}
	}
}

func TestPlanIndent(t *testing.T) {
	tree := buildPlanTree(planResult())
	want := []string{"", "├─ ", "└─ ", "│  └─ "}
	for i, w := range want {
		if got := tree.indent(i); got != w {
			t.Errorf("行 %d 的前缀 = %q, want %q", i, got, w)
		}
	}
}

func TestPlanNeedsNoTreeForAnOrdinaryResult(t *testing.T) {
	res := &dbcore.QueryResult{
		Columns: []dbcore.ColumnInfo{{Name: "id"}, {Name: "name"}},
		Rows:    [][]dbcore.Value{{{Kind: dbcore.ValInt, I: 1}, {Kind: dbcore.ValText, S: "a"}}},
	}
	if isPlanResult(res) {
		t.Error("普通结果被当成了执行计划")
	}
	if tree := buildPlanTree(res); tree != nil {
		t.Error("普通结果建出了计划树")
	}
}

func TestPlanTreeSurvivesABrokenParent(t *testing.T) {
	// parent 指向不存在的 id：那一行当根，其余照画。
	res := planResult()
	res.Rows[1][1] = dbcore.Value{Kind: dbcore.ValInt, I: 999}
	tree := buildPlanTree(res)
	if tree == nil {
		t.Fatal("坏 parent 让整棵树画不出来")
	}
	if tree.depth[1] != 0 {
		t.Errorf("parent 指向不存在的行，深度 = %d, want 0", tree.depth[1])
	}
}

func TestPlanDetailIsIndentedOnScreen(t *testing.T) {
	app := newViewApp(t, nil)
	app.conn = Conn{Name: "local", Kind: "sqlite"}
	res := planResult()
	tab := newTab("Plan", tabRun)
	tab.result = res
	tab.raw = res
	tab.pager.reset(false)
	tab.pager.total = len(res.Rows)
	app.tabs = []*resTab{tab}
	app.tabSel = 0
	app.loadTab(tab)

	tt := ui.NewTester(app.view, 1320, 800)
	tt.Frame()

	// 缩进后的文本出现在结果区。
	for _, want := range []string{"├─ child A", "└─ child B", "│  └─ grandchild"} {
		if !tt.HasText(want) {
			t.Errorf("结果区没有 %q；texts = %v", want, tt.Texts())
		}
	}
	// id / parent 两列不再显示（它们的作用由缩进承载）。
	if tt.HasText("parent") {
		t.Errorf("计划结果仍在显示 parent 列；texts = %v", tt.Texts())
	}
}
