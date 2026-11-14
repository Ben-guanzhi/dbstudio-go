package main

// The About box: what the product is, which version runs, and where it came
// from. 波 100（发布整改）：版本由 build.ps1 的 -ldflags 注入（-X main.version=
// …），未注入时是 dev——一个没有版本号的"可发布产品"无法被支持。

import (
	"github.com/egoist/mygo/ui"
)

// version is the release stamp build.ps1 injects. A developer build says dev.
var version = "dev"

// aboutOpen is the About box's state; openAbout raises it.
func (app *App) openAbout() {
	app.aboutOpen = true
	app.invalidate()
}

// aboutView draws the About box when open.
func (app *App) aboutView(c *ui.Context) {
	if !app.aboutOpen {
		return
	}
	t := c.Theme()
	ui.Modal(c, &app.aboutOpen, func() {
		ui.Column(c).Gap(8).Padding(16).Width(420).
			Background(t.Surface).Radius(10).Border(1, t.Border).
			Children(func() {
				ui.Text(c, "DB Studio Go").FontSize(16).Bold()
				ui.Text(c, "A focused SQL query workbench (dbstudio-go).").
					TextColor(t.TextMuted).Wrap()
				ui.Row(c).Gap(6).Children(func() {
					ui.Text(c, "Version").TextColor(t.TextMuted).FontSize(11)
					ui.Text(c, version).FontSize(11)
				})
				ui.Row(c).Gap(6).Children(func() {
					ui.Text(c, "UI").TextColor(t.TextMuted).FontSize(11)
					ui.Text(c, "mygo · Go").TextColor(t.TextMuted).FontSize(11)
				})
				ui.Row(c).Gap(8).Children(func() {
					ui.Spacer(c)
					if ui.Button(c, "Close").Clicked() {
						app.aboutOpen = false
						app.invalidate()
					}
				})
			})
	})
}
