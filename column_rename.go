package main

// 列重命名重构（DBeaver/DataGrip 的 Rename Column——表改名 §207 的姊妹）：
// 内核 RenameColumn（dbcore v0.14.0）直驱，树菜单「Rename Column…」→
// 输入新名 → live DDL 预览 → 执行。
//
// 与表改名的差异：**引用改写更精细**——SQL 里列既可裸写也可 `表.列` 限定
// （还可能是别名.列）。这里的改写只处理「裸列名」与「限定名中的列段」，
// 字符串字面量跳过；别名指向别的表时不改（无法从文本判定别名指向哪张表
// ——保守优先，宁可不改也不改错）。

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Ben-guanzhi/dbcore/ddl"
	"github.com/egoist/mygo/ui"
)

// startColumnRename 进入列改名状态（树菜单触发）。
func (app *App) startColumnRename(n treeNode) {
	if n.kind != nodeColumn || n.table == "" {
		return
	}
	app.colRenameNode = n
	app.colRenameInput = n.label
	app.colRenameOpen = true
	app.invalidate()
}

// colRenameView draws the column-rename modal: the new-name input, a live
// preview of the dialect DDL, and the reference-rewrite notice.
func (app *App) colRenameView(c *ui.Context) {
	if !app.colRenameOpen {
		return
	}
	t := c.Theme()
	ddlStmt := ""
	newName := strings.TrimSpace(app.colRenameInput)
	if app.db != nil && newName != "" {
		if stmt, err := ddl.RenameColumn(app.qbDialectForDDL(),
			app.colRenameNode.parent, app.colRenameNode.table,
			app.colRenameNode.label, newName); err == nil {
			ddlStmt = stmt
		}
	}
	ui.Modal(c, &app.colRenameOpen, func() {
		ui.Column(c).Gap(8).Padding(12).Width(560).
			Background(t.Surface).Radius(10).Border(1, t.Border).
			Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "Rename "+app.colRenameNode.table+"."+app.colRenameNode.label).FontSize(13).Bold()
					ui.Spacer(c)
					if ui.Button(c, "Cancel").Clicked() {
						app.colRenameOpen = false
					}
				})
				ui.TextInput(c, &app.colRenameInput).Label("新列名")
				if ddlStmt != "" {
					ui.Text(c, "将执行：").FontSize(10).TextColor(t.TextMuted)
					ui.Text(c, ddlStmt).FontSize(11).SingleLine()
				}
				if n := app.rewriteColumnRefsCount(); n > 0 {
					ui.Text(c, fmt.Sprintf("打开的编辑器里有 %d 处对旧列的引用，提交后会被改写。", n)).
						FontSize(10).TextColor(t.TextMuted)
				}
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Spacer(c)
					if ui.Button(c, "Rename").FontSize(11).
						Disabled(newName == "" || newName == app.colRenameNode.label).
						Clicked() {
						app.doColumnRename()
					}
				})
			})
	})
}

// rewriteColumnRefsCount 数编辑器里对旧列的裸引用与限定引用（字面量跳过）。
func (app *App) rewriteColumnRefsCount() int {
	if app.edBuf == nil {
		return 0
	}
	return strings.Count(stripSQLLiterals(app.edBuf.String()), app.colRenameNode.label)
}

// rewriteColumnRefs 把编辑器里的旧列名引用换成新名（裸列名与限定列段；
// 字面量跳过——stripSQLLiterals 保护，与表改名同一手法）。
func (app *App) rewriteColumnRefs(oldName, newName string) {
	if app.edBuf == nil {
		return
	}
	app.edBuf.Set(rewriteSQLIdentifiers(app.edBuf.String(), oldName, newName))
}

// doColumnRename 执行列改名：内核 DDL → 编辑器改写 → 执行 → 列缓存刷新。
func (app *App) doColumnRename() {
	newName := strings.TrimSpace(app.colRenameInput)
	if newName == "" || newName == app.colRenameNode.label || app.db == nil {
		return
	}
	ddlStmt, err := ddl.RenameColumn(app.qbDialectForDDL(),
		app.colRenameNode.parent, app.colRenameNode.table,
		app.colRenameNode.label, newName)
	if err != nil {
		app.errMsg = "生成 RENAME 失败：" + err.Error()
		app.invalidate()
		return
	}
	app.colRenameOpen = false
	// 只读锁在执行前挡（与 §243 的能力直发同款闸门）。
	if app.readOnly() {
		app.refuseWrite("RENAME COLUMN")
		return
	}
	app.rewriteColumnRefs(app.colRenameNode.label, newName)
	app.status = "Renaming column…"
	app.invalidate()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, err := app.db.SQL.Execute(ctx, ddlStmt)
		app.post(func() {
			if err != nil {
				app.errMsg = "Rename column failed: " + err.Error()
				app.status = ""
			} else {
				app.status = "Renamed " + app.colRenameNode.table + "." + app.colRenameNode.label +
					" → " + newName
				// 列缓存按表失效：refreshSchema 全量重置（与表改名一致——
				// 旧列名不应留在任何缓存里）。
				app.refreshSchema()
			}
			app.invalidate()
		})
	}()
}
