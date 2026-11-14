package main

// Editor consoles (P3 #24): more than one SQL editor, one tab each, the way a
// workbench holds several consoles open at once.
//
// The whole app reads and writes the editor through app.edBuf and app.edState —
// Run, Run Selection, Explain, Format, the parameter panel, completion, hover,
// go-to-definition, the history's load, the tree's browse fill. Those 31 sites
// are the editor's consumers, and none of them needs to know there is more than
// one console: a tab switch swaps which buffer edBuf points at, so "the editor"
// stays one thing to everyone and the consoles are its pages. That is why the
// tabs hold buffers instead of the consumers holding tabs.

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// consoleTab is one editor tab: its own buffer, its own editor state, and the
// name the tab shows.
//
// The state is per tab, not shared: an editor.State carries the undo stack, the
// hover cache and the completion context in its own storage, so one state for
// every console means undo reaches across tabs and a hover answered in one
// console shows in another. Two tabs that look independent should behave
// independently.
type consoleTab struct {
	buf   *editor.Buffer
	state *editor.State
	label string
}

// newEditorState builds the editor state one console runs on: the SQL editor's
// settings, the placeholder, and the three hooks the connection answers —
// completion, hover and go-to-definition.
func (app *App) newEditorState(buf *editor.Buffer) *editor.State {
	buf.SetLanguage(editor.LanguageFor("query.sql"))
	st := &editor.State{
		Buffer:      buf,
		Indent:      editor.IndentSpaces,
		TabSize:     2,
		LineNumbers: editor.LineNumbersOn,
		Placeholder: "-- Write SQL here.\n-- Run it with the Run button or Ctrl+Enter.\n\nSELECT 1;\n",
		// Answer the editor's requests for completions with the SQL the
		// connection knows, as a word is typed or Ctrl+Space is pressed.
		OnCompletion: app.onCompletion,
		// Hovering a name shows what it is; F12 or Cmd/Ctrl+click opens its
		// DDL, both read from the same metadata cache (#18).
		OnHover:      app.hoverIdentifier,
		OnDefinition: app.goToDefinition,
	}
	// Take Ctrl+Enter away from the editor, so the window runs the query.
	st.Bindings = append(st.Bindings,
		editor.Binding{Mods: ui.Cmd, Key: ui.KeyEnter, Command: editor.NoCommand})
	return st
}

// consoleMax is how many consoles one window holds. A tab is a buffer of text
// and an editor state, so the cap is about the tab bar's usability, not memory.
const consoleMax = 8

// consoleNew is the label a fresh tab carries until something gives it a name.
const consoleNew = "New Console"

// initConsoles gives the App its first editor tab, before anything reads edBuf.
// It runs where main builds the editor state, and a test builds its own.
func (app *App) initConsoles() {
	buf := editor.NewBuffer("")
	app.consoles = []consoleTab{{buf: buf, state: app.newEditorState(buf), label: "Console 1"}}
	app.consolesel = 0
}

// initEditorState points edBuf/edState at the shown console's buffer and state.
func (app *App) initEditorState() {
	c := app.current()
	app.edBuf = c.buf
	app.edState = c.state
}

// current is the tab the editor pane shows.
func (app *App) current() *consoleTab {
	if len(app.consoles) == 0 {
		app.initConsoles()
	}
	if app.consolesel < 0 || app.consolesel >= len(app.consoles) {
		app.consolesel = 0
	}
	return &app.consoles[app.consolesel]
}

// consoleLabels are the tab bar's labels.
func (app *App) consoleLabels() []string {
	out := make([]string, len(app.consoles))
	for i, c := range app.consoles {
		out[i] = c.label
	}
	return out
}

// newConsole opens a fresh tab, selects it, and swaps its buffer into the
// editor. It answers false when the cap is reached, which the tab bar reports
// by disabling +.
func (app *App) newConsole() bool {
	if len(app.consoles) >= consoleMax {
		return false
	}
	n := len(app.consoles) + 1
	label := "Console " + strconv.Itoa(n)
	// A name another tab dropped may come back; skip taken labels so two tabs
	// never read alike.
	taken := map[string]bool{}
	for _, c := range app.consoles {
		taken[c.label] = true
	}
	for i := n; taken[label]; i++ {
		label = "Console " + strconv.Itoa(i+1)
	}
	buf := editor.NewBuffer("")
	app.consoles = append(app.consoles, consoleTab{buf: buf, state: app.newEditorState(buf), label: label})
	app.consolesel = len(app.consoles) - 1
	app.selectConsole()
	return true
}

