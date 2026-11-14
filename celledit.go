package main

// The cell value editor.
//
// A grid cell shows one line of a cell. A long value, a BLOB or a multi-line
// text does not fit in one line, so a cell opens an editor of its own, the way
// DataGrip's value editor does. The editor holds the cell's text in full, shows
// a BLOB as hex or as text, and, when the grid is a table's working copy, writes
// the text back into that copy so the row's change is committed with the rest.
// A result the grid shows read-only is shown in the editor alone.

import (
	"strings"

	"github.com/dbcore/dbcore"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"dbstudio-go/internal/sqlcodec"
)

// openCell opens the value editor for one cell: the grid's row and column, so the
// cell looks its row up in the set the grid draws. row is the index within the
// page and col the grid's column, where 0 is the row marker.
func (app *App) openCell(row, col int) {
	res := app.tabResult()
	if res == nil {
		return
	}
	d := col - 1
	if d < 0 || d >= len(res.Columns) {
		return
	}
	full := app.pageStart() + row
	if full < 0 || full >= app.rowCount() {
		return
	}

	e := &app.edit
	blob := false
	var text string
	var orig []byte
	if e.active && e.rows[full] != nil && d < len(e.rows[full].strs) {
		// The grid is a working copy, so the editor shows its cell and writes
		// back to it. A blob's bytes are kept in the result row, as the working
		// copy stores a blob as text: the editor shows the hex of the bytes, or
		// the text itself when the cell does not hold a blob.
		r := e.rows[full]
		if r.kinds[d] == dbcore.ValBlob {
			blob = true
			if b := sqlcodec.DecodeHex(r.strs[d]); b != nil {
				orig = b
				text = sqlcodec.EncodeHex(b)
			} else if isBlobPlaceholder(r.strs[d]) && full < len(res.Rows) && d < len(res.Rows[full]) {
				orig = res.Rows[full][d].B
				text = sqlcodec.EncodeHex(orig)
			} else {
				text = r.strs[d]
			}
		} else {
			text = r.strs[d]
		}
	} else if full < len(res.Rows) {
		v := valueAt(res.Rows[full], d)
		if v.Kind == dbcore.ValBlob {
			blob = true
			orig = v.B
			text = sqlcodec.EncodeHex(v.B)
		} else {
			text = v.String()
		}
	}

	app.cellRow, app.cellCol = full, d
	app.cellBlob = blob
	app.cellHex = blob
	app.cellBytes = orig
	app.cellBuf.Set(text)
	app.cellState.ReadOnly = !e.active
	app.cellOpen = true
	app.cellErr = ""
	app.invalidate()
}

// isBlobPlaceholder reports whether a text is the "<blob N bytes>" the grid
// shows for a BLOB it did not load, rather than a text the user typed.
func isBlobPlaceholder(s string) bool {
	return strings.HasPrefix(s, "<blob ") && strings.HasSuffix(s, " bytes>")
}

// cellView draws the value editor's modal. The editor is read-only when the grid
// is not a working copy; a BLOB cell adds the two ways to show its bytes.
func (app *App) cellView(c *ui.Context) {
	if !app.cellOpen {
		return
	}
	t := c.Theme()
	ui.Modal(c, &app.cellOpen, func() {
		ui.Column(c).Gap(10).Padding(12).Width(620).Height(440).
			Background(t.Surface).Radius(10).Border(1, t.Border).
			Children(func() {
				ui.Row(c).Gap(6).Children(func() {
					ui.Text(c, "Value of "+app.cellColumnName()).FontSize(13).Bold().SingleLine()
					ui.Spacer(c)
					if app.cellBlob {
						if ui.Button(c, "Hex").Clicked() {
							app.cellShowHex(true)
						}
						if ui.Button(c, "Text").Clicked() {
							app.cellShowHex(false)
						}
					}
				})
				if app.cellErr != "" {
					ui.Text(c, app.cellErr).TextColor(t.Danger).Padding(2, 0).Wrap()
				}
				editor.View(c, app.cellState).Grow(1).FillWidth()
				ui.Row(c).Gap(8).Children(func() {
					ui.Spacer(c)
					if ui.Button(c, "Copy").Clicked() {
						mygo.Clipboard.WriteText(app.cellBuf.String())
						app.status = "Copied the cell value"
						app.invalidate()
					}
					if ui.Button(c, "Cancel").Clicked() {
						app.cellOpen = false
					}
					if ui.PrimaryButton(c, "Apply").Disabled(app.cellState.ReadOnly).Clicked() {
						app.applyCell()
					}
				})
			})
	})
}

