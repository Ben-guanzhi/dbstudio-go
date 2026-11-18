package main

// 会话管理 UI（Navicat「工具 ▸ 会话管理」/ DBeaver Sessions 的 mygo 形态）：
// 列出当前服务器上**其他会话**的活动状态，选中可终止。
//
// 数据与操作走内核的可选能力接口 ProcessLister（dbcore v0.5.0）——引擎支持
// （MySQL/PG/MSSQL）就显示面板，不支持（SQLite/Oracle/插件驱动）给一行明确
// 的说明而不是空白/报错。**杀会话是破坏性操作**：确认一步（confirmView 的
// 既有模式）后才真正执行。

import (
	"context"
	"strings"
	"time"

	"github.com/Ben-guanzhi/dbcore"
	"github.com/egoist/mygo/ui"
)

// openSessions 打开会话管理 modal（Tools 菜单）。
func (app *App) openSessions() {
	// 能力断言：引擎不支持时不打开空面板，状态行说清原因。
	pl, ok := dbcore.AsProcessLister(app.db.Driver)
	if !ok {
		app.errMsg = "当前引擎不支持会话管理（需要 MySQL / PostgreSQL / SQL Server）"
		app.invalidate()
		return
	}
	app.sessions = pl
	app.sessionsOpen = true
	app.sessionMsg = ""
	app.invalidate()
}

// sessionsView draws the sessions modal: a sidebar list of the server's other
// sessions plus a Kill action behind one confirmation step.
func (app *App) sessionsView(c *ui.Context) {
	if !app.sessionsOpen {
		return
	}
	t := c.Theme()
	ui.Modal(c, &app.sessionsOpen, func() {
		ui.Column(c).Gap(8).Padding(12).Width(720).Height(520).
			Background(t.Surface).Radius(10).Border(1, t.Border).
			Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "Sessions").FontSize(13).Bold()
					ui.Spacer(c)
					if ui.Button(c, "刷新").FontSize(10).Clicked() {
						app.loadSessions()
					}
					if ui.Button(c, "Kill 选中").FontSize(10).
						Disabled(app.sessionSel == "").
						Tooltip("终止选中的会话（破坏性，需二次确认）").
						Clicked() {
						if app.killPending == app.sessionSel {
							// 二次确认：同一个选中上连按两次才真正执行。
							app.killSelectedSession()
							app.killPending = ""
						} else {
							app.killPending = app.sessionSel
							app.sessionMsg = "再按一次 Kill 确认终止 " + app.sessionSel + "（未提交事务会回滚）"
						}
						app.invalidate()
					}
					if ui.Button(c, "Close").Clicked() {
						app.sessionsOpen = false
					}
				})
				if app.sessionMsg != "" {
					ui.Text(c, app.sessionMsg).FontSize(10).TextColor(t.TextMuted).SingleLine()
				}
				app.sessionsSidebar(c, t)
			})
	})
}

// sessionsSidebar 列出会话（刷新在打开时触发一次）。
func (app *App) sessionsSidebar(c *ui.Context, t *ui.Theme) {
	if app.sessionList == nil {
		app.loadSessions()
	}
	if len(app.sessionList) == 0 {
		ui.Text(c, "服务器上没有其它活动会话（或引擎没有返回）。").
			FontSize(10).TextColor(t.TextMuted).SingleLine()
		return
	}
	prev := app.sessionSel
	ui.Sidebar(c, &app.sessionSel, func() {
		for _, p := range app.sessionList {
			item := ui.SidebarItem(c, p.ID, nil, sessionLabel(p))
			// 活跃查询红色高亮——一眼看到谁在吃资源。
			if strings.EqualFold(p.State, "active") || strings.EqualFold(p.Command, "Query") {
				item.TextColor(t.Danger)
			}
		}
	})
	_ = prev
}

// sessionLabel 一行摘要：ID · user@client · db · state · 语句首行。
func sessionLabel(p dbcore.ServerProcess) string {
	parts := make([]string, 0, 5)
	if p.User != "" {
		who := p.User
		if p.Client != "" {
			who += "@" + p.Client
		}
		parts = append(parts, who)
	}
	if p.DB != "" {
		parts = append(parts, p.DB)
	}
	if p.State != "" {
		parts = append(parts, p.State)
	} else if p.Command != "" {
		parts = append(parts, p.Command)
	}
	if p.Query != "" {
		q := strings.ReplaceAll(p.Query, "\t", " ")
		r := []rune(q)
		if len(r) > 36 {
			q = string(r[:36]) + "…"
		}
		parts = append(parts, q)
	}
	return strings.Join(parts, " · ")
}

// loadSessions 拉一次会话列表（后台 → post 回 UI 线程）。
func (app *App) loadSessions() {
	if app.sessions == nil {
		return
	}
	pl := app.sessions
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		list, err := pl.Processes(ctx)
		app.post(func() {
			if err != nil {
				app.sessionMsg = "读取会话失败：" + err.Error()
				app.sessionList = nil
			} else {
				app.sessionList = list
				app.sessionMsg = ""
				app.sessionSel = ""
			}
			app.invalidate()
		})
	}()
}

// killSelectedSession 执行终止并刷新列表。
func (app *App) killSelectedSession() {
	// KILL 是服务端写操作——只读连接必须拒绝（与 DDL/DML 同纪律）。
	if app.readOnly() {
		app.refuseWrite("KILL " + app.sessionSel)
		return
	}
	if app.sessions == nil || app.sessionSel == "" {
		return
	}
	pl, id := app.sessions, app.sessionSel
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := pl.KillProcess(ctx, id)
		app.post(func() {
			if err != nil {
				app.sessionMsg = "终止失败：" + err.Error()
			} else {
				app.sessionMsg = "已终止 " + id
			}
			app.loadSessions()
		})
	}()
}
