package main

// 服务器变量查看 UI（Navicat「工具 ▸ 变量」/ HeidiSQL Variables 面板的
// mygo 形态）：内核 ServerVariablesReader 能力（dbcore v0.6.0）。
//
// **只读**——接口就没有改值（SET GLOBAL 的副作用与权限语义各家不同）。
// 面板 = 顶部过滤框 + 名字排序的列表；每行 Value 与 Default 不同时给
// 「已改未重启」的暗示（MSSQL 的 config vs run、PG 的 setting vs boot_val）。
// 引擎不支持（SQLite/Redis）时状态行说明而非空面板。

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Ben-guanzhi/dbcore"
	"github.com/egoist/mygo/ui"
)

// openServerVars 打开变量面板（Tools 菜单）。
func (app *App) openServerVars() {
	svr, ok := dbcore.AsServerVariables(app.db.Driver)
	if !ok {
		app.errMsg = "当前引擎不支持服务器变量查看（需要 MySQL / PostgreSQL / SQL Server）"
		app.invalidate()
		return
	}
	app.svReader = svr
	app.svOpen = true
	app.svLoaded = false
	app.invalidate()
}

// serverVarsView draws the variables modal: a filter box over the name-sorted
// list, loaded once on open (a few hundred rows, one cheap query).
func (app *App) serverVarsView(c *ui.Context) {
	if !app.svOpen {
		return
	}
	t := c.Theme()
	ui.Modal(c, &app.svOpen, func() {
		ui.Column(c).Gap(8).Padding(12).Width(760).Height(560).
			Background(t.Surface).Radius(10).Border(1, t.Border).
			Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "Server Variables").FontSize(13).Bold()
					ui.Text(c, app.svStatus).FontSize(10).TextColor(t.TextMuted).SingleLine()
					ui.Spacer(c)
					if ui.Button(c, "刷新").FontSize(10).Clicked() {
						app.svLoaded = false
					}
					if ui.Button(c, "Close").Clicked() {
						app.svOpen = false
					}
				})
				ui.TextInput(c, &app.svFilter).Width(240).Placeholder("过滤变量名/值…")
				ui.Scroll(c).Grow(1).FillWidth().KeepScrollbars(true).Children(func() {
					app.svSidebar(c, t)
				})
			})
	})
}

// svVisible 过滤后的变量（名字或值包含过滤词，大小写不敏感）。
func (app *App) svVisible() []dbcore.ServerVariable {
	term := strings.ToLower(strings.TrimSpace(app.svFilter))
	if term == "" {
		return app.svList
	}
	out := make([]dbcore.ServerVariable, 0, len(app.svList)/4+1)
	for _, v := range app.svList {
		if strings.Contains(strings.ToLower(v.Name), term) ||
			strings.Contains(strings.ToLower(v.Value), term) {
			out = append(out, v)
		}
	}
	return out
}

// svSidebar 列表本体：名字 + 值（与默认不同的标记 *）+ 说明。
func (app *App) svSidebar(c *ui.Context, t *ui.Theme) {
	if !app.svLoaded {
		app.loadServerVars()
	}
	if len(app.svList) == 0 && app.svStatus == "" {
		ui.Text(c, "没有读到变量（或引擎未返回）。").FontSize(10).TextColor(t.TextMuted).SingleLine()
		return
	}
	list := app.svVisible()
	if len(list) == 0 {
		ui.Text(c, "没有匹配过滤词的变量。").FontSize(10).TextColor(t.TextMuted).SingleLine()
		return
	}
	for _, v := range list {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(2, 4).Children(func() {
			ui.Text(c, v.Name).FontSize(11).Bold().SingleLine()
			mark := ""
			changed := v.Default != "" && v.Value != v.Default
			if changed {
				mark = " *"
			}
			val := ui.Text(c, v.Value+mark).FontSize(11).SingleLine()
			if changed {
				val = val.TextColor(t.Warning)
			}
			_ = val
			if v.Desc != "" {
				ui.Text(c, v.Desc).FontSize(10).TextColor(t.TextMuted).SingleLine()
			}
		})
	}
}

// loadServerVars 拉一次变量（后台 → post 回 UI）。
func (app *App) loadServerVars() {
	if app.svReader == nil || app.svLoading {
		return
	}
	app.svLoading = true
	svr := app.svReader
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		vars, err := svr.ServerVariables(ctx)
		dbcore.SortVariables(vars)
		app.post(func() {
			app.svLoading = false
			app.svLoaded = true
			if err != nil {
				app.svList = nil
				app.svStatus = "读取失败：" + err.Error()
			} else {
				app.svList = vars
				app.svStatus = fmt.Sprintf("%d 个变量（只读）", len(vars))
			}
			app.invalidate()
		})
	}()
}
