package main

// Tests for the Excel export: the result goes out as a workbook whose sheet
// holds the columns and the rows, numbers as numbers, and NULL as blank.

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/Ben-guanzhi/dbcore"
)

// sheetOf unzips a workbook and returns its sheet XML.
func sheetOf(t *testing.T, data []byte) string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("the workbook is not a zip: %v", err)
	}
	for _, f := range zr.File {
		if f.Name != "xl/worksheets/sheet1.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open sheet: %v", err)
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read sheet: %v", err)
		}
		return string(b)
	}
	t.Fatal("the workbook has no sheet")
	return ""
}

// A result becomes a workbook whose first row is the column names and whose rows
// are the values, with NULL left blank rather than written as "NULL".
func TestExcelExport(t *testing.T) {
	res := &dbcore.QueryResult{
		Columns: []dbcore.ColumnInfo{{Name: "id"}, {Name: "name"}, {Name: "note"}},
		Rows: [][]dbcore.Value{
			{{Kind: dbcore.ValInt, I: 1}, {Kind: dbcore.ValText, S: "ada"}, {Kind: dbcore.ValNull}},
			{{Kind: dbcore.ValInt, I: 2}, {Kind: dbcore.ValText, S: `a & b "c"`}, {Kind: dbcore.ValText, S: "x"}},
		},
	}
	data, err := excelBytes(res)
	if err != nil {
		t.Fatalf("excelBytes: %v", err)
	}
	sheet := sheetOf(t, data)

	if !strings.Contains(sheet, ">id<") || !strings.Contains(sheet, ">name<") || !strings.Contains(sheet, ">note<") {
		t.Errorf("the header row is missing a column:\n%s", sheet)
	}
	// The integer is a number cell.
	if !strings.Contains(sheet, `<c r="A2"><v>1</v></c>`) {
		t.Errorf("the id is not a number:\n%s", sheet)
	}
	// The NULL is an absent cell: Excel's blank, not the word. The cell after it
	// in the next row still exists, so the row is not truncated.
	if strings.Contains(sheet, "NULL") {
		t.Errorf("a NULL was written as text:\n%s", sheet)
	}
	if strings.Contains(sheet, `r="C2"`) {
		t.Errorf("the NULL cell was written instead of left blank:\n%s", sheet)
	}
	if !strings.Contains(sheet, `r="C3"`) {
		t.Errorf("the row after the NULL lost its cells:\n%s", sheet)
	}
	// A value with quotes and an ampersand survives escaping.
	if !strings.Contains(sheet, "a &amp; b &quot;c&quot;") {
		t.Errorf("the text was not escaped:\n%s", sheet)
	}
}

// A result with no rows still exports its header: an empty table is a fact worth
// having in a spreadsheet.
func TestExcelExportEmptyResult(t *testing.T) {
	res := &dbcore.QueryResult{
		Columns: []dbcore.ColumnInfo{{Name: "id"}, {Name: "name"}},
	}
	data, err := excelBytes(res)
	if err != nil {
		t.Fatalf("excelBytes: %v", err)
	}
	sheet := sheetOf(t, data)
	if !strings.Contains(sheet, ">id<") || !strings.Contains(sheet, ">name<") {
		t.Errorf("the header of an empty result is missing:\n%s", sheet)
	}
}

// The format's label and extension are what the Export menu and the file name
// use; a wrong extension makes a workbook open as a zip.
func TestExportFormatLabels(t *testing.T) {
	cases := map[exportFormat]struct{ label, ext string }{
		exportCSV:    {"CSV", ".csv"},
		exportJSON:   {"JSON", ".json"},
		exportINSERT: {"INSERT", ".sql"},
		exportExcel:  {"Excel", ".xlsx"},
	}
	for f, want := range cases {
		if got := f.label(); got != want.label {
			t.Errorf("label of %d = %q, want %q", f, got, want.label)
		}
		if got := f.ext(); got != want.ext {
			t.Errorf("ext of %d = %q, want %q", f, got, want.ext)
		}
	}
}
