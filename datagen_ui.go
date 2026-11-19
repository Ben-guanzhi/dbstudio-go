package main

// 数据生成器 UI（DBeaver 的 Mock Data / Navicat Data Generator 的 mygo 形态）：
// 内核 dbcore.GenerateInserts（v0.8.0）直驱——按当前表的列类型生成测试数据
// INSERT，进编辑器由用户确认后运行（与 §207 的重命名、§299 的清空表同一
// "生成语句→用户过目"纪律）。

import (
	"fmt"
	"strings"

	"github.com/Ben-guanzhi/dbcore"
	"github.com/egoist/mygo/ui"
)

// openDataGen 打开数据生成 modal（Tools 菜单）。
func (app *App) openDataGen() {
	if app.db == nil || app.db.SQL == nil {
		app.errMsg = "Not connected"
		app.invalidate()
		return
	}
	app.dgOpen = true
	app.dgMsg = ""
	// 当前选中的表名（树的 schemaloaded 表）作为默认目标。
	if app.selTable == "" {
		app.dgTable = "generated_rows"
	} else {
		app.dgTable = app.selTable
	}
	app.invalidate()
}

// dataGenView renders the generator modal: target table, row count, seed,
// SkipPrimaryKey, and the generated-INSERT preview count.
func (app *App) dataGenView(c *ui.Context) {
	if !app.dgOpen {
		return
	}
	t := c.Theme()
	ui.Modal(c, &app.dgOpen, func() {
		ui.Column(c).Gap(8).Padding(12).Width(560).
			Background(t.Surface).Radius(10).Border(1, t.Border).
			Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "Generate Test Data").FontSize(13).Bold()
					ui.Text(c, app.dgMsg).FontSize(10).TextColor(t.TextMuted).SingleLine()
					ui.Spacer(c)
					if ui.Button(c, "Close").Clicked() {
						app.dgOpen = false
					}
				})
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "表名").FontSize(11)
					ui.TextInput(c, &app.dgTable).Width(200).Label("目标表名")
					ui.Text(c, "行数").FontSize(11)
					ui.NumberInput(c, &app.dgRows, 1, 10000, 1)
				})
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "种子").FontSize(11)
					ui.NumberInput(c, &app.dgSeed, 0, 1<<40, 1)
					ui.Checkbox(c, &app.dgSkipPK, "跳过主键（自增由服务器填）")
				})
				ui.Text(c, "按当前表的列类型生成；复制的语句进编辑器，确认后再运行。").
					FontSize(10).TextColor(t.TextMuted)
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Spacer(c)
					if ui.Button(c, "生成 INSERT → 编辑器").FontSize(11).Clicked() {
						app.generateDataInserts()
					}
				})
			})
	})
}

// generateDataInserts 生成 INSERT 语句放进编辑器（不自动执行）。
func (app *App) generateDataInserts() {
	cols, _ := app.ColumnsOf(app.schema, app.selTable)
	if len(cols) == 0 {
		// 当前表未展开/未加载——从结构缓存再试一次（loadColumns 是异步的，
		// 这里给明确说明而不是静默生成空语句）。
		app.dgMsg = "先在左侧展开目标表以加载列类型"
		app.invalidate()
		return
	}
	stmts := dbcore.GenerateInserts(cols, dbcore.GenOptions{
		Rows:           int(app.dgRows),
		Seed:           uint64(app.dgSeed),
		Dialect:        app.qbDialectForDDL(),
		Table:          app.dgTable,
		SkipPrimaryKey: app.dgSkipPK,
	})
	if len(stmts) == 0 {
		app.dgMsg = "没有可生成的列"
		app.invalidate()
		return
	}
	app.edBuf.Set(strings.Join(stmts, "\n"))
	app.dgOpen = false
	app.status = fmt.Sprintf("已生成 %d 条 INSERT（未执行——先看一眼再跑）", len(stmts))
	app.invalidate()
}
