//go:build oracle

package main

// oracleEnabled reports whether this binary can speak Oracle (the driver is
// compiled in). The connection form reads it to offer the Kind choice.
const oracleEnabled = true

// oracleUnavailableNote is never read in this build (the driver is in); it
// exists so db.go compiles unchanged for both variants.
var oracleUnavailableNote string

// kindsExtra is the engine list segment this build adds: Oracle when its
// driver is compiled in, nothing otherwise.
func kindsExtra() []string { return []string{"oracle"} }
