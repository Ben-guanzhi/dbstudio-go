package main

// TSV：数据管道与日志最常用的分隔格式。
//
// CSV 与 TSV 只差分隔符，但 TSV 在管道里更常见（不含逗号的文本不用转义）。
// 这些测试钉住：分隔符是制表符、含制表符的字段被引号包起来（csv.Writer
// 的标准行为）、导出格式的标签与扩展名。

import (
	"testing"

	"github.com/Ben-guanzhi/dbcore"
)

func TestTSVText(t *testing.T) {
	res := &dbcore.QueryResult{
		Columns: []dbcore.ColumnInfo{{Name: "id"}, {Name: "name"}, {Name: "note"}},
		Rows: [][]dbcore.Value{
			{
				{Kind: dbcore.ValInt, I: 1},
				{Kind: dbcore.ValText, S: "Ada"},
				{Kind: dbcore.ValNull},
			},
			{
				{Kind: dbcore.ValInt, I: 2},
				{Kind: dbcore.ValText, S: "a\tb"},
				{Kind: dbcore.ValText, S: "say \"hi\""},
			},
		},
	}
	got, err := tsvText(res)
	if err != nil {
		t.Fatalf("tsvText: %v", err)
	}
	// 表头与行之间是换行，字段之间是制表符；含制表符的字段被引号包起来。
	want := "id\tname\tnote\n1\tAda\t\n2\t\"a\tb\"\t\"say \"\"hi\"\"\"\n"
	if got != want {
		t.Fatalf("tsvText =\n%q\nwant\n%q", got, want)
	}
}

func TestCSVAndTSVDifferOnlyByDelimiter(t *testing.T) {
	res := &dbcore.QueryResult{
		Columns: []dbcore.ColumnInfo{{Name: "a"}, {Name: "b"}},
		Rows:    [][]dbcore.Value{{{Kind: dbcore.ValInt, I: 1}, {Kind: dbcore.ValInt, I: 2}}},
	}
	csvGot, err := csvText(res)
	if err != nil {
		t.Fatalf("csvText: %v", err)
	}
	tsvGot, err := tsvText(res)
	if err != nil {
		t.Fatalf("tsvText: %v", err)
	}
	if csvGot != "a,b\n1,2\n" {
		t.Errorf("csvText = %q", csvGot)
	}
	if tsvGot != "a\tb\n1\t2\n" {
		t.Errorf("tsvText = %q", tsvGot)
	}
}

func TestTSVExportFormat(t *testing.T) {
	if got := exportTSV.label(); got != "TSV" {
		t.Errorf("exportTSV 的标签 = %q, want TSV", got)
	}
	if got := exportTSV.ext(); got != ".tsv" {
		t.Errorf("exportTSV 的扩展名 = %q, want .tsv", got)
	}
}

func TestCopyTSVKind(t *testing.T) {
	if got := copyTSV.label(); got != "TSV" {
		t.Errorf("copyTSV 的标签 = %q, want TSV", got)
	}
}
