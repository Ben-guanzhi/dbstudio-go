package main

// SQL 脚本文件树（DBeaver 的 SQL Editor 工作区）的最小高价值切片：
// **console tab 绑定 .sql 文件**——打开 / 保存 / 另存为。
//
// 此前的 console 是一次性的：关窗即丢，用户跑了一半的脚本没有归宿。
// 绑定后：tab label 显示文件名（改动时 ● 前缀），Ctrl+S 保存（无文件 =
// 另存为），Save As 弹保存对话框。脚本默认目录 = 连接存储目录旁的
// scripts/（用户可自由保存到任何地方）。
//
// 边界：不做**目录树浏览**（工作区文件树的导航——os.ReadDir 的面板）；
// 打开走系统的文件对话框（用户自己找文件，桌面应用的惯例）。树是下一步。

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
)

// scriptsDir 是脚本脚本的默认目录：connections.json 旁边（用户已知该目录）。
func (app *App) scriptsDir() string {
	return filepath.Join(app.store.Dir(), "scripts")
}

// scriptPath 是当前选中 console 的绑定文件路径（空 = 未绑定）。
func (app *App) scriptPath() string {
	i := app.consolesel
	if i < 0 || i >= len(app.consoles) {
		return ""
	}
	return app.consoles[i].file
}

// consoleDirty 当前 tab 是否有未保存改动（buffer 与绑定文件的最近保存内容比）。
func (app *App) consoleDirty() bool {
	i := app.consolesel
	if i < 0 || i >= len(app.consoles) {
		return false
	}
	c := &app.consoles[i]
	if c.file == "" {
		return strings.TrimSpace(c.buf.String()) != ""
	}
	saved, err := os.ReadFile(c.file)
	if err != nil {
		return true
	}
	return c.buf.String() != string(saved)
}

// openScriptFile 打开一个 .sql 文件到新 console（或复用空的当前 tab）。
func (app *App) openScriptFile() {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Title:       "Open SQL Script",
		ButtonLabel: "Open",
		Filters: []mygo.FileFilter{
			{Name: "SQL scripts", Extensions: []string{"sql"}},
		},
	})
	if err != nil || len(paths) == 0 || paths[0] == "" {
		return // cancelled
	}
	raw, err := os.ReadFile(paths[0])
	if err != nil {
		app.errMsg = "Open script failed: " + err.Error()
		app.invalidate()
		return
	}
	// 空的当前 tab 直接复用（不叠 tab）。
	i := app.consolesel
	if i < 0 || i >= len(app.consoles) || strings.TrimSpace(app.consoles[i].buf.String()) != "" {
		if len(app.consoles) < consoleMax {
			app.newConsole()
		}
		i = app.consolesel
	}
	c := &app.consoles[i]
	c.buf.Set(string(raw))
	c.file = paths[0]
	c.label = filepath.Base(paths[0])
	app.markConsole(i)
	app.status = fmt.Sprintf("Opened %s", filepath.Base(paths[0]))
	app.invalidate()
}

// saveScriptFile 保存当前 tab：有绑定文件就写它，没有走另存为（Ctrl+S 的
// 两态行为——TablePlus/VS Code 的惯例）。
func (app *App) saveScriptFile() {
	i := app.consolesel
	if i < 0 || i >= len(app.consoles) {
		return
	}
	if app.consoles[i].file == "" {
		app.saveScriptAs()
		return
	}
	app.writeScript(i, app.consoles[i].file)
}

// saveScriptAs 弹保存对话框，绑定文件并写入。
func (app *App) saveScriptAs() {
	i := app.consolesel
	if i < 0 || i >= len(app.consoles) {
		return
	}
	name := fmt.Sprintf("script-%s.sql", time.Now().Format("20060102-150405"))
	path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
		Title:       "Save SQL Script",
		ButtonLabel: "Save",
		Filters: []mygo.FileFilter{
			{Name: "SQL scripts", Extensions: []string{"sql"}},
		},
		DefaultPath: name,
	})
	if err != nil || path == "" {
		return // cancelled
	}
	if !strings.HasSuffix(strings.ToLower(path), ".sql") {
		path += ".sql"
	}
	app.consoles[i].file = path
	app.writeScript(i, path)
}

// writeScript 把 tab 的 buffer 写到绑定文件（0600——脚本可能含敏感值）。
func (app *App) writeScript(i int, path string) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		app.errMsg = "Save failed: " + err.Error()
		app.invalidate()
		return
	}
	if err := os.WriteFile(path, []byte(app.consoles[i].buf.String()), 0o600); err != nil {
		app.errMsg = "Save failed: " + err.Error()
		app.invalidate()
		return
	}
	app.markConsole(i)
	app.status = fmt.Sprintf("Saved %s", filepath.Base(path))
	app.invalidate()
}

// markConsole 刷新 tab 标签（绑定了文件显示文件名，未保存改动加 ● 前缀）。
func (app *App) markConsole(i int) {
	c := &app.consoles[i]
	if c.file == "" {
		if c.buf != nil && strings.TrimSpace(c.buf.String()) != "" {
			c.label = "● unsaved"
		}
		app.invalidate()
		return
	}
	label := filepath.Base(c.file)
	if !c.savedMatches() {
		label = "● " + label
	}
	c.label = label
	app.invalidate()
}

// savedMatches 比对 buffer 与绑定文件的当前内容。
func (c *consoleTab) savedMatches() bool {
	saved, err := os.ReadFile(c.file)
	if err != nil {
		return false
	}
	return c.buf.String() == string(saved)
}
