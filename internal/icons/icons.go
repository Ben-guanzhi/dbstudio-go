// Package icons embeds the toolbar's glyphs as inline SVG so the binary
// carries no asset files. Every shape is a single filled path with
// fill="currentColor", which mygo's icon painter resolves against the
// element's TextColor — so ui.Icon(c, icons.Run).TextColor(t.Text) follows
// the theme, and a button's hover state shades it for free.
//
// The set is the toolbar's own verbs (DataGrip's icon row): run, run
// selection, run to caret, stop, format, export, columns, history, new
// connection, and the row actions. All are drawn on a 24×24 grid with
// 2-unit walls, filled — not stroked — so they render identically at 14px
// and 16px.
package icons

import (
	"github.com/egoist/mygo/ui"
)

// glyph wraps one SVG: parsed once, shared by every use.
type Glyph string

func (g Glyph) icon() *ui.SVG { return ui.MustParseSVG([]byte(g)) }

func svg(body string) Glyph {
	return Glyph(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">` +
		`<path fill="currentColor" d="` + body + `"/></svg>`)
}

// Run: the filled play triangle.
var Run = svg("M7 4.5v15c0 .8.9 1.3 1.6.9l12-7.5c.6-.4.6-1.4 0-1.8l-12-7.5c-.7-.4-1.6.1-1.6.9z")

// RunSelection: play with the top-left quarter cut — "part of the script".
var RunSelection = svg("M7 4.5v9.3l7.2-4.5c.6-.4.6-1.4 0-1.8L8.6 3.6C7.9 3.2 7 3.7 7 4.5zM7 16.8v2.7c0 .8.9 1.3 1.6.9l5.9-3.6H7z")

// RunToCaret: play with a caret line at its left — "up to here".
var RunToCaret = svg("M3.5 4.5v15c0 .8.9 1.3 1.6.9l10-6.2c.6-.4.6-1.4 0-1.8l-10-6.2c-.7-.4-1.6.1-1.6.9zM18.5 4h1c.6 0 1 .4 1 1v14c0 .6-.4 1-1 1h-1c-.6 0-1-.4-1-1V5c0-.6.4-1 1-1z")

// Stop: the square.
var Stop = svg("M6.5 5h11C18.3 5 19 5.7 19 6.5v11c0 .8-.7 1.5-1.5 1.5h-11C5.7 19 5 18.3 5 17.5v-11C5 5.7 5.7 5 6.5 5z")

// Format: the aligned lines with an indent — pretty-printed SQL.
var Format = svg("M4 5h16c.6 0 1 .4 1 1s-.4 1-1 1H4c-.6 0-1-.4-1-1s.4-1 1-1zM8 11h12c.6 0 1 .4 1 1s-.4 1-1 1H8c-.6 0-1-.4-1-1s.4-1 1-1zM4 17h16c.6 0 1 .4 1 1s-.4 1-1 1H4c-.6 0-1-.4-1-1s.4-1 1-1z")

// Export: the tray with an arrow leaving it.
var Export = svg("M12 3.6l4.2 4.2c.4.4.4 1 0 1.4s-1 .4-1.4 0L13 7.4V14c0 .6-.4 1-1 1s-1-.4-1-1V7.4L9.2 9.2c-.4.4-1 .4-1.4 0s-.4-1 0-1.4L12 3.6zM5 13c.6 0 1 .4 1 1v4h12v-4c0-.6.4-1 1-1s1 .4 1 1v5c0 .6-.4 1-1 1H5c-.6 0-1-.4-1-1v-5c0-.6.4-1 1-1z")

// Columns: the table split in three, one column lifted — manage columns.
var Columns = svg("M4 4h16c.6 0 1 .4 1 1v14c0 .6-.4 1-1 1H4c-.6 0-1-.4-1-1V5c0-.6.4-1 1-1zm1 2v2.5h4V6H5zm6 0v2.5h3.5V6H11zm5.5 0v2.5H19V6h-2.5zM5 10.5v7.5h4v-7.5H5zm6 0v7.5h3.5v-7.5H11zm5.5 0v7.5H19v-7.5h-2.5z")

