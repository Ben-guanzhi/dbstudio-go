package main

// Refreshing the schema tree (P1-1 of the gap analysis).
//
// The catalog is cached with a generation counter: treeGen makes a load that was
// in flight when the connection changed drop its answer. A schema edited
// elsewhere — a migration run in another client, a table added by a script — is
// the same staleness one step smaller, so it takes the same mechanism: bump the
// generation (dropping every in-flight load's answer), empty the cache, and let
// the tree load what it next shows. No invalidation push is needed, because the
// tree is lazy: it asks for a branch when it opens it.

import (
	"context"
	"time"
)

// refreshSchema drops everything the catalog cached and makes the tree read it
// again. The open consoles, the results and the transaction are untouched: this
// is about the schema, not the connection.
func (app *App) refreshSchema() {
	if !app.open || app.db == nil {
		app.errMsg = "Not connected"
		app.status = "Not connected"
		app.invalidate()
		return
	}
	// The namespaces survive a refresh — they are the connection's, not the
	// schema's — and the tree needs the first one after the cache is emptied.
	names := app.SchemaNames()
	// A generation bump drops every answer a load still owes: the tables of the
	// schema that was open when the refresh began would otherwise land in the
	// emptied cache and look current.
	app.treeGen++
	app.pending = nil // in-flight loads are dropped by the generation
	app.Reset()
	app.SetSchemas(names)
	app.objects = nil
	app.search = treeSearch{}
	app.clearTreeErr()
	// The tree's rows are rebuilt from the roots; the selection it held no longer
	// addresses anything.
	app.treeSel = -1
	app.tree.Open.Clear()
	app.tree.List.Selected = nil
	app.selTable = ""
	app.status = "Schema refreshed"
	app.errMsg = ""
	app.invalidate()
	// The branch the tree next shows loads on demand; if a schema was open, ask
	// for its tables now so the refresh is visible without a second click.
	if len(names) > 0 {
		app.loadTables(names[0])
	}
	// The namespace list itself was frozen at connect time, so a database added
	// or renamed by another client never appeared and every schema picker kept
	// offering the old list (F3). Re-read it off the UI thread; the answer is
	// discarded when the connection changed underneath it.
	if cached := app.cached; cached != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := cached.LoadSchemas(ctx); err != nil {
				app.post(func() {
					app.errMsg = "Refresh schemas: " + err.Error()
					app.invalidate()
				})
				return
			}
			fresh := cached.SchemaNames()
			app.post(func() {
				if !app.open || app.cached != cached {
					return
				}
				app.SetSchemas(fresh)
				app.invalidate()
			})
		}()
	}
}
