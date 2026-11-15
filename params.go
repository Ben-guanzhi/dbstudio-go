package main

// The parameterized query panel (P2 #15).
//
// The kernel's Execute binds arguments — it never escapes them into the text —
// so a statement can be written with the placeholder style the author happens
// to know (?, $1, :name) and run with values filled in a form. What the kernel
// does NOT do is bridge styles: `?` is MySQL/SQLite, `$n` is PostgreSQL, `:n` is
// Oracle/DM, `@pn` is SQL Server. This file extracts the placeholders a piece of
// SQL holds (skipping strings and comments), names them in the order they
// appear, and rewrites them into the target engine's own marker with
// sqltext.Placeholder, so the panel collects values once and the driver binds
// them as natives.

import (
	"strconv"
	"strings"

	"github.com/Ben-guanzhi/dbcore"
	"github.com/Ben-guanzhi/dbcore/sqltext"
	"github.com/egoist/mygo/ui"
)

// paramRef is one placeholder occurrence: its byte range in the SQL, and the
// positional index it takes (named placeholders map to their first appearance).
type paramRef struct {
	start, end int // [start, end) in the original text
	index      int // 0-based position the value binds to
	name       string
}

// paramNames are the labels the panel shows: ":name" for named ones, the
// marker itself for positional ones.
func paramLabel(sqlText string, r paramRef) string {
	if r.name != "" {
		return r.name
	}
	return strconv.Itoa(r.index + 1)
}

// extractParams lists the placeholders in sqlText in the order they bind:
// `?` (positional), `$n` (positional), and `:name` (named, first use fixes its
// index). Quoted strings and comments are skipped, and `::type` casts (PG) are
// not parameters.
func extractParams(sqlText string) []paramRef {
	var refs []paramRef
	byName := map[string]int{}
	n := len(sqlText)
	for i := 0; i < n; {
		c := sqlText[i]
		switch {
		case c == '\'' || c == '"':
			// A string: skip it whole, doubled quotes included.
			quote := c
			j := i + 1
			for j < n {
				if sqlText[j] == quote {
					if j+1 < n && sqlText[j+1] == quote {
						j += 2
						continue
					}
					j++
					break
				}
				j++
			}
			i = j
		case c == '-' && i+1 < n && sqlText[i+1] == '-':
			j := strings.IndexByte(sqlText[i:], '\n')
			if j < 0 {
				i = n
			} else {
				i += j + 1
			}
		case c == '/' && i+1 < n && sqlText[i+1] == '*':
			j := strings.Index(sqlText[i+2:], "*/")
			if j < 0 {
				i = n
			} else {
				i += 2 + j + 2
			}
		case c == '?' && (i == 0 || sqlText[i-1] != ':' && sqlText[i-1] != '$'):
			refs = append(refs, paramRef{start: i, end: i + 1, index: len(refs)})
			i++
		case c == '$' && i+1 < n && isDigit(sqlText[i+1]):
			j := i + 1
			for j < n && isDigit(sqlText[j]) {
				j++
			}
			num, _ := strconv.Atoi(sqlText[i+1 : j])
			// $n is 1-based and self-indexing: $2 always binds the 2nd value.
			idx := num - 1
			if idx < 0 {
				idx = len(refs)
			}
			refs = append(refs, paramRef{start: i, end: j, index: idx})
			i = j
		case c == ':' && i+1 < n && (isIdentStart(sqlText[i+1]) || sqlText[i+1] == ':'):
			// ::type is a cast, not a parameter: step over both colons.
			if sqlText[i+1] == ':' {
				i += 2
				continue
			}
			j := i + 1
			for j < n && isIdentChar(sqlText[j]) {
				j++
			}
			name := sqlText[i+1 : j]
			idx, seen := byName[name]
			if !seen {
				idx = len(refs)
				byName[name] = idx
			}
			refs = append(refs, paramRef{start: i, end: j, index: idx, name: name})
			i = j
		default:
			i++
		}
	}
	return refs
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }
func isIdentStart(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b == '_'
}
func isIdentChar(b byte) bool { return isIdentStart(b) || isDigit(b) }

// rewriteParams replaces every placeholder with the target engine's own marker,
// in binding order. A statement already written in the engine's style comes
// back unchanged except for the whitespace, which is left alone.
func rewriteParams(sqlText string, refs []paramRef, dbType string) (string, error) {
	if len(refs) == 0 {
		return sqlText, nil
	}
	var b strings.Builder
	prev := 0
	for _, r := range refs {
		b.WriteString(sqlText[prev:r.start])
		b.WriteString(sqltext.Placeholder(dbType, r.index+1))
		prev = r.end
	}
	b.WriteString(sqlText[prev:])
	return b.String(), nil
}

