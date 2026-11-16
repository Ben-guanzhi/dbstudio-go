package main

// The entry point: build the App, open the window, wire the menu, connect the
// last active connection. Everything the window draws lives in app.go (the
// state) and view.go (the panes); the menu is in menu.go.

import (
	"log"
	"path/filepath"
	"strconv"

	"github.com/Ben-guanzhi/dbcore"
	"github.com/Ben-guanzhi/dbcore/usability"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func main() {
	app := &App{
		splitSide:     defaultSplitSide,
		splitEditor:   defaultSplitEditor,
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
		// 同一颗键被两处声明时，只有一个会真正触发（静默失效）。启动时记下来。
		if dups := app.duplicateShortcutCombos(); len(dups) > 0 {
			log.Printf("快捷键冲突（只有一个会触发）：%v", dups)
		}
		store, err := NewStore()
		if err != nil {
			log.Fatal(err)
		}
		if err := store.load(); err != nil {
			log.Printf("loading connections: %v", err)
		}
		store.loadHistory()
		app.store = store
		// 波 100 续：发现 exe 旁的驱动插件（dbstudio-plugin-<kind>.exe），
		// 注册进内核工厂表——连接表单的 Kind 下拉据此动态出现对应引擎。
		DiscoverPlugins(store.Dir())
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

		app.wantWidth, app.wantHeight = width, height
		app.win = mygo.NewWindow(mygo.WindowOptions{
			Title:     "DB Studio Go",
			Width:     width,
			Height:    height,
			MinWidth:  860,
			MinHeight: 520,
			Content:   ui.View(app.view),
		})
		// 波 100：窗口与任务栏的图标（Windows/Linux）。内嵌的 PNG 由
		// cmd/icon 生成——rebuild 时改一处图形，三处生效（窗口、exe 资源、文档）。
		if err := app.win.SetIcon(appIconPNG); err != nil {
			log.Printf("window icon: %v", err)
		}
		app.setMenu()
		// The menu bar takes its height out of the client area after the window
		// was sized, so the canvas would keep being laid out at a size the window
		// no longer has. Asking for the content size again, now that the bar
		// exists, makes the window grow to match (WORKSPACE §74).
		app.fitContentToWindow()
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
