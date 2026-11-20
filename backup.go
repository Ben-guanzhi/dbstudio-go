package main

// 备份：把一个 SQLite 库导成 SQL 文本（等价于 `sqlite3 .dump`）。
//
// 为什么是 SQL 文本而不是文件复制：SQL 文本跨版本、跨引擎可移植，能读、能
// 改、能进版本库；DBeaver / TablePlus 的 SQLite 备份也是 .dump。
//
// 三件事让产物可信：
//   - 结构来自 sqlite_master（权威 DDL，不是从列反推）；
//   - 数据分页读，大表不撑爆内存；
//   - 外层包一个事务并关掉外键检查——恢复时插入顺序不受外键约束限制，
//     这与 sqlite3 的 .dump 一致。
//
// MySQL / PostgreSQL 的备份走外部工具（mysqldump / pg_dump），下一步做。
//
// 波 101 续五十二。

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Ben-guanzhi/dbcore"
	"github.com/Ben-guanzhi/dbcore/sqlite"
	"github.com/egoist/mygo"
)

// backupBatchSize 是一次读多少行：足够小以控内存，足够大以减少往返。
const backupBatchSize = 500

// dumpSQLite 把 path 指向的 SQLite 库导成 SQL 文本。
//
// 它开自己的连接（备份不该占用也不该被正在跑的事务影响），读完即关。
// path 必须已存在：SQLite 打开一个不存在的文件会**创建**它，那样备份会
// 静默产出一个空库——比报错危险得多。
func dumpSQLite(ctx context.Context, path string) (string, error) {
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("数据库文件不存在: %s", path)
	}
	drv, err := sqlite.Open(path)
	if err != nil {
		return "", fmt.Errorf("打开数据库: %w", err)
	}
	defer drv.Close()
	sql, ok := drv.AsSQL()
	if !ok {
		return "", fmt.Errorf("该驱动没有 SQL 接口")
	}

	var out strings.Builder
	out.WriteString("PRAGMA foreign_keys=OFF;\n")
	out.WriteString("BEGIN TRANSACTION;\n")

	// 结构：sqlite_master 里的权威 DDL。表先，其余（索引/触发器/视图）后，
	// 与 .dump 的顺序一致——恢复时表先建好，索引后加。
	schemaRes, err := sql.Execute(ctx,
		"SELECT type, name, sql FROM sqlite_master "+
			"WHERE sql IS NOT NULL AND name NOT LIKE 'sqlite_%' "+
			"ORDER BY CASE type WHEN 'table' THEN 0 ELSE 1 END, name")
	if err != nil {
		return "", fmt.Errorf("读结构: %w", err)
	}
	var tables []string
	for _, row := range schemaRes.Rows {
		kind := textOf(row, 0)
		ddl := textOf(row, 2)
		if kind == "table" {
			tables = append(tables, textOf(row, 1))
			// 表名不带 sqlite_ 前缀的已经过滤；AUTO 索引（sql 为 NULL）
			// 也过滤掉了。
			out.WriteString(ddl)
			out.WriteString(";\n")
			continue
		}
		out.WriteString(ddl)
		out.WriteString(";\n")
	}

	// 数据：每张表分页读，逐行 INSERT。
	for _, table := range tables {
		if err := dumpTable(ctx, sql, &out, table); err != nil {
			return "", err
		}
	}

	out.WriteString("COMMIT;\n")
	out.WriteString("PRAGMA foreign_keys=ON;\n")
	return out.String(), nil
}

// dumpTable 把一张表的数据写成 INSERT 语句，分页读。
//
// 列名取自 `SELECT *` 的结果本身，而不是再问一次目录：那样与 schema 的写法
// 无关（SQLite 的 schema 是 main，但传空串还是 main 各驱动未必一致）。
func dumpTable(ctx context.Context, sql dbcore.SQLDriver, out *strings.Builder, table string) error {
	head, err := sql.Execute(ctx, "SELECT * FROM "+quoteIdent(table)+" LIMIT 0")
	if err != nil {
		return fmt.Errorf("读表 %s: %w", table, err)
	}
	names := head.Columns
	if len(names) == 0 {
		return nil
	}
	quoted := make([]string, len(names))
	for i, c := range names {
		quoted[i] = quoteIdent(c.Name)
	}
	prefix := "INSERT INTO " + quoteIdent(table) + " (" + strings.Join(quoted, ", ") + ") VALUES "

	offset := 0
	for {
		res, err := sql.Execute(ctx,
			fmt.Sprintf("SELECT %s FROM %s LIMIT %d OFFSET %d",
				strings.Join(quoted, ", "), quoteIdent(table), backupBatchSize, offset))
		if err != nil {
			return fmt.Errorf("读数据 %s: %w", table, err)
		}
		for _, row := range res.Rows {
			vals := make([]string, len(names))
			for i := range names {
				vals[i] = sqlLiteral("sqlite", valueAt(row, i))
			}
			out.WriteString(prefix + "(" + strings.Join(vals, ", ") + ");\n")
		}
		if len(res.Rows) < backupBatchSize {
			break
		}
		offset += backupBatchSize
	}
	return nil
}

