package main

// The query history: every statement that ran is kept, newest first, in a file
// of its own beside connections.json. The sidebar lists them; choosing one puts
// its SQL back in the editor, as DataGrip's history does.

import (
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

// historyWidth is how many runes of a statement the sidebar shows.
const historyWidth = 28

// historyEntry is the record a statement that ran leaves behind.
func historyEntry(stmt string, err error, t0 time.Time) HistoryEntry {
	return HistoryEntry{
		SQL:     stmt,
		OK:      err == nil,
		Elapsed: time.Since(t0).Round(time.Millisecond).String(),
		At:      t0,
	}
}

// historyLabel renders one history entry for the sidebar: when it ran, then the
// first line of the statement cut to the sidebar's width. A statement that
// failed is marked, so it can be found again.
func historyLabel(e HistoryEntry) string {
	when := e.At.Format("15:04:05")
	sql := strings.TrimSpace(firstLine(e.SQL))
	if r := []rune(sql); len(r) > historyWidth {
		sql = string(r[:historyWidth]) + "…"
	}
	if !e.OK {
		return when + "  " + sql + "  !"
	}
	return when + "  " + sql
}

// openHistory shows the query history in a modal: a tool window, the way
// DBeaver and DataGrip keep theirs, rather than a strip that takes the tree's
// room for the whole session.
func (app *App) openHistory() {
	app.historyOpen = true
	app.invalidate()
}

// historyView draws the history modal. Its list is the sidebar's: the same
// entries, newest first, with a clear for the whole list; choosing one puts its
// SQL in the editor.
func (app *App) historyView(c *ui.Context) {
	if !app.historyOpen {
		return
	}
	t := c.Theme()
	ui.Modal(c, &app.historyOpen, func() {
		ui.Column(c).Gap(8).Padding(12).Width(680).Height(520).
			Background(t.Surface).Radius(10).Border(1, t.Border).
			Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "History").FontSize(13).Bold()
					ui.Spacer(c)
					if ui.Button(c, "Clear").FontSize(10).Clicked() {
						app.clearHistory()
					}
					if ui.PrimaryButton(c, "Close").Clicked() {
						app.historyOpen = false
					}
				})
				ui.Text(c, "Choosing a statement puts it back in the editor.").
					FontSize(10).TextColor(t.TextMuted).SingleLine()
				ui.Scroll(c).Grow(1).FillWidth().Children(func() {
					app.historySidebar(c, t)
				})
			})
	})
}

// historySidebar draws the query history: the statements that ran, newest
// first, with a clear for the whole list. Choosing one puts its SQL in the
// editor.
func (app *App) historySidebar(c *ui.Context, t *ui.Theme) {
	ui.Text(c, "History").FontSize(11).Bold().TextColor(t.TextMuted)
	ui.Row(c).Gap(4).Children(func() {
		if ui.Button(c, "Clear").Clicked() {
			app.clearHistory()
		}
	})
	if app.store == nil {
		return
	}
	entries := app.store.ListHistory()
	if len(entries) == 0 {
		ui.Text(c, "No statements yet").FontSize(10).TextColor(t.TextMuted).SingleLine()
		return
	}
	prev := app.histID
	ui.Sidebar(c, &app.histID, func() {
		for i, e := range entries {
			item := ui.SidebarItem(c, strconv.Itoa(i), nil, historyLabel(e))
			if !e.OK {
				item.TextColor(t.Danger)
			}
		}
	})
	// Choosing a row is the moment the SQL is loaded: the statement that row
	// names goes back into the editor. A change that comes from the list being
	// rebuilt, as new statements arrive, leaves the SQL alone.
	if app.histID != prev && app.histID != "" {
		if i, err := strconv.Atoi(app.histID); err == nil && i >= 0 && i < len(entries) {
			app.loadHistory(entries[i])
		}
	}
}

// loadHistory puts a history entry's statement in the editor, so it can be run
// again or read whole.
func (app *App) loadHistory(e HistoryEntry) {
	app.edBuf.Set(e.SQL)
	app.status = "Loaded from history"
	app.invalidate()
}

// clearHistory drops every history entry.
func (app *App) clearHistory() {
	if app.store == nil {
		return
	}
	if err := app.store.ClearHistory(); err != nil {
		app.errMsg = "Clearing history failed: " + err.Error()
	} else {
		app.histID = ""
		app.errMsg = ""
	}
	app.invalidate()
}