// The panel's state: the statement it parameterizes, its parameters in binding
// order, and the text boxes the values are typed into.
type paramPanel struct {
	open    bool
	sql     string
	refs    []paramRef
	labels  []string
	values  []string
	rewrite string // the rewritten statement, built when the panel opens
	err     string
}

// openParams extracts the editor's placeholders and opens the panel. A
// statement with none still opens — the panel then says so and offers nothing
// to fill, which is honest about what Run would do.
func (app *App) openParams() {
	if !app.open || app.db == nil || app.db.SQL == nil {
		app.errMsg = "Not connected"
		app.status = "Not connected"
		app.invalidate()
		return
	}
	sql := app.explainStatement() // same picking as Run: selection, else caret's statement
	if sql == "" {
		app.errMsg = "Nothing to run"
		app.status = "Nothing to run"
		app.invalidate()
		return
	}
	refs := extractParams(sql)
	engine := app.engine()

	// Rewrite into the engine's own markers so the driver binds them natively.
	rewritten, err := rewriteParams(sql, refs, engine)
	if err != nil {
		app.errMsg = "Parameters: " + err.Error()
		app.status = "Parameters failed"
		app.invalidate()
		return
	}

	// The panel's slots: one per binding position.
	positions := 0
	for _, r := range refs {
		if r.index >= positions {
			positions = r.index + 1
		}
	}
	labels := make([]string, positions)
	values := make([]string, positions)
	for _, r := range refs {
		if labels[r.index] == "" {
			labels[r.index] = paramLabel(sql, r)
		}
	}
	for i := range labels {
		if labels[i] == "" {
			labels[i] = strconv.Itoa(i + 1)
		}
	}
	app.params = paramPanel{open: true, sql: sql, refs: refs,
		labels: labels, values: values, rewrite: rewritten}
	app.invalidate()
}

// runParams binds the panel's values and runs the rewritten statement, showing
// the result the way a plain run does.
func (app *App) runParams() {
	p := &app.params
	args := make([]dbcore.Value, len(p.values))
	for i, s := range p.values {
		args[i] = dbcore.ValueOf(paramValue(s))
	}
	p.open = false
	app.invalidate()
	// The rewritten statement carries the engine's own markers, so the driver
	// binds each value natively; the run itself is the ordinary pipeline.
	app.runBoundSQL(p.rewrite, args)
}

// paramValue interprets a text box the way a user means it: empty binds NULL,
// a number-looking text binds a number, anything else binds text. A number
// with a leading zero ("007") stays text — it is a code the user typed, not an
// arithmetic value to silently narrow.
func paramValue(s string) interface{} {
	t := strings.TrimSpace(s)
	if t == "" {
		return nil
	}
	if !leadingZero(t) {
		if i, err := strconv.ParseInt(t, 10, 64); err == nil {
			return i
		}
		if f, err := strconv.ParseFloat(t, 64); err == nil {
			return f
		}
	}
	return t
}

// leadingZero reports whether t looks like a number whose integer part begins
// with a lone zero ("007", "0.5" is fine, "0" is fine, "-07" is not).
func leadingZero(t string) bool {
	s := strings.TrimPrefix(t, "-")
	if len(s) < 2 || s[0] != '0' {
		return false
	}
	return s[1] >= '0' && s[1] <= '9'
}

// paramsView draws the panel's modal: one field per parameter and Run.
func (app *App) paramsView(c *ui.Context) {
	p := &app.params
	if !p.open {
		return
	}
	t := c.Theme()
	ui.Modal(c, &p.open, func() {
		ui.Column(c).Gap(10).Padding(12).Width(460).
			Background(t.Surface).Radius(10).Border(1, t.Border).
			Children(func() {
				ui.Text(c, "Parameters").FontSize(13).Bold()
				if len(p.refs) == 0 {
					ui.Text(c, "The statement has no parameters (? $n :name); Run executes it as it is.").TextColor(t.TextMuted).Wrap()
				}
				for i := range p.values {
					idx := i
					ui.Field(c, p.labels[idx], func() {
						ui.TextInput(c, &p.values[idx])
					})
				}
				if p.err != "" {
					ui.Text(c, p.err).TextColor(t.Danger).Wrap()
				}
				ui.Row(c).Gap(8).Children(func() {
					ui.Spacer(c)
					if ui.Button(c, "Cancel").Clicked() {
						p.open = false
					}
					if ui.PrimaryButton(c, "Run").Clicked() {
						app.runParams()
					}
				})
			})
	})
}
