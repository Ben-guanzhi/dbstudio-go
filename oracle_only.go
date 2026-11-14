//go:build oracle

package main

// The Oracle driver is opt-in (波 100 体积整改): go-ora alone adds ~13MB to
// the binary, which every non-Oracle user would pay for. `go build -tags
// oracle` brings it back; the released binaries ship without it.

import (
	_ "github.com/dbcore/dbcore/oracle" // registers the Oracle factory
)