// textOf 取一个文本单元格，NULL 给空串。
func textOf(row []dbcore.Value, i int) string {
	if i >= len(row) || row[i].Kind == dbcore.ValNull {
		return ""
	}
	return row[i].String()
}

// quoteIdent 用双引号包一个标识符，里面的双引号翻倍——SQL 的标准转义。
func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// restoreDB 把一个 .sql 备份导回当前连接的库——备份的另一半。
//
// SQLite 在**独立连接**上按 splitSQL 逐条执行（引号和注释里的分号不算）；
// MySQL / PostgreSQL 交给原生的 mysql / psql 读同一个文件。恢复是破坏性的：
// 备份产物自带 DROP，会把目标库现有的表换掉。
func (app *App) restoreDB() {
	// Restore 会执行备份文件里的全部 DDL/DML——只读连接必须拒绝。
	if app.readOnly() {
		app.refuseWrite("RESTORE")
		return
	}
	if !app.open {
		app.status = "Not connected"
		app.invalidate()
		return
	}
	engine := app.engine()
	pg := engine == "postgres"
	if engine != "sqlite" && !pg && engine != "mysql" {
		app.status = "Restore is not available for " + engine
		app.invalidate()
		return
	}
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Title:       "Restore into " + app.conn.Name,
		ButtonLabel: "Restore",
		// 恢复是破坏性的：它先删掉目标库现有的表，再重建。这句说明必须写在
		// 用户按下按钮之前。
		Message: "Restore replaces the database's current tables with the backup's.",
		Filters: []mygo.FileFilter{
			{Name: "SQL dump", Extensions: []string{"sql", "txt", "dump"}},
		},
	})
	if err != nil {
		return // 用户取消
	}
	if len(paths) == 0 || paths[0] == "" {
		return
	}
	file := paths[0]

	// 外部引擎先确认工具在（缺失时立刻说，不进 goroutine）。
	var tool, toolEnv, toolName string
	if engine != "sqlite" {
		if pg {
			toolEnv, toolName = envPgDump, "psql"
		} else {
			toolEnv, toolName = envMysqlDump, "mysql"
		}
		var err error
		tool, err = resolveDumpTool(toolEnv, toolName)
		if err != nil {
			app.status = err.Error()
			app.invalidate()
			return
		}
	}

	app.status = "Restoring…"
	app.invalidate()
	go func() {
		if engine == "sqlite" {
			n, err := restoreSQLFile(context.Background(), file, app.conn.File)
			app.post(func() {
				if err != nil {
					app.errMsg = "Restore failed: " + err.Error()
					app.status = ""
					return
				}
				app.errMsg = ""
				app.status = fmt.Sprintf("Restored %d statements from %s (current tables replaced)", n, filepath.Base(file))
			})
			return
		}
		// SSH 连接：隧道 + 本地转发，工具连转发端口。
		ctx, cancel := context.WithTimeout(context.Background(), sshTimeout)
		defer cancel()
		tun, terr := openTunnel(ctx, app.conn)
		if terr != nil {
			app.post(func() { app.errMsg = "Restore failed: " + terr.Error(); app.status = "" })
			return
		}
		if tun != nil {
			defer tun.Close()
		}
		useConn := app.conn
		if tun != nil {
			remote := net.JoinHostPort(app.conn.Host, strconv.Itoa(app.conn.Port))
			fwd, ferr := startPortForward(ctx, tun.Dialer(), remote)
			if ferr != nil {
				app.post(func() { app.errMsg = "Restore failed: " + ferr.Error(); app.status = "" })
				return
			}
			defer fwd.close()
			useConn = tunneledConn(app.conn, fwd)
		}
		err := restoreExternal(ctx, tool, useConn, file, pg)
		app.post(func() {
			if err != nil {
				app.errMsg = "Restore failed: " + err.Error()
				app.status = ""
				return
			}
			app.errMsg = ""
			app.status = fmt.Sprintf("Restored from %s (current tables replaced)", filepath.Base(file))
		})
	}()
}

