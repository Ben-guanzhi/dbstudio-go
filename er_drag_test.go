package main

// Dragging a table: the geometry, not the pointer.
//
// mygo's Draggable is driven by the frame clock (its own tests advance an
// internal clock), so the public ui.NewTester cannot simulate a drag — and
// pretending otherwise would be a test that passes without the drag working.
// What can be pinned is the half the diagram depends on: the offset accumulates
// on the table, and the box's position and the edges' endpoints are computed
// from it, so a drag moves both.

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestBoxPositionIncludesTheDragOffset: the position the box is drawn at, and
// the endpoints the edges use, both come from the layout plus the offset.
func TestBoxPositionIncludesTheDragOffset(t *testing.T) {
	tb := erTable{Name: "t", Box: erBox{X: 100, Y: 50, W: 180, H: 60}}
	if x, y := tb.at(); x != 100 || y != 50 {
		t.Fatalf("an undragged box is at %.0f,%.0f, want 100,50", x, y)
	}
	tb.off = ui.Point{X: 40, Y: -20}
	if x, y := tb.at(); x != 140 || y != 30 {
		t.Fatalf("a dragged box is at %.0f,%.0f, want 140,30", x, y)
	}
}

// TestDragOffsetIsPerTable: dragging one table does not move another.
func TestDragOffsetIsPerTable(t *testing.T) {
	m := &erModel{Tables: []erTable{
		{Name: "a", Box: erBox{X: 0, Y: 0, W: 10, H: 10}},
		{Name: "b", Box: erBox{X: 200, Y: 0, W: 10, H: 10}},
	}}
	m.Tables[0].off = ui.Point{X: 25, Y: 25}
	if x, _ := m.Tables[1].at(); x != 200 {
		t.Errorf("dragging a moved b to %.0f", x)
	}
}

// TestEdgeEndpointsFollowTheBoxes: an edge's start is the referencing box's
// right edge and its end the referenced box's left edge, each including its
// drag offset — the arithmetic paintEdges does.
func TestEdgeEndpointsFollowTheBoxes(t *testing.T) {
	from := erTable{Name: "emp", Box: erBox{X: 400, Y: 100, W: 180, H: 60}}
	to := erTable{Name: "dept", Box: erBox{X: 100, Y: 100, W: 180, H: 60}}
	from.off = ui.Point{X: 30, Y: 10}
	to.off = ui.Point{X: -20, Y: 5}

	fx, fy := from.at()
	tx, ty := to.at()
	x0, y0 := fx+from.Box.W, fy+from.Box.H/2
	x1, y1 := tx, ty+to.Box.H/2

	if x0 != 610 {
		t.Errorf("the edge starts at x=%.0f, want the dragged box's right edge 610", x0)
	}
	if x1 != 80 {
		t.Errorf("the edge ends at x=%.0f, want the dragged box's left edge 80", x1)
	}
	if y0 != fy+30 || y1 != ty+30 {
		t.Errorf("the edge's ends are not at the boxes' vertical centres: %.0f, %.0f", y0, y1)
	}
}
