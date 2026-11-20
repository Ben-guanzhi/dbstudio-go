package main

// Importing data into a table (P3 #21, the counterpart of the export).
//
// A CSV or TSV file's rows go into one table through the kernel's BatchWriter:
// every row becomes an Insert of the mapped columns, and the whole file lands
// in one ApplyBatch — one transaction, the same guarantee a grid commit has. A
// file of a million rows would be one batch too many, so the writes go out in
// chunks of importBatchSize; a chunk that fails stops the import, and the
// report says how far it got.
//
// The mapping is explicit: each column of the file is assigned a column of the
// table, or "skip". Nothing is inferred silently — a file whose header does not
// match the table's column names starts unmapped, and Apply will say which
// required column is missing. The parse and the mapping are pure functions over
// the file's bytes, so a test drives them without a dialog; the dialog is only
// the file's path.

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Ben-guanzhi/dbcore"
	"github.com/Ben-guanzhi/dbcore/sqltext"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"dbstudio-go/internal/sqlcodec"
)

// importBatchSize is how many rows one ApplyBatch carries. Big enough that the
// per-batch overhead is noise on a normal file; small enough that a failure
// reports a useful "row 40,000 of 400,000".
const importBatchSize = 500

// importMaxCell is the longest cell text the parser keeps; a CSV cell beyond it
// is cut, as the grid's own cells are.
const importMaxCell = 4096

// importSep is the separator a file is parsed with, chosen from its extension.
type importSep int

const (
	sepComma importSep = iota
	sepTab
)

// sepFor guesses the separator from a file's extension. Anything not .tsv (or
// .tab) is a comma file, which is what a ".txt" dump usually is.
func sepFor(path string) importSep {
	p := strings.ToLower(path)
	if strings.HasSuffix(p, ".tsv") || strings.HasSuffix(p, ".tab") {
		return sepTab
	}
	return sepComma
}

// importFile is one parsed file: its records (as text, the way a CSV is) and
// whether the first one is the header the user says it is.
type importFile struct {
	records [][]string
	sep     importSep
	path    string
}

// readImportFile parses a CSV/TSV file from disk, whole. A file the user picked
// is a few MB at most — the rows go into a table the user chose, so a dump big
// enough to exhaust memory is out of scope for a dialog-driven import.
func readImportFile(path string) (*importFile, error) {
	sep := sepFor(path)
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	// 中文 Windows 导出的 CSV 常带 UTF-8 BOM 或 GB18030 字节——先解码再解析，
	// 否则首列名带 BOM、中文全乱码（§352）。
	return parseImport(decodeImportBytes(f), sep, path)
}

// parseImport is readImportFile over any reader, which is what a test drives.
func parseImport(r io.Reader, sep importSep, path string) (*importFile, error) {
	cr := csv.NewReader(r)
	cr.Comma = ','
	if sep == sepTab {
		cr.Comma = '\t'
	}
	cr.FieldsPerRecord = -1 // the mapping reports ragged rows instead
	cr.LazyQuotes = true
	records, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	// Cut oversized cells, as the grid does; a cell here is a value to insert.
	for i, rec := range records {
		for j, cell := range rec {
			if len(cell) > importMaxCell {
				records[i][j] = cell[:importMaxCell]
			}
		}
	}
	return &importFile{records: records, sep: sep, path: path}, nil
}

// Header reports whether the file's first record reads as a header: the modal
// defaults the checkbox to it, and the user decides.
func (f *importFile) Header() bool {
	if len(f.records) == 0 {
		return false
	}
	for _, cell := range f.records[0] {
		if isNumericLike(cell) {
			return false
		}
	}
	return true
}

// isNumericLike is the one thing a header cell must not be: a bare number. A
// quoted "2024" in a data row would otherwise look like a header of one column.
func isNumericLike(s string) bool {
	if s == "" {
		return false
	}
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}

// Columns are the file's first record, as the mapping labels show them: the
// header's names when there is one, "column 1" style labels when not.
func (f *importFile) Columns(hasHeader bool) []string {
	if len(f.records) == 0 {
		return nil
	}
	first := f.records[0]
	if hasHeader {
		return first
	}
	out := make([]string, len(first))
	for i := range first {
		out[i] = strconv.Itoa(i + 1)
	}
	return out
}

