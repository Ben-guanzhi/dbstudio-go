package main

// 只读连接必须挡住所有写路径——不只是脚本/网格，也包括内核能力直发
// （rename/kill/user DDL/restore 不经过 runScriptGuard）。

import (
	"testing"
)

func TestReadOnlyBlocksCapabilityWrites(t *testing.T) {
	app := &App{open: true, readOnlyOn: true}
	// 这些入口在 readOnly 下必须**立刻返回**，不能走到各自的驱动调用。
	// 用 nil db 也能验证：若闸门失效就会 panic（nil deref）——这是刻意设计。
	app.renameNode.label = "users"
	app.renameInput = "staff" // 非空且不同于旧名——无闸门会继续走到 app.db

	app.doRename() // 无闸门 → panic（app.db nil）
	if app.status == "Renaming…" {
		t.Fatal("rename 在只读连接上没有被拒绝")
	}

	app.sessionSel = "42"
	app.killSelectedSession() // 无闸门 → panic（app.sessions nil）

	app.createUser()
	app.dropSelectedUser()
	app.grantSelectedUser() // 无闸门 → panic（app.db nil）

	app.restoreDB() // 无闸门 → panic（app.open 会过、engine() 用 app.db）

	if app.errMsg == "" && app.status == "" {
		t.Fatal("只读写路径既没报错也没状态——闸门可能没生效")
	}
}
