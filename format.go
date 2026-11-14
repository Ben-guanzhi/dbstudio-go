package main

// Formatting the SQL in the editor (P2 #14): the Database → Format SQL action
// (Ctrl+Shift+F) runs the editor's text through the client-side formatter and
// replaces it. The formatter only rearranges whitespace and the case of
// SQL-standard keywords, so a statement that ran before still means the same
// thing after — but is laid out the way a reader wants it. A selection is
// formatted in place, so one clause can be tidied without touching the rest.

import (
	"dbstudio-go/internal/sqlformat"
)

// formatSQL reformats the editor's text: the current selection when one is made,
// else the whole buffer. The result is reported in the status line.
func (app *App) formatSQL() {
	buf := app.edBuf.String()
	sel := app.edState.Selection()

	// Whole-buffer formatting is the common case and the simplest to reason
	// about: replace everything.
	if sel.Anchor == sel.Head {
		app.edBuf.Set(sqlformat.Format(buf))
		app.status = "Formatted"
		app.invalidate()
		return
	}

	// A selection is formatted in place, keeping the surrounding text intact.
	a, b := sel.Anchor, sel.Head
	if a > b {
		a, b = b, a
	}
	if b > len(buf) {
		b = len(buf)
	}
	selText := buf[a:b]
	formatted := sqlformat.Format(selText)
	if formatted == selText {
		app.status = "Formatted"
		app.invalidate()
		return
	}
	app.edBuf.Set(buf[:a] + formatted + buf[b:])
	app.status = "Formatted selection"
	app.invalidate()
}

// formatBuffer reformats the whole editor without a status report, for tests
// that drive the editor directly.
func (app *App) formatBuffer() string {
	return sqlformat.Format(app.edBuf.String())
}
