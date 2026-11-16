package main

// Copying the grid as SQL or CSV.
//
// The grid is a result set's rows, and the user often wants them as statements
// to paste somewhere else: an INSERT per row to load them back, an UPDATE or a
// DELETE to replay a change, or the rows as CSV for a spreadsheet. The
// statements are built from the result's own columns and rows — what the
// database returned. A working copy's uncommitted edits are not in it, so they
// are not copied; commit them first, as they would have to be for the change to
// exist.
//
// Only a table's working copy names a table: INSERT, UPDATE and DELETE address
// rows, and a JOIN or an aggregate has no single table to write back to — the
// same rule that makes the grid read-only. CSV needs no table and copies any
// result, including one that is not a table at all.
//
// The SQL is rendered in the dialect the connection speaks, so identifiers get
// the engine's own quoting and string literals are escaped as that engine
// parses them. QuotedLiteral and QuoteIdent are the two pieces of that.

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"strconv"
	"strings"

	"github.com/Ben-guanzhi/dbcore"
	"github.com/Ben-guanzhi/dbcore/sqltext"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"dbstudio-go/internal/sqlcodec"
)

// copyKind is what a copy action produces.
type copyKind int

const (
	copyInsert copyKind = iota
	copyUpdate
	copyDelete
	copyCSV
	copyTSV
)

// label is the name a kind shows in the status line.
func (k copyKind) label() string {
	switch k {
	case copyInsert:
		return "INSERT"
	case copyUpdate:
		return "UPDATE"
	case copyDelete:
		return "DELETE"
	case copyTSV:
		return "TSV"
	default:
		return "CSV"
	}
}

// copyTable is the table the grid's rows belong to, and the columns with their
// key flags; ok is false when the result is not one table's rows, which is what
// every statement but CSV needs. schema and table are the bare names, which a
// statement renders the way the dialect wants.
func (app *App) copyTable() (dbType, schema, table string, cols []dbcore.ColumnInfo, ok bool) {
	if !app.edit.active || app.edit.table == "" {
		return "", "", "", nil, false
	}
	return app.engine(), app.edit.schema, app.edit.table, app.edit.cols, true
}

// visibleCols narrows the table's columns to the ones the result shows, in the
// result's order. An UPDATE or DELETE needs its key to address a row, so a
// statement whose key column is hidden is refused rather than written without a
// WHERE that names it.
func visibleCols(all []dbcore.ColumnInfo, res *dbcore.QueryResult) ([]dbcore.ColumnInfo, error) {
	if res == nil {
		return all, nil
	}
	shown := make(map[string]bool, len(res.Columns))
	for _, c := range res.Columns {
		shown[c.Name] = true
	}
	out := make([]dbcore.ColumnInfo, 0, len(res.Columns))
	for _, c := range res.Columns {
		// The table's own entry carries the key and type; a projected column
		// keeps the name, so looking it up in the table's list is exact.
		cat := c
		for _, a := range all {
			if a.Name == c.Name {
				cat = a
				break
			}
		}
		out = append(out, cat)
	}
	// A hidden key would make a generated UPDATE or DELETE address no row.
	for _, a := range all {
		if a.IsPrimaryKey && !shown[a.Name] {
			return nil, fmt.Errorf("a key column (%s) is hidden; show it to copy as UPDATE or DELETE", a.Name)
		}
	}
	return out, nil
}

// engine is the dialect the connection speaks, for the quoting of generated SQL.
func (app *App) engine() string {
	if app.db != nil && app.db.Kind != "" {
		return string(app.db.Kind)
	}
	return app.conn.Kind
}

// valueAt reads column i of a row, which a short row leaves out as NULL.
func valueAt(row []dbcore.Value, i int) dbcore.Value {
	if i < 0 || i >= len(row) {
		return dbcore.Value{Kind: dbcore.ValNull}
	}
	return row[i]
}

// sqlLiteral renders a cell as the literal of its own kind in the dialect. It
// delegates to the codec: the one spelling of "this value as SQL text".
func sqlLiteral(dbType string, v dbcore.Value) string {
	return sqlcodec.Literal(dbType, v)
}

// renderTable is the table a statement writes to: quoted and, where the engine
// needs it, schema-qualified.
func renderTable(dbType, schema, table string) (string, error) {
	return sqltext.LocalName(dbType, schema, table)
}

