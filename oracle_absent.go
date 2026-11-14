//go:build !oracle

package main

// oracleEnabled is false in the default build: the Oracle driver is opt-in
// (波 100 体积整改), so the form does not offer it as a Kind. A saved Oracle
// connection still shows in the list; connecting fails with a clear note.

import "fmt"

const oracleEnabled = false

// oracleUnavailableNote explains why an Oracle connection cannot open in this
// build (db.go checks it when the kind has no registered driver).
var oracleUnavailableNote string

// kindsExtra is the engine list segment this build adds: nothing — the
// ~13MB Oracle driver is opt-in.
func kindsExtra() []string { return nil }

func init() {
	// Replace the kernel's "driver not registered" error with one that says
	// how to get Oracle back, before the first connect attempt can hit it.
	oracleUnavailableNote = fmt.Sprintf(
		"this build has no Oracle driver — rebuild with -tags oracle (adds ~13MB), or use the full build from the releases page")
}
