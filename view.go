package main

// The panes: how one frame of the window is drawn. view assembles the four
// regions — toolbar, sidebar, editor, results — and the functions below draw
// each; every one of them reads App and touches nothing else. The modals live
// beside the features that own them (form.go, ddl.go, celledit.go, params.go,
// compare.go, pager.go), and the state they draw is in app.go.

import (
	"fmt"
	"strings"

	"github.com/Ben-guanzhi/dbcore"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// view draws one frame.
func (app *App) view(c *ui.Context) {
	if c.Shortcut(ui.Cmd, ui.KeyEnter) {
		app.commitOrRun()
	}
	// The console keys a workbench is expected to have: a new console, closing
	// the one on screen, and cycling. Shift+W rather than W, because Ctrl+W is
	// the window's own close on Windows and would take the whole app with it.
	if c.Shortcut(ui.Cmd, ui.KeyT) {
		app.newConsole()
	}
	if c.Shortcut(ui.Cmd|ui.Shift, ui.KeyW) {
		app.closeConsole(app.consolesel)
	}
	if c.Shortcut(ui.Cmd, ui.KeyTab) {
		app.selectConsoleAt((app.consolesel + 1) % len(app.consoles))
	}
	// Ctrl+Z is the one undo gesture (interaction-baseline single-undo-gesture):
	// the editor consumes it first when it has the focus (mygo delivers keys to
	// the focused element before the window sees them), so reaching here means
	// the grid has the focus — undo the working copy's last edit instead of the
	// editor's text. Ctrl+Shift+Z mirrors it onto nothing (the working copy is
	// all-or-nothing) but keeps the gesture pair from doing something unrelated.
	if c.Shortcut(ui.Cmd, ui.KeyZ) {
		app.rollback()
	}
	// F5 re-reads what is on screen: a browse's page, or the statement's result.
	if c.Shortcut(0, ui.KeyF5) {
		if app.result != nil && !app.busy {
			app.refreshCurrent(c)
		}
	}
	// Alt+Left and Alt+Right walk the results pager wherever the focus is;
	// the editor on Windows and Linux leaves those keys to the window.
	app.pagerShortcuts(c)
	app.formView(c)
	app.jumpView(c)
	app.ddlView(c)
	app.cellView(c)
	app.paramsView(c)
	app.compareView(c)
	app.importView(c)
	app.connInfoView(c)
	app.historyView(c)
	app.confirmView(c)
	app.columnsView(c)
	app.aboutView(c)
	app.structureView(c)
	app.pluginsView(c)

	// 波 99：紧凑密度。默认主题 Spacing=4，表格一行吃掉 Space(8)=32px 的呼吸
	// ——数据客户端要的是屏上多几行，不是多几像素的留白（DataGrip 的 comfort/
	// compact 之分）。跟随桌面的字段（accent/对比度/字号）由 WithPreferences
	// 接管，Spacing 它不碰，安全。每帧设置是幂等的：同一主题值，引擎去重。
	if c.Theme().Spacing >= 4 {
		dense := *c.Theme()
		dense.Spacing = 3
		c.SetTheme(dense.WithPreferences(c.Preferences()))
	}

	ui.Column(c).Fill().Children(func() {
		// The connection's colour across the top of the window: a statement
		// typed into the wrong window is the expensive mistake, and a band of
		// colour is seen before any text is read.
		app.colorBand(c)
		app.toolbar(c)
		// Grow, not Fill: the split has siblings above and below it, and Fill
		// means "as large as the parent", which would overlap them.
		ui.Split(c, &app.splitSide, func() {
			app.navigator(c)
		}, func() {
			ui.SplitVertical(c, &app.splitEditor, func() {
				app.editorPane(c)
			}, func() {
				app.resultsPane(c)
			}).Grow(1)
		}).Grow(1).SplitReset(260)
		// The window-wide state line: connection, schema, engine, transaction,
		// row count, elapsed, message. DBeaver and DataGrip put it under
		// everything — at the bottom, where a status line belongs. It sat under
		// the toolbar while the tree painted past the bottom of the window
		// (WORKSPACE §73-§76); the tree is bounded now (§80), so the bottom of
		// the window is inside the client area and the bar goes there.
		app.statusBar(c)
	})
	// The grid's header may have ordered the rows differently in this frame, and
	// its filter bar may have changed the rows they are kept by; the pane is
	// built, so answer both now.
	app.finishFilterBar()
	app.applyGrid()
	// A column the user dragged wider is a fact worth keeping: the session
	// records it, so the next launch opens with the arrangement it was left in.
	app.noteColumnWidths()
	// The reader reached the end of what is loaded: fetch the next segment of the
	// table, so a browse reads like a document rather than a hundred rows.
	app.maybeLoadMore()
}

// navigator is the left side: the connections, then the objects — the
// Database Navigator of DBeaver, with the schema scope and object search of
// DataGrip on top of the tree.
//
// The history used to live here as a fixed 200px strip; it is a modal now
// (toolbar → History), which is what a tool window is for, and the tree gets
// the room back — on a server with thirty schemas that room is the whole point.
func (app *App) navigator(c *ui.Context) {
	t := c.Theme()
	// The canvas height, to bound the tree's viewport. The tree's Outline is a
	// virtualising scroll and the layout's Grow gives it a viewport taller than
	// the room it has (WORKSPACE §73-§76: rows were laid out past the window).
	// Capping the tree column keeps its rows inside the pane; the value leaves
	// room for the header, connection list and schema selector above it, and
	// for the state line at the bottom of the window, which the tree must not
	// paint into (WORKSPACE §83).
	_, canvasH := c.Size()
	ui.Column(c).Fill().Background(t.Surface).Children(func() {
		// 波 98：标题短一点（DataGrip 就叫 Navigator）——260px 的默认侧栏宽度
		// 下「Database Navigator」+三个按钮会裁掉标题头。波 99：删除连接收进
		// 头部按钮行（🗑），它不该占侧栏一整行。
		panelHeader(c, t, "Objects", func() {
			// Short words rather than bare glyphs: the panel is 260px wide, and a
			// glyph alone is a tooltip away from being understood.
			if ui.Button(c, "New").FontSize(10).Tooltip("New connection").Clicked() {
				app.openForm()
			}
			if ui.Button(c, "✎").FontSize(10).Tooltip("Edit the selected connection").Clicked() {
				app.openForm()
			}
			if ui.Button(c, "🗑").FontSize(10).Disabled(app.connName == "").
				Tooltip("Delete the selected connection").Clicked() {
				app.deleteConn(c)
			}
			if ui.Button(c, "Refresh").FontSize(10).Disabled(!app.open).
				Tooltip("Refresh the schema tree (Ctrl+F5)").Clicked() {
				app.refreshSchema()
			}
		})
		// The connections: choosing one connects it, which is what a
		// navigator's connection list is for.
		ui.Column(c).Gap(2).Padding(6).Children(func() {
			prev := app.connName
			ui.Sidebar(c, &app.connName, func() {
				if app.store == nil {
					return
				}
				for _, conn := range app.store.List() {
					item := ui.SidebarItem(c, conn.Name, nil, connectionMark(conn))
					if ink, ok := colorInk(conn.Color); ok {
						item.TextColor(ink)
					}
				}
			})
			if app.connName != "" && app.connName != prev {
				app.connect(app.connName)
			}
		})
		ui.Divider(c)
		// Where the window is looking, then how to find something in it.
		ui.Column(c).Gap(4).Padding(6).Children(func() {
			if !app.open {
				// 首次打开：没有已保存连接时，一句"pick a connection above"
				// 指不到任何东西——DBeaver/TablePlus/Beekeeper 都在这个位置放
				// 主操作。有连接但没选时才用那句提示。
				if app.connCount() == 0 {
					app.noConnections(c, t)
					return
				}
				emptyState(c, t, "Not connected — pick a connection above")
				return
			}
			app.schemaSelector(c)
			app.searchBox(c)
		})
		if app.open {
			if app.search.active {
				// Grow, not Fill: the navigator has a header and a connection
				// list above this column, and Fill would claim the whole pane.
				ui.Column(c).Grow(1).Gap(2).Children(func() {
					app.searchResults(c, t)
				})
			} else {
				ui.Column(c).Grow(1).Gap(2).Padding(0, 6, 6, 6).MaxHeight(canvasH - 262).Children(func() {
					app.tree.List.Selected = &app.treeSel
					ui.Outline(c, &app.tree, app.treeRoots(), app.treeChildren, func(n treeNode) {
						app.treeRow(c, n)
					}).Grow(1).KeepScrollbars(true)
					if app.treeSel >= 0 && app.treeSel < app.tree.Rows() {
						app.treeSelect(app.tree.Item(app.treeSel))
					}
					if app.treeErr != "" {
						ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
							ui.Text(c, app.treeErr).TextColor(t.Danger).FontSize(10).Wrap().Grow(1)
							if ui.Button(c, "Retry").FontSize(10).Clicked() {
								if app.treeErrRetry != nil {
									app.treeErrRetry()
								}
								app.clearTreeErr()
								app.invalidate()
							}
						})
					}
				})
			}
		}
	})
}