// restoreSQLFile 把一个 .sql 文件的语句逐条执行到 target 库（独立连接），
// 返回执行了多少条。它是恢复的核心，与对话框分开，好让测试直接驱动。
//
// **恢复前会删掉目标库里现有的表**：备份产物含 CREATE TABLE，而恢复的语义
// 是「回到备份时的状态」，不是「往现状里追加」。这与 sqlite3 .dump + 恢复
// 的语义一致；不删的话，恢复到还有表的库会卡在第一条 CREATE 上。
func restoreSQLFile(ctx context.Context, path, target string) (int, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	stmts := splitSQL(string(raw))
	if len(stmts) == 0 {
		return 0, fmt.Errorf("文件里没有可执行的语句")
	}
	drv, err := sqlite.Open(target)
	if err != nil {
		return 0, err
	}
	defer drv.Close()
	sql, ok := drv.AsSQL()
	if !ok {
		return 0, fmt.Errorf("没有 SQL 接口")
	}
	// 先清场：删掉现有表（索引/触发器随表一起走）。
	schema, err := sql.Execute(ctx,
		"SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'")
	if err != nil {
		return 0, fmt.Errorf("读现有表: %w", err)
	}
	for _, row := range schema.Rows {
		name := textOf(row, 0)
		if name == "" {
			continue
		}
		if _, err := sql.Execute(ctx, "DROP TABLE IF EXISTS "+quoteIdent(name)); err != nil {
			return 0, fmt.Errorf("删表 %s: %w", name, err)
		}
	}
	for i, stmt := range stmts {
		if _, err := sql.Execute(ctx, stmt); err != nil {
			return i, fmt.Errorf("第 %d 条: %w", i+1, err)
		}
	}
	return len(stmts), nil
}

// backupDB 把当前连接备份成一个 .sql 文件，写在保存连接的同一目录里（与
// 导出结果同一个约定位置），名字带时间戳。
//
// SQLite 用内置的 .dump 等价导出；MySQL / PostgreSQL 调它们原生的
// mysqldump / pg_dump（本机没装时明确报错，不静默产出空文件）。
func (app *App) backupDB() {
	if !app.open || app.store == nil {
		app.status = "Not connected"
		app.invalidate()
		return
	}
	dir := app.store.Dir()
	name := "backup-" + time.Now().Format("20060102-150405") + ".sql"
	outPath := filepath.Join(dir, name)

	if app.engine() == "sqlite" && app.conn.File != "" {
		app.status = "Backing up…"
		app.invalidate()
		go func() {
			dump, err := dumpSQLite(context.Background(), app.conn.File)
			if err != nil {
				app.post(func() { app.errMsg = "Backup failed: " + err.Error(); app.status = "" })
				return
			}
			if err := os.WriteFile(outPath, []byte(dump), 0o600); err != nil {
				app.post(func() { app.errMsg = "Backup failed: " + err.Error(); app.status = "" })
				return
			}
			app.post(func() { app.errMsg = ""; app.status = "Backup written " + name })
		}()
		return
	}

	// MySQL / PostgreSQL：原生 dump 工具。
	toolEnv, toolName := envMysqlDump, "mysqldump"
	pg := app.engine() == "postgres"
	if pg {
		toolEnv, toolName = envPgDump, "pg_dump"
	}
	if !pg && app.engine() != "mysql" {
		app.status = "Backup is not available for " + app.engine()
		app.invalidate()
		return
	}
	tool, err := resolveDumpTool(toolEnv, toolName)
	if err != nil {
		app.status = err.Error()
		app.invalidate()
		return
	}

	app.status = "Backing up…"
	app.invalidate()
	go func() {
		// SSH 连接：先建隧道，再在本地开一个转发端口给工具连。
		ctx, cancel := context.WithTimeout(context.Background(), sshTimeout)
		defer cancel()
		tun, err := openTunnel(ctx, app.conn)
		if err != nil {
			app.post(func() { app.errMsg = "Backup failed: " + err.Error(); app.status = "" })
			return
		}
		if tun != nil {
			defer tun.Close()
		}
		useConn := app.conn
		if tun != nil {
			remote := net.JoinHostPort(app.conn.Host, strconv.Itoa(app.conn.Port))
			fwd, err := startPortForward(ctx, tun.Dialer(), remote)
			if err != nil {
				app.post(func() { app.errMsg = "Backup failed: " + err.Error(); app.status = "" })
				return
			}
			defer fwd.close()
			useConn = tunneledConn(app.conn, fwd)
		}

		// 原子备份（§348）：工具只写临时文件，成功校验后才 rename 到目标。
		err = atomicWriteResult(outPath, func(tmp string) error {
			var args []string
			if pg {
				args = pgDumpArgs(useConn, tmp)
			} else {
				args = mysqlDumpArgs(useConn, tmp)
			}
			return runDumpTool(ctx, tool, args, dumpToolEnv(useConn, pg), tmp)
		})
		if err != nil {
			app.post(func() { app.errMsg = "Backup failed: " + err.Error(); app.status = "" })
			return
		}
		app.post(func() { app.errMsg = ""; app.status = "Backup written " + name })
	}()
}
