package main

// Guarding the statements that cannot be undone.
//
// Every mature client stops at the statements that drop or empty a table, or
// that update or delete every row: the SQL is one keystroke away from a typo and
// the engine will not ask. DataGrip calls these "potentially destructive
// statements" and asks before running them; the rule here is the same, and it is
// deliberately narrow — a DELETE or UPDATE with a WHERE clause is ordinary work
// and must not nag:
//
//	DROP TABLE t           ask
//	TRUNCATE t             ask
//	DELETE FROM t          ask (no WHERE: every row)
//	UPDATE t SET x = 1     ask (no WHERE: every row)
//	DELETE FROM t WHERE id = 1   run
//	SELECT ...             run
//
// The question is asked once per run, shows the statement, and the confirmation
// runs exactly what was shown.

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

// confirmRun is the modal that stands between a destructive statement and the
// server: the statement as written, why it is being questioned, and the run that
// goes ahead when the user says so.
type confirmRun struct {
	open bool
	why  string
	sql  string
	// again runs the same script without the question, so the confirmation is
	// not asked twice for one press.
	again func()
}

// riskyStatement answers why a script is dangerous, or "" when it is not. It
// looks at the first keyword of each statement; a statement it cannot classify
// is not questioned (the guard must never cry wolf, or it will be clicked
// through).
func riskyStatement(stmts []string) string {
	for _, stmt := range stmts {
		switch firstKeyword(stmt) {
		case "DROP":
			return "This statement drops an object. It cannot be undone."
		case "TRUNCATE":
			return "This statement empties a table without keeping the rows."
		case "DELETE":
			if !hasWhere(stmt) {
				return "This DELETE has no WHERE clause: it deletes every row."
			}
		case "UPDATE":
			if !hasWhere(stmt) {
				return "This UPDATE has no WHERE clause: it updates every row."
			}
		}
	}
	return ""
}

// firstKeyword is a statement's first word, upper-cased: comments, whitespace and
// parentheses are skipped, as the kernel's own read/write classifier does.
func firstKeyword(stmt string) string {
	s := stripLeadingTrivia(stmt)
	end := strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '_')
	})
	if end < 0 {
		end = len(s)
	}
	return strings.ToUpper(s[:end])
}

// stripLeadingTrivia drops whitespace, -- and /* */ comments and opening
// parentheses, so `/* note */ DROP TABLE t` still reads as a DROP.
func stripLeadingTrivia(s string) string {
	for {
		s = strings.TrimLeft(s, " \t\r\n(")
		switch {
		case strings.HasPrefix(s, "--"):
			if i := strings.IndexByte(s, '\n'); i >= 0 {
				s = s[i+1:]
				continue
			}
			return ""
		case strings.HasPrefix(s, "/*"):
			if i := strings.Index(s, "*/"); i >= 0 {
				s = s[i+2:]
				continue
			}
			return ""
		}
		return s
	}
}

// hasWhere reports whether a statement has a WHERE clause outside string
// literals and comments: a DELETE whose only "where" is inside a string is still
// deleting every row.
func hasWhere(stmt string) bool {
	// The literals are blanked out by the completion machinery's own helper: a
	// DELETE whose only "where" is inside a string is still deleting every row.
	upper := strings.ToUpper(stripSQLLiterals(stmt))
	// Word-boundary match, so a column named "somewhere" does not count.
	for i := 0; i+5 <= len(upper); i++ {
		if upper[i:i+5] != "WHERE" {
			continue
		}
		before := i == 0 || !isWordByte(upper[i-1])
		after := i+5 == len(upper) || !isWordByte(upper[i+5])
		if before && after {
			return true
		}
	}
	return false
}

// confirmView draws the question. Running it goes ahead with the statement that
// was shown, which is what the user agreed to; Cancel drops it.
func (app *App) confirmView(c *ui.Context) {
	cr := &app.confirm
	if !cr.open {
		return
	}
	t := c.Theme()
	ui.Modal(c, &cr.open, func() {
		ui.Column(c).Gap(10).Padding(12).Width(620).Height(320).
			Background(t.Surface).Radius(10).Border(1, t.Border).
			Children(func() {
				ui.Text(c, "Run this statement?").FontSize(13).Bold()
				ui.Text(c, cr.why).TextColor(t.Danger).FontSize(11).Wrap()
				ui.Scroll(c).Grow(1).FillWidth().Children(func() {
					ui.Text(c, cr.sql).FontSize(11).Wrap()
				})
				ui.Row(c).Gap(8).Children(func() {
					ui.Spacer(c)
					if ui.Button(c, "Cancel").Clicked() {
						cr.open = false
						app.status = "Cancelled"
						app.invalidate()
					}
					// The destructive action is not the primary button: the
					// enter key must not be the way into it.
					if ui.Button(c, "Run anyway").Clicked() {
						again := cr.again
						cr.open = false
						app.invalidate()
						if again != nil {
							again()
						}
					}
				})
			})
	})
}
