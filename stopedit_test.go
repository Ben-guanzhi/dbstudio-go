package main

// Esc 退出编辑模式——死代码清理时「接上」的那一个功能。
//
// stopEdit 曾是一个完整的函数，但没有入口（deadcode 报的不可达）。DataGrip
// 的 Esc 就是退出编辑，所以把它接上，而不是删掉。

import (
	"testing"

	"github.com/Ben-guanzhi/dbcore"
	"github.com/egoist/mygo/ui"
)

func TestEscapeLeavesEditMode(t *testing.T) {
	app, tab := gridApp(t)
	tab.schema, tab.table = "main", "employees"
	app.edit.init(tab.result, "main", "employees",
		[]dbcore.ColumnInfo{{Name: "id", Type: "INTEGER"}, {Name: "name", Type: "TEXT"}},
		nil)
	if !app.edit.active {
		t.Fatal("编辑模式没被激活")
	}

	tt := ui.NewTester(app.view, 1320, 800)
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	tt.Frame()

	if app.edit.active {
		t.Error("Esc 之后仍在编辑模式")
	}
}

// 速查表列出这颗键（不变量已钉住「处理的键都要列」，这里再从屏幕确认）。
func TestEscapeIsInTheShortcutSheet(t *testing.T) {
	app := newViewApp(t, nil)
	app.shortcutsOpen = true
	tt := ui.NewTester(app.view, 700, 700)
	tt.Frame()
	if !tt.HasText("Exit edit mode") || !tt.HasText("Escape") {
		t.Errorf("速查表没有 Escape 这一行；texts = %v", tt.Texts())
	}
}
