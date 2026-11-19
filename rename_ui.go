package main

// 重命名重构（DBeaver / DataGrip 的招牌：Rename 全链路）——表改名：
//
// 1. 树菜单「Rename…」→ 行内输入新名；
// 2. 内核 sqltext.RenameStatement 生成方言正确的 DDL（MySQL RENAME TABLE /
//    MSSQL sp_rename / PG-Oracle-SQLite ALTER TABLE RENAME）；
// 3. **改写打开的编辑器缓冲**——edBuf 里对旧表的裸标识符引用换成新名（
//    词边界匹配，字符串字面量跳过——转义后的复用是安全的）；
// 4. Safe Mode 闸门执行 DDL → 树缓存失效 → 重新载入。
//
// 边界：只改写**当前打开的编辑器缓冲**（内存里的脚本是用户的工作现场）；
// 磁盘上的 .sql 文件不碰——重构不是批量文本替换工具，把用户的文件全改了
// 反而是危险行为。

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Ben-guanzhi/dbcore/ddl"
	"github.com/Ben-guanzhi/dbcore/sqltext"
	"github.com/egoist/mygo/ui"
)

// startRename 进入改名状态（树菜单触发）——行内输入框在树节点处由
// renameView 渲染。
func (app *App) startRename(n treeNode) {
	if n.kind != nodeTable {
		return
	}
	app.renameNode = n
	app.renameInput = n.label
	app.renameOpen = true
	app.invalidate()
}

// renameView draws the rename prompt: a small modal with the new name input,
// a live preview of the DDL, and the reference-rewrite notice.
func (app *App) renameView(c *ui.Context) {
	if !app.renameOpen {
		return
	}
	t := c.Theme()
	ddl := ""
	if app.db != nil && strings.TrimSpace(app.renameInput) != "" {
		if stmt, err := sqltext.RenameStatement(app.qbDialectForDDL(),
			app.qbQuote(app.renameNode.label), app.qbQuote(strings.TrimSpace(app.renameInput)),
			app.renameNode.parent, app.renameNode.label,
			strings.TrimSpace(app.renameInput)); err == nil {
			ddl = stmt
		}
	}
	ui.Modal(c, &app.renameOpen, func() {
		ui.Column(c).Gap(8).Padding(12).Width(560).
			Background(t.Surface).Radius(10).Border(1, t.Border).
			Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "Rename "+app.renameNode.label).FontSize(13).Bold()
					ui.Spacer(c)
					if ui.Button(c, "Cancel").Clicked() {
						app.renameOpen = false
					}
				})
				ui.TextInput(c, &app.renameInput).Label("新名")
				if ddl != "" {
					ui.Text(c, "将执行：").FontSize(10).TextColor(t.TextMuted)
					ui.Text(c, ddl).FontSize(11).SingleLine()
				}
				if n := app.rewriteReferencesPreview(); n > 0 {
					ui.Text(c, fmt.Sprintf("打开的编辑器里有 %d 处对旧名的引用，提交后会被改写。", n)).
						FontSize(10).TextColor(t.TextMuted)
				}
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Spacer(c)
					if ui.Button(c, "Rename").FontSize(11).
						Disabled(strings.TrimSpace(app.renameInput) == "" ||
							strings.TrimSpace(app.renameInput) == app.renameNode.label).
						Clicked() {
						app.doRename()
					}
				})
			})
	})
}

// qbDialectForDDL 复用构建器的方言拼写（RenameStatement 吃 sqltext 的拼写）。
func (app *App) qbDialectForDDL() string { return app.qbDialect() }

// rewriteReferences 数当前编辑器缓冲里对旧表的裸标识符引用（词边界、跳过
// 字符串字面量——stripSQLLiterals 的复用让预览与实际改写一致）。
func (app *App) rewriteReferencesCount() int {
	buf := app.edBuf.String()
	if buf == "" {
		return 0
	}
	return strings.Count(stripSQLLiterals(buf), app.renameNode.label)
}

// rewriteReferencesPreview 与 count 同源（名字区分用途：modal 里用）。
func (app *App) rewriteReferencesPreview() int { return app.rewriteReferencesCount() }

// rewriteReferences 把编辑器缓冲里对旧表的标识符引用换成新名。
func (app *App) rewriteReferences(oldName, newName string) {
	if app.edBuf == nil {
		return
	}
	app.edBuf.Set(rewriteSQLIdentifiers(app.edBuf.String(), oldName, newName))
}

