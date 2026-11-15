package main

// The shortcut sheet lists what the menus declare.
//
// The sheet is read from the menu items, so the test asks the two questions
// that matter: does it show every accelerator, and does it skip the entries
// that are not shortcuts (no keys) or not ours (the system menus)?

import (
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func shortcutApp(t *testing.T) *App {
	t.Helper()
	app := newViewApp(t, nil)
	app.menuItems = []*mygo.MenuItem{
		{Label: "File", Submenu: []*mygo.MenuItem{
			{Label: "New Connection", Accelerator: "CmdOrCtrl+N"},
			{Label: "Disconnect"},
		}},
		{Label: "Database", Submenu: []*mygo.MenuItem{
			{Label: "Run Query", Accelerator: "CmdOrCtrl+Enter"},
			{Label: "Explain Query", Accelerator: "CmdOrCtrl+Shift+E"},
		}},
		{Role: mygo.RoleEditMenu},
	}
	return app
}

func TestShortcutRowsComeFromTheMenu(t *testing.T) {
	app := shortcutApp(t)
	rows := app.shortcutRows()

	if len(rows) != 3 {
		t.Fatalf("the sheet has %d rows, want 3: %+v", len(rows), rows)
	}
	want := []shortcutRow{
		{"File", "New Connection", "CmdOrCtrl+N"},
		{"Database", "Run Query", "CmdOrCtrl+Enter"},
		{"Database", "Explain Query", "CmdOrCtrl+Shift+E"},
	}
	for i, w := range want {
		if rows[i] != w {
			t.Errorf("row %d = %+v, want %+v", i, rows[i], w)
		}
	}
}

func TestShortcutSheetShowsTheKeys(t *testing.T) {
	app := shortcutApp(t)
	app.shortcutsOpen = true

	tt := ui.NewTester(app.view, 700, 600)
	tt.Frame()

	for _, want := range []string{"Keyboard Shortcuts", "File", "Database",
		"New Connection", "CmdOrCtrl+N", "Run Query", "CmdOrCtrl+Enter"} {
		if !tt.HasText(want) {
			t.Errorf("the sheet does not show %q; texts = %v", want, tt.Texts())
		}
	}
	// An item with no accelerator is not a shortcut and is not listed.
	if tt.HasText("Disconnect") {
		t.Errorf("an entry with no keys is listed as a shortcut")
	}
}
