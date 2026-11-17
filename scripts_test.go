package main

// 脚本文件绑定的核心：保存往返（内容一致）、0600 权限（脚本可能含敏感值）、
// tab 标签跟随文件。

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui/editor"
)

func TestWriteScriptRoundTrip(t *testing.T) {
	dir := t.TempDir()
	var app App
	app.store, _ = NewStoreAt(filepath.Join(dir, "connections.json"))
	path := filepath.Join(dir, "s.sql")

	// 手工构造一个绑定文件的 console（App 的 editor 依赖在 UI 测试里另有覆盖）。
	app.consoles = []consoleTab{{buf: editor.NewBuffer("-- q\nSELECT 1;"), label: "t"}}
	app.consolesel = 0
	app.consoles[0].file = path

	app.writeScript(0, path)

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "-- q\nSELECT 1;" {
		t.Errorf("往返不一致: %q", string(raw))
	}
	// 权限：0600（脚本可能含敏感值——表名、内网主机、内联口令）。
	// Windows 的 perm 位与 POSIX 不互通（NTFS ACL 是另一套模型，666 在
	// Windows 上就是「owner 可写」的正常映射）——只测 Unix。
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("权限 = %o, want 600", perm)
		}
	}
	// 标签跟随文件名。
	if app.consoles[0].label != "s.sql" {
		t.Errorf("标签 = %q, want s.sql", app.consoles[0].label)
	}
}

func TestConsoleDirtyDetectsUnsaved(t *testing.T) {
	dir := t.TempDir()
	var app App
	app.store, _ = NewStoreAt(filepath.Join(dir, "connections.json"))
	path := filepath.Join(dir, "s.sql")
	app.consoles = []consoleTab{{buf: editor.NewBuffer("SELECT 1;"), label: "t"}}
	app.consolesel = 0
	app.consoles[0].file = path
	app.writeScript(0, path)

	// 保存后不脏。
	if app.consoleDirty() {
		t.Error("刚保存的 tab 不应标记为脏")
	}
	// 改 buffer 后脏。
	app.consoles[0].buf.Set("SELECT 2;")
	if !app.consoleDirty() {
		t.Error("改动后应标记为脏")
	}
	// 标签加 ● 前缀。
	app.markConsole(0)
	if !strings.HasPrefix(app.consoles[0].label, "●") {
		t.Errorf("脏标签应带 ● 前缀: %q", app.consoles[0].label)
	}
}