// cellColumnName is the column the cell being edited sits in, for the editor's
// title.
func (app *App) cellColumnName() string {
	e := &app.edit
	if e.active && app.cellRow >= 0 && app.cellRow < len(e.rows) &&
		e.rows[app.cellRow] != nil && app.cellCol < len(e.rows[app.cellRow].strs) &&
		app.cellCol < len(e.cols) {
		return e.cols[app.cellCol].Name
	}
	res := app.tabResult()
	if res != nil && app.cellCol >= 0 && app.cellCol < len(res.Columns) {
		return res.Columns[app.cellCol].Name
	}
	return "cell"
}

// cellShowHex switches a BLOB cell's editor between its hex text and its bytes
// read as text, and rewrites the buffer to match.
func (app *App) cellShowHex(hexMode bool) {
	app.cellHex = hexMode
	if hexMode {
		app.cellBuf.Set(sqlcodec.EncodeHex(app.cellBytes))
	} else {
		app.cellBuf.Set(string(app.cellBytes))
	}
	app.invalidate()
}

// applyCell writes the editor's text back into the working copy's cell, marking
// its row as modified, and closes the editor. A text that is not valid hex where
// the cell holds a BLOB is reported, as the committed value would not be the
// bytes it says.
func (app *App) applyCell() {
	e := &app.edit
	if !e.active || app.cellRow < 0 || app.cellRow >= len(e.rows) {
		app.cellOpen = false
		return
	}
	r := e.rows[app.cellRow]
	if app.cellCol < 0 || app.cellCol >= len(r.strs) {
		app.cellOpen = false
		return
	}
	text := app.cellBuf.String()
	if app.cellBlob && app.cellHex {
		if sqlcodec.DecodeHex(text) == nil {
			app.cellErr = "The text is not valid hexadecimal"
			app.invalidate()
			return
		}
		// A blob edited as hex is stored as its bytes, which the working copy's
		// text cell sends back as the value's text.
		text = string(sqlcodec.DecodeHex(text))
	}
	r.strs[app.cellCol] = text
	e.markModified(app.cellRow)
	app.cellOpen = false
	app.errMsg = ""
	app.status = "Applied the cell value"
	app.invalidate()
}

// --- pasting and refreshing --------------------------------------------------

// pasteIntoCell reads the clipboard and puts its text into one cell of the
// working copy, the same change the cell editor's Apply makes. It is the cell
// menu's Paste item.
func (app *App) pasteIntoCell(row, col int) {
	app.pasteCellText(row, col, mygo.Clipboard.ReadText())
}

// pasteCellText puts a text into one cell of the working copy, the same change
// the cell editor's Apply makes. A text that is not hexadecimal cannot be put
// into a BLOB cell, which is reported where the cell is shown.
func (app *App) pasteCellText(row, col int, text string) {
	e := &app.edit
	if !e.active {
		app.errMsg = "The result is read-only"
		app.invalidate()
		return
	}
	full := app.pageStart() + row
	d := col - 1
	if full < 0 || full >= len(e.rows) || d < 0 || d >= len(e.rows[full].strs) {
		return
	}
	r := e.rows[full]
	if r.kinds[d] == dbcore.ValBlob {
		if text != "" && sqlcodec.DecodeHex(text) == nil {
			app.errMsg = "The clipboard text is not valid hexadecimal"
			app.invalidate()
			return
		}
		text = string(sqlcodec.DecodeHex(text))
	}
	r.strs[d] = text
	e.markModified(full)
	app.errMsg = ""
	app.status = "Pasted a cell"
	app.invalidate()
}

// refreshCurrent re-reads the result set the grid shows. A table's working copy
// is re-read from the server; a statement is run again. The ask is asked first
// when the working copy has changes, as re-reading it would throw them away.
func (app *App) refreshCurrent(c *ui.Context) {
	if app.busy {
		return
	}
	if app.edit.active && app.edit.dirty() {
		open := true
		switch ui.AlertDialog(c, &open, "Refresh",
			"The working copy has unsaved changes. Discard them and re-read the result?",
			"Cancel", "Discard and refresh") {
		case 1:
			app.rollback()
		default:
			return
		}
	}
	t := app.tab()
	if t == nil {
		return
	}
	if t.kind == tabBrowse {
		app.fetchBrowse(t.node)
		return
	}
	if t.sql != "" {
		app.runSQL(t.sql)
	}
}
