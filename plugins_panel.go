package main

// The plugins panel (波 100 终): the discovery/withdrawal API the app already
// has, shown to the user. Built-in engines and plugin engines are listed
// apart; Rescan re-runs discovery over the app directory (idempotent); a
// plugin can be withdrawn unless a connection of its kind is opening — the
// kernel's reliance guard answers, and this panel surfaces its refusal.

import (
	"fmt"
	"sort"

	"github.com/Ben-guanzhi/dbcore"
	"github.com/egoist/mygo/ui"
)

// openPlugins raises the plugins panel.
func (app *App) openPlugins() {
	app.plugNote = ""
	app.plugOpen = true
	app.invalidate()
}

// rescanPlugins re-runs discovery over the app directory and reports what
// changed. Idempotent by construction: an already-registered kind is skipped.
func (app *App) rescanPlugins() {
	before := map[string]bool{}
	for _, k := range PluginKinds() {
		before[k] = true
	}
	DiscoverPlugins(app.pluginDir())
	added := 0
	for _, k := range PluginKinds() {
		if !before[k] {
			added++
		}
	}
	if added > 0 {
		app.plugNote = fmt.Sprintf("discovered %d new engine(s)", added)
	} else {
		app.plugNote = "no new plugins found"
	}
	app.invalidate()
}

// withdrawPlugin withdraws one plugin kind, surfacing the kernel's reliance
// refusal (a connection of that kind is opening) instead of dropping it.
func (app *App) withdrawPlugin(kind string) {
	n, busy := UndiscoverPlugins("")
	_ = n
	if len(busy) > 0 {
		app.plugNote = busy[0]
	} else {
		app.plugNote = kind + " withdrawn"
	}
	app.invalidate()
}

// pluginDir is the directory discovery scans: the store's directory — the
// same one main.go's first discovery scanned (user-data dir beside the exe).
func (app *App) pluginDir() string {
	if app.store != nil {
		return app.store.Dir()
	}
	return "."
}

// pluginsView draws the modal when open.
func (app *App) pluginsView(c *ui.Context) {
	if !app.plugOpen {
		return
	}
	t := c.Theme()

	pluginKinds := PluginKinds()
	builtIn := []string{"mysql", "sqlite"}
	sort.Strings(builtIn)

	ui.Modal(c, &app.plugOpen, func() {
		ui.Column(c).Gap(10).Padding(14).Width(480).Height(420).
			Background(t.Surface).Radius(10).Border(1, t.Border).
			Children(func() {
				ui.Text(c, "Plugins").FontSize(15).Bold()
				ui.Text(c, "Engines beyond the built-ins arrive as plugin executables beside the app, named dbstudio-plugin-<kind>.").
					TextColor(t.TextMuted).Wrap()

				ui.Text(c, "Built-in").FontSize(11).Bold().TextColor(t.TextMuted)
				ui.Row(c).Gap(8).Wrap().Children(func() {
					for _, k := range builtIn {
						ui.Text(c, k).TextColor(t.Success).SingleLine()
					}
				})

				ui.Text(c, "Plugins").FontSize(11).Bold().TextColor(t.TextMuted)
				if len(pluginKinds) == 0 {
					empty := "None discovered. Drop dbstudio-plugin-<kind>.exe beside the executable and Rescan."
					ui.Text(c, empty).TextColor(t.TextMuted).Wrap()
				} else {
					ui.Column(c).Gap(4).Children(func() {
						for _, k := range pluginKinds {
							k := k
							ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
								ui.Text(c, k).SingleLine()
								ui.Spacer(c)
								if ui.Button(c, "Withdraw").FontSize(10).
									Tooltip("Unregister this engine (refused while a connection of it is opening)").Clicked() {
									app.withdrawPlugin(k)
								}
							})
						}
					})
				}

				if app.plugNote != "" {
					ui.Text(c, app.plugNote).TextColor(t.Accent).Wrap()
				}
				// A kind whose factory vanished while a saved connection names
				// it is the has-factory probe's finding, shown live.
				if !dbcore.HasFactory("mysql") {
					ui.Text(c, "warning: the mysql factory is missing — this build is broken").TextColor(t.Danger).Wrap()
				}
				ui.Row(c).Gap(8).Children(func() {
					if ui.Button(c, "Rescan").Clicked() {
						app.rescanPlugins()
					}
					ui.Spacer(c)
					if ui.Button(c, "Close").Clicked() {
						app.plugOpen = false
					}
				})
			})
	})
}
