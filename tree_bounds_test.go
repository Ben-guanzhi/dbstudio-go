package main

// Diagnose whether the schema tree's rows stay inside the window after the
// window-sizing fix (WORKSPACE §74/§76): the tree's Outline is a virtualising
// scroll and §73-§76 could not get it to clip to its pane. This asks the
// question where it can be answered — the y of every laid-out tree row label
// against the window, at a window size the screenshots showed the defect.
//
// 波 101 续三十二.

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestTreeRowsStayInWindow draws a long schema at a window size and checks
// that no tree row label is laid out below the window. Rows drawn past the
// pane's bottom would be the overflow the screenshots kept showing. It checks
// several window sizes: the tree's bound is computed from the canvas height, so
// it has to hold at each.
func TestTreeRowsStayInWindow(t *testing.T) {
	for _, size := range []struct{ w, h int }{{1304, 763}, {1320, 800}, {1024, 600}, {900, 500}} {
		app := connectedApp(t, 200)
		tt := ui.NewTester(app.view, size.w, size.h)
		tt.Frame()
		tt.Frame()

		// The first table row is the top of the tree's visible window.
		first, ok := tt.Find("erp_table_000")
		if !ok {
			t.Fatalf("%dx%d: the first tree row is not laid out; texts = %v", size.w, size.h, tt.Texts())
		}
		t.Logf("%dx%d: first tree row y=%.0f h=%.0f bottom=%.0f (window %d)", size.w, size.h, first.Y, first.H, first.Y+first.H, size.h)

		// Walk the tree row labels and find the lowest one laid out.
		maxBottom := float32(0)
		maxLabel := ""
		for _, txt := range tt.Texts() {
			if r, ok := tt.Find(txt); ok && r.Y+r.H > maxBottom {
				maxBottom = r.Y + r.H
				maxLabel = txt
			}
		}
		t.Logf("%dx%d: lowest laid-out label = %q, bottom = %.0f (window %d)", size.w, size.h, maxLabel, maxBottom, size.h)

		// A row laid out below the window is the overflow.
		if maxBottom > float32(size.h) {
			t.Errorf("%dx%d: a tree row (%q) is laid out below the window: bottom %.0f > %d", size.w, size.h, maxLabel, maxBottom, size.h)
		}
	}
}
