package main

// CSV 公式注入防护（OWASP CSV Injection）：以 = + - @ 开头的单元格写进 CSV，
// 用户用 Excel 打开会当公式执行。必须前缀单引号；纯数字字面量豁免。

import (
	"strings"
	"testing"

	"github.com/Ben-guanzhi/dbcore"
)

func TestFormulaGuard(t *testing.T) {
	cases := []struct{ in, want string }{
		{"=cmd|' /C calc'!A0", "'=cmd|' /C calc'!A0"},
		{"+cmd", "'+cmd"},
		{"-cmd", "'-cmd"},
		{"@SUM(A1)", "'@SUM(A1)"},
		// 纯数字不动——否则破坏 Excel 里的数值计算。
		{"-5", "-5"},
		{"+3.14", "+3.14"},
		{"1e5", "1e5"},
		{"hello", "hello"},
		{"", ""},
	}
	for _, c := range cases {
		if got := formulaGuard(c.in); got != c.want {
			t.Errorf("formulaGuard(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// 端到端：CSV 里危险值必须带单引号，普通值不受影响。
func TestCSVTextGuardsFormulaInjection(t *testing.T) {
	res := &dbcore.QueryResult{
		Columns: []dbcore.ColumnInfo{{Name: "a"}, {Name: "b"}},
		Rows: [][]dbcore.Value{
			{dbcore.ValueOf("=1+1"), dbcore.ValueOf("-5")},
		},
	}
	out, err := csvText(res)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "'=1+1") {
		t.Errorf("危险公式没有被转义:\n%s", out)
	}
	if !strings.Contains(out, "-5") || strings.Contains(out, "'-5") {
		t.Errorf("纯数字被误转义了:\n%s", out)
	}
}
