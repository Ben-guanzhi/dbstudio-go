package main

// The entry point: build the App, open the window, wire the menu, connect the
// last active connection. Everything the window draws lives in app.go (the
// state) and view.go (the panes); the menu is in menu.go.

import (
	"log"
	"path/filepath"
	"strconv"

	"github.com/dbcore/dbcore"
	"github.com/dbcore/dbcore/usability"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func main() {
	app := &App{
		splitSide:     260,
		splitEditor:   300,
		treeSel:       -1,
		gridSel:       -1,
		tabSel:        -1,
		tables:        map[string][]tableInfo{},
		cols:          map[string][]dbcore.ColumnInfo{},
		pager:         newResultPager(),
		pageSizeLabel: strconv.Itoa(defaultPageSize),
	}
	// The grid needs a live selection for its cursor; without one a click on a
	// row dereferences a nil pointer.
	app.grid.Selected = &app.gridSel
	// The header's sort order, which the table writes as the user clicks it and
	// the rows are ordered to match.
	app.grid.Sort = &app.gridSort
	app.grid.SortBy = &app.gridSortBy
	// The grid's cells open a menu that copies them as SQL or CSV.
	app.grid.CellMenu = app.gridCellMenu

	mygo.App.WhenReady(func() {
		store, err := NewStore()
		if err != nil {
			log.Fatal(err)
		}
		if err := store.load(); err != nil {
			log.Printf("loading connections: %v", err)
		}
		store.loadHistory()
		app.store = store
		// Usability metrics (S-01): a no-op unless POLYDB_USABILITY=1. The file
		// lives next to the connection store so a user can read/delete it; it is
		// never uploaded.
		app.um = usability.Open("dbstudio", filepath.Join(store.Dir(), "usability.ndjson"))
		defer app.um.Close()
		if c, ok := store.Active(); ok {
			app.connName = c.Name
		}

		// The workspace last left behind: its consoles, the schema it was
		// looking at, the window's size and the splits. Read before the window
		// exists, because the size has to be known then.
		sess := loadSession(filepath.Join(store.Dir(), sessionFile))
		if sess.Conn != "" {
			if _, ok := store.Get(sess.Conn); ok {
				app.connName = sess.Conn
			}
		}
		width, height := windowSize(sess)

		// The editor's consoles: each holds its own buffer and its own editor
		// state (the undo stack, the hover cache and the completion context live
		// in the state, so sharing one across tabs would carry them across).
		app.initConsoles()
		app.initEditorState()
		app.applySession(sess)

		// The DDL viewer's editor, read-only, and the cell value editor's, which
		// is written to when the grid is a working copy. Both are their own state
		// so they can be shown in a modal while the main editor keeps its place.
		app.initModalEditors()

		app.win = mygo.NewWindow(mygo.WindowOptions{
			Title:     "DB Studio",
			Width:     width,
			Height:    height,
			MinWidth:  860,
			MinHeight: 520,
			Content:   ui.View(app.view),
		})
		app.setMenu()
		// The session is written on quit, so a restart opens what was open. A
		// quit from the menu and one from the window's close button both pass
		// here.
		mygo.App.OnBeforeQuit(func(e *mygo.QuitEvent) { app.saveSession() })
		// The connection's health probe runs for the window's whole life: a
		// dropped link should show on the toolbar, not as the next statement's
		// confusing error.
		stop := make(chan struct{})
		defer close(stop)
		go app.pingLoop(stop)

		if app.connName != "" {
			app.connect(app.connName)
		}
	})

	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
