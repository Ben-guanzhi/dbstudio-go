package main

// A picture of the result grid with twenty columns, for the eye.
//
// The measurements say the grid keeps a horizontal scrollbar now (§87); this
// saves the frame so the claim can be checked without reading the numbers.
//
// 波 101 续四十二.

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestSaveWideResultFrame(t *testing.T) {
	app := connectedApp(t, 200)
	app.result = wideResult(20, 40)

	const w, h = 1320, 800
	tt := ui.NewTester(app.view, w, h)
	tt.Frame()
	tt.Frame()

	dir := filepath.Join("..", "shots")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Skip(err)
	}
	path := filepath.Join(dir, "ds-wide-result.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, tt.Image()); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %s", path)
}
