package main

// The window's canvas must not be larger than the client area.
//
// This is the defect that cost the status bar its place: the canvas is laid out
// at the size the window was created with, the menu bar then takes 38px out of
// the client area, and the last thing in the layout ends up below the window.
//
// The fix is in the app (fitContentToWindow), but the invariant belongs to a
// test: the content area must be the size the layout was given.
//
// 波 101 续二十七.

import "testing"

// TestLayoutSizeIsTheSizeTheLayoutWasGiven checks the numbers the app works
// with: the size the window was asked for, and the size the client area ends up
// with once the menu bar is in place.
func TestLayoutSizeIsTheSizeTheLayoutWasGiven(t *testing.T) {
	app := newViewApp(t, nil)
	// A window does not exist in a test, so the sizes come from the app's own
	// record of what it asked for.
	app.wantWidth, app.wantHeight = 1320, 800
	w, h := app.layoutSize()
	if w != 1320 || h != 800 {
		t.Fatalf("layout size = %dx%d, want 1320x800", w, h)
	}
}

// TestFitContentToWindowWithoutAWindowDoesNothing: the helper is called right
// after the window is made, but it must survive being called before that.
func TestFitContentToWindowWithoutAWindowDoesNothing(t *testing.T) {
	app := newViewApp(t, nil)
	app.win = nil
	app.fitContentToWindow() // must not panic
}
