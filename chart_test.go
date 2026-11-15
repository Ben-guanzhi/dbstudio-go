package main

import (
	"testing"

	"github.com/Ben-guanzhi/dbcore"
)

// chartPoints / chartNumericCol 的契约测试（Q-05 平移）：与 fynequery 的
// chart_test.go 同一套语义——数值列抽取、NULL/非数值跳过、类目截断。
func vtext(s string) dbcore.Value { return dbcore.Value{Kind: dbcore.ValText, S: s} }
func vint(n int64) dbcore.Value   { return dbcore.Value{Kind: dbcore.ValInt, I: n} }
func vnull() dbcore.Value         { return dbcore.Value{Kind: dbcore.ValNull} }

func TestChartPoints(t *testing.T) {
	res := &dbcore.QueryResult{
		Columns: []dbcore.ColumnInfo{{Name: "city", Type: "varchar"}, {Name: "amount", Type: "decimal"}},
		Rows: [][]dbcore.Value{
			{vtext("北京"), vint(120)},
			{vtext("上海"), vint(98)},
			{vtext("广州"), vnull()},            // NULL → 跳过
			{vtext("深圳"), vtext("not-a-num")}, // 非数值 → 跳过
			{vnull(), vint(7)},                // X 为 NULL → 标签 "NULL"
		},
	}
	labels, values, dropped := chartPoints(res, 0, 1)
	if dropped != 2 {
		t.Errorf("应跳过 2 行，实际 %d", dropped)
	}
	if len(labels) != 3 || len(values) != 3 {
		t.Fatalf("应得 3 个点，实际 %d", len(labels))
	}
	if labels[0] != "北京" || values[0] != 120 {
		t.Errorf("首点错误: %s=%v", labels[0], values[0])
	}
	if labels[2] != "NULL" || values[2] != 7 {
		t.Errorf("X 为 NULL 的标签应为 'NULL': %s=%v", labels[2], values[2])
	}
}

func TestChartPointsTruncates(t *testing.T) {
	rows := make([][]dbcore.Value, chartCategories+30)
	for i := range rows {
		rows[i] = []dbcore.Value{vtext("c" + itoaChart(i)), vint(int64(i * 10))}
	}
	res := &dbcore.QueryResult{
		Columns: []dbcore.ColumnInfo{{Name: "k", Type: "varchar"}, {Name: "v", Type: "int"}},
		Rows:    rows,
	}
	labels, _, dropped := chartPoints(res, 0, 1)
	if len(labels) != chartCategories {
		t.Errorf("应截断到 %d 类目，实际 %d", chartCategories, len(labels))
	}
	if dropped != 0 {
		t.Errorf("无跳过，实际 %d", dropped)
	}
}

func TestChartNumericCol(t *testing.T) {
	res := &dbcore.QueryResult{
		Columns: []dbcore.ColumnInfo{{Name: "a", Type: "varchar"}, {Name: "b", Type: "int"}, {Name: "c", Type: "decimal"}},
		Rows: [][]dbcore.Value{
			{vtext("x"), vint(1), vint(2)},
		},
	}
	if got := chartNumericCol(res); got != 1 {
		t.Errorf("第一个数值列应是 index 1，实际 %d", got)
	}
	allText := &dbcore.QueryResult{Columns: []dbcore.ColumnInfo{{Name: "a", Type: "text"}}}
	if got := chartNumericCol(allText); got != -1 {
		t.Errorf("无数值列应返回 -1，实际 %d", got)
	}
}

func itoaChart(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
