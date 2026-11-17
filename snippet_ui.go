package main

// 片段库 UI（Navicat 片段 / DataGrip live templates 的 mygo 形态）：一个 modal
// 列出片段——选中载入编辑器；「Save from editor」把当前编辑器内容存为片段
// （名字用行内输入框起）。数据在 snippet.go 的 SnippetStore。

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

// openSnippets 打开片段库 modal（Tools 菜单 / Ctrl+Shift+S）。
func (app *App) openSnippets() {
	app.snippetOpen = true
	app.snippetMsg = ""
	app.invalidate()
}

// snippetView draws the snippets modal: a sidebar list (same widget the history
// modal uses) plus the save/delete actions. Choosing a snippet loads its SQL
// into the editor; saving takes the editor's current text.
func (app *App) snippetView(c *ui.Context) {
	if !app.snippetOpen {
		return
	}
	t := c.Theme()
	ui.Modal(c, &app.snippetOpen, func() {
		ui.Column(c).Gap(8).Padding(12).Width(680).Height(520).
			Background(t.Surface).Radius(10).Border(1, t.Border).
			Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "Snippets").FontSize(13).Bold()
					ui.Spacer(c)
					// 从编辑器保存：名字取行内输入框；同名覆盖 = 更新。
					ui.TextInput(c, &app.snippetNameInput).Width(180).Placeholder("片段名")
					if ui.Button(c, "Save from editor").FontSize(10).
						Tooltip("把当前编辑器内容存为片段（同名覆盖）").
						Clicked() {
						name := strings.TrimSpace(app.snippetNameInput)
						if name != "" && strings.TrimSpace(app.edBuf.String()) != "" {
							_ = app.snippets.Put(Snippet{
								Name: name,
								SQL:  app.edBuf.String(),
							})
							app.snippetMsg = "已保存片段 " + name
							app.snippetID = name
							app.snippetNameInput = ""
						}
						app.invalidate()
					}
					// 删除选中的片段。
					if ui.Button(c, "Delete").FontSize(10).
						Disabled(app.snippetID == "").Clicked() {
						if err := app.snippets.Delete(app.snippetID); err == nil {
							app.snippetMsg = "已删除 " + app.snippetID
							app.snippetID = ""
						}
						app.invalidate()
					}
					if ui.Button(c, "Close").Clicked() {
						app.snippetOpen = false
					}
				})
				if app.snippetMsg != "" {
					ui.Text(c, app.snippetMsg).FontSize(10).TextColor(t.TextMuted).SingleLine()
				}
				ui.Text(c, "选中载入编辑器；同名保存即更新。片段跨连接复用（历史/收藏绑定当时的连接）。").
					FontSize(10).TextColor(t.TextMuted).SingleLine()
				app.snippetSidebar(c, t)
			})
	})
}

// snippetSidebar 列出片段（按名字排序）。行点击只选中，双击语义由「载入」
// 按钮承担（mygo 无双击事件）；选中变化即载入（与 history 一致）。
func (app *App) snippetSidebar(c *ui.Context, t *ui.Theme) {
	list := app.snippets.List()
	if len(list) == 0 {
		ui.Text(c, "还没有片段——把常用 SQL 放进编辑器，起个名字 Save。").
			FontSize(10).TextColor(t.TextMuted).SingleLine()
		return
	}
	prev := app.snippetID
	ui.Sidebar(c, &app.snippetID, func() {
		for _, sn := range list {
			item := ui.SidebarItem(c, sn.Name, nil, snippetLabel(sn))
			_ = item
		}
	})
	// 选中变化即载入：所选片段的 SQL 回编辑器。
	if app.snippetID != prev && app.snippetID != "" {
		for _, sn := range list {
			if sn.Name == app.snippetID {
				app.loadSnippet(sn)
				break
			}
		}
	}
}

// snippetLabel 一行摘要：名字 + 首行预览。
func snippetLabel(sn Snippet) string {
	first := strings.TrimSpace(strings.SplitN(sn.SQL, "\n", 2)[0])
	first = strings.ReplaceAll(first, "\t", " ")
	r := []rune(first)
	if len(r) > 44 {
		first = string(r[:44]) + "…"
	}
	return first
}

// loadSnippet 把片段 SQL 放进编辑器并关掉 modal。
func (app *App) loadSnippet(sn Snippet) {
	app.edBuf.Set(sn.SQL)
	app.snippetOpen = false
	app.status = "已载入片段 " + sn.Name
	app.invalidate()
}
