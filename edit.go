package main

// The editable results grid.
//
// A table browse is not just a result set: it is a working copy of one table's
// rows. Every cell the user changes is kept as text in that copy; Insert Row
// and Delete Row change which rows it holds; a commit parses the changed cells
// back into dbcore.Values of their own kind and turns the whole copy into one
// dbcore.BatchRequest, applied inside a single transaction. Rollback throws
// the copy away and shows the rows as they were read.
//
// Only a table browse is editable. A commit addresses rows by primary key and
// names the bare table, and an arbitrary SELECT carries neither — a JOIN or an
// aggregate has no single table to write back to, so those stay read-only, as
// they do in DataGrip. A plain "SELECT * FROM t" is the one query that does
// carry both, and it is recognized as a browse.

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/Ben-guanzhi/dbcore"

	"dbstudio-go/internal/sqlcodec"
)

// rowState is what the working copy holds for one row.
type rowState int

const (
	rowUntouched rowState = iota
	rowModified
	rowInserted
	rowDeleted
)

// editRow is one row of the working copy.
//
// strs is the display buffer the cells read and write: ui.EditableText keeps the
// *string it was given and writes the committed draft back into it. The row is
// therefore a pointer, and its strs slice is allocated once and never moved, so
// a cell's *string stays valid while the grid is being edited however the rows
// above and below it come and go.
type editRow struct {
	strs     []string // what the cells show and edit
	origStrs []string // what they showed when read, for change detection
	origNull []bool   // whether the cell read as NULL, to mark an empty one
	kinds    []dbcore.ValueKind
	state    rowState
}

// changed reports whether the row's text differs from what was read.
func (r *editRow) changed() bool {
	for d := range r.strs {
		if r.strs[d] != r.origStrs[d] {
			return true
		}
	}
	return false
}

// editState is the editable grid's working copy.
type editState struct {
	active bool // the grid shows one table's rows and can be edited
	schema string
	table  string
	cols   []dbcore.ColumnInfo
	rows   []*editRow // the working copy
	orig   []*editRow // the rows as read, for rollback

	refresh func() // re-read the rows after a commit
}

// cloneRows copies rows deeply: the working copy may be thrown away and shown
// again, and the grid holds pointers into the strings of the copy it is drawing.
func cloneRows(rows []*editRow) []*editRow {
	out := make([]*editRow, len(rows))
	for i, r := range rows {
		c := &editRow{
			strs:     append([]string(nil), r.strs...),
			origStrs: append([]string(nil), r.origStrs...),
			origNull: append([]bool(nil), r.origNull...),
			kinds:    append([]dbcore.ValueKind(nil), r.kinds...),
			state:    r.state,
		}
		out[i] = c
	}
	return out
}

// init makes the working copy of a browse result. cached carries the primary
// key and identity flags a result set's own column metadata leaves out.
func (e *editState) init(res *dbcore.QueryResult, schema, table string, cached []dbcore.ColumnInfo, refresh func()) {
	e.cols = mergeColumns(res.Columns, cached)
	e.schema, e.table, e.refresh = schema, table, refresh
	e.active = true

	n := len(e.cols)
	e.rows = make([]*editRow, len(res.Rows))
	for i, row := range res.Rows {
		r := &editRow{
			strs:     make([]string, n),
			origStrs: make([]string, n),
			origNull: make([]bool, n),
			kinds:    make([]dbcore.ValueKind, n),
		}
		for d := 0; d < n; d++ {
			var v dbcore.Value
			if d < len(row) {
				v = row[d]
			}
			r.strs[d] = textCell(v)
			r.origStrs[d] = r.strs[d]
			r.origNull[d] = v.Kind == dbcore.ValNull
			r.kinds[d] = v.Kind
		}
		e.rows[i] = r
	}
	e.orig = cloneRows(e.rows)
}

// clear turns the grid back into a read-only result.
func (e *editState) clear() { *e = editState{} }

// dirty reports whether the working copy differs from the table. A row marked
// deleted is a difference, as committing it takes it out of the table.
func (e *editState) dirty() bool {
	for _, r := range e.rows {
		switch r.state {
		case rowInserted, rowDeleted:
			return true
		case rowModified:
			if r.changed() {
				return true
			}
		}
	}
	return false
}

// syncState promotes rows whose text changed but were never marked, so a
// change the grid took without a Changed report is not lost.
func (e *editState) syncState() {
	for _, r := range e.rows {
		if r.state == rowUntouched && r.changed() {
			r.state = rowModified
		}
	}
}

