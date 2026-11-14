package main

// The window's chrome: the pieces every panel shares, so the layout is a
// composition of a few shapes rather than a page of ad-hoc rows.
//
// The arrangement follows DBeaver's: a toolbar of actions on top, a database
// navigator down the left side, the workbench (editor over results) on the
// right, and one status bar across the bottom. What each region says follows
// DataGrip: the navigator is the object tree with its own schema scope, the
// workbench holds consoles and, under them, the results with their own
// Messages tab, and the status bar answers "where am I, what did it cost".
//
// Two rules make the shapes hold:
//   - a panel *title* and its *empty state* are part of the panel's contract,
//     not an afterthought: panelHeader and emptyState are the only ways either
//     is drawn, so no panel can silently have neither;
//   - the toolbar only *changes* state and the status bar only *reads* it. A
//     fact shown while it is also being changed in the same row reads as two
//     different facts (the status line used to sit between the buttons).

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"
)

// panelHeader draws a region's title row: the title, whatever actions belong to
// it, and its own bottom border. body draws the actions, right of the title.
func panelHeader(c *ui.Context, t *ui.Theme, title string, body func()) {
	ui.Row(c).Gap(6).Padding(4, 8).AlignItems(ui.Center).
		BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
		ui.Text(c, title).FontSize(11).Bold().TextColor(t.TextMuted).SingleLine()
		ui.Spacer(c)
		if body != nil {
			body()
		}
	})
}

// emptyState is what a panel says when it has nothing to show: one muted line
// that names the reason, so a blank region is never ambiguous.
func emptyState(c *ui.Context, t *ui.Theme, text string) {
	ui.Text(c, text).TextColor(t.TextMuted).Padding(10).FontSize(11).SingleLine()
}

// colorBand is the connection's colour as a strip across the top of the window,
// and the window's title carries the connection's name and lock. A title cannot
// be coloured by the platform, so the band is what makes the colour part of the
// window rather than of a label inside it.
func (app *App) colorBand(c *ui.Context) {
	if !app.open {
		return
	}
	ink, ok := colorInk(app.conn.Color)
	if !ok {
		return
	}
	ui.Box(c).FillWidth().Height(3).Background(ink)
}

// windowTitle is what the platform shows: the app, then the connection and its
// lock, so a taskbar button names the server it is pointed at.
func (app *App) windowTitle() string {
	const base = "DB Studio"
	if !app.open || app.connName == "" {
		return base
	}
	title := base + " — " + connectionMark(app.conn)
	if app.schema != "" {
		title += " @ " + app.schema
	}
	return title
}

// refreshTitle pushes the title after a connect, a lock change or a rename.
func (app *App) refreshTitle() {
	if app.win != nil {
		app.win.SetTitle(app.windowTitle())
	}
}

// statusBar is the bottom bar: the one place that reports state — the
// connection's health and identity, the working schema, the transaction, the
// rows and time of the last run, and the last message. It reads; it never
// changes anything but the two probes the dot offers.
func (app *App) statusBar(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Gap(8).Padding(3, 8).AlignItems(ui.Center).
		BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
		// The health dot, first: a red dot is the one thing worth seeing before
		// reading anything else.
		if app.open {
			dot := ui.Button(c, "●").FontSize(10).Tooltip(app.healthLabel() + " (click to check now)").
				ContextMenu(func(m *ui.Menu) {
					if m.Item("Check Connection").Chosen() {
						app.pingOnce()
					}
					if m.Item("Connection Info…").Chosen() {
						app.openConnInfo()
					}
				})
			switch app.health {
			case "ok":
				dot.TextColor(t.Success)
			case "down":
				dot.TextColor(t.Danger)
			default:
				dot.TextColor(t.TextMuted)
			}
			if dot.Clicked() {
				app.pingOnce()
			}
		}
		if app.open {
			// The saved name, not the full label: a status bar is a line, and a
			// file path would push everything else off it. The label (path, host,
			// database) is the tooltip.
			name := ui.Text(c, connectionMark(app.conn)).FontSize(10).SingleLine().
				Tooltip(app.conn.label())
			if ink, ok := colorInk(app.conn.Color); ok {
				name.TextColor(ink)
			}
			ui.Text(c, "│").TextColor(t.Border).FontSize(10)
			ui.Text(c, "schema: "+app.labeledSchema()).FontSize(10).TextColor(t.TextMuted).SingleLine()
			ui.Text(c, "│").TextColor(t.Border).FontSize(10)
			ui.Text(c, engineLabel(app.engine())).FontSize(10).TextColor(t.TextMuted).SingleLine()
			if app.txActive {
				ui.Text(c, "│").TextColor(t.Border).FontSize(10)
				ui.Text(c, "in transaction").FontSize(10).TextColor(t.Accent).SingleLine()
			} else if app.txCapable() {
				ui.Text(c, "│").TextColor(t.Border).FontSize(10)
				state := "autocommit"
				if !app.autocommit {
					state = "manual commit"
				}
				if app.txIsolation != "" {
					state += " · " + app.txIsolation
				}
				ui.Text(c, state).FontSize(10).TextColor(t.TextMuted).SingleLine()
			}
		} else {
			ui.Text(c, "Not connected").FontSize(10).TextColor(t.TextMuted).SingleLine()
		}
		// The last run's cost, when there was one. The active tab's elapsed
		// rides beside the count (波 97: DataGrip/Navicat 惯例——耗时是评估
		// 慢查询的第一信号，Messages 页有全程，状态栏给一眼值)。
		if app.result != nil {
			if n := len(app.result.Rows); n > 0 {
				ui.Text(c, "│").TextColor(t.Border).FontSize(10)
				label := fmt.Sprintf("%d row(s)", n)
				if tt := app.tab(); tt != nil && tt.elapsed > 0 {
					label += " · " + tt.elapsed.Round(time.Millisecond).String()
				}
				ui.Text(c, label).FontSize(10).TextColor(t.TextMuted).SingleLine()
			}
		}
		if app.busy {
			ui.Spinner(c)
		}
		ui.Spacer(c)
		// The message: short, on one line; the Messages tab has the whole of it.
		if msg := app.statusMessage(); msg != "" {
			ui.Text(c, msg).FontSize(10).TextColor(t.TextMuted).SingleLine()
		}
	})
}

// statusMessage is the line the status bar ends with: the app's own message, or
// the current tab's error when it has one.
func (app *App) statusMessage() string {
	if app.busy {
		return "Running…"
	}
	if t := app.tab(); t != nil && t.errMsg != "" {
		return firstLine("Error: " + t.errMsg)
	}
	if app.errMsg != "" {
		return firstLine(app.errMsg)
	}
	return firstLine(app.status)
}