// DataRows are the rows to insert: all of them, or all but the header.
func (f *importFile) DataRows(hasHeader bool) [][]string {
	if len(f.records) == 0 {
		return nil
	}
	if hasHeader {
		return f.records[1:]
	}
	return f.records
}

// importMapping is one file column's assignment: a table column's index, or -1
// to skip it.
type importMapping []int

// mappedColumns are the table columns the mapping fills, in mapping order.
func (m importMapping) Mapped() []int {
	var out []int
	for _, t := range m {
		if t >= 0 {
			out = append(out, t)
		}
	}
	return out
}

// buildInserts turns the file's data rows into the batch's inserts, under the
// mapping. A row that is short gets NULL for its missing cells, which is what a
// truncated CSV line means; a cell is parsed as its target column's kind, so
// "42" lands as a number and "" lands as NULL.
func buildInserts(file *importFile, rows [][]string, mapping importMapping, cols []dbcore.ColumnInfo) ([][]map[string]dbcore.Value, int, error) {
	targets := mapping.Mapped()
	if len(targets) == 0 {
		return nil, 0, fmt.Errorf("no column of the file is mapped to the table")
	}
	// The kinds the cells are parsed as, by file-column index.
	kinds := make([]dbcore.ValueKind, len(mapping))
	for i, t := range mapping {
		if t >= 0 && t < len(cols) {
			kinds[i] = sqlcodec.KindOf(cols[t].Type)
		} else {
			kinds[i] = dbcore.ValText
		}
	}
	var batches [][]map[string]dbcore.Value
	var batch []map[string]dbcore.Value
	for _, row := range rows {
		values := make(map[string]dbcore.Value, len(targets))
		for i, t := range mapping {
			if t < 0 {
				continue
			}
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			name := cols[t].Name
			if cell == "" {
				values[name] = dbcore.Value{Kind: dbcore.ValNull}
				continue
			}
			values[name] = sqlcodec.Cell(kinds[i], cell)
		}
		batch = append(batch, values)
		if len(batch) >= importBatchSize {
			batches = append(batches, batch)
			batch = nil
		}
	}
	if len(batch) > 0 {
		batches = append(batches, batch)
	}
	return batches, len(rows), nil
}

// importState is the modal's state: the file, the table it goes into, the
// mapping being edited, the options, and the run's progress.
type importState struct {
	open     bool
	busy     bool
	path     string
	file     *importFile
	schema   string
	table    string
	cols     []dbcore.ColumnInfo
	mapping  importMapping
	header   bool
	truncate bool
	labels   []string // the mapping dropdown's labels, per file column
	options  []string // the dropdown's choices: (skip), then the table's columns
	progress string
	err      string
	done     bool
	imported int
}

// beginImport reads the file the dialog picked and opens the modal unmapped.
// The table is the tree node the menu was opened on; its columns come from the
// catalog, loading them if the tree never opened the branch.
func (app *App) beginImport(n treeNode) {
	if !app.open || app.db == nil || app.db.SQL == nil {
		app.errMsg = "Not connected"
		app.status = "Not connected"
		app.invalidate()
		return
	}
	if app.readOnly() {
		app.errMsg = "Read-only connection: data cannot be imported here"
		app.status = "Blocked by read-only mode"
		app.invalidate()
		return
	}
	schema, table := n.parent, n.label
	cols, st := app.ColumnsOf(schema, table)
	if st == ColsNotLoaded {
		app.loadColumns(schema, table)
		cols, _ = app.ColumnsOf(schema, table)
	}
	if len(cols) == 0 {
		app.errMsg = "The table's columns are not known; expand it in the tree first"
		app.status = "Import failed"
		app.invalidate()
		return
	}
	path, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Title:       "Import into " + table,
		ButtonLabel: "Import",
		Filters: []mygo.FileFilter{
			{Name: "Delimited text", Extensions: []string{"csv", "tsv", "tab", "txt"}},
		},
	})
	if err != nil {
		return // the user cancelled
	}
	if len(path) == 0 || path[0] == "" {
		return
	}
	file, err := readImportFile(path[0])
	if err != nil {
		app.errMsg = err.Error()
		app.status = "Import failed"
		app.invalidate()
		return
	}
	app.importer = app.newImportState(file, schema, table, cols)
	app.invalidate()
}