// markModified marks a row the user edited.
func (e *editState) markModified(row int) {
	if row < 0 || row >= len(e.rows) {
		return
	}
	if r := e.rows[row]; r.state == rowUntouched {
		r.state = rowModified
	}
}

// addRow appends a row of cells to the working copy, guessing each cell's kind
// from its column's type name.
func (e *editState) addRow(cells []string, state rowState) *editRow {
	n := len(e.cols)
	if len(cells) < n {
		cells = append(append([]string(nil), cells...), make([]string, n-len(cells))...)
	}
	r := &editRow{
		strs:     cells,
		origStrs: append([]string(nil), cells...),
		origNull: make([]bool, n),
		kinds:    make([]dbcore.ValueKind, n),
		state:    state,
	}
	for d := range r.kinds {
		// A row that was inserted, rather than read, starts with no NULLs to
		// mark: its blank cells are simply empty.
		r.kinds[d] = kindOf(e.cols[d].Type)
	}
	e.rows = append(e.rows, r)
	return r
}

// insertRow appends an empty row and makes the grid show it as inserted.
func (app *App) insertRow() {
	e := &app.edit
	if !e.active {
		return
	}
	e.addRow(nil, rowInserted)
	// The new row is at the end of the set; show it on its page.
	app.gotoRow(len(e.rows) - 1)
	app.invalidate()
}

// deleteRow marks the chosen row deleted, or drops it when it was inserted and
// so never reached the table. A deleted row stays in the grid, struck through,
// until the commit takes it out of the table.
func (app *App) deleteRow() {
	app.deleteRowAt(app.gridSel)
	if app.gridSel >= app.rowsShown() {
		app.gridSel = app.rowsShown() - 1
	}
	app.invalidate()
}

// deleteRowAt marks the row of the page deleted, the one a cell's menu named.
// The toolbar's Delete Row marks the row the grid's cursor is on, which is the
// same call with that row.
func (app *App) deleteRowAt(row int) {
	e := &app.edit
	if !e.active {
		return
	}
	i := app.pageStart() + row
	if i < 0 || i >= len(e.rows) {
		return
	}
	if e.rows[i].state == rowInserted {
		e.rows = append(e.rows[:i], e.rows[i+1:]...)
	} else {
		e.rows[i].state = rowDeleted
	}
}

// rollback throws the working copy away and shows the rows as they were read:
// inserted rows go, deleted rows come back, edits vanish.
func (app *App) rollback() {
	e := &app.edit
	if !e.active || !e.dirty() {
		return
	}
	e.rows = cloneRows(e.orig)
	app.gridSel = -1
	if app.pageStart() < len(e.rows) {
		app.gridSel = 0
	} else {
		app.gotoRow(len(e.rows) - 1)
	}
	app.invalidate()
}

// commit turns the working copy into one batch and applies it.
func (app *App) commit() {
	if app.readOnly() {
		app.errMsg = "Read-only connection: the working copy cannot be committed"
		app.status = "Blocked by read-only mode"
		app.invalidate()
		return
	}
	e := &app.edit
	if !e.active || !e.dirty() {
		return
	}
	// The kernel's batch writer commits its own transaction, so applying the
	// working copy with a connection transaction open would land half the work
	// inside it and half outside. The user has to finish one first.
	if app.txActive {
		app.errMsg = "A transaction is open; commit or roll it back before committing the working copy"
		app.status = "Cannot commit"
		app.invalidate()
		return
	}
	e.syncState()
	req, why := e.request()
	if why != "" {
		app.errMsg = why
		app.status = "Cannot commit"
		app.invalidate()
		return
	}
	app.beginBusy("edit")
	app.errMsg = ""
	app.status = "Committing…"
	app.invalidate()

	db := app.db
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		w, ok := dbcore.AsBatchWriter(db.Driver)
		if !ok {
			app.post(func() {
				app.endBusy("edit")
				app.errMsg = "This driver cannot commit a batch of edits"
				app.status = "Commit failed"
			})
			return
		}
		err := w.ApplyBatch(ctx, req)
		app.post(func() {
			app.endBusy("edit")
			if err != nil {
				// A row the kernel could not match is not a statement failure:
				// someone else edited or deleted it after this grid read it. Say
				// so, and keep the working copy so the user can compare and
				// re-apply instead of retyping the edits.
				if errors.Is(err, dbcore.ErrRowConflict) {
					app.errMsg = "Not committed: " + err.Error() +
						" — reload the rows to see the current values, then re-apply your edits"
					app.status = "Row changed by someone else"
					return
				}
				app.errMsg = "Commit failed: " + err.Error()
				app.status = "Commit failed"
				return
			}
			app.status = "Committed"
			if e.refresh != nil {
				e.refresh()
			}
		})
	}()
}

