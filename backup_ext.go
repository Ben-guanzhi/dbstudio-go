package main

// MySQL / PostgreSQL 的备份：调它们原生的 dump 工具。
//
// 这两个引擎的备份没有「读 catalog 拼 SQL」的可靠做法——表空间、锁、字符集、
// 存储引擎的差异让自研导出迟早漏东西。mysqldump / pg_dump 是权威实现，也是
// DBeaver / TablePlus 的底层做法。
//
// 两条安全/正确性的底线：
//   - **口令走环境变量**（MYSQL_PWD / PGPASSWORD），**绝不进 argv**——argv 对
//     同机的任何进程可见（任务管理器、/proc），那是密码的泄露面；
//   - 产物要校验：工具「成功退出但什么都没写」是真实发生过的静默失败
//     （pg_dump 匹配不到 -t 的表时），备份的价值全在产物上。
//
// 波 101 续五十四。

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// dumpToolEnv 是允许覆盖 dump 工具路径的环境变量（测试与打包部署用）。
const (
	envPgDump    = "DBSTUDIO_PGDUMP"
	envMysqlDump = "DBSTUDIO_MYSQLDUMP"
)

// errToolMissing 说明本机没装对应的 dump 工具。
var errToolMissing = errors.New("dump tool not found")

// resolveDumpTool 找 dump 工具：先环境变量（指向显式路径），再 PATH 里的名字。
func resolveDumpTool(envVar, name string) (string, error) {
	if p := os.Getenv(envVar); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
		return "", fmt.Errorf("%s 指向的工具不存在: %s", envVar, p)
	}
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("%w: %s（装它，或设 %s 指向它）", errToolMissing, name, envVar)
}

// pgDumpArgs 拼 pg_dump 参数（口令不在这里——走 PGPASSWORD）。
func pgDumpArgs(conn Conn, outPath string) []string {
	args := []string{
		"--host=" + conn.Host,
		"--port=" + strconv.Itoa(conn.Port),
		"--username=" + conn.User,
		"--dbname=" + conn.Database,
	}
	if outPath != "" {
		args = append(args, "--file="+outPath)
	}
	return args
}

// mysqlDumpArgs 拼 mysqldump 参数（口令不在这里——走 MYSQL_PWD）。
//
// --single-transaction 给 InnoDB 一个一致性快照且不加锁；--quick 让大表流式
// 读，不缓存在内存里。这两个是「备份时不影响线上」的关键。
func mysqlDumpArgs(conn Conn, outPath string) []string {
	args := []string{
		"--single-transaction", "--quick",
		"--host=" + conn.Host,
		"--port=" + strconv.Itoa(conn.Port),
		"--user=" + conn.User,
		"--protocol=TCP",
	}
	if conn.SSLMode == "require" || conn.SSLMode == "disabled" {
		args = append(args, "--ssl-mode="+strings.ToUpper(conn.SSLMode))
	}
	if outPath != "" {
		args = append(args, "--result-file="+outPath)
	}
	return append(args, conn.Database)
}

// dumpToolEnv 返回工具需要的环境变量：只加口令那一项，其余继承。
func dumpToolEnv(conn Conn, pg bool) []string {
	if pg {
		return append(os.Environ(), "PGPASSWORD="+conn.Password)
	}
	return append(os.Environ(), "MYSQL_PWD="+conn.Password)
}

// runDumpTool 跑工具并把产物写出来。outPath 为空时把 stdout 落到 outPath
// （本客户端总是给路径——备份要有个文件）。
func runDumpTool(ctx context.Context, tool string, args, env []string, outPath string) error {
	cmd := exec.CommandContext(ctx, tool, args...)
	cmd.Env = env
	if outPath == "" {
		return errors.New("备份未指定输出路径")
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return fmt.Errorf("%s: %s", err, dumpErrLine(msg))
		}
		return err
	}
	return nil
}

// dumpErrLine 取多行错误的第一行：工具的长输出里，第一行通常是原因。
func dumpErrLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// verifyDumpFile 校验产物：存在、非空。空备份比没有备份更危险。
func verifyDumpFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("备份产物不存在: %w", err)
	}
	if info.Size() == 0 {
		return fmt.Errorf("备份产物是空文件（工具没写出任何东西）: %s", path)
	}
	return nil
}
