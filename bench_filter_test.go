package main

// dbstudio 加载路径的客户端侧基准：结果行 → 过滤 / 排序。这两个是网格
// 每次结果返回后的纯逻辑热点，行数大时（5000 行）会重复计算。

import (
	"fmt"
	"testing"

	"github.com/Ben-guanzhi/dbcore"
)

// benchRows 构造真实形状的 20 列结果：混合类型，主键行号。
func benchRows(n int) [][]dbcore.Value {
	rows := make([][]dbcore.Value, n)
	for i := range rows {
		row := make([]dbcore.Value, 20)
		for j := range row {
			switch j % 4 {
			case 0:
				row[j] = dbcore.Value{Kind: dbcore.ValText, S: fmt.Sprintf("row%06d", i)}
			case 1:
				row[j] = dbcore.Value{Kind: dbcore.ValText, S: fmt.Sprintf("%d", i*7+j)}
			case 2:
				row[j] = dbcore.Value{Kind: dbcore.ValText, S: "active"}
			case 3:
				row[j] = dbcore.Value{Kind: dbcore.ValNull}
			}
		}
		rows[i] = row
	}
	return rows
}

func benchCols() []dbcore.ColumnInfo {
	cols := make([]dbcore.ColumnInfo, 20)
	for i := range cols {
		cols[i] = dbcore.ColumnInfo{Name: fmt.Sprintf("c%d", i), Type: "VARCHAR(120)"}
	}
	return cols
}

// BenchmarkFilterRows5000：5000 行 × 20 列 × 3 条件（文本相等 + 数字比较 +
// LIKE）。这是用户在结果上筛选最频繁的路径。
func BenchmarkFilterRows5000(b *testing.B) {
	rows := benchRows(5000)
	cols := benchCols()
	exprs := []dbcore.FilterExpr{
		{Column: "c2", Op: dbcore.FilterEq, Value: dbcore.ValueOf("active")},
		{Column: "c1", Op: dbcore.FilterGt, Value: dbcore.ValueOf("2500")},
		{Column: "c0", Op: dbcore.FilterLike, Value: dbcore.ValueOf("row%")},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got := filterRows(rows, cols, exprs)
		if got == nil {
			b.Fatal("nil result")
		}
	}
}
