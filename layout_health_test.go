package main

// A layout health check for the whole window.
//
// The tree overflow (§80), the status bar's place (§81) and the tree's missing
// scrollbar (§82) were all found by looking at one thing at a time. This looks
// at everything at once: every element the accessibility tree reports, with its
// box, checked against the window's — and against each other.
//
// Two defects show up that way:
//   - an element laid out outside the window (the tree's rows, §80);
//   - two elements of the same kind painted on top of each other (a control
//     drawn twice, a label under a panel).
//
// 波 101 续三十七.

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// layoutHealth reports what the layout got wrong at one window size.
type layoutHealth struct {
	outside []string // elements laid out beyond the window
	overlap []string // pairs of text elements painted over each other
}

// checkLayout draws the app and reports the defects it can see.
func checkLayout(t *testing.T, w, h int) layoutHealth {
	t.Helper()
	return checkLayoutApp(t, connectedApp(t, 200), w, h)
}

// checkLayoutApp is the check for any app state.
func checkLayoutApp(t *testing.T, app *App, w, h int) layoutHealth {
	t.Helper()
	tt := ui.NewTester(app.view, w, h)
	tt.Frame()
	tt.Frame()

	var out layoutHealth

	// Every element the accessibility tree knows, with its box.
	type box struct {
		name string
		role ui.Role
		r    ui.Rect
	}
	var boxes []box
	for _, a := range tt.AccessAll() {
		boxes = append(boxes, box{a.Name, a.Role, a.Bounds})
	}

	// Outside the window.
	for _, b := range boxes {
		if b.r.W <= 0 || b.r.H <= 0 {
			continue
		}
		if b.r.X < -0.5 || b.r.Y < -0.5 || b.r.X+b.r.W > float32(w)+0.5 || b.r.Y+b.r.H > float32(h)+0.5 {
			out.outside = append(out.outside, fmt.Sprintf("%q(%v) at %v", b.name, b.role, b.r))
		}
	}
	sort.Strings(out.outside)

	// Text elements painted over each other. The same name at the same place is
	// one element reported twice (a label and its accessible name), not a
	// defect; two different names in the same place are.
	type textBox struct {
		text string
		r    ui.Rect
	}
	var texts []textBox
	for _, a := range tt.AccessAll() {
		if a.Name == "" || a.Bounds.W <= 0 || a.Bounds.H <= 0 {
			continue
		}
		texts = append(texts, textBox{a.Name, a.Bounds})
	}
	// Keep one box per name and place.
	seen := map[string]bool{}
	var uniq []textBox
	for _, tb := range texts {
		key := fmt.Sprintf("%s@%.0f,%.0f,%.0f,%.0f", tb.text, tb.r.X, tb.r.Y, tb.r.W, tb.r.H)
		if seen[key] {
			continue
		}
		seen[key] = true
		uniq = append(uniq, tb)
	}
	for i := 0; i < len(uniq); i++ {
		for j := i + 1; j < len(uniq); j++ {
			a, b := uniq[i], uniq[j]
			if a.text == b.text {
				continue
			}
			if isDecoration(a.text) || isDecoration(b.text) {
				continue
			}
			ox := max32(a.r.X, b.r.X)
			oy := max32(a.r.Y, b.r.Y)
			ox2 := min32(a.r.X+a.r.W, b.r.X+b.r.W)
			oy2 := min32(a.r.Y+a.r.H, b.r.Y+b.r.H)
			interW := ox2 - ox
			interH := oy2 - oy
			if interW > 1 && interH > 1 {
				// One inside the other is a label and its container, not a
				// collision; only a partial overlap is a defect.
				aInB := a.r.X >= b.r.X-0.5 && a.r.Y >= b.r.Y-0.5 &&
					a.r.X+a.r.W <= b.r.X+b.r.W+0.5 && a.r.Y+a.r.H <= b.r.Y+b.r.H+0.5
				bInA := b.r.X >= a.r.X-0.5 && b.r.Y >= a.r.Y-0.5 &&
					b.r.X+b.r.W <= a.r.X+a.r.W+0.5 && b.r.Y+b.r.H <= a.r.Y+a.r.H+0.5
				if aInB || bInA {
					continue
				}
				out.overlap = append(out.overlap, fmt.Sprintf("%q and %q overlap at %v",
					a.text, b.text,
					ui.Rect{X: ox, Y: oy, W: interW, H: interH}))
			}
		}
	}
	sort.Strings(out.overlap)
	return out
}

// isDecoration reports whether s is a separator glyph rather than a label.
func isDecoration(s string) bool {
	switch strings.TrimSpace(s) {
	case "", "│", "|", "·", "…", "×", "+", "-", "▾", "▦", "∅", "●":
		return true
	}
	return false
}

func min32(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func max32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

// TestLayoutHealth is the check itself, over several window sizes.
func TestLayoutHealth(t *testing.T) {
	for _, size := range []struct{ w, h int }{{1320, 800}, {1304, 763}, {1024, 600}} {
		out := checkLayout(t, size.w, size.h)
		t.Logf("%dx%d: %d elements outside the window, %d overlapping text pairs",
			size.w, size.h, len(out.outside), len(out.overlap))
		for _, s := range out.outside {
			t.Errorf("%dx%d: outside the window: %s", size.w, size.h, s)
		}
		for _, s := range out.overlap {
			t.Errorf("%dx%d: %s", size.w, size.h, s)
		}
	}
}

// TestLayoutHealthSizes sweeps more window sizes, including the narrow and
// short ones, where a layout has the least room and a control is most likely
// to be cut off.
func TestLayoutHealthSizes(t *testing.T) {
	for _, size := range []struct{ w, h int }{
		{1920, 1080}, {1600, 900}, {1440, 900}, {1280, 800}, {1152, 720},
		{1024, 768}, {960, 540}, {900, 500}, {860, 520},
	} {
		out := checkLayout(t, size.w, size.h)
		if len(out.outside) == 0 && len(out.overlap) == 0 {
			t.Logf("%dx%d: clean", size.w, size.h)
			continue
		}
		for _, s := range out.outside {
			t.Errorf("%dx%d: outside the window: %s", size.w, size.h, s)
		}
		for _, s := range out.overlap {
			t.Errorf("%dx%d: %s", size.w, size.h, s)
		}
	}
}

// TestLayoutHealthNotConnected is the first-run state: no connection, the
// navigator showing its own empty state.
func TestLayoutHealthNotConnected(t *testing.T) {
	for _, size := range []struct{ w, h int }{{1320, 800}, {1024, 600}, {900, 500}} {
		app := newViewApp(t, nil)
		out := checkLayoutApp(t, app, size.w, size.h)
		t.Logf("%dx%d: %d outside, %d overlapping", size.w, size.h, len(out.outside), len(out.overlap))
		for _, s := range out.outside {
			t.Errorf("%dx%d: outside the window: %s", size.w, size.h, s)
		}
		for _, s := range out.overlap {
			t.Errorf("%dx%d: %s", size.w, size.h, s)
		}
	}
}
