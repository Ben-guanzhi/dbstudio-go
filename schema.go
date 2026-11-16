package main

// The working schema: which namespace the window is looking at (P3 gap fix).
//
// A server holds many schemas (the MySQL instance in the field has dozens), and
// the interesting questions — "where do my tables come from", "search only this
// one", "what does a bare table name mean" — all have the schema as their
// answer. So the window carries one: app.schema, empty meaning "every schema the
// user can see", else the chosen one.
//
// What it drives:
//   - the object search's scope: empty searches everywhere, a chosen schema
//     narrows it (the kernel's MySQL recipe needs the database to filter);
//   - the browse SQL and the DDL/insert defaults, through defaultSchema;
//   - the connection's own default: switching to a schema reopens the
//     connection with that schema as its default database (MySQL's DSN dbname,
//     PostgreSQL's search_path), which is the only way it can apply to every
//     pooled connection rather than to whichever one a statement happened to
//     borrow. Selecting "(all schemas)" reopens with the connection's own value.

import (

	"github.com/Ben-guanzhi/dbcore"
	"github.com/egoist/mygo/ui"

	"dbstudio-go/internal/sqlcodec"
)

// schemaAll is the selector's label for "no schema chosen".
const schemaAll = "(all schemas)"

// schemaChoice is the selector's current label.
func (app *App) schemaChoice() string {
	if app.schema == "" {
		return schemaAll
	}
	return app.schema
}

// schemaOptions are the selector's labels: every schema, then "(all schemas)".
func (app *App) schemaOptions() []string {
	names := app.SchemaNames()
	out := make([]string, 0, len(names)+1)
	out = append(out, schemaAll)
	out = append(out, names...)
	return out
}

// setActiveSchema makes name the working schema and, when the connection can
// carry it, reopens the connection so the choice applies to every pooled
// connection. name "" means no choice.
func (app *App) setActiveSchema(name string) {
	if name == app.schema {
		return
	}
	app.schema = name
	// The search box's scope just changed, so a live search asks again.
	if app.search.active {
		app.runTreeSearch()
	}
	if !app.open || app.db == nil {
		app.invalidate()
		return
	}
	if !schemaIsConnectionDefault(app.engine()) {
		// The engine has no per-connection default schema (SQLite's file is its
		// schema, SQL Server's schemas are object-level): the choice still
		// scopes the search and the DDL defaults, without a reconnect.
		app.status = "Working schema: " + app.labeledSchema()
		app.invalidate()
		return
	}
	app.reconnectInSchema(name)
}

// labeledSchema is the choice as the status line shows it.
func (app *App) labeledSchema() string {
	if app.schema == "" {
		return "all schemas"
	}
	return app.schema
}

// schemaIsConnectionDefault reports whether an engine's connection carries a
// default schema or database (so switching one needs a reopen to take effect on
// every pooled connection).
func schemaIsConnectionDefault(engine string) bool {
	switch engine {
	case "mysql", "postgres", "postgresql", "mssql", "oracle", "dm":
		return true
	}
	return false
}

// reconnectInSchema reopens the current connection with schema as its default.
// It is the same path as connecting, so the tree, the consoles' connection and
// the session's transaction state settle exactly as a fresh connect does.
func (app *App) reconnectInSchema(schema string) {
	name := app.connName
	if name == "" {
		app.invalidate()
		return
	}
	app.disconnect()
	app.connectIn(name, schema)
}

// schemaSelector draws the working-schema dropdown. It sits above the tree's
// search box: the two together are "where am I" and "find something in there".
func (app *App) schemaSelector(c *ui.Context) {
	t := c.Theme()
	options := app.schemaOptions()
	// The widget writes the label it shows; a stale value (a schema that
	// disappeared) falls back to the first option rather than showing a label
	// the options do not contain.
	choice := app.schemaChoice()
	known := false
	for _, o := range options {
		if o == choice {
			known = true
			break
		}
	}
	if !known {
		choice = schemaAll
	}
	ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "Schema").FontSize(10).TextColor(t.TextMuted).SingleLine()
		ui.Spacer(c)
		sel := ui.Select(c, &choice, options).Label("Schema")
		if sel.Changed() {
			name := choice
			if name == schemaAll {
				name = ""
			}
			app.setActiveSchema(name)
		}
	})
}

// alignOf is a column's text alignment from its SQL type: numbers to the right,
// everything else to the left. The type string is the engine's own spelling, so
// KindOf is what decides (the same classifier the cell editor uses), not a
// hand-written list of type names.
func alignOf(typeName string) ui.Align {
	switch sqlcodec.KindOf(typeName) {
	case dbcore.ValInt, dbcore.ValFloat, dbcore.ValDecimal, dbcore.ValBool:
		return ui.End
	}
	return ui.Start
}

// connInSchema is the connection with schema as its default database (MySQL) or
// search_path (PostgreSQL). Other engines keep the connection as it is: their
// schema is addressed per object, not per session.
func connInSchema(c Conn, schema string) Conn {
	switch c.Kind {
	case "mysql":
		c.Database = schema
	case "postgres", "postgresql":
		c.Schema = schema
	case "mssql", "sqlserver":
		c.Database = schema
	}
	return c
}

// schemaOfConn is the default namespace a connection carries: MySQL's database,
// or the schema the other engines address by default.
func schemaOfConn(c Conn) string {
	switch c.Kind {
	case "mysql":
		return c.Database
	case "postgres", "postgresql", "mssql", "sqlserver":
		return c.Schema
	}
	return ""
}
