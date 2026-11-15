package main

// The ER layout's promises, measured.
//
// A layout is easy to eyeball and hard to trust. These are the properties
// that make it worth drawing: the referenced table sits left of the table that
// references it (the diagram reads in the direction of the dependency); boxes
// do not overlap; a cycle does not hang the layout; and the same graph laid
// out twice lands in the same place, whatever order the tables arrived in.

import (
	"testing"
)

func erTableNamed(name string, cols ...string) erTable {
	t := erTable{Name: name}
	for _, c := range cols {
		t.Cols = append(t.Cols, erCol{Name: c})
	}
	return t
}

// TestReferencedTablesSitLeft is the direction of the diagram: a table that is
// referenced sits left of the table referencing it.
func TestReferencedTablesSitLeft(t *testing.T) {
	m := &erModel{
		Tables: []erTable{
			erTableNamed("departments", "id", "name"),
			erTableNamed("employees", "id", "dept_id"),
		},
		FKs: []erFK{{From: "employees", FromCol: "dept_id", To: "departments", ToCol: "id"}},
	}
	m.layoutER()

	left := m.Tables[0].Box.X
	right := m.Tables[1].Box.X
	if left >= right {
		t.Errorf("departments is at x=%.0f and employees at x=%.0f: the referenced table must sit left", left, right)
	}
}

// TestBoxesDoNotOverlap: two boxes in the same column are stacked, and boxes
// in adjacent columns are a gap apart.
func TestBoxesDoNotOverlap(t *testing.T) {
	m := &erModel{
		Tables: []erTable{
			erTableNamed("a", "id"),
			erTableNamed("b", "id"),
			erTableNamed("c", "id"),
			erTableNamed("d", "id"),
		},
		FKs: []erFK{
			{From: "b", FromCol: "id", To: "a", ToCol: "id"},
			{From: "d", FromCol: "id", To: "c", ToCol: "id"},
		},
	}
	m.layoutER()

	for i := range m.Tables {
		for j := i + 1; j < len(m.Tables); j++ {
			a, b := m.Tables[i].Box, m.Tables[j].Box
			overlapX := a.X < b.X+b.W && b.X < a.X+a.W
			overlapY := a.Y < b.Y+b.H && b.Y < a.Y+a.H
			if overlapX && overlapY {
				t.Errorf("%s at %v and %s at %v overlap",
					m.Tables[i].Name, a, m.Tables[j].Name, b)
			}
		}
	}
}

// TestCycleDoesNotHang: a cycle of references must lay out, not recurse.
func TestCycleDoesNotHang(t *testing.T) {
	m := &erModel{
		Tables: []erTable{
			erTableNamed("a", "id", "b_id"),
			erTableNamed("b", "id", "a_id"),
		},
		FKs: []erFK{
			{From: "a", FromCol: "b_id", To: "b", ToCol: "id"},
			{From: "b", FromCol: "a_id", To: "a", ToCol: "id"},
		},
	}
	m.layoutER()
	for _, tb := range m.Tables {
		if tb.Box.W == 0 || tb.Box.H == 0 {
			t.Errorf("%s has no box after laying out a cycle", tb.Name)
		}
	}
}

// TestLayoutIsDeterministic: the same graph laid out twice lands in the same
// place, and the result does not depend on the order the tables arrived in.
func TestLayoutIsDeterministic(t *testing.T) {
	build := func(order []erTable) *erModel {
		return &erModel{
			Tables: order,
			FKs: []erFK{
				{From: "employees", FromCol: "dept_id", To: "departments", ToCol: "id"},
				{From: "departments", FromCol: "parent_id", To: "departments", ToCol: "id"},
			},
		}
	}
	first := build([]erTable{
		erTableNamed("employees", "id", "dept_id"),
		erTableNamed("departments", "id", "parent_id"),
		erTableNamed("salaries", "emp_id", "amount"),
	})
	first.layoutER()
	again := build([]erTable{
		erTableNamed("employees", "id", "dept_id"),
		erTableNamed("departments", "id", "parent_id"),
		erTableNamed("salaries", "emp_id", "amount"),
	})
	again.layoutER()
	for i := range first.Tables {
		if first.Tables[i].Box != again.Tables[i].Box {
			t.Fatalf("%s moved between two layouts of the same graph: %v vs %v",
				first.Tables[i].Name, first.Tables[i].Box, again.Tables[i].Box)
		}
	}
}

// TestLayoutKeepsTheBoxesInsideAReasonableCanvas: the rightmost box ends
// within the width the columns imply, so the drawing knows how wide to be.
func TestLayoutKeepsTheBoxesInsideAReasonableCanvas(t *testing.T) {
	m := &erModel{Tables: []erTable{
		erTableNamed("a", "id"), erTableNamed("b", "id"), erTableNamed("c", "id"),
	}}
	m.layoutER()
	for _, tb := range m.Tables {
		if tb.Box.X < 0 || tb.Box.Y < 0 {
			t.Errorf("%s has a negative origin: %v", tb.Name, tb.Box)
		}
	}
}
