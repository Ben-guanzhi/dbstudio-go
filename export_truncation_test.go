package main

// 导出一个被截断的结果时必须说明——否则用户以为拿到全量，实际少了后面的行。

import (
	"os"
	"strings"
	"testing"
)

func TestExportWarnsWhenTruncated(t *testing.T) {
	raw, err := os.ReadFile("export.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, "result.Truncated") {
		t.Error("导出逻辑没有读 result.Truncated")
	}
	if !strings.Contains(s, "result was truncated") {
		t.Error("导出完成的状态没有说明结果被截断")
	}
}
