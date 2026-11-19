package main

// 端到端用户旅程（§324）：把散落的能力串成一条真实数据流——
// 连接 → 建表 → 造数 → 浏览 → 筛选 → 编辑 → DDL 变更 → 验证。
//
// 每一步都用**真实 SQLite 驱动 + 内核 API**，不是 mock：
// 如果某个环节的状态在传递中丢失（列名不匹配、类型被吞、DDL 拼错），
// 这条链就会断。它验证的是"能力集成"而非"单元行为"。

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/Ben-guanzhi/dbcore"
	"github.com/Ben-guanzhi/dbcore/ddl"
	"github.com/Ben-guanzhi/dbcore/sqlite"
	"github.com/Ben-guanzhi/dbcore/sqltext"
)

func TestJourneyBrowseFilterEditRename(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journey.db")
	drv, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer drv.Close()
	ctx := context.Background()

	// 1. 建表（DDL 生成 → 执行）
	cols := []dbcore.ColumnInfo{
		{Name: "id", Type: "INTEGER", IsPrimaryKey: true},
		{Name: "name", Type: "VARCHAR(80)"},
		{Name: "amount", Type: "DECIMAL(18,2)"},
		{Name: "active", Type: "BOOLEAN"},
	}
	ddlCols := make([]ddl.Column, len(cols))
	for i, c := range cols {
		ddlCols[i] = ddl.Column{Name: c.Name, Type: c.Type}
	}
	create := ddl.CreateTable("sqlite", "", "orders", ddlCols, nil, nil, nil)
	if _, err := drv.Execute(ctx, create); err != nil {
		t.Fatalf("建表失败: %v", err)
	}

	// 2. 造数（内核生成器 → 执行）——显式值，便于逐步验证。
	seed := []struct {
		name   string
		amount string
		active string
	}{
		{"Alice", "120.50", "1"},
		{"Bob", "80.00", "0"},
		{"Carol", "250.75", "1"},
		{"Dave", "5.25", "0"},
	}
	for i, r := range seed {
		stmt := fmt.Sprintf(
			"INSERT INTO orders (id,name,amount,active) VALUES (%d,'%s',%s,%s)",
			i+1, r.name, r.amount, r.active)
		if _, err := drv.Execute(ctx, stmt); err != nil {
			t.Fatalf("插入 %d 失败: %v", i, err)
		}
	}

	// 3. 浏览（结果集 → 行数/列名字段完整）
	res, err := drv.Execute(ctx, "SELECT id, name, amount, active FROM orders ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 4 || len(res.Columns) != 4 {
		t.Fatalf("浏览结果 shape 不对: rows=%d cols=%d", len(res.Rows), len(res.Columns))
	}
	if res.Columns[1].Name != "name" || res.Columns[2].Name != "amount" {
		t.Fatalf("列名丢失: %v", res.Columns)
	}

	// 4. 筛选（SQL WHERE，与 UI 的 filterRows 语义对齐）
	filtered, err := drv.Execute(ctx, "SELECT name FROM orders WHERE active = 1 ORDER BY amount DESC")
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered.Rows) != 2 || filtered.Rows[0][0].String() != "Carol" {
		t.Fatalf("筛选结果不对: %v", filtered.Rows)
	}

	// 5. 编辑（UPDATE 一行后，值在后续查询中可见）
	if _, err := drv.Execute(ctx, "UPDATE orders SET amount = 999.99 WHERE name = 'Bob'"); err != nil {
		t.Fatal(err)
	}
	check, _ := drv.Execute(ctx, "SELECT amount FROM orders WHERE name = 'Bob'")
	if check.Rows[0][0].String() != "999.99" {
		t.Fatalf("编辑未生效: %v", check.Rows)
	}

	// 6. DDL 变更：列改名（内核 RenameColumn → 执行 → 数据保留）
	rename, err := ddl.RenameColumn("sqlite", "", "orders", "amount", "total")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := drv.Execute(ctx, rename); err != nil {
		t.Fatalf("改名失败: %v", rename)
	}
	after, err := drv.Execute(ctx, "SELECT name, total FROM orders ORDER BY id")
	if err != nil {
		t.Fatalf("改名后查询失败: %v", err)
	}
	if after.Rows[1][1].String() != "999.99" || len(after.Rows) != 4 {
		t.Fatalf("改名后数据丢失: %v", after.Rows)
	}
	if after.Columns[1].Name != "total" {
		t.Fatalf("改名后列名不对: %v", after.Columns)
	}

	// 7. 清空表（内核 TruncateStatement → 数据没、表还在）
	quoted, _ := sqltext.QualifiedName("sqlite", "orders")
	trunc, err := sqltext.TruncateStatement("sqlite", quoted)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := drv.Execute(ctx, trunc); err != nil {
		t.Fatal(err)
	}
	empty, _ := drv.Execute(ctx, "SELECT COUNT(*) FROM orders")
	if empty.Rows[0][0].String() != "0" {
		t.Fatalf("清空后仍有行: %v", empty.Rows)
	}
	// 表结构仍在（INSERT 还能成功）。注意改名后的 total 保留了 NOT NULL
	// 约束——SQLite RENAME COLUMN 保约束是正确行为，插入必须提供该列。
	if _, err := drv.Execute(ctx,
		"INSERT INTO orders (id,name,total,active) VALUES (99,'after-truncate',1.00,1)"); err != nil {
		t.Fatalf("清空后表结构丢失: %v", err)
	}
	verify, _ := drv.Execute(ctx, "SELECT name,total FROM orders WHERE id = 99")
	if len(verify.Rows) != 1 || verify.Rows[0][0].String() != "after-truncate" {
		t.Fatalf("清空后新行不可见: %v", verify.Rows)
	}
}
