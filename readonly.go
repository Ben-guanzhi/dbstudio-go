package main

// Read-only mode: a lock on a connection, not a reminder.
//
// A production connection is worth a switch that refuses to change anything —
// not a dialogue that can be clicked through, but a refusal. It is what
// DataGrip's read-only connection does, and it is the only thing that helps when
// the dangerous keystroke is a paste.
//
// The lock is checked where the work would happen, not where it is typed:
//
//   - a script that would write is refused before it reaches the driver;
//   - the grid never becomes a working copy, so a cell cannot be edited and
//     Commit is never offered;
//   - an import is refused, as are the row actions.
//
// The classification is deliberately conservative: a driver's CALL or EXEC may
// write, so on a locked connection it is refused with the reason (a stored
// procedure that only reads can be run by unlocking). It reads the first keyword
// of each statement, skipping comments, and treats a WITH… statement as a write
// when it carries INSERT/UPDATE/DELETE/MERGE, since a data-modifying CTE is a
// write whatever its first word says.
//
// The lock is per connection and saved with it. The toolbar's switch flips the
// session's lock and writes the choice back to the connection, so a connection
// that was locked stays locked on the next launch — a lock that resets is not a
// lock.

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

// writeKeywords are the first keywords that make a statement a write: DML, DDL,
// privilege changes, and the call forms that may do any of them.
var writeKeywords = map[string]bool{
	"INSERT": true, "UPDATE": true, "DELETE": true, "REPLACE": true, "MERGE": true,
	"CREATE": true, "ALTER": true, "DROP": true, "TRUNCATE": true, "RENAME": true,
	"GRANT": true, "REVOKE": true,
	"CALL": true, "EXEC": true, "EXECUTE": true,
	"COPY": true, "LOAD": true, "VACUUM": true, "ANALYZE": true,
	"ATTACH": true, "DETACH": true,
}

// writeStatement reports the first statement of a script that would change
// something, or "" when the script only reads.
func writeStatement(stmts []string) string {
	for _, stmt := range stmts {
		kw := firstKeyword(stmt)
		if writeKeywords[kw] {
			return stmt
		}
		if kw == "WITH" || kw == "SELECT" {
			// A data-modifying CTE is still a write: WITH x AS (...) DELETE ...
			// has a SELECT's first word and a DELETE's effect.
			if modifyingCTE(stmt) {
				return stmt
			}
		}
	}
	return ""
}

// modifyingCTE reports whether a WITH/SELECT statement carries a data- changing
// keyword outside its literals and comments.
func modifyingCTE(stmt string) bool {
	clean := strings.ToUpper(stripSQLLiterals(stmt))
	for _, kw := range []string{"INSERT", "UPDATE", "DELETE", "MERGE"} {
		if containsWord(clean, kw) {
			return true
		}
	}
	return false
}

// containsWord reports whether s holds kw as a whole word.
func containsWord(s, kw string) bool {
	for i := 0; i+len(kw) <= len(s); i++ {
		if s[i:i+len(kw)] != kw {
			continue
		}
		if i > 0 && isWordByte(s[i-1]) {
			continue
		}
		if i+len(kw) < len(s) && isWordByte(s[i+len(kw)]) {
			continue
		}
		return true
	}
	return false
}

// readOnly reports whether the open connection is locked against writes.
func (app *App) readOnly() bool { return app.open && app.readOnlyOn }

// refuseWrite turns a would-be write away with a status the user can act on: the
// statement, why it was refused, and how to unlock.
func (app *App) refuseWrite(stmt string) {
	app.errMsg = "Read-only connection: " + firstLine(stmt) + " was not run"
	app.status = "Blocked by read-only mode — unlock with the toolbar's lock"
	app.invalidate()
}

// readOnlyView draws the toolbar's lock switch when a connection can be locked:
// a pressed lock means writes are refused.
func (app *App) readOnlyView(c *ui.Context) {
	if !app.open {
		return
	}
	t := c.Theme()
	label := "Writable"
	ink := t.TextMuted
	if app.readOnlyOn {
		label = "Read-only"
		ink = t.Accent
	}
	if ui.Toggle(c, &app.readOnlyOn, label).Changed() {
		app.setReadOnly(app.readOnlyOn)
	}
	_ = ink
}

// setReadOnly turns the lock on or off for this session and saves the choice
// with the connection, so it survives a restart.
func (app *App) setReadOnly(on bool) {
	app.readOnlyOn = on
	conn := app.conn
	conn.ReadOnly = on
	app.conn = conn
	if app.store != nil && conn.Name != "" {
		// The same connection, with the lock's state. Saving the whole record is
		// what keeps the two in step.
		if err := app.store.Upsert(conn); err != nil {
			app.errMsg = "Could not save the read-only setting: " + err.Error()
		}
	}
	if on {
		app.status = "Read-only: statements that write are refused"
	} else {
		app.status = "Writable: this connection can change data"
	}
	app.refreshTitle()
	app.invalidate()
}
