package main

// CSV 导入编码（§352）：UTF-8 BOM 与 GB18030 的中文 CSV 必须正确解码——
// 否则首列名带 BOM（列映射失败）、中文全乱码。

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestReadImportFileStripsBOM(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bom.csv")
	// Excel 导出的 UTF-8 CSV：BOM + 表头。
	bom := []byte{0xEF, 0xBB, 0xBF}
	content := append(bom, []byte("name,city\n张三,北京\n")...)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := readImportFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.records) < 2 || f.records[0][0] != "name" {
		t.Fatalf("header = %v, want name (BOM must be stripped)", f.records)
	}
	if len(f.records) != 2 || f.records[1][0] != "张三" {
		t.Fatalf("records = %v", f.records)
	}
}

func TestReadImportFileGB18030(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gbk.csv")
	gb, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte("name,city\n张三,北京\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, gb, 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := readImportFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.records) != 2 || f.records[1][0] != "张三" || f.records[1][1] != "北京" {
		t.Fatalf("GB18030 records = %v (mojibake?)", f.records)
	}
}
