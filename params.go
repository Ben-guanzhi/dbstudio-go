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

// paramRef is the kernel's sqltext.ParamRef (§382 收编：提取/重写纯函数
// 归内核，本文件只剩面板与值收集）。
type paramRef = sqltext.ParamRef

// paramNames are the labels the panel shows: ":name" for named ones, the
// marker itself for positional ones.
func paramLabel(sqlText string, r paramRef) string {
	if r.Name != "" {
		return r.Name
	}
	return strconv.Itoa(r.Index + 1)
}

// extractParams/rewriteParams 委托内核（§382）——语义与向量原样迁移。
func extractParams(sqlText string) []paramRef {
	return sqltext.ExtractParams(sqlText)
}

func rewriteParams(sqlText string, refs []paramRef, dbType string) (string, error) {
	return sqltext.RewritePlaceholders(sqlText, refs, dbType), nil
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
		if r.Index >= positions {
			positions = r.Index + 1
		}
	}
	labels := make([]string, positions)
	values := make([]string, positions)
	for _, r := range refs {
		if labels[r.Index] == "" {
			labels[r.Index] = paramLabel(sql, r)
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