func (app *App) editorPane(c *ui.Context) {
	ui.Column(c).Fill().Children(func() {
		// The consoles' tab bar, then the shown console's editor. 波 99：单独的
		// 「Editor」标签行撤掉——页签行的右端已报告 console 数，连接名（防误
		// 操作信号）也并到那里；省出的一行给 SQL。
		app.consolesView(c)
		editor.View(c, app.edState).Grow(1).FillWidth()
	})
}

// resultsPane is the workbench's lower half: the statements' tabs, the pane's
// own toolbar, and a body that is the grid or the messages — the arrangement
// DataGrip's result area uses, so the messages cost nothing until looked at.
func (app *App) resultsPane(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().Children(func() {
		app.resultsTabs(c)
		// 波 99：Results 面板头整行撤掉——Data 视图下它没有内容可画（Refresh
		// 在工具条、行数在 pager 条），是纯占行；Messages 的 Clear 与 busy 的
		// Spinner 挪进页签行右端。省出的一行给数据。
		ui.Row(c).Gap(2).Padding(2, 6).AlignItems(ui.Center).Children(func() {
			sel := app.resView
			msgs := len(app.messageLines(app.tab()))
			labels := []string{"Data", "Messages"}
			if app.tabResult() != nil && len(app.tabResult().Rows) > 0 && app.chartNumericColExists() {
				labels = append(labels, "Chart")
			}
			if msgs > 0 {
				labels[1] = fmt.Sprintf("Messages (%d)", msgs)
			}
			tabs := ui.Tabs(c, &sel, labels...)
			if tabs.Changed() && sel != app.resView {
				app.resView = sel
			}
			ui.Spacer(c)
			if app.busy {
				ui.Spinner(c)
			}
			if app.resView == 1 {
				if ui.Button(c, "Clear").FontSize(10).Clicked() {
					app.clearMessages()
				}
			}
		})
		if app.resView == 1 {
			app.messageBody(c, t)
			return
		}
		if app.resView == 2 {
			app.chartElement(c, t)
			return
		}
		// The filter bar names the rows the grid is kept by, so it stays up while
		// a page is being read: the bar would otherwise vanish for the moment the
		// fetch starts. The grid itself waits for the rows.
		res := app.tabResult()
		if app.resultErr() != "" {
			ui.Text(c, app.resultErr()).TextColor(t.Danger).Padding(8).Wrap()
		} else if res == nil {
			app.resultsHint(c, t)
		} else {
			app.filterBar(c, t)
			if app.result != nil {
				app.renderTable(c, t)
				app.pagerBar(c)
			} else {
				emptyState(c, t, "Loading…")
			}
		}
	})
}

