package main

// 外部 dump 工具调用的安全底线与参数正确性。
//
// 最要紧的一条：**口令绝不进 argv**。argv 对同机的任何进程可见（任务管理器、
// /proc），那是密码的泄露面——mysqldump / pg_dump 都因此提供了环境变量
// （MYSQL_PWD / PGPASSWORD）。这些测试直接断言参数里没有口令。

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPgDumpArgsCarryTheConnection(t *testing.T) {
	conn := Conn{Name: "pg", Kind: "postgres", Host: "db.example", Port: 5433, User: "alice", Password: "s3cret", Database: "shop"}
	args := pgDumpArgs(conn, `C:\backups\pg.sql`)

	joined := strings.Join(args, " ")
	for _, want := range []string{"--host=db.example", "--port=5433", "--username=alice", "--dbname=shop"} {
		if !strings.Contains(joined, want) {
			t.Errorf("参数里没有 %q：%v", want, args)
		}
	}
	if strings.Contains(joined, "s3cret") {
		t.Errorf("口令进了 argv：%v", args)
	}
	if !strings.Contains(joined, "--file=") {
		t.Errorf("产物路径没传给工具：%v", args)
	}
}

func TestMysqlDumpArgsCarryTheConnection(t *testing.T) {
	conn := Conn{Name: "my", Kind: "mysql", Host: "db.example", Port: 3307, User: "bob", Password: "hunter2", Database: "shop", SSLMode: "require"}
	args := mysqlDumpArgs(conn, "/tmp/my.sql")

	joined := strings.Join(args, " ")
	for _, want := range []string{"--host=db.example", "--port=3307", "--user=bob", "--protocol=TCP"} {
		if !strings.Contains(joined, want) {
			t.Errorf("参数里没有 %q：%v", want, args)
		}
	}
	// 一致性快照与流式读：备份不该锁表、不该把大表读进内存。
	for _, want := range []string{"--single-transaction", "--quick", "--result-file=/tmp/my.sql"} {
		if !strings.Contains(joined, want) {
			t.Errorf("参数里没有 %q：%v", want, args)
		}
	}
	// 库名是最后一个位置参数。
	if args[len(args)-1] != "shop" {
		t.Errorf("库名不是最后一个参数：%v", args)
	}
	if strings.Contains(joined, "hunter2") {
		t.Errorf("口令进了 argv：%v", args)
	}
}

func TestDumpPasswordGoesToTheEnvironment(t *testing.T) {
	conn := Conn{Kind: "postgres", Password: "pg-pass"}
	env := dumpToolEnv(conn, true)
	if !hasEnv(env, "PGPASSWORD=pg-pass") {
		t.Errorf("postgres 的口令没进环境变量：%v", env)
	}
	for _, kv := range env {
		if strings.HasPrefix(kv, "PGPASSWORD=") && kv != "PGPASSWORD=pg-pass" {
			t.Errorf("PGPASSWORD 被设成了别的值：%s", kv)
		}
	}

	conn = Conn{Kind: "mysql", Password: "my-pass"}
	env = dumpToolEnv(conn, false)
	if !hasEnv(env, "MYSQL_PWD=my-pass") {
		t.Errorf("mysql 的口令没进环境变量：%v", env)
	}
}

func hasEnv(env []string, want string) bool {
	for _, kv := range env {
		if kv == want {
			return true
		}
	}
	return false
}

// TestResolveDumpToolHonoursTheEnvOverride：环境变量指向的工具优先（测试与
// 打包部署用它指定路径）。
func TestResolveDumpToolHonoursTheEnvOverride(t *testing.T) {
	dir := t.TempDir()
	tool := dir + "/my-pg_dump"
	if err := os.WriteFile(tool, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Skipf("建不了假工具：%v", err)
	}
	t.Setenv(envPgDump, tool)
	got, err := resolveDumpTool(envPgDump, "pg_dump")
	if err != nil {
		t.Fatalf("resolveDumpTool: %v", err)
	}
	if got != tool {
		t.Errorf("工具 = %q, want %q", got, tool)
	}
}

func TestResolveDumpToolMissing(t *testing.T) {
	t.Setenv(envMysqlDump, "")
	t.Setenv("PATH", t.TempDir())
	if _, err := resolveDumpTool(envMysqlDump, "mysqldump"); err == nil {
		t.Error("工具缺失时报错")
	}
}

// TestRunDumpToolCallsTheToolAndVerifiesOutput 是端到端的那一条：一个真的可执行
// 文件被调用，产物被写出，参数与环境变量都传到。
func TestRunDumpToolCallsTheToolAndVerifiesOutput(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.sql")
	tool := filepath.Join(dir, "fake-dump")
	var script, name string
	if runtime.GOOS == "windows" {
		name = tool + ".bat"
		// cmd：%* 是全部参数，> 写文件。
		script = "@echo off\r\n> \"" + out + "\" echo %*\r\n>> \"" + out + "\" echo MYSQL_PWD=%MYSQL_PWD%\r\n"
	} else {
		name = tool
		script = "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"" + out + "\"\nprintf 'MYSQL_PWD=%s\\n' \"$MYSQL_PWD\" >> \"" + out + "\"\n"
	}
	if err := os.WriteFile(name, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := runDumpTool(context.Background(), name,
		[]string{"--host=db", "--user=alice"}, []string{"MYSQL_PWD=secret"}, out); err != nil {
		t.Fatalf("runDumpTool: %v", err)
	}
	if err := verifyDumpFile(out); err != nil {
		t.Fatalf("verifyDumpFile: %v", err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	if !strings.Contains(got, "--host=db") || !strings.Contains(got, "--user=alice") {
		t.Errorf("工具没收到参数：%s", got)
	}
	if !strings.Contains(got, "MYSQL_PWD=secret") {
		t.Errorf("工具没收到环境变量里的口令：%s", got)
	}
}

// TestVerifyDumpFileRejectsEmpty：空产物必须失败——备份的价值全在产物上。
func TestVerifyDumpFileRejectsEmpty(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.sql")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyDumpFile(empty); err == nil {
		t.Error("空产物没有被判为失败")
	}
	if err := verifyDumpFile(filepath.Join(dir, "missing.sql")); err == nil {
		t.Error("不存在的产物没有被判为失败")
	}
}
