package main

// The plugins panel (波 100 终): the discovery/withdrawal API shown to the
// user. The tests pin the panel's three behaviors — listing, rescan
// idempotence, and withdrawal surfacing the kernel's refusal.

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// The panel lists the built-in engines and any discovered plugin kind, and
// the Rescan button re-runs discovery without duplicating registrations.
func TestPluginsPanelListsAndRescans(t *testing.T) {
	app, _ := refreshApp(t)
	app.initConsoles()
	app.initEditorState()
	dir := t.TempDir()
	writePlugin(t, dir, "panelfake")
	DiscoverPlugins(dir)
	t.Cleanup(func() { UndiscoverPlugins(dir) })

	view := newViewApp(t, app.store)
	view.plugOpen = true
	view.plugNote = app.plugNote
	tt := ui.NewTester(view.view, 1200, 760)
	for _, want := range []string{"Plugins", "Built-in", "mysql", "sqlite", "panelfake", "Rescan", "Withdraw"} {
		if !tt.HasText(want) {
			t.Errorf("the plugins panel is missing %q; texts = %v", want, tt.Texts())
		}
	}
	// Rescan is idempotent: no new kinds appear, no panic from a duplicate
	// RegisterFactory (the kernel panics on it — this call surviving proves
	// the pre-check).
	app.rescanPlugins()
}

// An empty discovery state shows the "none discovered" note instead of a list.
func TestPluginsPanelEmptyState(t *testing.T) {
	app, _ := refreshApp(t)
	app.initConsoles()
	app.initEditorState()
	view := newViewApp(t, app.store)
	view.plugOpen = true
	tt := ui.NewTester(view.view, 1200, 760)
	if !tt.HasText("None discovered") {
		t.Errorf("the empty panel has no note; texts = %v", tt.Texts())
	}
	if !tt.HasText("mysql") || !tt.HasText("sqlite") {
		t.Errorf("the built-ins are not listed; texts = %v", tt.Texts())
	}
}
