package main

// Schema comparison (P2 #16): two saved connections are collected with the
// kernel's schemadiff, their differences listed, and the migration script the
// kernel renders can be previewed and applied to the target connection.
//
// The kernel does the comparing and the DDL rendering — Collect reads a
// connection's tables/columns/indexes/foreign keys into a Snapshot, Diff
// returns what the target lacks, Script renders the statements in the target's
// own dialect. What stays here is everything with a connection: opening the
// second connection for the comparison, showing the entries, and applying the
// script — one statement at a time, stopping at the first failure, because
// MySQL and Oracle commit DDL implicitly and a transaction around it would only
// pretend to be atomic. An entry the engine cannot express lands in Warnings,
// which are shown rather than silently skipped.

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/Ben-guanzhi/dbcore/schemadiff"
	"github.com/egoist/mygo/ui"
)

// compareState is the comparison modal's state: the two connections it compares
// (by saved name), their schemas, the entries Diff returned, the script Script
// rendered, and how much of an apply ran.
type compareState struct {
	open     bool
	busy     bool
	srcName  string
	dstName  string
	srcSchem string
	dstSchem string
	err      string

	entries   []schemadiff.Entry
	stmts     []schemadiff.Statement
	warnings  []string
	script    string // the statements joined, for preview
	applied   int    // how many statements an apply ran
	applyErr  string // the failure an apply stopped at, if any
	applyDone bool
}

// connNames lists the saved connection names the pickers choose between.
func (app *App) connNames() []string {
	var out []string
	for _, c := range app.store.List() {
		out = append(out, c.Name)
	}
	return out
}

// openCompare opens the comparison modal with the current connection as the
// source, and the next saved one as the target's default.
func (app *App) openCompare() {
	if app.store == nil || len(app.store.List()) == 0 {
		app.errMsg = "No saved connections to compare"
		app.status = "Nothing to compare"
		app.invalidate()
		return
	}
	names := app.connNames()
	c := compareState{open: true, srcName: app.connName, dstName: pick(names, 1)}
	if c.dstName == "" || c.dstName == c.srcName {
		if c.srcName == "" {
			c.srcName = pick(names, 0)
		}
		c.dstName = nextName(names, c.srcName)
	}
	app.compare = c
	app.invalidate()
}

// nextName is the first saved name after name, wrapping, that is not name
// itself.
func nextName(names []string, name string) string {
	if len(names) < 2 {
		return ""
	}
	i := indexOf(names, name)
	if i < 0 {
		i = 0
	}
	return names[(i+1)%len(names)]
}

func indexOf(names []string, name string) int {
	for i, n := range names {
		if n == name {
			return i
		}
	}
	return -1
}

// runCompare collects both connections' schemas and diffs them, off the view
// thread. The two connections are opened for the comparison and closed after
// it: the comparison does not touch the connection the app is browsing with.
func (app *App) runCompare() {
	c := &app.compare
	if c.srcName == "" || c.dstName == "" || c.srcName == c.dstName {
		c.err = "Pick two different connections"
		app.invalidate()
		return
	}
	srcConn, ok := app.store.Get(c.srcName)
	if !ok {
		c.err = "Connection " + c.srcName + " is not saved"
		app.invalidate()
		return
	}
	dstConn, ok := app.store.Get(c.dstName)
	if !ok {
		c.err = "Connection " + c.dstName + " is not saved"
		app.invalidate()
		return
	}
	c.busy = true
	c.err = ""
	c.entries = nil
	c.stmts = nil
	c.warnings = nil
	c.script = ""
	c.applyErr = ""
	c.applyDone = false
	c.applied = 0
	app.invalidate()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		src, err := OpenDB(ctx, srcConn)
		if err != nil {
			app.post(func() {
				c.busy = false
				c.err = "Open source: " + err.Error()
				app.invalidate()
			})
			return
		}
		defer src.Close()
		dst, err := OpenDB(ctx, dstConn)
		if err != nil {
			app.post(func() {
				c.busy = false
				c.err = "Open target: " + err.Error()
				app.invalidate()
			})
			return
		}
		defer dst.Close()

		srcSchema, dstSchema := firstSchema(ctx, src), firstSchema(ctx, dst)
		srcSnap, err := schemadiff.Collect(ctx, src.SQL, srcSchema)
		if err != nil {
			app.post(func() {
				c.busy = false
				c.err = "Collect source: " + err.Error()
				app.invalidate()
			})
			return
		}
		dstSnap, err := schemadiff.Collect(ctx, dst.SQL, dstSchema)
		if err != nil {
			app.post(func() {
				c.busy = false
				c.err = "Collect target: " + err.Error()
				app.invalidate()
			})
			return
		}
		entries := schemadiff.Diff(srcSnap, dstSnap, schemadiff.Options{})
		result := schemadiff.Script(srcSnap, dstSnap, entries)

		var b strings.Builder
		for i, st := range result.Statements {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString(st.SQL)
		}
		app.post(func() {
			c.busy = false
			c.srcSchem, c.dstSchem = srcSchema, dstSchema
			c.entries = entries
			c.stmts = result.Statements
			c.warnings = result.Warnings
			c.script = b.String()
			if len(entries) == 0 {
				c.err = ""
			}
			app.invalidate()
		})
	}()
}

