package main

// The application state: one App holds everything the view draws, and the
// builders the views and tests share. What a field means lives beside the
// feature that owns it (see the file list in README); this file is the state
// itself and the pieces every pane needs.

import (
	"context"
	"time"

	"github.com/Ben-guanzhi/dbcore"
	"github.com/Ben-guanzhi/dbcore/usability"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// App holds everything the view draws. The fields the view touches are plain
// values; the background goroutines that fill them from the database reach the
// view only through Window.Update, which runs on the main thread.
type App struct {
	win    *mygo.Window
	store  *Store
	cached *Cached

	connName string // connection chosen in the sidebar
	open     bool
	db       *DB
	conn     Conn // the connection that is open

	// The connection-level transaction (#10). autocommit is the toolbar switch's
	// state; when it is off, txActive and txID name the transaction the editor's
	// statements run in, and txBusy is a commit or rollback in flight.
	autocommit bool
	txActive   bool
	txID       string
	txBusy     bool
	txErr      string
	// txIsolation is the isolation level a transaction opened from here asks
	// for: "" means the engine's default, the rest are the kernel's TxMode
	// spellings. The dropdown shows it only where a transaction can be opened.
	txIsolation string

	// The connection's health (#26): the last probe's verdict and when it ran,
	// with a generation so a slow ping cannot overrule a newer one. Checking is
	// a toolbar indicator; it never blocks a statement.
	health    string // "" unknown, "ok", "down"
	healthGen uint64

	// The connection info panel (#27): the capability rows, read when it opens.
	connInfo connInfo

	// schema is the working namespace: empty means "every schema the user can
	// see". It scopes the object search and the bare-name defaults, and for an
	// engine whose connection carries a default schema it reopens the
	// connection with it (see schema.go).
	schema string

	// historyOpen is the query-history modal: a tool window, not a strip.
	historyOpen bool

	// lastDrawnRow is the deepest row the grid built in the last frame: the table
	// only builds the rows in view, so it is how far down the reader has scrolled.
	lastDrawnRow int

	// colLayouts is each table's remembered column arrangement, keyed
	// "schema\x00table"; a browse applies it and every change writes it back.
	colLayouts map[string]colLayout

	// columns is the Columns dialog's state.
	columns columnsModal

	// rowEst caches each table's row estimate for the pager, misses included;
	// savedWidths is the column widths the session last wrote, so a resize is
	// noticed once rather than every frame.
	rowEst      map[string]rowEstimate
	savedWidths map[string]float32

	// readOnlyOn is the session's read-only lock, seeded from the connection's
	// saved setting and flipped by the toolbar's switch.
	readOnlyOn bool

	// confirm is the question that stands between a destructive statement and
	// the server (DROP/TRUNCATE, or DELETE/UPDATE with no WHERE).
	confirm confirmRun

	// resView is the results area's tab: 0 the grid, 1 the messages. The
	// messages used to be a panel under the grid that was always there; as a
	// tab it takes the room only while it is being read.
	resView int

	// A metadata load's failure (a schema's tables, a table's columns, an
	// object section). It is kept apart from errMsg on purpose: a catalog read
	// that timed out is not a failed query, and showing it in the results pane
	// reads as one. retry is what the tree's error row runs to try again.
	treeErr      string
	treeErrRetry func()

	// pending holds the metadata loads in flight, keyed like the catalog keys.
	// The tree asks for a branch's children on every frame, and the cache only
	// stops being "not loaded" when the answer lands — so without this guard a
	// slow load was re-issued once per frame, which on a big schema buried the
	// server (and the pool) and turned one slow read into a deadline timeout
	// for everything behind it.
	pending map[string]bool

	edBuf   *editor.Buffer
	edState *editor.State

	// The running query's cancel handle and generation (#25). Stop calls the
	// cancel, whose ctx the kernel's QueryContext already honors; runGen drops
	// a result that answers after the run it belonged to was replaced — the
	// same generation discipline the tree loads use.
	runGen    uint64
	runCancel context.CancelFunc

	// The editor consoles (#24): each tab its own buffer, and consolesel the
	// one the editor pane shows. edBuf/edState above are the shown tab's buffer
	// and state — every consumer of "the editor" reads those two and swaps are
	// a pointer change, so no consumer knows there is more than one console.
	consoles   []consoleTab
	consolesel int

	// The DDL viewer's modal and its read-only editor. ddlOpen is the modal,
	// ddlBuf/ddlState its editor, and ddlGen is the load it belongs to: opening
	// the DDL of one table while another's is on its way discards the older one.
	ddlOpen   bool
	ddlBusy   bool
	ddlErr    string
	ddlSchema string
	ddlTable  string
	ddlNode   treeNode
	ddlGen    uint64

	// The structure viewer's modal (波 100): the table's columns as a grid.
	// stOpen is the modal, stCols the loaded columns, stGen the load guard —
	// the same discard-the-older-load rule the DDL viewer applies.
	stOpen  bool
	stBusy  bool
	stErr   string
	stTable string
	stCols  []dbcore.ColumnInfo
	stGen   uint64
	stGrid  ui.ListState

	// The cell value editor's modal and its editor. cellRow and cellCol address
	// the cell in the grid's full set; cellBytes and cellHex are its blob, if it
	// holds one, and how that blob is shown.
	cellOpen  bool
	cellErr   string
	cellRow   int
	cellCol   int
	cellBlob  bool
	cellHex   bool
	cellBytes []byte

	ddlBuf    *editor.Buffer
	ddlState  *editor.State
	cellBuf   *editor.Buffer
	cellState *editor.State

	// The results pane's tabs; tabSel is the one the view shows, -1 when there
	// is none. The fields below are the live view of that tab.
	tabs   []*resTab
	tabSel int

	result *dbcore.QueryResult
	errMsg string
	busy   bool
	// busyOwners records which asynchronous layer is running. A single bool
	// lost ownership: a short browse finishing first cleared the flag a long
	// script run still owned, so Stop was disabled mid-run and the status bar
	// lied. busy stays the derived answer the renderer reads.
	busyOwners map[string]bool
	start      time.Time

	grid       ui.ListState
	gridSel    int // the row the grid's selection points at, -1 when none
	gridSelSet ui.Selection[int]

	// The grid's sort state, which grid.Sort and grid.SortBy point at: the
	// columns its header sorts the rows by. The table header click handler
	// rewrites them, so the view shows the arrows; the data is ordered to
	// match, server-side for a browse and here for a result the driver loaded.
	gridSort   ui.SortOrder
	gridSortBy []ui.SortOrder

	// The grid's filter bar: gridFilter is the filter the rows are kept by, as
	// the bar's conditions ask for it, and filterDraft holds the bar's own
	// conditions while they are being edited. filterRemove is the condition the
	// bar asked to drop, applied after the build that drew it.
	gridFilter   []dbcore.FilterExpr
	filterDraft  []filterRow
	filterRemove int

	pager         resultPager // the results grid's paging
	pageSizeLabel string      // the pager's rows-per-page selector

	jump    bool    // the pager's jump-to-page modal is open
	jumpNum float64 // the page the modal asks for
	jumpErr string  // the modal's error, or ""

	tree     ui.OutlineState[treeNode]
	treeSel  int
	selTable string // the table whose browse SQL is in the editor

	// The schema tree's object search (#19): its box, its results, and the
	// generation that drops a slow answer.
	search treeSearch

	histID string // the history entry the history sidebar shows, "" when none

	treeGen uint64
	// The connection's metadata cache, behind the catalog selectors. The fields
	// stay here (embedded struct, promoted), so a test building an App by hand
	// keeps working; production code reads only through the selectors.
	catalog
	// objects holds the extended tree's resolved sections (indexes, FKs, CHECK
	// constraints, partitions, sequences, routines, enums), keyed as the
	// object cache keys them. A nil value for a loaded-but-empty section is a
	// *objectLeaves with no names; a not-supported driver marks the section.
	objects objectCache

	// The parameterized query panel's modal (#15): the statement it holds, its
	// extracted placeholders, and the values being typed in.
	params paramPanel

	// The schema comparison modal (#16): the two connections it compares, the
	// entries and script the kernel returned, and an apply's progress.
	compare compareState

	// The import modal (#21): the file, its mapping onto the table's columns,
	// and the run's progress.
	importer importState

	edit editState // the editable working copy of the grid, when there is one

	// um 是可用性事件记录器（S-01，dbcore/usability）：默认关闭，只有
	// POLYDB_USABILITY=1/true 时才落盘；关闭时所有调用都是 no-op。
	um *usability.Recorder

	form     Conn
	formKind string // the Kind the form last drew, to reset the schema on change
	port     float64
	sshPort  float64 // the SSH bastion's port, as the number input holds it
	// The form's Test button: whether a test is running, and its verdict, kept
	// apart from formErr so a failed test does not look like a failed save.
	formTesting bool
	formTest    string
	formTestOK  bool
	formErr     string
	modal       bool
	aboutOpen   bool
	plugOpen    bool
	plugNote    string

	splitSide   float32
	splitEditor float32

	status string
}

// initModalEditors builds the two editors the modals show, which are separate
// from the query editor: the DDL viewer's, which is read-only, and the cell
// value editor's, which is written to when the grid is a table's working copy.
// A test that draws the view calls it too.
func (app *App) initModalEditors() {
	app.ddlBuf = editor.NewBuffer("")
	app.ddlBuf.SetLanguage(editor.LanguageFor("schema.sql"))
	app.ddlState = &editor.State{
		Buffer:      app.ddlBuf,
		Indent:      editor.IndentSpaces,
		TabSize:     2,
		LineNumbers: editor.LineNumbersOn,
		ReadOnly:    true,
		Placeholder: "Loading DDL…",
	}
	app.cellBuf = editor.NewBuffer("")
	app.cellBuf.SetLanguage(editor.LanguageFor("query.sql"))
	app.cellState = &editor.State{
		Buffer:      app.cellBuf,
		Indent:      editor.IndentSpaces,
		TabSize:     2,
		LineNumbers: editor.LineNumbersOn,
		Placeholder: "Value",
	}
}

func (app *App) resetSplit() {
	app.splitSide = 260
	app.splitEditor = 300
	app.invalidate()
}

// clearMessages drops the report the current tab has, and the app's message
// too, so the panel can be read again from the next thing that happens.
func (app *App) clearMessages() {
	if t := app.tab(); t != nil {
		t.status = ""
		t.errMsg = ""
	}
	app.errMsg = ""
	if app.status != "" && !app.edit.active {
		app.status = ""
	}
	app.invalidate()
}