// request builds the batch the working copy asks for. It returns why when the
// rows cannot be addressed, leaving req unused.
func (e *editState) request() (dbcore.BatchRequest, string) {
	req := dbcore.BatchRequest{Table: e.table}
	for _, r := range e.rows {
		switch r.state {
		case rowDeleted:
			pk := e.pkOf(r)
			if pk == nil {
				return req, "A deleted row has no primary key; delete it from the table directly"
			}
			req.Deletes = append(req.Deletes, pk)
		case rowInserted:
			req.Inserts = append(req.Inserts, e.insertValues(r))
		case rowModified:
			pk := e.pkOf(r)
			if pk == nil {
				return req, "A changed row has no primary key; it cannot be updated"
			}
			ch := e.changesOf(r)
			if len(ch) == 0 {
				continue
			}
			req.Updates = append(req.Updates, dbcore.RowChange{
				PK:      pk,
				Changes: ch,
				// What the row held when it was read: the kernel puts these into
				// the UPDATE's WHERE, so a row another writer edited between the
				// read and this commit stops matching instead of being silently
				// overwritten (the lost-update window, Red Book ch13).
				Expect: e.expectOf(r, ch),
			})
		}
	}
	return req, ""
}

// pkOf is the primary key of a row, or nil when it has none or a key column is
// NULL — a row that cannot be addressed.
func (e *editState) pkOf(r *editRow) map[string]dbcore.Value {
	pk := map[string]dbcore.Value{}
	for d, c := range e.cols {
		if !c.IsPrimaryKey {
			continue
		}
		v := e.valueAt(r, d)
		if v.Kind == dbcore.ValNull {
			return nil
		}
		pk[c.Name] = v
	}
	if len(pk) == 0 {
		return nil
	}
	return pk
}

// expectOf is what the row's changed columns held when it was read, which is
// the optimistic guard for the UPDATE. A cell that read as NULL is guarded with
// IS NULL (the kernel renders it that way), not with the empty string its text
// form shows: on a nullable column those are different values.
func (e *editState) expectOf(r *editRow, changed map[string]dbcore.Value) map[string]dbcore.Value {
	exp := make(map[string]dbcore.Value, len(changed))
	for d, c := range e.cols {
		if _, isChanged := changed[c.Name]; !isChanged {
			continue
		}
		if d < len(r.origNull) && r.origNull[d] {
			exp[c.Name] = dbcore.Value{Kind: dbcore.ValNull}
			continue
		}
		if d >= len(r.origStrs) {
			continue
		}
		exp[c.Name] = parseCell(r.kinds[d], r.origStrs[d])
	}
	return exp
}

// changesOf is the columns of a row whose text differs from what was read.
func (e *editState) changesOf(r *editRow) map[string]dbcore.Value {
	ch := map[string]dbcore.Value{}
	for d, c := range e.cols {
		if d >= len(r.strs) || r.strs[d] == r.origStrs[d] {
			continue
		}
		ch[c.Name] = e.valueAt(r, d)
	}
	return ch
}

// insertValues is the row an insert sends: every column with a value, leaving
// identity columns to the table.
func (e *editState) insertValues(r *editRow) map[string]dbcore.Value {
	v := map[string]dbcore.Value{}
	for d, c := range e.cols {
		if c.IsAutoIncrement {
			continue
		}
		val := e.valueAt(r, d)
		if val.Kind == dbcore.ValNull {
			continue
		}
		v[c.Name] = val
	}
	return v
}

// valueAt parses a cell's text back into a Value of the cell's own kind.
func (e *editState) valueAt(r *editRow, d int) dbcore.Value {
	if d < 0 || d >= len(r.strs) {
		return dbcore.Value{Kind: dbcore.ValNull}
	}
	return parseCell(r.kinds[d], r.strs[d])
}

// mergeColumns takes the columns a result carries and fills in what the
// catalog knows about them: the primary key and identity flags a result set's
// own column metadata does not carry.
func mergeColumns(res, cached []dbcore.ColumnInfo) []dbcore.ColumnInfo {
	out := append([]dbcore.ColumnInfo(nil), res...)
	if len(out) == 0 {
		return append([]dbcore.ColumnInfo(nil), cached...)
	}
	byName := make(map[string]dbcore.ColumnInfo, len(cached))
	for _, c := range cached {
		byName[c.Name] = c
	}
	for i := range out {
		c, ok := byName[out[i].Name]
		if !ok {
			continue
		}
		if !out[i].IsPrimaryKey {
			out[i].IsPrimaryKey = c.IsPrimaryKey
		}
		if !out[i].IsAutoIncrement {
			out[i].IsAutoIncrement = c.IsAutoIncrement
		}
		if out[i].Table == "" {
			out[i].Table = c.Table
		}
	}
	return out
}