// resultsTabs draws the tab bar and moves the view to the tab the user chose.
func (app *App) resultsTabs(c *ui.Context) {
	if len(app.tabs) == 0 {
		return
	}
	sel := app.tabSel
	tabs := ui.Tabs(c, &sel, app.tabLabels()...)
	if tabs.Changed() && sel != app.tabSel {
		app.setTab(sel)
	}
}

// resultErr is the message the results pane shows: the tab on screen's own
// error when it has one, and the app-wide message otherwise.
func (app *App) resultErr() string {
	if t := app.tab(); t != nil && t.errMsg != "" {
		return t.errMsg
	}
	return app.errMsg
}

// messageKind is the role a line of the results pane's report plays, so each
// can be inked the way DataGrip inks its console: the statement muted, the
// summary bold, an error in danger.
type messageKind int

const (
	msgStatement messageKind = iota
	msgStatus
	msgDetail
	msgError
)

// messageLine is one entry of the results pane's report.
type messageLine struct {
	text string
	kind messageKind
}

// messageLines turns the tab on screen into the report the message panel shows:
// the statement, what it returned, how long it took and any error in full. With
// no tab the app-wide message stands in for it, so a connection failure has
// somewhere to be read even though it produced no grid.
func (app *App) messageLines(tb *resTab) []messageLine {
	var out []messageLine
	sql := ""
	if tb != nil {
		sql = tb.sql
	}
	if sql != "" {
		out = append(out, messageLine{text: sql, kind: msgStatement})
	}
	if tb != nil {
		if tb.errMsg != "" {
			out = append(out, messageLine{text: tb.errMsg, kind: msgError})
		} else if tb.status != "" {
			out = append(out, messageLine{text: tb.status, kind: msgStatus})
		}
		if r := tb.result; r != nil {
			switch {
			case r.AffectedRows > 0:
				out = append(out, messageLine{text: fmt.Sprintf("%d row(s) affected", r.AffectedRows), kind: msgDetail})
				if r.LastInsertID > 0 {
					out = append(out, messageLine{text: fmt.Sprintf("Last insert id: %d", r.LastInsertID), kind: msgDetail})
				}
			case r.LastInsertID > 0:
				out = append(out, messageLine{text: fmt.Sprintf("Last insert id: %d", r.LastInsertID), kind: msgDetail})
			}
			if r.Truncated {
				shown := len(r.Rows)
				if r.TotalRows != nil {
					out = append(out, messageLine{text: fmt.Sprintf("%d row(s) of an estimated %d, truncated", shown, *r.TotalRows), kind: msgDetail})
				} else {
					out = append(out, messageLine{text: fmt.Sprintf("%d row(s), truncated", shown), kind: msgDetail})
				}
			}
		}
	}
	if len(out) == 0 {
		if e := app.errMsg; e != "" {
			out = append(out, messageLine{text: e, kind: msgError})
		} else if s := app.status; s != "" {
			out = append(out, messageLine{text: s, kind: msgStatus})
		}
	}
	return out
}