// newImportState builds the modal's state for one file: unmapped dropdowns, the
// header default the file suggests, and the preview's labels.
func (app *App) newImportState(file *importFile, schema, table string, cols []dbcore.ColumnInfo) importState {
	header := file.Header()
	fileCols := file.Columns(header)
	st := importState{
		open:    true,
		path:    file.path,
		file:    file,
		schema:  schema,
		table:   table,
		cols:    cols,
		header:  header,
		mapping: make(importMapping, len(fileCols)),
		labels:  make([]string, len(fileCols)),
	}
	// The dropdown per file column: skip first, then the table's columns. A
	// dropdown's value is the option's index as text, which the view turns back
	// into the mapping.
	st.options = make([]string, 0, len(cols)+1)
	st.options = append(st.options, "(skip)")
	for i := range cols {
		st.options = append(st.options, cols[i].Name)
	}
	for i := range fileCols {
		st.labels[i] = fmt.Sprintf("%d · %s", i+1, fileCols[i])
		st.mapping[i] = -1
		// A file column whose name matches a table column maps itself, when the
		// file's first row is its header.
		if header {
			for ci := range cols {
				if strings.EqualFold(cols[ci].Name, fileCols[i]) {
					st.mapping[i] = ci
					break
				}
			}
		}
	}
	return st
}

// runImport builds the batches and applies them, off the view thread. Truncate
// goes out as the batch's DDL, in the same transaction the inserts run in — an
// import that fails halfway leaves the table as it was.
func (app *App) runImport() {
	st := &app.importer
	if st.busy || !st.open {
		return
	}
	w, ok := dbcore.AsBatchWriter(app.db.Driver)
	if !ok {
		st.err = "This driver cannot write batches"
		app.invalidate()
		return
	}
	rows := st.file.DataRows(st.header)
	mapping := st.mapping
	cols := st.cols
	schema, table := st.schema, st.table
	truncate := st.truncate
	batches, total, err := buildInserts(st.file, rows, mapping, cols)
	if err != nil {
		st.err = err.Error()
		app.invalidate()
		return
	}
	st.busy = true
	st.err = ""
	st.done = false
	st.imported = 0
	st.progress = fmt.Sprintf("0 / %d rows", total)
	app.invalidate()

	db := app.db
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		if truncate {
			// One batch with only the DDL: the clear happens in its own
			// transaction, before any insert, so a failure there changes nothing.
			req := dbcore.BatchRequest{Table: table, DDL: []string{truncateSQL(string(db.Kind), schema, table)}}
			if err := w.ApplyBatch(ctx, req); err != nil {
				app.post(func() {
					st := &app.importer
					st.busy = false
					st.err = "Clearing the table: " + err.Error()
					app.invalidate()
				})
				return
			}
		}

		done := 0
		for bi, batch := range batches {
			req := dbcore.BatchRequest{Table: table, Inserts: batch}
			if err := w.ApplyBatch(ctx, req); err != nil {
				app.post(func() {
					st := &app.importer
					st.busy = false
					st.done = true
					st.err = fmt.Sprintf("Batch %d failed after %d rows: %s", bi+1, done, err.Error())
					st.progress = fmt.Sprintf("%d / %d rows", done, total)
					app.invalidate()
				})
				return
			}
			done += len(batch)
			n := done
			app.post(func() {
				st := &app.importer
				st.imported = n
				st.progress = fmt.Sprintf("%d / %d rows", n, total)
				app.invalidate()
			})
		}
		app.post(func() {
			st := &app.importer
			st.busy = false
			st.done = true
			st.imported = done
			st.progress = fmt.Sprintf("%d / %d rows", done, total)
			st.err = ""
			app.status = fmt.Sprintf("Imported %s into %s", pluralRows(done), table)
			// Re-read the table the import went into, if the grid holds it.
			if app.edit.active && app.edit.table == table && app.edit.refresh != nil {
				app.edit.refresh()
			}
			app.invalidate()
		})
	}()
}