// insertStatement renders one row as an INSERT of every column.
func insertStatement(dbType, schema, table string, cols []dbcore.ColumnInfo, row []dbcore.Value) (string, error) {
	name, err := renderTable(dbType, schema, table)
	if err != nil {
		return "", err
	}
	names := make([]string, len(cols))
	lits := make([]string, len(cols))
	for i, c := range cols {
		q, err := sqltext.QuoteIdent(dbType, c.Name)
		if err != nil {
			return "", err
		}
		names[i] = q
		lits[i] = sqlLiteral(dbType, valueAt(row, i))
	}
	return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s);", name,
		strings.Join(names, ", "), strings.Join(lits, ", ")), nil
}

// updateStatement renders one row as an UPDATE of its non-key columns, kept
// apart by its key. A table without a key has rows that cannot be addressed,
// which is the same limit the working copy's commit has.
func updateStatement(dbType, schema, table string, cols []dbcore.ColumnInfo, row []dbcore.Value) (string, error) {
	name, err := renderTable(dbType, schema, table)
	if err != nil {
		return "", err
	}
	var sets, where []string
	for i, c := range cols {
		q, err := sqltext.QuoteIdent(dbType, c.Name)
		if err != nil {
			return "", err
		}
		lit := sqlLiteral(dbType, valueAt(row, i))
		if c.IsPrimaryKey {
			where = append(where, q+" = "+lit)
			continue
		}
		sets = append(sets, q+" = "+lit)
	}
	if len(where) == 0 {
		return "", fmt.Errorf("the table has no primary key, so its rows cannot be updated")
	}
	return fmt.Sprintf("UPDATE %s SET %s WHERE %s;", name,
		strings.Join(sets, ", "), strings.Join(where, " AND ")), nil
}

// deleteStatement renders one row as a DELETE kept apart by its key.
func deleteStatement(dbType, schema, table string, cols []dbcore.ColumnInfo, row []dbcore.Value) (string, error) {
	name, err := renderTable(dbType, schema, table)
	if err != nil {
		return "", err
	}
	var where []string
	for i, c := range cols {
		if !c.IsPrimaryKey {
			continue
		}
		q, err := sqltext.QuoteIdent(dbType, c.Name)
		if err != nil {
			return "", err
		}
		where = append(where, q+" = "+sqlLiteral(dbType, valueAt(row, i)))
	}
	if len(where) == 0 {
		return "", fmt.Errorf("the table has no primary key, so its rows cannot be deleted")
	}
	return fmt.Sprintf("DELETE FROM %s WHERE %s;", name, strings.Join(where, " AND ")), nil
}

// statement renders one row in the way kind addresses rows.
func statement(kind copyKind, dbType, schema, table string, cols []dbcore.ColumnInfo, row []dbcore.Value) (string, error) {
	switch kind {
	case copyInsert:
		return insertStatement(dbType, schema, table, cols, row)
	case copyUpdate:
		return updateStatement(dbType, schema, table, cols, row)
	default:
		return deleteStatement(dbType, schema, table, cols, row)
	}
}

// delimitedText renders the result's rows with a delimiter: a header of the
// column names and one record per row, NULL left empty as a spreadsheet reads
// it. ',' is CSV; '\t' is TSV, the format logs and data pipes are written in.
func delimitedText(res *dbcore.QueryResult, comma rune) (string, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	w.Comma = comma

	hdr := make([]string, len(res.Columns))
	for i, c := range res.Columns {
		hdr[i] = c.Name
	}
	if err := w.Write(hdr); err != nil {
		return "", err
	}
	for _, row := range res.Rows {
		rec := make([]string, len(res.Columns))
		for i := range res.Columns {
			rec[i] = textCell(valueAt(row, i))
		}
		if err := w.Write(rec); err != nil {
			return "", err
		}
	}
	w.Flush()
	return buf.String(), w.Error()
}

// csvText 是逗号分隔的那一种。
func csvText(res *dbcore.QueryResult) (string, error) { return delimitedText(res, ',') }

// tsvText 是制表符分隔的那一种。
func tsvText(res *dbcore.QueryResult) (string, error) { return delimitedText(res, '\t') }

