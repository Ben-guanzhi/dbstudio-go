package main

import _ "embed"

// The product icon, drawn by cmd/icon and embedded here. The same artwork is
// the exe's .ico resource (resources/icon.ico via go-winres) and the README
// asset — one glyph, three places.
//
//go:embed resources/icon.png
var appIconPNG []byte
