package main

// 重命名重构的核心：编辑器缓冲的旧名引用改写——词边界匹配、字符串字面量
// 跳过。改错位（列名的一部分、字面量里的文本）比不改更糟。

import (
	"strings"
	"testing"
)

func TestRewriteSQLIdentifiersWordBoundary(t *testing.T) {
	got := rewriteSQLIdentifiers(
		"SELECT * FROM employees WHERE employees.id = 1;", "employees", "staff")
	if !strings.Contains(got, "SELECT * FROM staff WHERE staff.id = 1;") {
		t.Errorf("引用未改写: %s", got)
	}
	// 前缀重叠的名字不被误改（employees_archive 不是 employees）。
	got = rewriteSQLIdentifiers(
		"SELECT * FROM employees_archive JOIN employees ON 1=1;", "employees", "staff")
	if strings.Contains(got, "staff_archive") {
		t.Errorf("前缀重叠被误改: %s", got)
	}
	if !strings.Contains(got, "JOIN staff") {
		t.Errorf("完整名未改写: %s", got)
	}
}

func TestRewriteSQLIdentifiersSkipsStringLiterals(t *testing.T) {
	got := rewriteSQLIdentifiers(
		"INSERT INTO log (msg) VALUES ('we renamed employees today');", "employees", "staff")
	if !strings.Contains(got, "'we renamed employees today'") {
		t.Errorf("字符串字面量被误改: %s", got)
	}
}

func TestRewriteSQLIdentifiersMixed(t *testing.T) {
	got := rewriteSQLIdentifiers(
		"SELECT count(*) FROM employees;\n-- old employees report\nSELECT 'employees' AS label FROM employees;",
		"employees", "staff")
	// 代码里的引用都改写（包括注释——表改名后注释提到旧表也该更新，
	// DBeaver 的重构同样改注释）；**字符串字面量**不动（那是数据/展示文本，
	// 改它就改变了程序行为）。
	if strings.Count(got, "staff") != 3 {
		t.Errorf("代码引用与注释都应改写（字面量不动）: %s", got)
	}
	if !strings.Contains(got, "'employees' AS label") {
		t.Errorf("字面量别名被误改: %s", got)
	}
}

func TestRewriteSQLIdentifiersNoops(t *testing.T) {
	if got := rewriteSQLIdentifiers("", "a", "b"); got != "" {
		t.Error("空文本应原样")
	}
	if got := rewriteSQLIdentifiers("SELECT 1", "x", "x"); got != "SELECT 1" {
		t.Error("同名替换应原样")
	}
	if got := rewriteSQLIdentifiers("SELECT 'x'", "x", "y"); got != "SELECT 'x'" {
		t.Errorf("纯字面量不应被改: %s", got)
	}
}
