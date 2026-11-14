package xlsx

// Tests for the workbook writer: the parts a reader needs, the cell types that
// keep a spreadsheet honest (numbers as numbers, but never "007" as 7), and the
// escaping that keeps a quote or an ampersand from breaking the XML.

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"strings"
	"testing"
)

// readParts unzips the workbook and returns its parts by name.
func readParts(t *testing.T, data []byte) map[string]string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("the workbook is not a zip: %v", err)
	}
	out := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", f.Name, err)
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read %s: %v", f.Name, err)
		}
		out[f.Name] = string(b)
	}
	return out
}

// Every part a reader needs is there, and each XML part parses: a workbook that
// Excel refuses is worse than no export at all.
func TestWorkbookHasRequiredParts(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, []string{"id", "name"}, [][]string{{"1", "ada"}}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	parts := readParts(t, buf.Bytes())
	for _, name := range []string{
		"[Content_Types].xml",
		"_rels/.rels",
		"xl/workbook.xml",
		"xl/_rels/workbook.xml.rels",
		"xl/styles.xml",
		"xl/worksheets/sheet1.xml",
	} {
		if _, ok := parts[name]; !ok {
			t.Errorf("the workbook is missing %s; parts = %v", name, keys(parts))
		}
	}
	for name, body := range parts {
		dec := xml.NewDecoder(strings.NewReader(body))
		for {
			_, err := dec.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("%s is not well-formed XML: %v", name, err)
			}
		}
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// The sheet holds the header and the rows, and a number stays a number while a
// leading-zero code stays text: "7" as a number, "007" as a string.
func TestCellTypes(t *testing.T) {
	var buf bytes.Buffer
	err := Write(&buf,
		[]string{"id", "code", "amount", "empty"},
		[][]string{
			{"7", "007", "12.50", ""},
			{"-3", "0.5", "1e3", "x"},
		})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	sheet := readParts(t, buf.Bytes())["xl/worksheets/sheet1.xml"]

	// The number cells have no inline string: <c r="A2"><v>7</v></c>.
	if !strings.Contains(sheet, `<c r="A2"><v>7</v></c>`) {
		t.Errorf("the id cell is not a number:\n%s", sheet)
	}
	// The leading-zero code is text.
	if !strings.Contains(sheet, `r="B2" t="inlineStr"`) {
		t.Errorf("007 was not written as text:\n%s", sheet)
	}
	if !strings.Contains(sheet, `<t xml:space="preserve">007</t>`) {
		t.Errorf("the leading zero was lost:\n%s", sheet)
	}
	// A decimal keeps its spelling; Excel reads it as a number.
	if !strings.Contains(sheet, `<c r="C2"><v>12.50</v></c>`) {
		t.Errorf("the decimal cell is not numeric:\n%s", sheet)
	}
	// An empty cell is simply absent (no <c> for it), which Excel reads as blank.
	if strings.Contains(sheet, `r="D2"`) {
		t.Errorf("an empty cell was written:\n%s", sheet)
	}
	// The header row is bold: s="1".
	if !strings.Contains(sheet, `<c r="A1" t="inlineStr" s="1">`) {
		t.Errorf("the header is not bold:\n%s", sheet)
	}
}

// Metacharacters and control characters cannot break the XML, and a newline
// survives as a character reference.
func TestEscaping(t *testing.T) {
	var buf bytes.Buffer
	err := Write(&buf, []string{"v"}, [][]string{
		{`a & b < c > d "e" 'f'`},
		{"line\nbreak"},
		{"bell\x07char"},
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	sheet := readParts(t, buf.Bytes())["xl/worksheets/sheet1.xml"]
	if !strings.Contains(sheet, "a &amp; b &lt; c &gt; d &quot;e&quot; &apos;f&apos;") {
		t.Errorf("metacharacters were not escaped:\n%s", sheet)
	}
	if !strings.Contains(sheet, "line&#10;break") {
		t.Errorf("a newline was not preserved as a reference:\n%s", sheet)
	}
	if strings.Contains(sheet, "\x07") {
		t.Errorf("a control character reached the XML:\n%s", sheet)
	}
	dec := xml.NewDecoder(strings.NewReader(sheet))
	for {
		_, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("the sheet is not well-formed after escaping: %v", err)
		}
	}
}

// Column letters run past Z the way Excel names them.
func TestColumnName(t *testing.T) {
	cases := map[int]string{0: "A", 1: "B", 25: "Z", 26: "AA", 27: "AB", 51: "AZ", 52: "BA", 701: "ZZ", 702: "AAA"}
	for col, want := range cases {
		if got := columnName(col); got != want {
			t.Errorf("columnName(%d) = %q, want %q", col, got, want)
		}
	}
}

// A header row is optional: a result always has one, but the writer does not
// require it (the first sheet row is then data).
func TestNoHeader(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, nil, [][]string{{"1", "2"}}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	sheet := readParts(t, buf.Bytes())["xl/worksheets/sheet1.xml"]
	if strings.Contains(sheet, `r="A1" t="inlineStr"`) {
		t.Errorf("a header row was written for no header:\n%s", sheet)
	}
	if !strings.Contains(sheet, `<c r="A1"><v>1</v></c>`) {
		t.Errorf("the first data row is not row 1:\n%s", sheet)
	}
}

// An empty result still makes a readable workbook (the header alone).
func TestHeaderOnly(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, []string{"id"}, nil); err != nil {
		t.Fatalf("Write: %v", err)
	}
	sheet := readParts(t, buf.Bytes())["xl/worksheets/sheet1.xml"]
	if !strings.Contains(sheet, `<c r="A1" t="inlineStr" s="1">`) {
		t.Errorf("the header is missing:\n%s", sheet)
	}
	// Widths are written for the header's columns.
	if !strings.Contains(sheet, `<col min="1" max="1" width=`) {
		t.Errorf("no column width was written:\n%s", sheet)
	}
}
