package main

// The menu bar: what the menus run. Every item is one call on App, so this file
// is the menu's wiring and nothing else — the actions themselves live beside
// the features that own them.
//
// 波 99（DataGrip 惯例的菜单骨架）：File 打头（连接管理 + 退出——Windows 上第
// 一个菜单叫 File，RoleAppMenu 是 macOS 专属），编辑动作归 Edit/Tools，数据动作
// 归 Database，呈现归 View。快捷键沿用 DataGrip 的肌肉记忆：Ctrl+Enter 跑、
// Ctrl+Shift+E 解释、Ctrl+Alt+Shift+F 重排、Ctrl+F4 关页签。

import (
	"github.com/egoist/mygo"
)

// setMenu wires the window's menu bar. It also keeps the items, so the
// keyboard-shortcuts window can list exactly what the menus declare — one
// source, no drift between the two.
func (app *App) setMenu() {
	items := []*mygo.MenuItem{
		{Label: "File", Submenu: []*mygo.MenuItem{
			{Label: "New Connection", Accelerator: "CmdOrCtrl+N",
				Click: func(it *mygo.MenuItem, w *mygo.Window) { app.openForm() }},
			{Label: "Connect", Accelerator: "CmdOrCtrl+Shift+N",
				Click: func(it *mygo.MenuItem, w *mygo.Window) { app.connect(app.connName) }},
			{Label: "Disconnect",
				Click: func(it *mygo.MenuItem, w *mygo.Window) { app.disconnect() }},
			mygo.Separator(),
			{Label: "Export Result as CSV",
				Click: func(it *mygo.MenuItem, w *mygo.Window) { app.exportResult(exportCSV) }},
			{Label: "Export Result as TSV",
				Click: func(it *mygo.MenuItem, w *mygo.Window) { app.exportResult(exportTSV) }},
			{Label: "Export Result as JSON",
				Click: func(it *mygo.MenuItem, w *mygo.Window) { app.exportResult(exportJSON) }},
			{Label: "Export Result as Excel",
				Click: func(it *mygo.MenuItem, w *mygo.Window) { app.exportResult(exportExcel) }},
			mygo.Separator(),
			{Role: mygo.RoleQuit},
		}},
		{Role: mygo.RoleEditMenu},
		{Label: "View", Submenu: []*mygo.MenuItem{
			{Label: "Refresh Schema", Accelerator: "CmdOrCtrl+F5",
				Click: func(it *mygo.MenuItem, w *mygo.Window) { app.refreshSchema() }},
			{Label: "Refresh Result", Accelerator: "F5",
				Click: func(it *mygo.MenuItem, w *mygo.Window) { app.refreshCurrent(nil) }},
			mygo.Separator(),
			{Label: "Clear Messages", Accelerator: "CmdOrCtrl+Shift+M",
				Click: func(it *mygo.MenuItem, w *mygo.Window) { app.clearMessages() }},
			mygo.Separator(),
			{Label: "Reset Split",
				Click: func(it *mygo.MenuItem, w *mygo.Window) { app.resetSplit() }},
			{Label: "Fullscreen", Accelerator: "F11",
				Click: func(it *mygo.MenuItem, w *mygo.Window) {
					if app.win != nil {
						app.win.ToggleFullScreen()
					}
				}},
		}},
		{Label: "Database", Submenu: []*mygo.MenuItem{
			{Label: "Connection Info…", Accelerator: "CmdOrCtrl+I",
				Click: func(it *mygo.MenuItem, w *mygo.Window) { app.openConnInfo() }},
			{Label: "Compare Schemas…",
				Click: func(it *mygo.MenuItem, w *mygo.Window) { app.openCompare() }},
			{Label: "ER Diagram", Accelerator: "CmdOrCtrl+Shift+G",
				Click: func(it *mygo.MenuItem, w *mygo.Window) { app.openER() }},
			mygo.Separator(),
			{Label: "Run Query", Accelerator: "CmdOrCtrl+Enter",
				Click: func(it *mygo.MenuItem, w *mygo.Window) { app.commitOrRun() }},
			{Label: "Run Selection", Accelerator: "CmdOrCtrl+Shift+Enter",
				Click: func(it *mygo.MenuItem, w *mygo.Window) { app.runSelection() }},
			{Label: "Run to Caret", Accelerator: "CmdOrCtrl+Shift+R",
				Click: func(it *mygo.MenuItem, w *mygo.Window) { app.runToCaret() }},
			{Label: "Explain Query", Accelerator: "CmdOrCtrl+Shift+E",
				Click: func(it *mygo.MenuItem, w *mygo.Window) { app.runExplain() }},
			{Label: "Execute with Parameters…", Accelerator: "CmdOrCtrl+Shift+P",
				Click: func(it *mygo.MenuItem, w *mygo.Window) { app.openParams() }},
			{Label: "Format SQL", Accelerator: "CmdOrCtrl+Shift+F",
				Click: func(it *mygo.MenuItem, w *mygo.Window) { app.formatSQL() }},
			mygo.Separator(),
			{Label: "Commit Changes",
				Click: func(it *mygo.MenuItem, w *mygo.Window) { app.commit() }},
			{Label: "Rollback Changes",
				Click: func(it *mygo.MenuItem, w *mygo.Window) { app.rollback() }},
			mygo.Separator(),
			{Label: "Commit Transaction",
				Click: func(it *mygo.MenuItem, w *mygo.Window) { app.commitTx() }},
			{Label: "Rollback Transaction",
				Click: func(it *mygo.MenuItem, w *mygo.Window) { app.rollbackTx() }},
		}},
		{Label: "Tools", Submenu: []*mygo.MenuItem{
			{Label: "Statement History", Accelerator: "CmdOrCtrl+H",
				Click: func(it *mygo.MenuItem, w *mygo.Window) { app.openHistory() }},
			{Label: "Snippets…", Accelerator: "CmdOrCtrl+Shift+S",
				Click: func(it *mygo.MenuItem, w *mygo.Window) { app.openSnippets() }},
			{Label: "Sessions…",
				Click: func(it *mygo.MenuItem, w *mygo.Window) { app.openSessions() }},
			{Label: "Server Variables…",
				Click: func(it *mygo.MenuItem, w *mygo.Window) { app.openServerVars() }},
			{Label: "Users…",
				Click: func(it *mygo.MenuItem, w *mygo.Window) { app.openUsers() }},
			{Label: "Query Builder…",
				Click: func(it *mygo.MenuItem, w *mygo.Window) { app.openBuilder() }},
			{Label: "Data Transfer…",
				Click: func(it *mygo.MenuItem, w *mygo.Window) { app.openTransfer() }},
			{Label: "Model Designer…",
				Click: func(it *mygo.MenuItem, w *mygo.Window) { app.openModelEditor() }},
			{Label: "Export Data Dictionary…",
				Click: func(it *mygo.MenuItem, w *mygo.Window) { app.exportDictionary() }},
			{Label: "Generate Data…",
				Click: func(it *mygo.MenuItem, w *mygo.Window) { app.openDataGen() }},
			mygo.Separator(),
			{Label: "Open Script…", Accelerator: "CmdOrCtrl+O",
				Click: func(it *mygo.MenuItem, w *mygo.Window) { app.openScriptFile() }},
			{Label: "Save Script", Accelerator: "CmdOrCtrl+S",
				Click: func(it *mygo.MenuItem, w *mygo.Window) { app.saveScriptFile() }},
			{Label: "Save Script As…",
				Click: func(it *mygo.MenuItem, w *mygo.Window) { app.saveScriptAs() }},
			{Label: "Backup Database…",
				Click: func(it *mygo.MenuItem, w *mygo.Window) { app.backupDB() }},
			{Label: "Restore Database…",
				Click: func(it *mygo.MenuItem, w *mygo.Window) { app.restoreDB() }},
			{Label: "Columns…",
				Click: func(it *mygo.MenuItem, w *mygo.Window) { app.openColumns() }},
			{Label: "Plugins…",
				Click: func(it *mygo.MenuItem, w *mygo.Window) { app.openPlugins() }},
			mygo.Separator(),
			{Label: "Copy Row as INSERT",
				Click: func(it *mygo.MenuItem, w *mygo.Window) { app.copyAs(copyInsert) }},
			{Label: "Copy Row as UPDATE",
				Click: func(it *mygo.MenuItem, w *mygo.Window) { app.copyAs(copyUpdate) }},
			{Label: "Copy Row as DELETE",
				Click: func(it *mygo.MenuItem, w *mygo.Window) { app.copyAs(copyDelete) }},
			{Label: "Copy Rows as CSV",
				Click: func(it *mygo.MenuItem, w *mygo.Window) { app.copyAs(copyCSV) }},
			{Label: "Copy Rows as TSV",
				Click: func(it *mygo.MenuItem, w *mygo.Window) { app.copyAs(copyTSV) }},
			mygo.Separator(),
			{Label: "Clear History",
				Click: func(it *mygo.MenuItem, w *mygo.Window) { app.clearHistory() }},
		}},
		{Role: mygo.RoleWindowMenu},
		{Label: "Help", Submenu: []*mygo.MenuItem{
			{Label: "Keyboard Shortcuts", Accelerator: "CmdOrCtrl+Shift+/",
				Click: func(it *mygo.MenuItem, w *mygo.Window) { app.openShortcuts() }},
			mygo.Separator(),
			{Label: "About DB Studio Go",
				Click: func(it *mygo.MenuItem, w *mygo.Window) { app.openAbout() }},
			{Label: "Connection Info…", Accelerator: "CmdOrCtrl+I",
				Click: func(it *mygo.MenuItem, w *mygo.Window) { app.openConnInfo() }},
		}},
	}
	app.menuItems = items
	app.win.SetMenu(mygo.NewMenu(items))
}
