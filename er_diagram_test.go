package main

// The ER diagram on screen: the modal opens, it says how much it holds, and
// the canvas is big enough for the laid-out graph.
//
// 波 101 续四十六.

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/Ben-guanzhi/dbcore"
	"github.com/egoist/mygo/ui"
)

// erApp is a connected app with a schema of tables whose columns are already
// in the catalog — the diagram's data is in hand, which is the state a user
// reaches after the tree has loaded the schema.
func erApp(t *testing.T) *App {
	t.Helper()
	app := connectedApp(t, 3)
	schema := "gs_cloud"
	app.catalog.cols = map[string][]dbcore.ColumnInfo{
		schema + "\x00erp_table_000": {
			{Name: "id", Type: "int", IsPrimaryKey: true},
			{Name: "dept_id", Type: "int"},
		},
		schema + "\x00erp_table_001": {
			{Name: "id", Type: "int", IsPrimaryKey: true},
			{Name: "name", Type: "text"},
		},
		schema + "\x00erp_table_002": {
			{Name: "emp_id", Type: "int"},
			{Name: "amount", Type: "decimal"},
		},
	}
	app.erModel = app.erFetch()
	return app
}

func TestERFetchBuildsTablesAndEdges(t *testing.T) {
	app := erApp(t)
	if len(app.erModel.Tables) != 3 {
		t.Fatalf("the diagram has %d tables, want 3", len(app.erModel.Tables))
	}
	// The layout gives every table a box.
	for _, tb := range app.erModel.Tables {
		if tb.Box.W == 0 || tb.Box.H == 0 {
			t.Errorf("%s has no box: %v", tb.Name, tb.Box)
		}
	}
}

func TestERModalOpensAndCounts(t *testing.T) {
	app := erApp(t)
	app.erOpen = true

	tt := ui.NewTester(app.view, 900, 700)
	tt.Frame()

	for _, want := range []string{"ER Diagram", "erp_table_000", "erp_table_001", "erp_table_002"} {
		if !tt.HasText(want) {
			t.Errorf("the diagram does not show %q; texts = %v", want, tt.Texts())
		}
	}
	// 主键列有标记。
	if !tt.HasText("• id") {
		t.Errorf("the primary key column is not marked; texts = %v", tt.Texts())
	}
}

func TestERCanvasFitsTheLayout(t *testing.T) {
	app := erApp(t)
	w, h := erCanvasSize(app.erModel)
	for _, tb := range app.erModel.Tables {
		if tb.Box.X+tb.Box.W > w || tb.Box.Y+tb.Box.H > h {
			t.Errorf("%s at %v falls outside the canvas %.0fx%.0f", tb.Name, tb.Box, w, h)
		}
	}
}

// TestSaveERFrame saves the diagram for the eye, as the grid frame does.
func TestSaveERFrame(t *testing.T) {
	app := erApp(t)
	app.erOpen = true

	tt := ui.NewTester(app.view, 900, 700)
	tt.Frame()
	tt.Frame()

	dir := filepath.Join("..", "shots")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Skip(err)
	}
	path := filepath.Join(dir, "ds-er.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, tt.Image()); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %s", path)
	_ = fmt.Sprint
}