// textCell is what a cell shows: the value's text, or nothing for NULL. It
// delegates to the codec: the one spelling of "this value as editable text".
func textCell(v dbcore.Value) string {
	return sqlcodec.Text(v)
}

// parseCell turns edited text back into a Value of the cell's own kind. It
// delegates to the codec: the one rule for "this text as a value of this kind".
func parseCell(kind dbcore.ValueKind, s string) dbcore.Value {
	return sqlcodec.Cell(kind, s)
}

// kindOf guesses a ValueKind from a column's type name, for the cells of a row
// that was inserted rather than read. It delegates to the codec.
func kindOf(typeName string) dbcore.ValueKind {
	return sqlcodec.KindOf(typeName)
}

// simpleSelectStar matches the one query that carries a table's name and all of
// its columns: "SELECT * FROM t" or "SELECT * FROM s.t", with the row cap each
// engine spells differently. Anything else — a projection, a join, an aggregate
// — is left read-only.
var simpleSelectStar = regexp.MustCompile(`(?is)^\s*select\s+(?:top\s+\d+\s+|distinct\s+)?\*\s+from\s+([^\s;]+)\s*(?:limit\s+\d+|fetch\s+first\s+\d+\s+rows\s+only)?\s*;?\s*$`)

// tableOf reports the schema and table a "SELECT *" names, and whether the
// catalog knows it. A name without a schema uses the connection's default one.
func (app *App) tableOf(sql string) (schema, table string, ok bool) {
	m := simpleSelectStar.FindStringSubmatch(strings.TrimSpace(sql))
	if m == nil {
		return "", "", false
	}
	name := strings.Trim(m[1], `"`+"`")
	parts := strings.Split(name, ".")
	switch len(parts) {
	case 1:
		table = strings.Trim(parts[0], `"`+"`")
		if table == "" {
			return "", "", false
		}
		schema = app.defaultSchema()
	case 2:
		schema = strings.Trim(parts[0], `"`+"`")
		table = strings.Trim(parts[1], `"`+"`")
	default:
		return "", "", false
	}
	if table == "" {
		return "", "", false
	}
	schema = app.catalogSchema(schema)
	cols, _ := app.ColumnsOf(schema, table)
	if len(cols) == 0 {
		return "", "", false
	}
	return schema, table, true
}

// catalogSchema is the namespace a table's schema has in the catalog. SQLite
// has one namespace it addresses without a name, but the tree and the columns
// cache name it, so a bare name is put back to the catalog's.
func (app *App) catalogSchema(schema string) string {
	if app.conn.Kind == "sqlite" && schema == "" {
		return app.FirstSchema()
	}
	return schema
}

// defaultSchema is the namespace a bare table name belongs to: the working
// schema when one is chosen, else the connection's first namespace.
func (app *App) defaultSchema() string {
	if app.schema != "" {
		return app.schema
	}
	return app.FirstSchema()
}

// startEdit makes the grid the working copy of a table and shows it.
func (app *App) startEdit(res *dbcore.QueryResult, schema, table string, refresh func()) {
	if app.readOnly() {
		// A locked connection's grid stays a plain result: no working copy, so
		// no cell edit and no Commit to offer.
		app.errMsg = ""
		app.status = "Read-only connection: the grid is not editable here"
		app.edit.clear()
		app.beginEditMode()
		app.invalidate()
		return
	}
	cols, _ := app.ColumnsOf(schema, table)
	app.edit.init(res, schema, table, cols, refresh)
	app.gridSel = 0
	app.beginEditMode()
}

// stopEdit leaves the grid read-only.
func (app *App) stopEdit() {
	app.edit.clear()
	app.beginEditMode()
}

// commitOrRun is what Ctrl+Enter and the Run button do: commit the working copy
// when it has changes, and run the editor's SQL otherwise.
func (app *App) commitOrRun() {
	if app.edit.active && app.edit.dirty() {
		// TablePlus 的 Review Changes 交互：先预览全部差异，确认才提交
		// （Ctrl+Enter 与 Run 按钮同路径；面板里的 Confirm commit 再调
		// 本函数会先经过预览——由 openReview 设置 reviewOpen，confirm 里
		// 直接调 commit()）。
		app.openReview()
		return
	}
	app.runQuery()
}
