package main

// Executing a plan (P2 #13): EXPLAIN turns the statement under the caret (or the
// selection) into the plan the engine would take to run it, shown in the same
// result grid as a query. The kernel's sqltext.ExplainPlanStructured gives the
// per-engine recipe: SQLite, PostgreSQL and MySQL answer with a single stateless
// EXPLAIN statement whose result set *is* the plan, so the prepared statement
// runs through the ordinary query pipeline. SQL Server and Oracle need the plan
// statement to share one session with a Prepare/Cleanup bracket, which the
// pool-backed Execute cannot offer yet, so those answer a clear message instead
// of silently running the wrong thing.

import (
	"strings"

	"github.com/dbcore/dbcore/sqltext"
)

// explainStatement returns the editor's SQL to explain: the selection when one
// is made, else the statement the caret sits in. It mirrors runSelection and
// runToCaret so the same text is explained as would be run.
func (app *App) explainStatement() string {
	text := app.edBuf.String()
	sel := app.edState.Selection()
	if sel.Anchor != sel.Head {
		a, b := sel.Anchor, sel.Head
		if a > b {
			a, b = b, a
		}
		if b > len(text) {
			b = len(text)
		}
		if s := strings.TrimSpace(text[a:b]); s != "" {
			return s
		}
	}
	pos := app.edState.Cursor()
	if pos > len(text) {
		pos = len(text)
	}
	start, ok := statementStart(text, pos)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text[start:pos])
}

// runExplain builds the EXPLAIN of the statement being explained, and runs it
// through the normal query pipeline so the plan shows in a result tab.
func (app *App) runExplain() {
	if !app.open || app.db == nil || app.db.SQL == nil {
		app.errMsg = "Not connected"
		app.status = "Not connected"
		app.invalidate()
		return
	}
	sql := app.explainStatement()
	if sql == "" {
		app.errMsg = "Nothing to explain"
		app.status = "Nothing to explain"
		app.invalidate()
		return
	}
	plan, _, _, err := sqltext.ExplainPlanStructured(app.engine(), sql)
	if err != nil {
		app.errMsg = "Explain: " + err.Error()
		app.status = "Explain failed"
		app.invalidate()
		return
	}
	// A sessionless engine answers with exactly one statement we can run. An
	// engine that needs a Prepare/Cleanup bracket (SQL Server, Oracle) cannot be
	// run on one pool session here yet.
	if plan.Prepare != "" || plan.Cleanup != "" || plan.Query == "" {
		app.errMsg = "EXPLAIN for " + app.engine() +
			" needs a dedicated session, which this build does not open yet"
		app.status = "Explain unsupported"
		app.invalidate()
		return
	}
	// The plan runs inside an open transaction when one is active, so an
	// uncommitted schema change is explained as it would be run.
	app.runSQL(plan.Query)
}

// explainable reports whether a statement can be explained by this build; kept
// as a pure check for tests.
func explainable(engine, sqlText string) (bool, error) {
	plan, _, _, err := sqltext.ExplainPlanStructured(engine, sqlText)
	if err != nil {
		return false, err
	}
	return plan.Prepare == "" && plan.Cleanup == "" && plan.Query != "", nil
}
