package main

// Where the status bar goes, and whether the tree paints over it.
//
// The screenshot review could only say "the bar is not visible". These tests say
// what the layout does — the bar's box at several window sizes, and with a
// schema long enough to fill the navigator, whether the tree pushes the bar out
// of the window. That is the whole question, asked where it can be answered.
//
// 波 101 续二十一.

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ben-guanzhi/dbcore"
	"github.com/egoist/mygo/ui"
)

// TestStatusBarWithinWindow draws the whole app at several window sizes. The bar
// sits at the bottom, under everything, so it must be laid out near the bottom and
// never pushed out at any size — the defect the screenshot review kept finding.
func TestStatusBarWithinWindow(t *testing.T) {
	for _, size := range []struct{ w, h int }{{1320, 800}, {1304, 763}, {1200, 700}, {1024, 600}, {900, 500}} {
		app := newViewApp(t, nil)
		tt := ui.NewTester(app.view, size.w, size.h)
		tt.Frame()
		r, ok := tt.Find("Not connected")
		if !ok {
			t.Fatalf("%dx%d: the status bar's text is not laid out; texts = %v", size.w, size.h, tt.Texts())
		}
		// At the bottom: below the halves, whatever the window is.
		if r.Y < float32(size.h)/2 {
			t.Errorf("%dx%d: the status bar sits at y=%.0f, not at the bottom", size.w, size.h, r.Y)
		}
		if r.Y+r.H > float32(size.h) {
			t.Errorf("%dx%d: the status bar is outside the window (bottom %.0f > %d)", size.w, size.h, r.Y+r.H, size.h)
		}
	}
}

// connectedApp is a session with a long schema: the state the screenshots were
// taken in.
func connectedApp(t *testing.T, rows int) *App {
	t.Helper()
	app := newViewApp(t, nil)
	app.open = true
	app.connName = "22"
	app.conn = Conn{Name: "22", Kind: "mysql", Schema: "gs_cloud"}
	app.schema = "gs_cloud"
	tables := make([]tableInfo, 0, rows)
	for i := 0; i < rows; i++ {
		tables = append(tables, tableInfo{Name: fmt.Sprintf("erp_table_%03d", i)})
	}
	app.catalog = catalog{
		schemas: []string{"gs_cloud"},
		tables:  map[string][]tableInfo{"gs_cloud": tables},
		cols:    map[string][]dbcore.ColumnInfo{},
	}
	return app
}

// TestTreeDoesNotPushStatusBarOut is the case the screenshots showed: a connected
// session with a long schema. The tree must not move the bar, and now that the bar
// sits at the bottom, the tree must not paint into it either.
func TestTreeDoesNotPushStatusBarOut(t *testing.T) {
	for _, rows := range []int{200} {
		app := connectedApp(t, rows)
		const w, h = 1304, 763
		tt := ui.NewTester(app.view, w, h)
		tt.Frame()
		// The bar's first cell is the connection mark and name.
		r, ok := tt.Find("schema: gs_cloud")
		if !ok {
			t.Fatalf("rows=%d: the status bar is not laid out; texts = %v", rows, tt.Texts())
		}
		bottom := r.Y + r.H
		t.Logf("rows=%3d -> bar y=%.0f h=%.0f bottom=%.0f inside=%v", rows, r.Y, r.H, bottom, bottom <= h)
		if bottom > h {
			t.Errorf("rows=%d: the tree pushed the status bar out (bottom %.0f > %d)", rows, bottom, h)
		}
		// The tree's lowest laid-out row must stay above the bar's top. Only the
		// tree's own rows count: the bar itself carries "│" separators, which are
		// labels too and would otherwise be read as tree rows.
		maxBottom := float32(0)
		maxLabel := ""
		for _, txt := range tt.Texts() {
			if txt != "gs_cloud" && !strings.HasPrefix(txt, "erp_table_") {
				continue
			}
			if tr, ok := tt.Find(txt); ok && tr.Y+tr.H > maxBottom {
				maxBottom = tr.Y + tr.H
				maxLabel = txt
			}
		}
		if maxBottom > r.Y {
			t.Errorf("rows=%d: the tree (%q) paints into the status bar: tree bottom %.0f > bar top %.0f", rows, maxLabel, maxBottom, r.Y)
		}
	}
}

// TestConnectedFrame saves a frame of the long-schema session for inspection.
func TestConnectedFrame(t *testing.T) {
	app := connectedApp(t, 80)
	const w, h = 1320, 800
	tt := ui.NewTester(app.view, w, h)
	tt.Frame()
	dir := filepath.Join("..", "shots")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Skip(err)
	}
	path := filepath.Join(dir, "ds-tester-tree.png")
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
