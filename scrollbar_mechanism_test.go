package main

// A scroll container that keeps its bar shows it when the content overflows.
//
// The tree (WORKSPACE §82), the result grid and the seven other scrolling
// panes all rely on this one mechanism: mygo draws a scrollbar only while
// the pointer is over the container unless the container keeps it. So the
// question is not "does this pane show a bar" but "does the mechanism work
// for a container that asks for it" — this answers that once, for all of
// them.
//
// 波 101 续四十二.

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestKeptScrollbarAppearsOnOverflow draws a tall box inside a scrolling
// container that keeps its bar, and checks the bar is painted.
func TestKeptScrollbarAppearsOnOverflow(t *testing.T) {
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Children(func() {
			ui.Scroll(c).Grow(1).FillWidth().KeepScrollbars(true).Children(func() {
				for i := 0; i < 200; i++ {
					ui.Text(c, strings.Repeat("line ", 40)).SingleLine()
				}
			})
		})
	}, 400, 300)
	tt.Frame()

	img := tt.Image()
	hits := 0
	for y := 0; y < 300; y++ {
		for x := 340; x < 400; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if isThumbColour(int(r>>8), int(g>>8), int(b>>8)) {
				hits++
			}
		}
	}
	t.Logf("thumb pixels along the right edge: %d", hits)
	if hits == 0 {
		t.Errorf("a scrolling container that keeps its bar shows no bar with content overflowing it")
	}
}

// TestKeptScrollbarStaysHiddenWhenContentFits is the other half: the bar is
// only drawn when there is something to scroll, so a short list does not grow
// a stray stripe.
func TestKeptScrollbarStaysHiddenWhenContentFits(t *testing.T) {
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Children(func() {
			ui.Scroll(c).Grow(1).FillWidth().KeepScrollbars(true).Children(func() {
				ui.Text(c, "one").SingleLine()
			})
		})
	}, 400, 300)
	tt.Frame()

	img := tt.Image()
	hits := 0
	for y := 0; y < 300; y++ {
		for x := 340; x < 400; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if isThumbColour(int(r>>8), int(g>>8), int(b>>8)) {
				hits++
			}
		}
	}
	t.Logf("thumb pixels with content that fits: %d", hits)
	if hits > 0 {
		t.Errorf("a container whose content fits still paints %d thumb pixels", hits)
	}
}
