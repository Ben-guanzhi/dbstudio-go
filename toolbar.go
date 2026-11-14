package main

// The toolbar: icon buttons in the DataGrip arrangement — run group, editor,
// transact, tools, and the working copy's row actions. 波 100：图标来自
// internal/icons（内嵌 SVG，currentColor 跟随主题），文字住进 tooltip；Run
// 保持唯一的主按钮（PrimaryButton 的强调色指路），其余图标化省出宽度。

import (
	"dbstudio/internal/icons"
	"github.com/egoist/mygo/ui"
)

// iconButton draws one toolbar icon: the glyph in the theme's text color, the
// action in the tooltip and in the accessible name (Label) — an icon button's
// name comes from the label, not from visible text, which is what the a11y
// contract's interactive-name rule asks for.
func iconButton(c *ui.Context, g icons.Glyph, name, tip string, clicked func()) {
	b := ui.Button(c, "").Label(name).Tooltip(tip).Children(func() {
		icons.Draw(c, g, ui.Color{})
	})
	if b.Clicked() {
		clicked()
	}
}

// toolbar is the action bar: it changes state and never reports it (the status
// bar does that). The groups read left to right as the work goes: connect, run,
// transact, look, then the working copy's own row actions.
func (app *App) toolbar(c *ui.Context) {
	t := c.Theme()
	ui.Toolbar(c, func() {
		ui.Row(c).Gap(4).Wrap().AlignItems(ui.Center).Children(func() {
			// Run: the primary action, with the two narrower runs beside it.
			if ui.PrimaryButton(c, "Run").Tooltip("Run the script (Ctrl+Enter)").Clicked() {
				app.commitOrRun()
			}
			iconButton(c, icons.RunSelection, "Run Selection", "Run only the selected text (Ctrl+Shift+Enter)", app.runSelection)
			iconButton(c, icons.RunToCaret, "Run to Caret", "Run everything up to the caret as one script (Ctrl+Shift+R)", app.runToCaret)
			if app.busy {
				// Stop cancels the running statement's context, which the
				// kernel's QueryContext honors: the driver interrupts it.
				iconButton(c, icons.Stop, "Stop", "Cancel the running query", app.cancelRun)
			}
			ui.Divider(c)
			iconButton(c, icons.Format, "Clear", "Empty the editor", func() { app.edBuf.Set("") })
			ui.Divider(c)
			// The transaction group: the switch, the isolation the next
			// transaction asks for, and the open transaction's two endings.
			if app.open && app.txCapable() {
				if ui.Toggle(c, &app.autocommit, "Autocommit").Changed() {
					app.setAutocommit()
				}
				if !app.autocommit {
					labels := make([]string, len(IsolationLevels))
					for i, l := range IsolationLevels {
						labels[i] = isolationLabel(l)
					}
					level := app.txIsolation
					sel := ui.Select(c, &level, labels).Label("Isolation")
					if sel.Changed() {
						app.txIsolation = level
						app.status = "Isolation: " + isolationLabel(level)
						app.invalidate()
					}
				}
				if app.txActive {
					iconButton(c, icons.Commit, "Commit Tx", "Commit the open transaction", app.commitTx)
					iconButton(c, icons.Rollback, "Rollback Tx", "Roll back the open transaction", app.rollbackTx)
				}
			}
			if app.open {
				app.readOnlyView(c)
			}
			ui.Divider(c)
			// The tools group: what looks at a result rather than producing one.
			iconButton(c, icons.Refresh, "Refresh", "Re-read the result set (F5)", func() {
				if !app.busy && app.result != nil {
					app.refreshCurrent(c)
				}
			})
			iconButton(c, icons.Export, "Export", "Export the result (CSV / JSON / Excel / INSERT)", func() {
				if app.result != nil {
					app.exportResult(exportCSV)
				}
			})
			iconButton(c, icons.Columns, "Columns", "Hide, show and reorder the result's columns", func() {
				if app.result != nil {
					app.openColumns()
				}
			})
			iconButton(c, icons.History, "History", "Statements that ran (Ctrl+H)", app.openHistory)
			iconButton(c, icons.NewConnection, "New Connection", "Open the connection form (Ctrl+N)", app.openForm)
			// The working copy's row actions appear only while the grid is one
			// table's rows.
			if app.edit.active {
				ui.Divider(c)
				iconButton(c, icons.InsertRow, "Insert Row", "Insert a row", app.insertRow)
				rowOK := app.gridSel >= 0 && app.pageStart()+app.gridSel < len(app.edit.rows)
				iconButton(c, icons.DeleteRow, "Delete Row", "Delete the selected row", func() {
					if rowOK {
						app.deleteRow()
					}
				})
				if ui.Button(c, "Commit").Disabled(app.busy || !app.edit.dirty() || app.txActive).
					Tooltip("Apply the working copy's changes").Clicked() {
					app.commit()
				}
				if ui.Button(c, "Rollback").Disabled(app.busy || !app.edit.dirty()).Clicked() {
					app.rollback()
				}
			}
			ui.Spacer(c)
			if app.edit.active {
				ui.Text(c, "Working copy: "+app.edit.table).FontSize(10).
					TextColor(t.TextMuted).SingleLine()
			}
		})
	})
}