// Refresh: the circular arrows — re-read.
var Refresh = svg("M12 5V2.9c0-.5.6-.7.9-.4l3.5 3.5c.2.2.2.6 0 .8l-3.5 3.5c-.3.3-.9.1-.9-.4V7.5c-3 0-5.5 2.5-5.5 5.5s2.5 5.5 5.5 5.5c1.5 0 2.9-.6 3.9-1.6.4-.4 1-.4 1.4 0s.4 1 0 1.4C15.9 19.8 14 20.5 12 20.5c-4.1 0-7.5-3.4-7.5-7.5S7.9 5.5 12 5.5V5z")

// History: the clock with a rewind arrow — statements that ran.
var History = svg("M12 4c4.4 0 8 3.6 8 8s-3.6 8-8 8-8-3.6-8-8c0-.6.4-1 1-1s1 .4 1 1c0 3.3 2.7 6 6 6s6-2.7 6-6-2.7-6-6-6c-1.6 0-3 .6-4.1 1.6l1.4 1.4c.3.3.1.9-.4.9H4.5c-.3 0-.5-.2-.5-.5V4.9c0-.5.6-.7.9-.4l1.4 1.4C7.8 4.7 9.8 4 12 4zm-.5 4.5c.6 0 1 .4 1 1v2.9l2 2c.4.4.4 1 0 1.4s-1 .4-1.4 0l-2.2-2.2c-.2-.2-.3-.4-.3-.7V9.5c0-.6.4-1 1-1z")

// NewConnection: the plug — make a connection.
var NewConnection = svg("M7.5 3C8.3 3 9 3.7 9 4.5V8h6V4.5C15 3.7 15.7 3 16.5 3s1.5.7 1.5 1.5V8h.5c.8 0 1.5.7 1.5 1.5v3c0 3.1-2.1 5.7-5 6.4v1.6c0 .8-.7 1.5-1.5 1.5h-3c-.8 0-1.5-.7-1.5-1.5v-1.6c-2.9-.7-5-3.3-5-6.4v-3C4 8.7 4.7 8 5.5 8H6V4.5C6 3.7 6.7 3 7.5 3z")

// InsertRow: the table with a plus beneath — add a row.
var InsertRow = svg("M4 4h16c.6 0 1 .4 1 1v9c0 .6-.4 1-1 1H4c-.6 0-1-.4-1-1V5c0-.6.4-1 1-1zm1 2v2h14V6H5zm0 4v3h14v-3H5zm11 8h1.5v1.5H19v2h-1.5V21H16v-1.5h-1.5v-2H16V18zm-11 0h5v2H5v-2z")

// DeleteRow: the table with a minus beneath — drop a row.
var DeleteRow = svg("M4 4h16c.6 0 1 .4 1 1v9c0 .6-.4 1-1 1H4c-.6 0-1-.4-1-1V5c0-.6.4-1 1-1zm1 2v2h14V6H5zm0 4v3h14v-3H5zm9 8h7v2h-7v-2z")

// Commit: the check — apply the working copy.
var Commit = svg("M9.5 16.2L5.3 12c-.4-.4-1-.4-1.4 0s-.4 1 0 1.4l4.9 4.9c.4.4 1 .4 1.4 0L20.1 8.4c.4-.4.4-1 0-1.4s-1-.4-1.4 0L9.5 16.2z")

// Rollback: the counter-clockwise arrow — undo the working copy.
var Rollback = svg("M12 5V2.9c0-.5.6-.7.9-.4l3.5 3.5c.2.2.2.6 0 .8l-3.5 3.5c-.3.3-.9.1-.9-.4V7.5c-3 0-5.5 2.5-5.5 5.5s2.5 5.5 5.5 5.5c1.5 0 2.9-.6 3.9-1.6.4-.4 1-.4 1.4 0s.4 1 0 1.4C15.9 19.8 14 20.5 12 20.5c-4.1 0-7.5-3.4-7.5-7.5S7.9 5.5 12 5.5V5z")

// Icon returns the parsed SVG, ready for ui.Icon(c, icons.Run.Icon()).
func (g Glyph) Icon() *ui.SVG { return g.icon() }

// Draw places the glyph in c at the toolbar's icon size (15px, between the
// panel's 10px captions and the grid's 13px text). The icon inherits the
// row's text color; pass a non-zero ink to tint it explicitly.
func Draw(c *ui.Context, g Glyph, ink ui.Color) {
	e := ui.Icon(c, g.Icon()).FontSize(15)
	if ink.A > 0 {
		e.TextColor(ink)
	}
}