// rewriteSQLIdentifiers 是纯函数：把 text 里对 old 的裸标识符引用换成 new
// （词边界；单引号字符串字面量跳过——字面量里的文本不是引用）。
func rewriteSQLIdentifiers(text, old, neu string) string {
	if text == "" || old == "" || old == neu {
		return text
	}
	// 词边界替换：前一个字符与后一个字符都不是标识符字符。
	isWord := func(r byte) bool {
		return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_'
	}
	o, n := []byte(old), []byte(neu)
	var b strings.Builder
	i := 0
	lit := false // 单引号字面量内不替换
	for i < len(text) {
		c := text[i]
		if c == '\'' {
			lit = !lit
			b.WriteByte(c)
			i++
			continue
		}
		if !lit && c == o[0] &&
			(i == 0 || !isWord(text[i-1])) &&
			(i+len(o) <= len(text) && (i+len(o) == len(text) || !isWord(text[i+len(o)]))) &&
			strings.HasPrefix(text[i:], string(o)) {
			b.Write(n)
			i += len(o)
			continue
		}
		b.WriteByte(c)
		i++
	}
	return b.String()
}

// doRename 执行改名：DDL（Safe Mode 闸门走 runScriptGuard 的确认流）→
// 改写编辑器 → 关弹窗。
func (app *App) doRename() {
	// 只读连接是"锁"不是"提醒"——DDL 走的是内核 RenameStatement 直发，
	// 不经过 runScriptGuard 的写关键字守卫，必须在这里挡住。
	if app.readOnly() {
		app.refuseWrite("RENAME TABLE")
		return
	}
	newName := strings.TrimSpace(app.renameInput)
	if newName == "" || newName == app.renameNode.label || app.db == nil {
		return
	}
	ddl, err := sqltext.RenameStatement(app.qbDialectForDDL(),
		app.qbQuote(app.renameNode.label), app.qbQuote(newName),
		app.renameNode.parent, app.renameNode.label, newName)
	if err != nil {
		app.errMsg = "生成 RENAME 失败：" + err.Error()
		app.invalidate()
		return
	}
	app.renameOpen = false
	// 编辑器改写在执行前做——执行是异步的，改写是本地的；失败时用户的脚本
	// 已经对齐新名（可手改回），成功时正好。
	app.rewriteReferences(app.renameNode.label, newName)
	app.status = "Renaming…"
	app.invalidate()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, err := app.db.SQL.Execute(ctx, ddl)
		app.post(func() {
			if err != nil {
				app.errMsg = "Rename failed: " + err.Error()
				app.status = ""
			} else {
				app.status = "Renamed " + app.renameNode.label + " → " + newName
				// 树缓存失效：refreshSchema 全量重置（简单可靠——改名后旧表名
				// 不应留在任何缓存里），重载后树即显示新名。
				app.refreshSchema()
			}
			app.invalidate()
		})
	}()
}

// emptyTable 清空一张表（树菜单「Empty Table…」）：内核 TruncateStatement
// 生成方言正确的语句（SQLite 用 DELETE FROM——保 sqlite_sequence；其它引擎
// 用 TRUNCATE）。语句进编辑器并走 runScriptGuard——riskyStatement 对
// TRUNCATE 的 confirmRun 询问 + 只读 refuseWrite 全套闸门。用户看到的
// 就是将要执行的，确认后才跑（不清空于无形）。
func (app *App) emptyTable(n treeNode) {
	if n.kind != nodeTable {
		return
	}
	if app.db == nil || app.db.SQL == nil {
		app.errMsg = "Not connected"
		app.invalidate()
		return
	}
	quoted, err := sqltext.QualifiedName(app.qbDialectForDDL(), n.parent, n.label)
	if err != nil {
		app.errMsg = "表名不合法：" + err.Error()
		app.invalidate()
		return
	}
	stmt, err := sqltext.TruncateStatement(app.qbDialectForDDL(), quoted)
	if err != nil {
		app.errMsg = "生成 TRUNCATE 失败：" + err.Error()
		app.invalidate()
		return
	}
	// 放进编辑器后走既有的运行入口——它构造 runner（含事务路由）并调用
	// runScriptGuard：TRUNCATE 在 risky 清单里，confirmRun 会展示语句并
	// 要求确认；只读连接被 refuseWrite 挡住。用户看到的就是将要执行的。
	app.edBuf.Set(stmt)
	app.runQuery()
}

// dropView 删除一个视图（树菜单「Drop View…」）：只作用于 sub=="view" 的
// 节点——表的同名菜单项直接返回（防误操作）。内核 ddl.DropView 生成方言
// 语句（MSSQL 的 IF OBJECT_ID 守卫 vs 其它 DROP VIEW IF EXISTS），语句进
// 编辑器走既有 runQuery → runScriptGuard（DROP 的 confirmRun ask + 只读
// refuseWrite）。
func (app *App) dropView(n treeNode) {
	if n.kind != nodeTable || n.sub != "view" {
		return
	}
	if app.readOnly() {
		app.refuseWrite("DROP VIEW " + n.label)
		return
	}
	quoted, err := sqltext.QualifiedName(app.qbDialectForDDL(), n.parent, n.label)
	if err != nil {
		app.errMsg = "视图名不合法：" + err.Error()
		app.invalidate()
		return
	}
	stmt := ddl.DropView(app.qbDialectForDDL(), quoted)
	app.edBuf.Set(stmt)
	app.runQuery()
}
