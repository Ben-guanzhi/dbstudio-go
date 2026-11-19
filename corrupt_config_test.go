package main

// 坏配置保护（§339）：connections.json 解析失败后，第一次 save 必须拒绝
// 覆盖（内存里没有旧连接——直接写=抹掉全部记录），坏文件留档 .corrupt；
// 用户处理后第二次 save 放行。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCorruptConnectionsRefuseOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "connections.json")
	// 一个解析失败的文件（截断的 JSON）。
	if err := os.WriteFile(path, []byte(`{"connections":[{"name":"lost"`), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewStoreAt(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.load(); err == nil {
		t.Fatal("the truncated file must fail to parse")
	}
	// 内存里加一条新连接（模拟用户重建）。
	store.mu.Lock()
	store.conns = []Conn{{Name: "rebuilt", Host: "h"}}
	store.mu.Unlock()

	// 第一次 save：拒绝覆盖，坏文件留档 .corrupt，错误可操作。
	err = store.save()
	if err == nil || !strings.Contains(err.Error(), ".corrupt") {
		t.Fatalf("save must refuse with a .corrupt note, got %v", err)
	}
	if _, err := os.Stat(path + ".corrupt"); err != nil {
		t.Fatalf("the broken file must be archived: %v", err)
	}
	// 原文件必须仍是坏内容（复制留档而非改名——坏文件留在原位让用户看见）。
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), `"lost"`) {
		t.Fatal("the broken content vanished before the user could see it")
	}

	// 第二次 save（loadFailed 已清）：放行，写的是内存里的新清单。
	if err := store.save(); err != nil {
		t.Fatalf("the follow-up save should proceed: %v", err)
	}
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), `"rebuilt"`) {
		t.Fatal("the follow-up save should carry the new connections")
	}
}
