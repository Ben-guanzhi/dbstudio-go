package main

// Connection colours: seeing which server this window is pointed at.
//
// A colour is not decoration here. The expensive mistakes in a SQL client are
// the ones made on the wrong connection — a DROP typed against production
// because the window looked like the staging one. A coloured dot on the
// connection, on the navigator's row and in the status bar answers "which
// server" before the statement is written, which is the only time it helps.
//
// The amber tone is #d97706 (not #f5a524): the lighter amber fails 3:1 on the
// light canvas (2.04), while this one clears both themes (3.19/5.46) — S-04.
// The palette is small and named, not a colour picker: five colours people can
// tell apart, plus none. The keys are what the connection stores, so a saved
// connection keeps its colour across launches and themes.

import (
	"github.com/egoist/mygo/ui"
)

// connColor is one palette entry.
type connColor struct {
	key   string
	label string
	ink   ui.Color
}

// connPalette is the offered colours, in the order the picker shows them. The
// hues are chosen to stay distinguishable on the light and the dark theme, and
// to read as "stop", "caution", "go", "info", "other" in that order.
var connPalette = []connColor{
	{key: "", label: "none", ink: ui.Color{}},
	{key: "red", label: "red (production)", ink: ui.Hex("#e5484d")},
	{key: "amber", label: "amber (staging)", ink: ui.Hex("#d97706")},
	{key: "green", label: "green (local)", ink: ui.Hex("#30a46c")},
	{key: "blue", label: "blue", ink: ui.Hex("#0091ff")},
	{key: "purple", label: "purple", ink: ui.Hex("#8e4ec6")},
}

// connDot is the glyph a coloured connection is marked with. A square, not the
// status bar's circle: the health dot is round, and two round dots meaning
// different things in one window is a puzzle.
const connDot = "■"

// colorInk returns a palette key's colour, and whether it is set.
func colorInk(key string) (ui.Color, bool) {
	for _, c := range connPalette {
		if c.key == key && key != "" {
			return c.ink, true
		}
	}
	return ui.Color{}, false
}

// colorLabel is the palette key's label, for the picker and for messages.
func colorLabel(key string) string {
	for _, c := range connPalette {
		if c.key == key {
			return c.label
		}
	}
	return key
}

// colorLabels are the picker's labels, in palette order.
func colorLabels() []string {
	out := make([]string, len(connPalette))
	for i, c := range connPalette {
		out[i] = c.label
	}
	return out
}

// colorOfLabel turns a picker label back into the key.
func colorOfLabel(label string) string {
	for _, c := range connPalette {
		if c.label == label {
			return c.key
		}
	}
	return ""
}

// connectionMark is a connection's name with its colour and lock, as the
// navigator and the status bar show it: "■ prod 🔒" reads as "this window can
// only read production".
func connectionMark(c Conn) string {
	mark := c.Name
	if _, ok := colorInk(c.Color); ok {
		mark = connDot + " " + mark
	}
	if c.ReadOnly {
		mark = mark + " 🔒"
	}
	return mark
}