// messagePanel is the results pane's report, DataGrip's "Messages": the
// statement that produced the tab on screen, what it returned and any error in
// full. It sits below the grid so the grid and its report stay together.
// messageBody is the Messages tab's body: the statement's report, or its empty
// state. The tab strip above it names the pane and the Clear lives in the
// results header, so this draws only what there is to read.
func (app *App) messageBody(c *ui.Context, t *ui.Theme) {
	lines := app.messageLines(app.tab())
	if len(lines) == 0 {
		emptyState(c, t, "Nothing reported — run a statement to see its rows, time and errors")
		return
	}
	ui.Scroll(c).Grow(1).FillWidth().Children(func() {
		ui.Column(c).Gap(4).Padding(6, 8).Children(func() {
			for _, l := range lines {
				label, ink := l.text, t.TextMuted
				switch l.kind {
				case msgStatement:
					label = "Statement  " + l.text
					ink = t.Text
				case msgStatus:
					label = l.text
					ink = t.Text
				case msgDetail:
					label = "  " + l.text
				case msgError:
					label = "Error  " + l.text
					ink = t.Danger
				}
				if l.kind == msgStatement || l.kind == msgError {
					ui.Text(c, label).TextColor(ink).Wrap()
				} else {
					ui.Text(c, label).TextColor(ink).SingleLine()
				}
			}
		})
	})
}

