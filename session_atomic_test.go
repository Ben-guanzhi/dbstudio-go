package main

// 会话持久化的原子性：saveSession 必须走 writeFileAtomic（tmp + rename），
// 与 connections/history 同纪律——崩溃不会留下半截 JSON 让整个工作区丢失。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveSessionWritesValidJSON(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStoreAt(filepath.Join(dir, "connections.json"))
	if err != nil {
		t.Fatal(err)
	}
	app := &App{store: store, consolesel: 0}
	app.initConsoles()
	app.consoles[0].buf.Set("SELECT 42;")
	app.saveSession()

	raw, err := os.ReadFile(app.sessionPath())
	if err != nil {
		t.Fatal(err)
	}
	var s sessionState
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("session.json 不是合法 JSON: %v", err)
	}
	if len(s.Consoles) != 1 || !strings.Contains(s.Consoles[0].SQL, "SELECT 42") {
		t.Fatalf("session 内容不对: %+v", s.Consoles)
	}
	// 原子写不会在同目录留下 .tmp 残留。
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Errorf("原子写留下了临时文件: %s", e.Name())
		}
	}
}