// closeConsole drops the tab at i and moves the editor to a neighbor. The last
// tab stays: one editor always exists, as before the tabs did.
func (app *App) closeConsole(i int) {
	if len(app.consoles) <= 1 || i < 0 || i >= len(app.consoles) {
		return
	}
	app.consoles = append(app.consoles[:i], app.consoles[i+1:]...)
	if app.consolesel >= len(app.consoles) {
		app.consolesel = len(app.consoles) - 1
	} else if app.consolesel > i {
		app.consolesel--
	}
	app.selectConsole()
}

// selectConsoleAt chooses the console at i, for the keys that cycle tabs.
func (app *App) selectConsoleAt(i int) {
	if i < 0 || i >= len(app.consoles) || i == app.consolesel {
		return
	}
	app.consolesel = i
	app.selectConsole()
}

// selectConsole swaps the tab's buffer into edBuf/edState, the editor every
// consumer reads. The buffer's language is per-tab, set at creation, so the
// swap is two pointers and one invalidate.
func (app *App) selectConsole() {
	c := app.current()
	app.edBuf = c.buf
	app.edState = c.state
	// The workspace changed shape (a console opened, closed or was chosen): the
	// session is written now rather than only on quit, so a crash costs the
	// typing since the last run, not the whole set of consoles.
	app.saveSession()
	app.invalidate()
}

// renameConsole names the current tab after its first statement, the way a
// console's tab shows what it holds. An empty editor keeps its label.
func (app *App) renameConsoleFromContent() {
	c := app.current()
	text := firstLine(c.buf.String())
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if r := []rune(text); len(r) > 24 {
		text = string(r[:24]) + "…"
	}
	c.label = text
}

// consolesView draws the editor pane's tab bar: one tab per console, a + to
// open another, an × on the current one to close it. 波 99：右端并报连接名——
// 单独的「Editor」标签行撤掉后，防误操作信号住在这里（consolesApp 测试宽 1200，
// 名字有地方放）。
func (app *App) consolesView(c *ui.Context) {
	t := c.Theme()
	if len(app.consoles) == 0 {
		app.initConsoles()
	}
	ui.Row(c).Gap(6).Padding(4, 8).Children(func() {
		sel := app.consolesel
		tabs := ui.Tabs(c, &sel, app.consoleLabels()...)
		if tabs.Changed() && sel != app.consolesel {
			if sel >= 0 && sel < len(app.consoles) {
				app.consolesel = sel
				app.selectConsole()
			}
		}
		if ui.Button(c, "+").Tooltip("New console").
			Disabled(len(app.consoles) >= consoleMax).Clicked() {
			app.newConsole()
			app.renameConsoleFromContent()
		}
		if ui.Button(c, "×").Tooltip("Close this console").
			Disabled(len(app.consoles) <= 1).Clicked() {
			app.closeConsole(app.consolesel)
		}
		ui.Spacer(c)
		// The connection the console is pointed at, then the console count.
		if app.open && app.connName != "" {
			ui.Text(c, app.connName).TextColor(t.TextMuted).FontSize(10).SingleLine()
			ui.Text(c, "·").TextColor(t.Border).FontSize(10)
		}
		ui.Text(c, fmt.Sprintf("%d / %d", len(app.consoles), consoleMax)).
			TextColor(t.TextMuted).FontSize(10).SingleLine()
	})
}

// consoleOfLabel finds a tab by the label a history entry or a browse fill
// named, for callers that re-target a console rather than the current one.
func (app *App) consoleOfLabel(label string) (*consoleTab, bool) {
	for i := range app.consoles {
		if app.consoles[i].label == label {
			return &app.consoles[i], true
		}
	}
	return nil, false
}
