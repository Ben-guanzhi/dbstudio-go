package main

// 备份的可恢复性——备份的价值全在「能恢复」。
//
// 这些测试不走「看起来对」的路线：它们把 dump 的产物**真的导入一个新库**，
// 再比对两边的内容。一个导不回去的备份比没有备份更危险（它给人已经备份的
// 错觉）。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ben-guanzhi/dbcore/sqlite"
)

// newSQLite 建一个临时库并返回它的绝对路径。
func newSQLite(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "src.db")
}

// execAll 在 path 指向的库上逐条执行 statements（分号分隔）。
func execAll(t *testing.T, path, statements string) {
	t.Helper()
	drv, err := sqlite.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer drv.Close()
	sql, ok := drv.AsSQL()
	if !ok {
		t.Fatal("no SQL interface")
	}
	ctx := context.Background()
	for _, stmt := range strings.Split(statements, ";\n") {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := sql.Execute(ctx, stmt); err != nil {
			t.Fatalf("执行 %q: %v", truncate(stmt, 60), err)
		}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// queryAll 读一个库里的 (id, name, note)，按 id 排序，用于比对。
func queryAll(t *testing.T, path string) [][3]string {
	t.Helper()
	drv, err := sqlite.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer drv.Close()
	sql, _ := drv.AsSQL()
	res, err := sql.Execute(context.Background(), "SELECT id, name, note FROM t ORDER BY id")
	if err != nil {
		t.Fatalf("查询: %v", err)
	}
	var out [][3]string
	for _, row := range res.Rows {
		rec := [3]string{}
		for i := 0; i < 3 && i < len(row); i++ {
			if row[i].Kind == 0 { // ValNull
				rec[i] = "NULL"
			} else {
				rec[i] = row[i].String()
			}
		}
		out = append(out, rec)
	}
	return out
}

func TestDumpSQLiteRestores(t *testing.T) {
	src := newSQLite(t)
	execAll(t, src, `
CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT NOT NULL, note BLOB);
INSERT INTO t (id, name, note) VALUES (1, 'Ada', X'48656C6C6F');
INSERT INTO t (id, name, note) VALUES (2, 'Alan', NULL);
CREATE INDEX idx_t_name ON t(name);
`)
	want := queryAll(t, src)

	dump, err := dumpSQLite(context.Background(), src)
	if err != nil {
		t.Fatalf("dumpSQLite: %v", err)
	}
	t.Logf("dump 共 %d 字节", len(dump))

	// 产物里该有的东西。（BLOB 写作 X'…'，十六进制小写随编码器。）
	for _, wantSub := range []string{"CREATE TABLE", "CREATE INDEX", "'Ada'", "X'48656c6c6f'", "BEGIN TRANSACTION", "COMMIT"} {
		if !strings.Contains(dump, wantSub) {
			t.Errorf("dump 里没有 %q:\n%s", wantSub, dump)
		}
	}

	// 真的导回去：新库执行 dump，再比对。
	dst := filepath.Join(t.TempDir(), "dst.db")
	execAll(t, dst, dump)
	got := queryAll(t, dst)

	if len(got) != len(want) {
		t.Fatalf("恢复后有 %d 行, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 行 = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestDumpSQLiteEmptyTable(t *testing.T) {
	src := newSQLite(t)
	execAll(t, src, `CREATE TABLE empty (id INTEGER PRIMARY KEY, name TEXT);`)
	dump, err := dumpSQLite(context.Background(), src)
	if err != nil {
		t.Fatalf("dumpSQLite: %v", err)
	}
	if !strings.Contains(dump, "CREATE TABLE empty") {
		t.Errorf("空表的结构没导出")
	}
	// 空表没有 INSERT。
	if strings.Contains(dump, "INSERT INTO") {
		t.Errorf("空表却导出了 INSERT")
	}
}

func TestDumpSQLiteMissingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such.db")
	if _, err := dumpSQLite(context.Background(), missing); err == nil {
		t.Error("不存在的库文件没有报错")
	}
}

func TestDumpSQLiteRefusesANonSQLiteDriver(t *testing.T) {
	// dumpSQLite 只接文件路径（SQLite 专有）；别的引擎该由调用方分流。
	// 这里确认它面对一个空路径时报错而不是崩。
	if _, err := dumpSQLite(context.Background(), ""); err == nil {
		t.Error("空路径没有报错")
	}
	_ = os.Getenv
}

// TestBackupWritesARestorableFile 是端到端的那一条：backupDB 真的写出一个
// 文件，而那个文件导回新库后数据一致。
func TestBackupWritesARestorableFile(t *testing.T) {
	src := newSQLite(t)
	execAll(t, src, `
CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT NOT NULL, note BLOB);
INSERT INTO t (id, name, note) VALUES (1, 'Ada', X'48656C6C6F');
INSERT INTO t (id, name, note) VALUES (2, 'Alan', NULL);
`)
	want := queryAll(t, src)

	dir := t.TempDir()
	app := &App{open: true}
	app.conn = Conn{Name: "local", Kind: "sqlite", File: src}
	store, err := NewStoreAt(filepath.Join(dir, "connections.json"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	app.store = store

	app.backupDB()

	// 备份在 goroutine 里，等文件出现（最多几秒）。
	deadline := time.Now().Add(5 * time.Second)
	var path string
	for time.Now().Before(deadline) {
		m, _ := filepath.Glob(filepath.Join(dir, "backup-*.sql"))
		if len(m) > 0 {
			path = m[0]
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if path == "" {
		t.Fatalf("备份没有写出文件；状态 = %q", app.status)
	}
	if app.status == "" || !strings.Contains(app.status, "Backup written") {
		t.Errorf("状态没有报告备份完成：%q", app.status)
	}

	// 产物导回新库，比对。
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取备份: %v", err)
	}
	dst := filepath.Join(t.TempDir(), "restored.db")
	execAll(t, dst, string(raw))
	got := queryAll(t, dst)
	if len(got) != len(want) {
		t.Fatalf("恢复后 %d 行, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 行 = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestBackupRefusesANonSQLiteEngine(t *testing.T) {
	app := &App{open: true}
	app.conn = Conn{Name: "remote", Kind: "mysql", Host: "127.0.0.1"}
	app.backupDB()
	if !strings.Contains(app.status, "SQLite only") {
		t.Errorf("非 SQLite 的备份请求没有被拒绝：%q", app.status)
	}
}