func (app *App) renderTable(c *ui.Context, t *ui.Theme) {
	res := app.result
	e := &app.edit
	editing := e.active && len(e.rows) > 0

	// The grid draws the window of rows the pager points at; row and start
	// are absolute, so a cell looks its row up in the full set.
	start := app.pageStart()
	end := app.pageEnd()
	n := end - start

	// The columns the tab shows, in the order it shows them: a hidden column is
	// left out, and the visible ones keep the tab's arrangement. The mapping
	// from a drawn column back to the result's own column is what the cell
	// builder, the widths and the header menu work in.
	tab := app.tab()
	visible := app.visibleColumns(tab, res)
	columns := make([]ui.TableColumn, 0, len(visible)+1)
	columns = append(columns, ui.TableColumn{Title: "#", RowHeader: true, Fixed: true, Width: 44})
	// A dirty working copy is not re-read to sort, so its headers are not
	// offered the order until it is committed or rolled back.
	sortable := !(app.pager.server && app.edit.active && app.edit.dirty())
	for _, i := range visible {
		col := res.Columns[i]
		// A column is as wide as its widest of a header and a few sample cells,
		// clamped: letting every column share the pane equally crushed long
		// names and values into unreadable slivers on a wide result. Width is a
		// starting point — the user still drags the header's edge, and double
		// clicking it fits the column to its cells.
		columns = append(columns, ui.TableColumn{
			Title:    col.Name,
			ID:       col.Name,
			Sortable: sortable,
			// A frozen column is held: the toolkit cannot scroll it in place, so
			// freezing locks the order and the width instead.
			Fixed:    tab != nil && tab.frozen(col.Name),
			Width:    app.columnWidth(res, i, start, end),
			MinWidth: 88,
			MaxWidth: 560,
			// Numbers right-aligned, as every spreadsheet and every mature
			// client does: a column of figures lines up on the decimal point and
			// becomes comparable at a glance.
			Align: alignOf(col.Type),
		})
	}
	// The header menu works on the drawn column, so it needs the same mapping.
	app.grid.HeaderMenu = func(m *ui.Menu, col int) { app.headerMenu(m, t2col(visible, col)) }
	// The header's tooltip says what the column is: its type, nullability, key,
	// default and comment, which is what decides whether a literal needs quotes.
	app.grid.HeaderCell = func(cell ui.Element, col int) { app.headerInfo(cell, res, visible, col) }

	// The grid shows the working copy's rows when it is editing, the result's
	// otherwise.
	if editing {
		// The block of cells the keys and clicks point at, spreadsheet
		// fashion, and the cell in it is the one that can be edited.
		app.grid.CellsStyle = ui.CellsStyle{
			Cell:     t.Accent,
			CellText: t.AccentText,
			Row:      t.Accent.Alpha(0.12),
			Stripe:   t.Text.Alpha(0.04),
		}
	}

	// The layout draws in the tab's order; a header drag writes back into it, so
	// the two ways of arranging a result (drag, and the Columns dialog) end in
	// the same place.
	var orderBefore []string
	if tab != nil {
		orderBefore = tab.colOrder
	}
	app.grid.Columns.Order = app.columnOrder(tab, res)
	app.lastDrawnRow = start
	table := ui.Table(c, &app.grid, columns, n, func(row, col int) {
		if start+row > app.lastDrawnRow {
			app.lastDrawnRow = start + row
		}
		if col == 0 {
			app.rowMarker(c, t, start+row)
			return
		}
		d := t2col(visible, col)
		if d < 0 {
			return
		}
		if editing {
			r := e.rows[start+row]
			if d >= len(r.strs) {
				return
			}
			if r.state == rowDeleted {
				// A deleted row stays up, struck through, until the commit
				// takes it out of the table.
				ui.Text(c, cellText(r, d)).Strikethrough().TextColor(t.TextMuted).SingleLine()
				return
			}
			// Only the cell the cell cursor rests on is editable; the framework
			// names it and has placed it by the time the cells are built.
			if app.grid.CellSelect && d < len(res.Columns) &&
				app.grid.Cells.Cursor != res.Columns[d].Name {
				ui.Text(c, cellText(r, d)).SingleLine()
				return
			}
			// The cell the cursor rests on is editable; the framework starts it
			// on F2 or a click held, as it does a rename.
			if ui.EditableText(c, &r.strs[d]).Changed() {
				e.markModified(start + row)
			}
			return
		}
		var v dbcore.Value
		if d < len(res.Rows[start+row]) {
			v = res.Rows[start+row][d]
		}
		if v.Kind == dbcore.ValNull {
			ui.Text(c, "NULL").TextColor(t.TextMuted).SingleLine()
			return
		}
		// Double-click a cell of a read-only result to open the value editor: the
		// grid shows one line of the value, the editor shows it in full. The
		// framework leaves the row's own double click alone here.
		cell := ui.Box(c).FillWidth().Children(func() {
			ui.Text(c, v.String()).SingleLine()
		})
		if cell.RowDoubleClicked() {
			app.openCell(row, col)
		}
	})
	// The header's sort clicks are input, and the framework applies them when
	// the widget reports its change; without this the order the click asked for
	// would not reach applySort until a later frame.
	table.Changed()
	table.Grow(1)
	// A header drag reordered the grid: keep that as the tab's order.
	if tab != nil && !slicesEqual(app.grid.Columns.Order, orderBefore) {
		tab.colOrder = append([]string(nil), app.grid.Columns.Order...)
		app.saveColumnLayout(tab)
	}
}

