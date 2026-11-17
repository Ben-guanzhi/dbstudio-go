package main

// Review Changes 面板（TablePlus 的招牌交互）：网格编辑后、提交前，把工作
// 副本的**全部差异**摊开看——新增几行 / 修改几行（列级 old → new）/ 删除几行
// ——确认后才真正 commit。
//
// dbstudio 此前的 commit 是一键直发（edit.go 的 commit() 直接 ApplyBatch）；
// 这里加一个**预览步骤**：Review 按钮 → 本 modal → 「Confirm commit」走
// 原有 commit()。语句文本由内核 BatchRequest 推导（只读展示，不在这里执行）。

import (
	"fmt"
	"strings"

	"github.com/Ben-guanzhi/dbcore"
	"github.com/egoist/mygo/ui"
)

// openReview 打开 Review Changes modal（编辑工具条的按钮）。
func (app *App) openReview() {
	e := &app.edit
	if !e.active || !e.dirty() {
		app.status = "工作副本没有改动——Review 无可预览"
		app.invalidate()
		return
	}
	e.syncState()
	req, why := e.request()
	if why != "" {
		app.errMsg = why
		app.invalidate()
		return
	}
	app.reviewReq = req
	app.reviewOpen = true
	app.invalidate()
}

// reviewView draws the Review Changes modal: three sections (inserts / changes
// / deletes) with column-level old → new detail, and the confirm button.
func (app *App) reviewView(c *ui.Context) {
	if !app.reviewOpen {
		return
	}
	t := c.Theme()
	ui.Modal(c, &app.reviewOpen, func() {
		req := app.reviewReq
		ui.Column(c).Gap(8).Padding(12).Width(760).Height(560).
			Background(t.Surface).Radius(10).Border(1, t.Border).
			Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "Review Changes").FontSize(13).Bold()
					ui.Text(c, fmt.Sprintf("%s · +%d ~%d -%d",
						req.Table, len(req.Inserts), countModified(req), len(req.Deletes))).
						FontSize(11).TextColor(t.TextMuted).SingleLine()
					ui.Spacer(c)
					if ui.Button(c, "Close").Clicked() {
						app.reviewOpen = false
					}
				})
				ui.Scroll(c).Grow(1).FillWidth().KeepScrollbars(true).Children(func() {
					app.reviewBody(c, t, req)
				})
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Spacer(c)
					if ui.Button(c, "Confirm commit").FontSize(11).Clicked() {
						app.reviewOpen = false
						app.commit()
					}
				})
			})
	})
}

// countModified 数修改行数（req 里没有行状态，用「Updates 的 pk 数」计）。
func countModified(req dbcore.BatchRequest) int { return len(req.Updates) }

// reviewBody 差异本体：三区。
func (app *App) reviewBody(c *ui.Context, t *ui.Theme, req dbcore.BatchRequest) {
	// 插入区。
	if len(req.Inserts) > 0 {
		ui.Text(c, fmt.Sprintf("新增 %d 行", len(req.Inserts))).FontSize(11).Bold()
		for _, ins := range req.Inserts {
			ui.Text(c, "  + "+briefValues(ins)).FontSize(10).SingleLine()
		}
	}
	// 修改区（列级 old → new——Updates 的 Where 带旧值、Changes 带新值）。
	if len(req.Updates) > 0 {
		ui.Text(c, fmt.Sprintf("修改 %d 行", len(req.Updates))).FontSize(11).Bold()
		for _, u := range req.Updates {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "  ~ "+briefValues(u.PK)).FontSize(10).TextColor(t.Warning).SingleLine()
				ui.Text(c, "→").FontSize(10).TextColor(t.TextMuted)
				ui.Text(c, briefValues(u.Changes)).FontSize(10).SingleLine()
			})
		}
	}
	// 删除区。
	if len(req.Deletes) > 0 {
		ui.Text(c, fmt.Sprintf("删除 %d 行", len(req.Deletes))).FontSize(11).Bold()
		for _, d := range req.Deletes {
			ui.Text(c, "  − "+briefValues(d)).FontSize(10).TextColor(t.Danger).SingleLine()
		}
	}
	if len(req.Inserts) == 0 && len(req.Updates) == 0 && len(req.Deletes) == 0 {
		ui.Text(c, "没有差异。").FontSize(10).TextColor(t.TextMuted)
	}
}

// briefValues 一行 map 的紧凑摘要（k=v，逗号分隔，超长截断）。
func briefValues(m map[string]dbcore.Value) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// 稳定顺序（map 无序——展示要可预期）。
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+m[k].String())
	}
	out := strings.Join(parts, ", ")
	r := []rune(out)
	if len(r) > 120 {
		out = string(r[:120]) + "…"
	}
	return out
}
