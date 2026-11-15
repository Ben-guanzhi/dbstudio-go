package main

// Coming back to what you were doing.
//
// Mature clients reopen the workspace: the consoles you left open, the schema
// you were looking at, the window's size, the split. Losing eight consoles to a
// restart is the kind of thing that makes an editor feel disposable.
//
// The session is one small JSON file beside the saved connections:
//
//	{ "conn": "22", "schema": "gs_prod",
//	  "consoles": [{"label": "Console 1", "sql": "SELECT ..."}, ...],
//	  "consoleSel": 0, "splitSide": 260, "splitEditor": 300,
//	  "width": 1320, "height": 860 }
//
// It is written on the console-level changes and on quit, and read before the
// window is built (so the size is the one last used). Nothing here is a
// correctness matter: a missing or unreadable session starts a fresh one.

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui/editor"
)

// sessionFile is the session's name beside connections.json.
const sessionFile = "session.json"

// sessionConsole is one console as the file keeps it: the name the tab shows and
// the SQL it held.
type sessionConsole struct {
	Label string `json:"label"`
	SQL   string `json:"sql"`
}

// sessionState is the workspace saved between launches.
type sessionState struct {
	Conn       string           `json:"conn,omitempty"`
	Schema     string           `json:"schema,omitempty"`
	Consoles   []sessionConsole `json:"consoles,omitempty"`
	ConsoleSel int              `json:"consoleSel,omitempty"`
	SplitSide  float32          `json:"splitSide,omitempty"`
	SplitEdit  float32          `json:"splitEditor,omitempty"`
	Width      int              `json:"width,omitempty"`
	Height     int              `json:"height,omitempty"`
	// Widths are the columns the user dragged, by column name, so the grid opens
	// with the arrangement it was left in.
	Widths map[string]float32 `json:"widths,omitempty"`
	// ColLayouts is how a table's result was arranged last time — the order and
	// the hidden columns — keyed "schema\x00table". A browse opens with the
	// arrangement it was left in, the way a matured client remembers it per
	// table rather than per window.
	ColLayouts map[string]colLayout `json:"colLayouts,omitempty"`
}

// colLayout is one table's remembered arrangement of its result columns.
type colLayout struct {
	Order  []string `json:"order,omitempty"`
	Hidden []string `json:"hidden,omitempty"`
	Frozen []string `json:"frozen,omitempty"`
}

// sessionPath is the session file in the connections directory.
func (app *App) sessionPath() string {
	if app.store == nil {
		return ""
	}
	return filepath.Join(app.store.Dir(), sessionFile)
}

