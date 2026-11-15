package main

// 乐观并发守卫的客户端侧（波 101 续十一）：编辑提交时把「读到时的旧值」一起送进
// 内核，由 UPDATE 的 WHERE 承担 lost-update 检测。这里钉三件事：
//
//  1. Expect 取的是 r.origStrs（读到的值），不是 strs（改后的值）——取错就把
//     守卫写成了永真谓词，等于没有守卫；
//  2. 读过为 NULL 的单元格必须以 ValNull 参与守卫（空串在可空列上是另一个值）；
//  3. Expect 只覆盖本次真正改变的列，避免别人的无关编辑被误报成冲突。

import (
	"testing"

	"github.com/Ben-guanzhi/dbcore"
)

func TestEditRequestGuardsWithReadValues(t *testing.T) {
	e := sampleRows()
	// Row 0: change name only. Row 1: change salary from NULL to 150000.
	e.rows[0].strs[1] = "Ada Lovelace"
	e.rows[0].state = rowModified
	e.rows[1].strs[2] = "150000"
	e.rows[1].state = rowModified

	req, why := e.request()
	if why != "" {
		t.Fatalf("request = %q", why)
	}
	if len(req.Updates) != 2 {
		t.Fatalf("updates = %d, want 2", len(req.Updates))
	}

	first := req.Updates[0]
	if got := first.Expect["name"]; got.Kind != dbcore.ValText || got.S != "Ada" {
		t.Errorf("guard for name = %+v, want the READ value Ada (not the edit)", got)
	}
	if len(first.Expect) != 1 {
		t.Errorf("guard should cover exactly the changed column, got %+v", first.Expect)
	}

	// The salary cell read as NULL; the guard must say NULL, not "" (the text form
	// the grid showed) — on a nullable column those are different values.
	second := req.Updates[1]
	if got, ok := second.Expect["salary"]; !ok || got.Kind != dbcore.ValNull {
		t.Errorf("guard for the NULL salary = %+v (present=%v), want ValNull", got, ok)
	}
}

func TestEditRequestWithoutEditsHasNoGuard(t *testing.T) {
	// A deleted row carries no update at all, so nothing to guard.
	e := sampleRows()
	e.rows[0].state = rowDeleted
	req, why := e.request()
	if why != "" {
		t.Fatalf("request = %q", why)
	}
	if len(req.Updates) != 0 {
		t.Fatalf("a delete must not produce an update: %+v", req.Updates)
	}
}
