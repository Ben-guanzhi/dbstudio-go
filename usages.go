package main

// Find usages (P3 #23), the counterpart of go-to-definition (#18).
//
// "Where is this table used" means, concretely: which of the connection's other
// tables hold a foreign key into it, and which of its columns those keys point
// at. The kernel has no cross-engine "who references me" recipe (its
// ObjectDependencies is Oracle's own catalog), but it does have
// ListForeignKeys on every SQL driver — so the search walks the loaded tables
// and reads each one's foreign keys. That is N reads over the catalog the tree
// has already touched, not a server-side scan, and it only sees what the tree
// has loaded: a schema nobody expanded has no FKs cached yet, and the search
// says so rather than pretending to have looked.
//
// A column's usage is the same walk, one level deeper: the keys whose
// RefColumns name the column.
//
// The results go into the results pane as a read-only table, so they can be
// sorted, filtered and exported like any other result.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dbcore/dbcore"
)

// usageRow is one row of a find-usages result: the referencing table and its
// key, with the columns of ours it points at.
type usageRow struct {
	schema    string
	table     string
	key       string
	columns   string // the target table's columns this key references
	ourColumn string // the referencing table's own columns, for a column search
}

// findUsages walks the loaded tables' foreign keys for references to the named
// table (or, when column is not empty, to that column of it), off the view
// thread.
func (app *App) findUsages(n treeNode) {
	if !app.open || app.db == nil || app.db.SQL == nil {
		app.errMsg = "Not connected"
		app.status = "Not connected"
		app.invalidate()
		return
	}
	schemas := app.SchemaNames()
	if len(schemas) == 0 {
		app.errMsg = "No schemas loaded"
		app.status = "Nothing to search"
		app.invalidate()
		return
	}
	// The table lists are snapshot on the main thread: the walk below reads them
	// from a goroutine, and a load landing mid-walk must not race it.
	type schemaTables struct {
		schema string
		tabs   []tableInfo
	}
	var walk []schemaTables
	for _, schema := range schemas {
		tabs, st := app.TablesOf(schema)
		if st == TablesNotLoaded {
			continue // a schema nobody expanded is not one we can walk
		}
		walk = append(walk, schemaTables{schema: schema, tabs: tabs})
	}
	db := app.db
	gen := app.treeGen
	table := n.label
	column := ""
	app.beginBusy("usages")
	app.status = "Finding usages…"
	app.invalidate()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		var rows []usageRow
		var errs []string
		seen := 0
		for _, s := range walk {
			for _, tab := range s.tabs {
				if strings.EqualFold(tab.Name, table) && strings.EqualFold(s.schema, n.parent) {
					continue // the table itself is not a usage of itself
				}
				fks, err := db.SQL.ListForeignKeys(ctx, s.schema, tab.Name)
				if err != nil {
					errs = append(errs, s.schema+"."+tab.Name+": "+err.Error())
					continue
				}
				seen++
				for _, fk := range fks {
					if !strings.EqualFold(fk.RefTable, table) {
						continue
					}
					if column != "" && !refColumnsContain(fk.RefColumns, column) {
						continue
					}
					rows = append(rows, usageRow{
						schema:    s.schema,
						table:     tab.Name,
						key:       fk.Name,
						columns:   strings.Join(fk.RefColumns, ", "),
						ourColumn: strings.Join(fk.Columns, ", "),
					})
				}
			}
		}
		app.post(func() {
			if gen != app.treeGen {
				return
			}
			app.endBusy("usages")
			app.showUsages(n, column, rows, seen, errs)
		})
	}()
}

// refColumnsContain reports whether the key's referenced columns name column.
func refColumnsContain(cols []string, column string) bool {
	for _, c := range cols {
		if strings.EqualFold(c, column) {
			return true
		}
	}
	return false
}

// showUsages puts the findings into the results pane as a read-only result.
func (app *App) showUsages(n treeNode, column string, rows []usageRow, walked int, errs []string) {
	what := n.label
	if column != "" {
		what = n.label + "." + column
	}
	res := &dbcore.QueryResult{
		Columns: []dbcore.ColumnInfo{
			{Name: "schema"},
			{Name: "table"},
			{Name: "foreign key"},
			{Name: "references"},
			{Name: "columns"},
		},
	}
	for _, r := range rows {
		res.Rows = append(res.Rows, []dbcore.Value{
			{Kind: dbcore.ValText, S: r.schema},
			{Kind: dbcore.ValText, S: r.table},
			{Kind: dbcore.ValText, S: r.key},
			{Kind: dbcore.ValText, S: r.columns},
			{Kind: dbcore.ValText, S: r.ourColumn},
		})
	}

	// The findings land in their own tab, titled after what was searched.
	app.saveTab()
	t := newTab("Usages of "+what, tabRun)
	t.result = res
	t.raw = res
	t.status = fmt.Sprintf("%d usage(s) across %d table(s) checked", len(rows), walked)
	t.pager.reset(false)
	t.pager.total = len(res.Rows)
	if len(errs) > 0 {
		t.errMsg = firstLine(strings.Join(errs, "; "))
	}
	app.tabs = append(app.tabs, t)
	app.showTab(t)
	app.status = fmt.Sprintf("Found %d usage(s) of %s", len(rows), what)
	if len(rows) == 0 && len(errs) == 0 {
		app.status = "No usages found for " + what
	}
	app.invalidate()
}
