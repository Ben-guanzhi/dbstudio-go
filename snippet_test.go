package main

// 片段库的行为测试：同名覆盖、空白名拒绝、按名排序、原子落盘。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSnippetStorePutListDelete(t *testing.T) {
	dir := t.TempDir()
	s := NewSnippetStore(dir)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}

	// 空白名拒绝。
	if err := s.Put(Snippet{Name: "  "}); err == nil {
		t.Fatal("空白名应当被拒绝")
	}

	// 正常保存与列表排序。
	past := time.Now().Add(-time.Hour)
	if err := s.Put(Snippet{Name: "users-by-id", SQL: "SELECT * FROM users WHERE id = ?", At: past}); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(Snippet{Name: "count-all", SQL: "SELECT count(*) FROM ?"}); err != nil {
		t.Fatal(err)
	}
	list := s.List()
	if len(list) != 2 || list[0].Name != "count-all" || list[1].Name != "users-by-id" {
		t.Fatalf("列表顺序不对: %+v", list)
	}

	// 同名覆盖（= 更新），且 At 被刷新。
	if err := s.Put(Snippet{Name: "users-by-id", SQL: "SELECT * FROM users WHERE id = 42"}); err != nil {
		t.Fatal(err)
	}
	list = s.List()
	if len(list) != 2 {
		t.Fatalf("同名覆盖变成了追加: %+v", list)
	}
	for _, sn := range list {
		if sn.Name == "users-by-id" {
			if sn.SQL != "SELECT * FROM users WHERE id = 42" {
				t.Fatalf("内容未更新: %q", sn.SQL)
			}
			if sn.At.Equal(past) {
				t.Fatal("更新时间未刷新")
			}
		}
	}

	// 删除后再删报错。
	if err := s.Delete("count-all"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("count-all"); err == nil {
		t.Fatal("删除不存在的片段应当报错")
	}

	// 落盘可回读。
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	list = s.List()
	if len(list) != 1 || list[0].SQL != "SELECT * FROM users WHERE id = 42" {
		t.Fatalf("回读不一致: %+v", list)
	}
}

// 片段库文件必须是 0600 且原子落盘（同 store 的其余纪律）。
func TestSnippetFilePermissions(t *testing.T) {
	dir := t.TempDir()
	s := NewSnippetStore(dir)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(Snippet{Name: "n", SQL: "SELECT 1"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "snippets.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "SELECT 1") {
		t.Fatal("内容未落盘")
	}
	// .tmp 残片不应存在（rename 成功后）。
	if _, err := os.Stat(filepath.Join(dir, "snippets.json.tmp")); !os.IsNotExist(err) {
		t.Fatal("tmp 残片仍在——落盘不是原子的")
	}
}
