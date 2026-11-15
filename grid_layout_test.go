package main

// The result grid can reach its columns.
//
// A table's columns run past the pane's right edge on purpose: the table
// scrolls, and a column outside the pane is one the user reaches by scrolling,
// not one that is lost. So "a header is outside the window" is not a defect
// here — the defect is the table having no scrollbar to reach those columns
// with. With twenty columns and a 1304-wide window the widest header sits at
// x=2197, and before the fix the pane held no thumb at all: half the result
// was unreachable.
//
// The same rule the tree needs (WORKSPACE §82): mygo draws a scrollbar only
// when the pointer is over the container unless the container keeps it.
//
// 波 101 续四十一.

import (
	"fmt"
	"testing"

	"github.com/Ben-guanzhi/dbcore"
	"github.com/egoist/mygo/ui"
)

// wideResult builds a result with n columns and n rows, the values long
// enough that the columns want more room than a pane has.
func wideResult(cols, rows int) *dbcore.QueryResult {
	head := make([]dbcore.ColumnInfo, cols)
	body := make([][]dbcore.Value, rows)
	for c := 0; c < cols; c++ {
		head[c] = dbcore.ColumnInfo{Name: fmt.Sprintf("column_%02d", c), Type: "text"}
	}
	for r := 0; r < rows; r++ {
		row := make([]dbcore.Value, cols)
		for c := 0; c < cols; c++ {
			row[c] = dbcore.Value{Kind: dbcore.ValText, S: fmt.Sprintf("v_%d_%d_%s", r, c, "abcdefghijklmnop")}
		}
		body[r] = row
	}
	return &dbcore.QueryResult{Columns: head, Rows: body}
}

// isThumbColour reports whether a pixel is the light theme's scrollbar thumb:
// Scrollbar is RGBA(0,0,0,0.32), which over the Surface #f4f4f5 composites to
// about rgb(166,166,166).
func isThumbColour(r, g, b int) bool {
	return r >= 140 && r <= 200 && g >= 140 && g <= 200 && b >= 140 && b <= 200 && r == g && g == b
}

// TestGridHasAHorizontalScrollbar draws a result wider than the pane and
// checks that the table's horizontal thumb is drawn along its bottom — the
// bar that makes the columns to the right reachable.
func TestGridHasAHorizontalScrollbar(t *testing.T) {
	app := connectedApp(t, 200)
	app.result = wideResult(20, 40)

	for _, size := range []struct{ w, h int }{{1320, 800}, {1024, 600}, {900, 500}} {
		tt := ui.NewTester(app.view, size.w, size.h)
		tt.Frame()
		tt.Frame()

		// The content really is wider than the window: the widest header.
		widest := float32(0)
		for _, txt := range tt.Texts() {
			if len(txt) >= 6 && txt[:6] == "column" {
				if r, ok := tt.Find(txt); ok && r.X+r.W > widest {
					widest = r.X + r.W
				}
			}
		}
		if widest <= float32(size.w) {
			t.Fatalf("%dx%d: the result is only %.0f wide, not wider than the window", size.w, size.h, widest)
		}

		// The thumb, somewhere along the results pane's bottom.
		img := tt.Image()
		hits := 0
		for y := size.h / 2; y < size.h; y++ {
			for x := 270; x < size.w; x++ {
				r, g, b, _ := img.At(x, y).RGBA()
				if isThumbColour(int(r>>8), int(g>>8), int(b>>8)) {
					hits++
				}
			}
		}
		if hits == 0 {
			t.Errorf("%dx%d: the grid is %.0f wide in a %d window and shows no horizontal scrollbar",
				size.w, size.h, widest, size.w)
		} else {
			t.Logf("%dx%d: grid %.0f wide, %d thumb pixels", size.w, size.h, widest, hits)
		}
	}
}

// TestGridRowsStayInsideThePane checks the row half: the grid's rows are
// clipped by the pane, not laid out over the state line below it.
func TestGridRowsStayInsideThePane(t *testing.T) {
	app := connectedApp(t, 200)
	app.result = wideResult(6, 60)

	const w, h = 1304, 763
	tt := ui.NewTester(app.view, w, h)
	tt.Frame()
	tt.Frame()

	bar, ok := tt.Find("schema: gs_cloud")
	if !ok {
		t.Fatalf("the state line is not laid out; texts = %v", tt.Texts())
	}
	lowest := float32(0)
	worst := ""
	for _, txt := range tt.Texts() {
		// A grid cell starts with "v_"; a header with "column_".
		if len(txt) < 2 || (txt[:2] != "v_" && (len(txt) < 6 || txt[:6] != "column")) {
			continue
		}
		r, ok := tt.Find(txt)
		if !ok {
			continue
		}
		if r.Y+r.H > lowest {
			lowest, worst = r.Y+r.H, txt
		}
	}
	if lowest > bar.Y {
		t.Errorf("the grid's lowest cell (%q) ends at %.0f, below the state line's top %.0f",
			worst, lowest, bar.Y)
	}
}
