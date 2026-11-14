package main

// Exporting a result to a file (P2 #17): CSV was the only format the toolbar's
// Export button knew; now the result can go out as CSV, JSON or an INSERT
// script. The working copy (what the user sees, including inserted rows and
// omitted deleted ones) is what a grid export writes, exactly as the old CSV
// export did; an INSERT needs the result to be one table's rows, which a browse
// or `SELECT * FROM t` is, and which the copy-as-INSERT code has always known how
// to tell apart from an arbitrary projection.
//
// The rendering is shared with the copy-as machinery so a script and a clipboard
// paste cannot drift: JSON is rendered here because nothing copies it yet, but
// INSERT reuses statement(), and CSV reuses csvText().

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dbcore/dbcore"

	"dbstudio-go/internal/sqlcodec"
	"dbstudio-go/internal/xlsx"
)

// exportFormat names a file format the Export button writes.
type exportFormat int

const (
	exportCSV exportFormat = iota
	exportJSON
	exportINSERT
	exportExcel
)

// label is the format's display name and its file extension follows lower()
// (csv / json / sql / xlsx).
func (f exportFormat) label() string {
	switch f {
	case exportJSON:
		return "JSON"
	case exportINSERT:
		return "INSERT"
	case exportExcel:
		return "Excel"
	default:
		return "CSV"
	}
}

func (f exportFormat) ext() string {
	switch f {
	case exportJSON:
		return ".json"
	case exportINSERT:
		return ".sql"
	case exportExcel:
		return ".xlsx"
	default:
		return ".csv"
	}
}

// exportResult snaps the current result (the working copy when the grid is one)
// and writes it in format to a new file beside the saved connections, as the
// toolbar Export button asks. It runs the file write off the view thread and
// reports where the file went in the status line.
func (app *App) exportResult(format exportFormat) {
	if app.result == nil {
		app.status = "Nothing to export"
		app.invalidate()
		return
	}
	// What the grid shows, not what it hides: the file holds the visible columns
	// in the order they are drawn.
	res := app.visibleResult(app.editResult(), app.tab())

	// Rendering JSON can error only on values the result cannot produce, but it
	// must happen before the goroutine, as it reads the grid's state.
	var data []byte
	var renderErr error
	switch format {
	case exportJSON:
		data, renderErr = app.jsonExport(res)
	case exportCSV:
		var s string
		s, renderErr = csvText(res)
		data = []byte(s)
	case exportExcel:
		data, renderErr = excelBytes(res)
	default:
		var s string
		s, renderErr = app.insertScript(res)
		data = []byte(s)
	}
	if renderErr != nil {
		app.errMsg = "Export failed: " + renderErr.Error()
		app.status = ""
		app.invalidate()
		return
	}

	app.status = "Exporting…"
	dir := app.store.Dir()
	ext := format.ext()
	go func() {
		name := "query-" + time.Now().Format("20060102-150405") + ext
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			app.post(func() {
				app.errMsg = "Export failed: " + err.Error()
				app.status = ""
			})
			return
		}
		app.post(func() {
			app.errMsg = ""
			app.status = "Exported " + name
		})
	}()
}

// excelBytes renders a result as a workbook. Excel is where a data export
// usually ends up, so it is offered beside CSV: numbers stay numbers, no comma
// or quote needs escaping, and the columns come out wide enough to read.
func excelBytes(res *dbcore.QueryResult) ([]byte, error) {
	header := make([]string, len(res.Columns))
	for i, c := range res.Columns {
		header[i] = c.Name
	}
	rows := make([][]string, len(res.Rows))
	for r, row := range res.Rows {
		cells := make([]string, len(row))
		for c, v := range row {
			cells[c] = sqlcodec.Text(v)
		}
		rows[r] = cells
	}
	var buf bytes.Buffer
	if err := xlsx.Write(&buf, header, rows); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// editResult is the result's rows, with the working copy's edits folded in when
// the grid is one. The result's own typed rows are kept so numbers stay numbers
// in JSON and INSERT; when the grid edits the table, each cell is re-parsed from
// the working copy's text in the cell's own kind.
func (app *App) editResult() *dbcore.QueryResult {
	res := app.result
	if res == nil {
		return nil
	}
	out := &dbcore.QueryResult{Columns: res.Columns}
	if app.edit.active && len(app.edit.rows) > 0 {
		out.Rows = make([][]dbcore.Value, 0, len(app.edit.rows))
		for _, r := range app.edit.rows {
			if r.state == rowDeleted {
				continue
			}
			vals := make([]dbcore.Value, len(r.strs))
			for i := range r.strs {
				vals[i] = app.edit.valueAt(r, i)
			}
			out.Rows = append(out.Rows, vals)
		}
	} else {
		out.Rows = res.Rows
	}
	return out
}

// jsonExport renders the result as a JSON array of objects, one per row. NULL is
// JSON null; a blob is a hex string; text and time keep their text forms so a
// round trip through a file does not mangle the value.
func (app *App) jsonExport(res *dbcore.QueryResult) ([]byte, error) {
	if res == nil {
		return []byte("[]"), nil
	}
	objects := make([]map[string]interface{}, 0, len(res.Rows))
	for _, row := range res.Rows {
		obj := make(map[string]interface{}, len(res.Columns))
		for i, col := range res.Columns {
			obj[col.Name] = jsonValue(valueAt(row, i))
		}
		objects = append(objects, obj)
	}
	return json.MarshalIndent(objects, "", "  ")
}

// jsonValue converts a cell to the JSON value a file export writes. It
// delegates to the codec: the X'..' blob spelling is the one Literal uses, so a
// JSON export and an INSERT export of one cell agree.
func jsonValue(v dbcore.Value) interface{} {
	return sqlcodec.JSON(v)
}

// insertScript renders every row as an INSERT statement, one per line. It
// requires the result to be one table's rows, which copy-as-INSERT already
// required; the error message names the need the same way it does.
func (app *App) insertScript(res *dbcore.QueryResult) (string, error) {
	if res == nil {
		return "", nil
	}
	dbType, schema, table, cols, ok := app.copyTable()
	if !ok {
		return "", fmt.Errorf("the result is not one table's rows, so it cannot be exported as INSERT")
	}
	// The visible columns, in the order shown: a hidden column is not exported.
	cols, err := visibleCols(cols, res)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for i, row := range res.Rows {
		if i > 0 {
			b.WriteByte('\n')
		}
		stmt, err := insertStatement(dbType, schema, table, cols, row)
		if err != nil {
			return "", err
		}
		b.WriteString(stmt)
	}
	return b.String(), nil
}
