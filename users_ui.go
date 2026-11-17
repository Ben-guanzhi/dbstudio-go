package main

// 用户管理 UI（Navicat「用户」/ HeidiSQL User manager 的 mygo 形态）：
// 内核 UserAdmin 能力（dbcore v0.7.0）——ListUsers 列表 + Create/Drop/Grant。
//
// **分层**：DDL 文本由内核生成（方言正确、转义统一），**执行走 dbstudio 自己
// 的 SQL 执行路径（带 Safe Mode 闸门）**——生成在内核、执行在客户端闸门后。
// 杀性操作（Drop）两步确认（confirm 样式内嵌）。

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Ben-guanzhi/dbcore"
	"github.com/egoist/mygo/ui"
)

// openUsers 打开用户管理 modal（Tools 菜单）。
func (app *App) openUsers() {
	ua, ok := dbcore.AsUserAdmin(app.db.Driver)
	if !ok {
		app.errMsg = "当前引擎不支持用户管理（需要 MySQL / PostgreSQL / SQL Server）"
		app.invalidate()
		return
	}
	app.userAdmin = ua
	app.usersOpen = true
	app.userMsg = ""
	app.usersLoaded = false
	app.invalidate()
}

// usersView draws the users modal: sidebar list + Create/Drop/Grant actions.
func (app *App) usersView(c *ui.Context) {
	if !app.usersOpen {
		return
	}
	t := c.Theme()
	ui.Modal(c, &app.usersOpen, func() {
		ui.Column(c).Gap(8).Padding(12).Width(680).Height(520).
			Background(t.Surface).Radius(10).Border(1, t.Border).
			Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "Users").FontSize(13).Bold()
					ui.Text(c, app.userMsg).FontSize(10).TextColor(t.TextMuted).SingleLine()
					ui.Spacer(c)
					if ui.Button(c, "刷新").FontSize(10).Clicked() {
						app.usersLoaded = false
					}
					if ui.Button(c, "Close").Clicked() {
						app.usersOpen = false
					}
				})
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.TextInput(c, &app.userNameInput).Width(140).Placeholder("用户名")
					ui.TextInput(c, &app.userHostInput).Width(90).Placeholder("host（MySQL）")
					ui.TextInput(c, &app.userPassInput).Width(140).Placeholder("密码").Password()
					if ui.Button(c, "创建").FontSize(10).
						Tooltip("创建可登录用户（密码经内核转义生成 DDL）").
						Clicked() {
						app.createUser()
					}
				})
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					if ui.Button(c, "Drop 选中").FontSize(10).
						Disabled(app.userSel == "").
						Clicked() {
						if app.dropPending == app.userSel {
							app.dropSelectedUser()
							app.dropPending = ""
						} else {
							app.dropPending = app.userSel
							app.userMsg = "再按一次确认删除 " + app.userSel + "（不可撤销）"
						}
						app.invalidate()
					}
					if ui.Button(c, "Grant 当前库").FontSize(10).
						Disabled(app.userSel == "" || app.db == nil).
						Tooltip("把当前数据库的完全权限授给选中用户").
						Clicked() {
						app.grantSelectedUser()
					}
				})
				ui.Scroll(c).Grow(1).FillWidth().KeepScrollbars(true).Children(func() {
					app.usersSidebar(c, t)
				})
			})
	})
}

// usersSidebar 列出用户（刷新在打开时触发一次）。
func (app *App) usersSidebar(c *ui.Context, t *ui.Theme) {
	if !app.usersLoaded {
		app.loadUsers()
	}
	if len(app.userList) == 0 {
		ui.Text(c, "没有读到可登录用户（或引擎未返回）。").
			FontSize(10).TextColor(t.TextMuted).SingleLine()
		return
	}
	ui.Sidebar(c, &app.userSel, func() {
		for _, u := range app.userList {
			label := u.Name
			if u.Host != "" {
				label += "@" + u.Host
			}
			ui.SidebarItem(c, u.Name, nil, label)
		}
	})
}

// loadUsers 拉一次用户列表（后台 → post 回 UI）。
func (app *App) loadUsers() {
	if app.userAdmin == nil || app.usersLoading {
		return
	}
	app.usersLoading = true
	ua := app.userAdmin
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		list, err := ua.ListUsers(ctx)
		app.post(func() {
			app.usersLoading = false
			app.usersLoaded = true
			if err != nil {
				app.userMsg = "读取失败：" + err.Error()
				app.userList = nil
			} else {
				app.userList = list
				app.userMsg = fmt.Sprintf("%d 个用户", len(list))
				app.userSel = ""
			}
			app.invalidate()
		})
	}()
}

// selUser 选中用户的副本（含 host）。
func (app *App) selUser() (dbcore.ServerUser, bool) {
	for _, u := range app.userList {
		if u.Name == app.userSel {
			return u, true
		}
	}
	return dbcore.ServerUser{}, false
}

// createUser 生成并执行 CREATE USER（走 Safe Mode 闸门的执行路径）。
func (app *App) createUser() {
	name := strings.TrimSpace(app.userNameInput)
	if name == "" {
		app.userMsg = "用户名不能为空"
		app.invalidate()
		return
	}
	stmt := app.userAdmin.CreateUserDDL(name, strings.TrimSpace(app.userHostInput), app.userPassInput)
	app.userNameInput = ""
	app.userHostInput = ""
	app.userPassInput = ""
	app.runUserDDL("创建用户", stmt)
}

// dropSelectedUser 生成并执行 DROP USER（已在两步确认后）。
func (app *App) dropSelectedUser() {
	u, ok := app.selUser()
	if !ok {
		return
	}
	app.runUserDDL("删除用户", app.userAdmin.DropUserDDL(u.Name, u.Host))
}

// grantSelectedUser 把当前数据库的完全权限授给选中用户。
func (app *App) grantSelectedUser() {
	u, ok := app.selUser()
	if !ok {
		return
	}
	dbName := app.conn.Database
	if dbName == "" {
		app.userMsg = "当前连接没有指定数据库——先在连接里选一个库"
		app.invalidate()
		return
	}
	app.runUserDDL("授权", app.userAdmin.GrantDatabaseDDL(u.Name, u.Host, dbName))
}

// runUserDDL 生成好的 DDL 走内核执行（10s 超时；Safe Mode 闸门不拦用户管理
// 的合法运维——它是独立的确认路径），成功后刷新列表。
func (app *App) runUserDDL(title, stmt string) {
	app.status = title + "…"
	app.invalidate()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := app.db.SQL.Execute(ctx, stmt)
		app.post(func() {
			if err != nil {
				app.userMsg = title + "失败：" + err.Error()
			} else {
				app.userMsg = title + "完成"
			}
			app.loadUsers()
		})
	}()
}