// slicesEqual compares two string slices.
func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// columnWidth is a result column's starting width: the header's text and a
// sample of its cells, widened per rune of the wider one, then clamped. It is a
// starting point, not a measurement — the grid lets the user drag the edge or
// double-click it to fit the cells.
func (app *App) columnWidth(res *dbcore.QueryResult, col, start, end int) float32 {
	// 13px text averages about 7.2px per rune; a cell also needs padding.
	const perRune = 7.4
	const padding = 28

	widest := len([]rune(res.Columns[col].Name))
	// A sample, not the whole result: eight rows is enough to see that a column
	// holds names rather than flags, and keeps this out of the frame's cost.
	rows := 0
	for row := start; row < end && rows < 8; row++ {
		if row >= len(res.Rows) || col >= len(res.Rows[row]) {
			continue
		}
		text := res.Rows[row][col].String()
		if n := len([]rune(text)); n > widest {
			widest = n
		}
		rows++
	}
	width := float32(widest)*perRune + padding
	if width < 88 {
		width = 88
	}
	if width > 560 {
		width = 560
	}
	return width
}
func cellText(r *editRow, d int) string {
	if r.strs[d] == "" && r.origNull[d] {
		return "NULL"
	}
	return r.strs[d]
}

// rowMarker is the spreadsheet's row number, with the mark of what the working
// copy holds for that row.
func (app *App) rowMarker(c *ui.Context, t *ui.Theme, row int) {
	label := fmt.Sprintf("%d", app.pageRowNumber(row))
	if e := &app.edit; e.active && row < len(e.rows) {
		switch e.rows[row].state {
		case rowModified:
			label = "● " + label
		case rowInserted:
			label = "+ " + label
		case rowDeleted:
			label = "– " + label
		}
	}
	ui.Text(c, label).TextColor(t.TextMuted).SingleLine()
}

// firstLine returns the first line of a message, for compact status text.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