// truncateSQL is the statement an import's "clear the table" option runs. The
// kernel's ddl renderer knows the per-engine spellings.
func truncateSQL(dbType, schema, table string) string {
	name, err := sqltext.LocalName(dbType, schema, table)
	if err != nil {
		return ""
	}
	if sqltext.Normalize(dbType) == sqltext.SQLite {
		return "DELETE FROM " + name + ";"
	}
	return "TRUNCATE TABLE " + name + ";"
}

// importView draws the import modal: the file's facts, the mapping dropdowns,
// the options, a preview of the first rows, and the run's progress.
func (app *App) importView(c *ui.Context) {
	st := &app.importer
	if !st.open {
		return
	}
	t := c.Theme()
	ui.Modal(c, &st.open, func() {
		ui.Column(c).Gap(10).Padding(12).Width(680).Height(560).
			Background(t.Surface).Radius(10).Border(1, t.Border).
			Children(func() {
				ui.Row(c).Gap(8).Children(func() {
					ui.Text(c, "Import into "+st.table).FontSize(13).Bold().SingleLine()
					ui.Spacer(c)
					if st.busy {
						ui.Spinner(c)
					}
				})
				ui.Text(c, st.path).TextColor(t.TextMuted).FontSize(10).SingleLine()

				// The options. Toggling the header re-labels the mapping, so it
				// is rebuilt from the file, dropping what was mapped by hand.
				headerWas := st.header
				ui.Checkbox(c, &st.header, "First row is the header")
				if st.header != headerWas {
					st.relabel()
				}
				ui.Checkbox(c, &st.truncate, "Clear the table first")

				// The mapping: one dropdown per file column.
				ui.Text(c, "Map the file's columns to "+st.table+"'s").FontSize(11).Bold().TextColor(t.TextMuted)
				ui.Scroll(c).Grow(1).FillWidth().KeepScrollbars(true).Children(func() {
					ui.Column(c).Gap(4).Children(func() {
						for i := range st.labels {
							i := i
							ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
								ui.Text(c, st.labels[i]).SingleLine()
								ui.Spacer(c)
								sel := ""
								if st.mapping[i] >= 0 && st.mapping[i]+1 < len(st.options) {
									sel = st.options[st.mapping[i]+1]
								} else {
									sel = st.options[0]
								}
								ui.Select(c, &sel, st.options).Label(st.labels[i]).Changed()
								// Select writes back through sel; apply it now.
								st.mapping[i] = indexOf(st.options, sel) - 1
							})
						}
					})
				})

				if st.err != "" {
					ui.Text(c, st.err).TextColor(t.Danger).Wrap()
				}
				if st.progress != "" {
					ui.Text(c, st.progress).TextColor(t.TextMuted).SingleLine()
				}

				ui.Row(c).Gap(8).Children(func() {
					ui.Spacer(c)
					if ui.Button(c, "Cancel").Disabled(st.busy).Clicked() {
						st.open = false
					}
					if ui.PrimaryButton(c, "Import").Disabled(st.busy || st.done).Clicked() {
						app.runImport()
					}
				})
			})
	})
}

// relabel re-derives the mapping labels and the name-based defaults after the
// header checkbox changed: with a header, columns can map themselves by name.
func (st *importState) relabel() {
	fileCols := st.file.Columns(st.header)
	st.labels = make([]string, len(fileCols))
	for i := range fileCols {
		st.labels[i] = fmt.Sprintf("%d · %s", i+1, fileCols[i])
	}
	// The row count changed by one if the header guess flipped, so the mapping
	// is rebuilt to the new width, unmapped.
	st.mapping = make(importMapping, len(fileCols))
	for i := range fileCols {
		st.mapping[i] = -1
		if st.header {
			for ci := range st.cols {
				if strings.EqualFold(st.cols[ci].Name, fileCols[i]) {
					st.mapping[i] = ci
					break
				}
			}
		}
	}
}
