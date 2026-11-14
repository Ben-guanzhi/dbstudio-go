// Package xlsx writes a minimal Excel workbook: one sheet, a header row, and
// the rows given.
//
// Enterprise data work ends in Excel, and DataGrip offers it beside CSV — so a
// result needs a way out that opens in Excel without a "text import" dialog,
// keeps numbers as numbers, and does not mangle the commas and quotes a CSV
// leaves ambiguous. A .xlsx is a zip of XML parts; writing the six parts it
// takes is smaller than depending on a spreadsheet library, and the parts that
// are written here are the ones a reader actually needs (content types, the
// package and workbook relationships, the workbook, the sheet, and a minimal
// style table).
//
// What is deliberately not written: shared strings (inline strings are legal
// and simpler), formulas, multiple sheets, merged cells, and anything a
// spreadsheet writes but a reader can live without. Column widths are written,
// because a column of names at the default width is unusable.
package xlsx

import (
	"archive/zip"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

// sheetName is the one sheet's name.
const sheetName = "Sheet1"

// maxCellRunes bounds one cell. Excel's own limit is 32767 characters; a value
// beyond it is cut rather than letting Excel refuse the file.
const maxCellRunes = 32767

// Write writes a workbook with one sheet: header as the first row (when it is
// not empty), then rows. A cell that parses as a number is written as a number,
// so Excel does not offer to convert a column of ids; everything else is an
// inline string.
func Write(w io.Writer, header []string, rows [][]string) error {
	zw := zip.NewWriter(w)
	if err := writePart(zw, "[Content_Types].xml", contentTypes); err != nil {
		return err
	}
	if err := writePart(zw, "_rels/.rels", packageRels); err != nil {
		return err
	}
	if err := writePart(zw, "xl/workbook.xml", workbook); err != nil {
		return err
	}
	if err := writePart(zw, "xl/_rels/workbook.xml.rels", workbookRels); err != nil {
		return err
	}
	if err := writePart(zw, "xl/styles.xml", styles); err != nil {
		return err
	}
	if err := writeSheet(zw, header, rows); err != nil {
		return err
	}
	return zw.Close()
}

// writePart adds one XML part from a constant.
func writePart(zw *zip.Writer, name, body string) error {
	f, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = io.WriteString(f, body)
	return err
}

// writeSheet streams the sheet part: the rows are written as they come, so a
// large export does not hold a second copy of itself in memory as XML.
func writeSheet(zw *zip.Writer, header []string, rows [][]string) error {
	f, err := zw.Create("xl/worksheets/sheet1.xml")
	if err != nil {
		return err
	}
	bw := &errWriter{w: f}
	bw.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	bw.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`)
	// The header row is frozen and bold, and the columns wide enough to read.
	if len(header) > 0 {
		bw.WriteString(`<sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews>`)
	}
	bw.WriteString(`<cols>`)
	for i, name := range header {
		width := columnWidth(name, rows, i)
		fmt.Fprintf(bw, `<col min="%d" max="%d" width="%.1f" customWidth="1"/>`, i+1, i+1, width)
	}
	bw.WriteString(`</cols><sheetData>`)

	row := 0
	if len(header) > 0 {
		row++
		fmt.Fprintf(bw, `<row r="%d">`, row)
		for i, name := range header {
			writeCell(bw, i, row, name, true)
		}
		bw.WriteString(`</row>`)
	}
	for _, r := range rows {
		row++
		fmt.Fprintf(bw, `<row r="%d">`, row)
		for i, cell := range r {
			writeCell(bw, i, row, cell, false)
		}
		bw.WriteString(`</row>`)
	}
	bw.WriteString(`</sheetData></worksheet>`)
	if bw.err != nil {
		return bw.err
	}
	return nil
}

// writeCell writes one cell: a number when the text is one, otherwise an inline
// string. The header is always a string, even when it reads "2024".
func writeCell(bw *errWriter, col, row int, text string, header bool) {
	if text == "" && !header {
		// A blank cell is an absent cell: Excel reads the gap as empty, and the
		// sheet does not carry thousands of empty string cells.
		return
	}
	ref := columnName(col) + strconv.Itoa(row)
	if n, ok := number(text); ok && !header {
		fmt.Fprintf(bw, `<c r="%s"><v>%s</v></c>`, ref, n)
		return
	}
	style := ""
	if header {
		style = ` s="1"`
	}
	fmt.Fprintf(bw, `<c r="%s" t="inlineStr"%s><is><t xml:space="preserve">%s</t></is></c>`,
		ref, style, escape(text))
}

// number reports whether text is a number Excel should keep as one, and its
// canonical spelling. A value with a leading zero ("007", a phone number, a
// zip code) is not a number here: turning it into 7 is the classic spreadsheet
// data loss.
func number(text string) (string, bool) {
	s := strings.TrimSpace(text)
	if s == "" || s != text {
		return "", false
	}
	if len(s) > 1 && (s[0] == '0' && s[1] != '.') {
		return "", false
	}
	if len(s) > 1 && (s[0] == '-' && len(s) > 2 && s[1] == '0' && s[2] != '.') {
		return "", false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
		return "", false
	}
	// Excel keeps 15 significant digits: a value beyond that is a string, or
	// Excel would round it and the file would disagree with the database.
	if math.Abs(f) >= 1e15 {
		return "", false
	}
	// A scientific spelling ("1e3") is a number to Go and not necessarily to
	// OOXML, which wants a decimal: write the plain one.
	if strings.ContainsAny(s, "eE") {
		s = strconv.FormatFloat(f, 'f', -1, 64)
	}
	return s, true
}

// columnWidth is a column's width in Excel's character units: the widest of the
// header and the first rows, clamped to something a reader can use.
func columnWidth(header string, rows [][]string, col int) float64 {
	widest := len([]rune(header))
	for i, r := range rows {
		if i >= 200 || col >= len(r) {
			continue
		}
		if n := len([]rune(r[col])); n > widest {
			widest = n
		}
	}
	w := float64(widest) + 2
	if w < 8 {
		w = 8
	}
	if w > 60 {
		w = 60
	}
	return w
}

// columnName is a zero-based column's letters: A, B, …, Z, AA, AB, ….
func columnName(col int) string {
	name := ""
	for col >= 0 {
		name = string(rune('A'+col%26)) + name
		col = col/26 - 1
	}
	return name
}

// escape writes text as XML character data. Every rune is written as itself
// (Excel reads UTF-8), except the five XML metacharacters; control characters
// XML forbids are dropped.
func escape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		case '\'':
			b.WriteString("&apos;")
		case '\t', '\n', '\r':
			b.WriteString("&#" + strconv.Itoa(int(r)) + ";")
		default:
			if r < 0x20 || r == utf8.RuneError {
				continue // XML has no representation for these
			}
			b.WriteRune(r)
		}
	}
	out := b.String()
	if n := len([]rune(out)); n > maxCellRunes {
		out = string([]rune(out)[:maxCellRunes])
	}
	return out
}

// errWriter records the first write error and stops writing, so the sheet's
// many small writes need one error check.
type errWriter struct {
	w   io.Writer
	err error
}

func (e *errWriter) WriteString(s string) {
	if e.err != nil {
		return
	}
	_, e.err = io.WriteString(e.w, s)
}

func (e *errWriter) Write(p []byte) (int, error) {
	if e.err != nil {
		return 0, e.err
	}
	n, err := e.w.Write(p)
	e.err = err
	return n, err
}

// The parts, minimal but complete enough for Excel, LibreOffice and Numbers.

const contentTypes = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>
<Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>
<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>
</Types>`

const packageRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>
</Relationships>`

const workbook = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
<sheets><sheet name="Sheet1" sheetId="1" r:id="rId1"/></sheets>
</workbook>`

const workbookRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>
<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>
</Relationships>`

// styles is the minimum a workbook needs: one font, one fill, one border, one
// cell format, the bold font the header row uses, and the default cell style a
// reader looks for (openpyxl warns without it, and Excel's own files carry it).
const styles = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
<fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><name val="Calibri"/></font></fonts>
<fills count="1"><fill><patternFill patternType="none"/></fill></fills>
<borders count="1"><border/></borders>
<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>
<cellXfs count="2"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/><xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1"/></cellXfs>
<cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles>
</styleSheet>`
