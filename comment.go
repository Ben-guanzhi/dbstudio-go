package main

// 编辑器注释切换（§347）：Ctrl+/ 对光标所在行加/移除 "-- " 前缀。
// 行内语义走内核 sqltext.ToggleLineComment（与 fynequery/TableProGo 同源）；
// 光标定位用 edState 的字节偏移换算行号，写回后光标落在该行行尾。

import (
	"strings"

	"github.com/Ben-guanzhi/dbcore/sqltext"
)

// toggleComment 是 Ctrl+/ 的入口：编辑器当前行切换 "-- " 注释。
func (app *App) toggleComment() {
	if app.edBuf == nil || app.edState == nil {
		return
	}
	text := app.edBuf.String()
	pos := app.edState.Cursor()
	if pos > len(text) {
		pos = len(text)
	}
	lines := strings.Split(text, "\n")
	// 字节偏移 → 行号：逐行累加长度（含换行符）。
	row, acc := 0, 0
	for i, l := range lines {
		if pos <= acc+len(l) {
			row = i
			break
		}
		acc += len(l) + 1 // the '\n'
		row = i
	}
	if row >= len(lines) {
		row = len(lines) - 1
	}
	lines[row] = sqltext.ToggleLineComment(lines[row])
	app.edBuf.Set(strings.Join(lines, "\n"))
	// 光标回到切换行的行尾（与 fynequery 同款行为）。
	lineStart := 0
	for i := 0; i < row; i++ {
		lineStart += len(lines[i]) + 1
	}
	app.edState.Select(lineStart+len(lines[row]), lineStart+len(lines[row]))
	app.invalidate()
}
