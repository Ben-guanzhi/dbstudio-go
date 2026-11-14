package main

// Searching the connection's objects from the schema tree.
//
// The tree only knows what the user has expanded, so filtering it could only
// ever find objects already opened. The kernel's sqltext.SearchObjectsQuery asks
// the catalog instead: one statement per engine, already dialect-correct (bound
// pattern vs inlined literal, ILIKE vs LIKE, SQLite's ESCAPE), returning
// (schema, name, type) for every object whose name contains the fragment. So a
// search finds a table the tree has never loaded.
//
// The box searches as the text changes and on Enter; every search carries a
// generation so a slow answer cannot overwrite a newer one.

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/dbcore/dbcore"
	"github.com/dbcore/dbcore/sqltext"
	"github.com/egoist/mygo/ui"
)

// searchLimit caps a search's rows: a box that answers "50 shown" is useful, one
// that materializes a whole catalog is not.
const searchLimit = 50

// searchHit is one object the catalog matched.
type searchHit struct {
	schema string
	table  string
	kind   string // "TABLE" / "VIEW" as the catalog spells it
}

// treeSearch is the schema tree's search box and what it holds. sel is the
// sidebar selection: the row index as text, "" when none.
type treeSearch struct {
	text   string
	active bool // the box has a query, so the results replace the tree
	busy   bool
	err    string
	hits   []searchHit
	sel    string
	gen    uint64
}

// searchSchema is the namespace the search asks about: the working schema when
// one is chosen, else "" (every schema the user can see). Only MySQL's recipe
// needs it — the others search their whole catalog, which is what "" asks for.
func (app *App) searchSchema() string {
	return app.schema
}

// runTreeSearch asks the catalog for objects whose name contains the box.
func (app *App) runTreeSearch() {
	s := &app.search
	s.gen++
	gen := s.gen
	fragment := strings.TrimSpace(s.text)

	// An empty box is not a search: it gives the tree back.
	if fragment == "" {
		s.active, s.busy, s.err, s.hits, s.sel = false, false, "", nil, ""
		app.invalidate()
		return
	}
	if app.db == nil || app.db.SQL == nil {
		s.active, s.busy, s.err, s.hits = true, false, "Not connected", nil
		app.invalidate()
		return
	}
	engine := app.engine()
	db := app.db
	query, args, err := sqltext.SearchObjectsQuery(engine, app.searchSchema(), fragment, searchLimit)
	if err != nil {
		s.active, s.busy, s.err, s.hits = true, false, err.Error(), nil
		app.invalidate()
		return
	}
	s.active, s.busy, s.err, s.sel = true, true, "", ""
	app.invalidate()

	fallbackSchema := app.FirstSchema()
	// read asks the catalog and maps the rows; it never touches App.
	read := func() ([]searchHit, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		res, err := db.SQL.Execute(ctx, query, dbcore.ValuesOf(args)...)
		if err != nil {
			return nil, err
		}
		if res == nil {
			return nil, nil
		}
		hits := make([]searchHit, 0, len(res.Rows))
		for _, row := range res.Rows {
			schema := cellString(row, 0)
			// SQLite's recipe reports "" for the schema: its objects live in the
			// one namespace the connection opened.
			if schema == "" {
				schema = fallbackSchema
			}
			hits = append(hits, searchHit{
				schema: schema,
				table:  cellString(row, 1),
				kind:   cellString(row, 2),
			})
		}
		return hits, nil
	}
	// apply stores a read's answer, dropping one a newer search replaced.
	apply := func(hits []searchHit, err error) {
		if gen != s.gen {
			return
		}
		s.busy = false
		if err != nil {
			s.err = firstLine(err.Error())
			s.hits = nil
		} else {
			s.err = ""
			s.hits = hits
		}
		app.invalidate()
	}
	if app.win == nil {
		// Headless: run inline so a test sees the answer when it returns.
		apply(read())
		return
	}
	go func() {
		hits, err := read()
		app.post(func() { apply(hits, err) })
	}()
}

// cellString reads a result cell as text, which is what every row of an object
// search is.
func cellString(row []dbcore.Value, i int) string {
	if i < 0 || i >= len(row) {
		return ""
	}
	return row[i].String()
}

// clearTreeSearch puts the tree back and empties the box.
func (app *App) clearTreeSearch() {
	s := &app.search
	s.gen++
	s.text, s.active, s.busy, s.err, s.hits, s.sel = "", false, false, "", nil, ""
	app.invalidate()
}

// openHit browses the table a search result names, leaving the search open so
// several results can be visited in a row.
func (app *App) openHit(h searchHit) {
	if h.table == "" {
		return
	}
	app.browseTable(treeNode{label: h.table, parent: h.schema, kind: nodeTable})
}

// searchBox draws the input and its clear button.
func (app *App) searchBox(c *ui.Context) {
	s := &app.search
	ui.Row(c).Gap(4).Children(func() {
		box := ui.TextInput(c, &s.text).Placeholder("Search objects…")
		if box.Changed() || box.Submitted() {
			app.runTreeSearch()
		}
		if s.active {
			if ui.Button(c, "×").Tooltip("Clear the search").Clicked() {
				app.clearTreeSearch()
			}
		}
	})
}

// searchResults draws the result list in the tree's place: one selectable row
// per object, the first click browsing it.
func (app *App) searchResults(c *ui.Context, t *ui.Theme) {
	s := &app.search
	if s.busy {
		ui.Row(c).Gap(6).Padding(2, 4).Children(func() {
			ui.Spinner(c)
			ui.Text(c, "Searching…").TextColor(t.TextMuted).FontSize(11)
		})
		return
	}
	if s.err != "" {
		ui.Text(c, s.err).TextColor(t.Danger).FontSize(11).Wrap()
		return
	}
	if len(s.hits) == 0 {
		ui.Text(c, "No objects match.").TextColor(t.TextMuted).FontSize(11).Wrap()
		return
	}
	ui.Text(c, strconv.Itoa(len(s.hits))+" object(s)").TextColor(t.TextMuted).FontSize(11)
	prev := s.sel
	ui.Sidebar(c, &s.sel, func() {
		for i, h := range s.hits {
			label := h.table
			if h.schema != "" {
				label = h.schema + "." + h.table
			}
			item := ui.SidebarItem(c, strconv.Itoa(i), nil, label)
			if h.kind != "" {
				item.TextColor(t.TextMuted)
			}
		}
	})
	// Choosing a row is the moment it opens: only a change caused by the user
	// counts, so a list rebuilt by a new answer does not browse anything.
	if s.sel != prev && s.sel != "" {
		if i, err := strconv.Atoi(s.sel); err == nil && i >= 0 && i < len(s.hits) {
			app.openHit(s.hits[i])
		}
	}
}
