package main

// The keyboard shortcuts, on one screen.
//
// Every accelerator the menus declare is already in the menu bar, but a user
// has to open each menu to find one. DBeaver, DataGrip and TablePlus all put
// the whole list in a window, and it is the first thing a new user looks for.
//
// The list is read from the menu items themselves (setMenu keeps them), so
// adding a menu entry cannot leave this sheet behind: one source, no drift.

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// openShortcuts shows the shortcut sheet.
func (app *App) openShortcuts() {
	app.shortcutsOpen = true
	app.invalidate()
}

// shortcutRow is one line of the sheet: the menu it lives under, what it does
// and the keys that do it.
type shortcutRow struct {
	menu, action, keys string
}

// shortcutRows walks the menus and collects every item that declares an
// accelerator. Items without one (an action with no keys) are not shortcuts
// and are left out, and the system menus (Edit, Window) carry no label of
// their own.
// workbenchShortcuts 是**不在菜单里**的快捷键：编辑器与工作台的手势，由
// view.go / pager.go 直接处理。它们必须单列一组——否则速查只列菜单声明过的
// 键，用户看不到这九颗。
var workbenchShortcuts = []shortcutRow{
	{menu: "Workbench", action: "Run statement / commit", keys: "CmdOrCtrl+Enter"},
	{menu: "Workbench", action: "New console", keys: "CmdOrCtrl+T"},
	{menu: "Workbench", action: "Close console", keys: "CmdOrCtrl+Shift+W"},
	{menu: "Workbench", action: "Next console", keys: "CmdOrCtrl+Tab"},
	{menu: "Workbench", action: "Undo working-copy edit", keys: "CmdOrCtrl+Z"},
	{menu: "Workbench", action: "Refresh result", keys: "F5"},
	{menu: "Workbench", action: "Fullscreen", keys: "F11"},
	{menu: "Workbench", action: "Previous page", keys: "Alt+Left"},
	{menu: "Workbench", action: "Next page", keys: "Alt+Right"},
}

func (app *App) shortcutRows() []shortcutRow {
	var out []shortcutRow
	for _, m := range app.menuItems {
		if m.Submenu == nil {
			continue
		}
		for _, it := range m.Submenu {
			if it.Accelerator == "" || it.Label == "" {
				continue
			}
			out = append(out, shortcutRow{menu: m.Label, action: it.Label, keys: it.Accelerator})
		}
	}
	return append(out, workbenchShortcuts...)
}

// shortcutsView draws the sheet: the menus as sections, each item a row of
// what it does and the keys that do it, the keys in a keycap so they read as
// keys rather than as prose.
func (app *App) shortcutsView(c *ui.Context) {
	if !app.shortcutsOpen {
		return
	}
	t := c.Theme()
	ui.Modal(c, &app.shortcutsOpen, func() {
		ui.Column(c).Gap(8).Padding(12).Width(640).Height(520).
			Background(t.Surface).Radius(10).Border(1, t.Border).
			Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "Keyboard Shortcuts").FontSize(13).Bold()
					ui.Spacer(c)
					if ui.Button(c, "Close").Clicked() {
						app.shortcutsOpen = false
					}
				})
				ui.Text(c, "Every key the app answers to — menus and editor.").
					FontSize(10).TextColor(t.TextMuted).SingleLine()
				ui.Scroll(c).Grow(1).FillWidth().KeepScrollbars(true).Children(func() {
					rows := app.shortcutRows()
					if len(rows) == 0 {
						ui.Text(c, "No shortcuts yet").FontSize(10).TextColor(t.TextMuted).SingleLine()
						return
					}
					group := ""
					for _, r := range rows {
						if r.menu != group {
							group = r.menu
							ui.Text(c, group).FontSize(11).Bold().TextColor(t.Accent).
								Padding(0, 0, 4, 0).SingleLine()
						}
						ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
							ui.Text(c, r.action).FontSize(10).SingleLine().FillWidth()
							ui.Text(c, r.keys).FontSize(10).TextColor(t.TextMuted).
								Padding(1, 4).BorderWidth(1).BorderColor(t.Border)
						})
					}
				})
			})
	})
}

var _ = mygo.Separator
