package main

// 注释切换的行为测试（§347）：光标所在行加/移除 "-- "，缩进保留，
// 光标落回该行行尾；光标偏移与行号的换算正确（多行文本中段）。

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui/editor"
)

func TestToggleCommentAtCursor(t *testing.T) {
	app := &App{}
	app.edBuf = editor.NewBuffer("SELECT 1\n  SELECT 2\nSELECT 3")
	app.edState = &editor.State{Buffer: app.edBuf}

	// 光标在第二行（偏移 = len("SELECT 1\n") + 2）。
	app.edState.Select(11, 11)
	app.toggleComment()

	want := "SELECT 1\n  -- SELECT 2\nSELECT 3"
	if got := app.edBuf.String(); got != want {
		t.Fatalf("buffer = %q, want %q", got, want)
	}
	// 光标落在该行行尾。
	wantCursor := 11 + len("-- SELECT 2")
	if got := app.edState.Cursor(); got != wantCursor {
		t.Fatalf("cursor = %d, want %d", got, wantCursor)
	}

	// 再切一次回到原文。
	app.toggleComment()
	if got := app.edBuf.String(); !strings.Contains(got, "  SELECT 2") || strings.Contains(got, "--") {
		t.Fatalf("second toggle = %q", got)
	}
}

func TestToggleCommentCursorAtEnd(t *testing.T) {
	app := &App{}
	app.edBuf = editor.NewBuffer("SELECT 1")
	app.edState = &editor.State{Buffer: app.edBuf}
	app.edState.Select(99, 99) // 越界偏移钳到末尾
	app.toggleComment()
	if got := app.edBuf.String(); got != "-- SELECT 1" {
		t.Fatalf("buffer = %q", got)
	}
}