// copyText renders the rows named, all of them when rows is nil, as kind. The
// result's own columns and rows are what it renders, so a table's name comes
// from the working copy and a key from its columns.
func (app *App) copyText(kind copyKind, res *dbcore.QueryResult, rows []int) (string, error) {
	if kind == copyCSV {
		return csvText(res)
	}
	if kind == copyTSV {
		return tsvText(res)
	}
	dbType, schema, table, cols, ok := app.copyTable()
	if !ok {
		return "", fmt.Errorf("the result is not one table's rows, so it cannot be copied as %s", kind.label())
	}
	// The columns written are the ones on screen: a hidden column must not appear
	// in a statement the user copied from the grid.
	cols, err := visibleCols(cols, res)
	if err != nil {
		return "", err
	}
	n := len(res.Rows)
	if rows == nil {
		rows = make([]int, n)
		for i := range rows {
			rows[i] = i
		}
	}
	var out strings.Builder
	for i, idx := range rows {
		if idx < 0 || idx >= n {
			continue
		}
		stmt, err := statement(kind, dbType, schema, table, cols, res.Rows[idx])
		if err != nil {
			return "", err
		}
		if i > 0 {
			out.WriteByte('\n')
		}
		out.WriteString(stmt)
	}
	return out.String(), nil
}

// copySelected puts the grid's rows in kind on the clipboard, and reports what
// it did in the status line.
func (app *App) copySelected(kind copyKind, rows []int) {
	// The same source the export uses: the working copy's values (what the grid
	// shows) with the visible columns only.
	res := app.visibleResult(app.editResult(), app.tab())
	if res == nil {
		app.status = "Nothing to copy"
		app.invalidate()
		return
	}
	text, err := app.copyText(kind, res, rows)
	if err != nil {
		app.errMsg = "Copy failed: " + err.Error()
		app.status = ""
		app.invalidate()
		return
	}
	mygo.Clipboard.WriteText(text)
	app.errMsg = ""
	n := len(rows)
	if n == 0 {
		n = len(res.Rows)
	}
	app.status = fmt.Sprintf("Copied %s as %s", pluralRows(n),  kind.label())
	app.invalidate()
}

// copyAs copies every row of the grid.
func (app *App) copyAs(kind copyKind) { app.copySelected(kind, nil) }

// copyRow copies one row of the grid, the one its context menu opened on: the
// grid row is the page's, so the result's index is that far past the page's.
func (app *App) copyRow(row int, kind copyKind) {
	app.copySelected(kind, []int{app.pageStart() + row})
}

// copyCellValue copies the one cell a context menu opened on: a data column by
// its value, the "#" column by the row's number.
func (app *App) copyCellValue(row, col int) {
	res := app.tabResult()
	if res == nil {
		return
	}
	full := app.pageStart() + row
	if full < 0 || full >= len(res.Rows) {
		return
	}
	text := strconv.Itoa(full + 1)
	if col > 0 {
		if d := col - 1; d < len(res.Columns) && d < len(res.Rows[full]) {
			text = textCell(res.Rows[full][d])
		}
	}
	mygo.Clipboard.WriteText(text)
	app.errMsg = ""
	app.status = "Copied a cell"
	app.invalidate()
}

// gridCellMenu gives a cell of the grid its context menu: the cell itself, the
// row it sits in as a statement, and the whole grid as CSV. A result that is
// not a table's rows leaves the statements out, as it cannot name a table.
func (app *App) gridCellMenu(m *ui.Menu, row, col int) {
	res := app.tabResult()
	if res == nil {
		return
	}
	if m.Item("Copy Cell").Chosen() {
		app.copyCellValue(row, col)
		return
	}
	if m.Item("Edit Value…").Chosen() {
		app.openCell(row, col)
		return
	}
	_, _, _, _, tableOK := app.copyTable()
	if tableOK {
		m.Separator()
		if m.Item("Paste").Chosen() {
			app.pasteIntoCell(row, col)
			return
		}
		if m.Item("Delete Row").Chosen() {
			app.deleteRowAt(row)
			return
		}
	}
	m.Separator()
	if tableOK && m.Item("Copy Row as INSERT").Chosen() {
		app.copyRow(row, copyInsert)
		return
	}
	if tableOK && m.Item("Copy Row as UPDATE").Chosen() {
		app.copyRow(row, copyUpdate)
		return
	}
	if tableOK && m.Item("Copy Row as DELETE").Chosen() {
		app.copyRow(row, copyDelete)
		return
	}
	m.Separator()
	if m.Item("Copy Grid as CSV").Chosen() {
		app.copyAs(copyCSV)
		return
	}
	if tableOK && m.Item("Copy Grid as INSERT").Chosen() {
		app.copyAs(copyInsert)
	}
}