// firstSchema is a connection's first schema, which is where a comparison
// looks by default (SQLite "main", the first database otherwise).
func firstSchema(ctx context.Context, db *DB) string {
	if db == nil || db.SQL == nil {
		return ""
	}
	cached := newCached(db)
	if err := cached.LoadSchemas(ctx); err != nil {
		return ""
	}
	names := cached.SchemaNames()
	if len(names) == 0 {
		return ""
	}
	return names[0]
}

func pick(names []string, i int) string {
	if i < 0 || i >= len(names) {
		return ""
	}
	return names[i]
}

// applyScript runs the migration statements on the target connection, one at
// a time, stopping at the first failure. DDL commits implicitly on MySQL and
// Oracle, so a transaction around the script would only pretend to be atomic:
// the honest semantics are "apply until it breaks", reported as applied/total.
func (app *App) applyScript() {
	c := &app.compare
	if len(c.stmts) == 0 || c.busy {
		return
	}
	dstConn, ok := app.store.Get(c.dstName)
	if !ok {
		c.err = "Connection " + c.dstName + " is not saved"
		app.invalidate()
		return
	}
	c.busy = true
	c.applyErr = ""
	c.applyDone = false
	c.applied = 0
	app.invalidate()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		dst, err := OpenDB(ctx, dstConn)
		if err != nil {
			app.post(func() {
				c.busy = false
				c.err = "Open target: " + err.Error()
				app.invalidate()
			})
			return
		}
		defer dst.Close()

		applied := 0
		var failure string
		for _, st := range c.stmts {
			if _, err := dst.SQL.Execute(ctx, st.SQL); err != nil {
				failure = st.EntryID + ": " + err.Error()
				break
			}
			applied++
		}
		app.post(func() {
			c.busy = false
			c.applied = applied
			c.applyDone = true
			if failure != "" {
				c.applyErr = "Applied " + strconv.Itoa(applied) + " of " +
					strconv.Itoa(len(c.stmts)) + ", then: " + failure
			} else {
				c.applyErr = ""
			}
			app.invalidate()
		})
	}()
}

// compareView draws the comparison modal: the two connection pickers, the
// entries, the script preview, the warnings, and the apply button.
func (app *App) compareView(c *ui.Context) {
	cm := &app.compare
	if !cm.open {
		return
	}
	t := c.Theme()
	names := app.connNames()
	ui.Modal(c, &cm.open, func() {
		ui.Column(c).Gap(10).Padding(12).Width(720).Height(560).
			Background(t.Surface).Radius(10).Border(1, t.Border).
			Children(func() {
				ui.Text(c, "Compare Schemas").FontSize(13).Bold()
				ui.Row(c).Gap(8).Children(func() {
					ui.Field(c, "Source", func() {
						ui.Select(c, &cm.srcName, names)
					})
					ui.Field(c, "Target", func() {
						ui.Select(c, &cm.dstName, names)
					})
					ui.Spacer(c)
					if cm.busy {
						ui.Spinner(c)
					}
					if ui.Button(c, "Diff").Disabled(cm.busy).Clicked() {
						app.runCompare()
					}
				})
				if cm.err != "" {
					ui.Text(c, cm.err).TextColor(t.Danger).Wrap()
				}
				if len(cm.entries) == 0 && cm.script == "" && cm.err == "" && !cm.busy {
					ui.Text(c, "Pick a source and a target, then Diff.").TextColor(t.TextMuted)
				}
				if len(cm.entries) > 0 {
					ui.Text(c, strconv.Itoa(len(cm.entries))+" change(s)").FontSize(12).Bold()
				}
				// The entries: one line each, destructive ones marked.
				if len(cm.entries) > 0 {
					ui.Scroll(c).Grow(1).FillWidth().Children(func() {
						ui.Column(c).Gap(2).Children(func() {
							for _, e := range cm.entries {
								row := string(e.Op) + "  " + e.Table
								if e.Object != "" && e.Object != e.Table {
									row += " · " + e.Object
								}
								if e.Reason != "" {
									row += "  — " + e.Reason
								}
								tx := ui.Text(c, row).SingleLine()
								if e.Destructive {
									tx.TextColor(t.Danger)
								}
							}
						})
					})
				}
				if len(cm.warnings) > 0 {
					ui.Text(c, "Warnings").FontSize(11).Bold().TextColor(t.TextMuted)
					for _, w := range cm.warnings {
						ui.Text(c, w).TextColor(t.TextMuted).Wrap()
					}
				}
				if cm.script != "" {
					ui.Text(c, "Migration script (runs on "+cm.dstName+")").FontSize(11).Bold().TextColor(t.TextMuted)
					ui.Scroll(c).Height(140).FillWidth().Children(func() {
						ui.Column(c).Children(func() {
							ui.Text(c, cm.script).FontSize(11)
						})
					})
				}
				if cm.applyErr != "" {
					ui.Text(c, cm.applyErr).TextColor(t.Danger).Wrap()
				}
				if cm.applyDone && cm.applyErr == "" {
					ui.Text(c, "Applied "+strconv.Itoa(cm.applied)+" statement(s)").TextColor(t.TextMuted)
				}
				ui.Row(c).Gap(8).Children(func() {
					ui.Spacer(c)
					if ui.Button(c, "Cancel").Clicked() {
						cm.open = false
					}
					if ui.PrimaryButton(c, "Apply to Target").Disabled(cm.busy || len(cm.stmts) == 0).Clicked() {
						app.applyScript()
					}
				})
			})
	})
}