// loadSession reads the session, answering a zero value when there is none: a
// first launch has no session, and that is not an error.
func loadSession(path string) sessionState {
	var s sessionState
	if path == "" {
		return s
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	_ = json.Unmarshal(data, &s)
	return s
}

// sessionNow snapshots the workspace as it stands.
func (app *App) sessionNow() sessionState {
	s := sessionState{
		Conn:       app.connName,
		Schema:     app.schema,
		ConsoleSel: app.consolesel,
		SplitSide:  app.splitSide,
		SplitEdit:  app.splitEditor,
	}
	if len(app.colLayouts) > 0 {
		s.ColLayouts = make(map[string]colLayout, len(app.colLayouts))
		for k, v := range app.colLayouts {
			s.ColLayouts[k] = v
		}
	}
	if len(app.grid.Columns.Widths) > 0 {
		s.Widths = make(map[string]float32, len(app.grid.Columns.Widths))
		for k, v := range app.grid.Columns.Widths {
			s.Widths[k] = v
		}
	}
	for _, c := range app.consoles {
		s.Consoles = append(s.Consoles, sessionConsole{Label: c.label, SQL: c.buf.String()})
	}
	if app.win != nil {
		// The size the user gave the window, so the next launch opens the same.
		s.Width, s.Height = app.win.Size()
	}
	return s
}

// saveSession writes the workspace if there is one to write. A failure is
// reported in the status line: the session is a convenience, not a guarantee,
// and losing it must not interrupt the work.
func (app *App) saveSession() {
	path := app.sessionPath()
	if path == "" {
		return
	}
	s := app.sessionNow()
	data, err := json.MarshalIndent(s, "", " ")
	if err != nil {
		return
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		app.errMsg = "Session not saved: " + err.Error()
	}
}

// applySession puts a saved workspace back: the consoles and which one was
// showing, the working schema, and the splits. The window's size is applied
// where the window is built, as it has to be known before then.
func (app *App) applySession(s sessionState) {
	if len(s.Consoles) > 0 {
		consoles := make([]consoleTab, 0, len(s.Consoles))
		for _, c := range s.Consoles {
			buf := editor.NewBuffer(c.SQL)
			consoles = append(consoles, consoleTab{
				buf:   buf,
				state: app.newEditorState(buf),
				label: c.Label,
			})
		}
		app.consoles = consoles
		app.consolesel = clampInt(s.ConsoleSel, 0, len(consoles)-1)
		app.initEditorState()
	}
	if s.Schema != "" {
		app.schema = s.Schema
	}
	if s.SplitSide > 0 {
		app.splitSide = s.SplitSide
	}
	if s.SplitEdit > 0 {
		app.splitEditor = s.SplitEdit
	}
	if len(s.ColLayouts) > 0 {
		app.colLayouts = make(map[string]colLayout, len(s.ColLayouts))
		for k, v := range s.ColLayouts {
			app.colLayouts[k] = v
		}
	}
	if len(s.Widths) > 0 {
		widths := make(map[string]float32, len(s.Widths))
		for k, v := range s.Widths {
			widths[k] = v
		}
		app.grid.Columns.Widths = widths
		// Remember what was restored, so the first frame does not think the user
		// dragged something and write the session again.
		app.savedWidths = make(map[string]float32, len(widths))
		for k, v := range widths {
			app.savedWidths[k] = v
		}
	}
}

// clampInt keeps i within [lo, hi].
func clampInt(i, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if i < lo {
		return lo
	}
	if i > hi {
		return hi
	}
	return i
}

// workAreaOf is the screen-facts source windowSize clamps against, split out so
// a test can hand a display in without a running app (mygo's Screen calls wait
// for the main loop, which a test has none of).
var workAreaOf = func() (int, int) {
	if d := mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint()); d.WorkArea.Width > 0 {
		return d.WorkArea.Width, d.WorkArea.Height
	}
	return 0, 0
}

// windowSize is the size a new window opens at: the session's, clamped to the
// screen the cursor is on. 波 100：一个换过显示器的会话（2×1440p 外接回到笔记本
// 屏）原样恢复就是把任务栏和半张桌面顶出屏幕——尺寸是会话的建议，屏幕才是
// 事实。调用时机在 WhenReady 内，Screen 已可用。
func windowSize(s sessionState) (int, int) {
	const defW, defH = 1320, 800
	const minW, minH = 860, 520
	// The chrome the platform adds around the content: borders, the caption and
	// the menu bar. The size the app asks for is the content's, so the room left
	// for it is the work area minus this.
	// Measured: a 1320x800 content area is a 1349x869 window — the borders, the
	// caption and the menu bar add 29 wide and 69 tall.
	const chromeW, chromeH = 29, 69
	w, h := s.Width, s.Height
	if w < minW || h < minH {
		w, h = defW, defH
	}
	if aw, ah := workAreaOf(); aw > 0 && ah > 0 {
		// Keep a small margin so the window's frame and the taskbar both fit;
		// the window is never larger than the work area.
		maxW, maxH := aw-16-chromeW, ah-16-chromeH
		if maxW < minW {
			maxW = minW
		}
		if maxH < minH {
			maxH = minH
		}
		if w > maxW {
			w = maxW
		}
		if h > maxH {
			h = maxH
		}
	}
	return w, h
}
