package main

// Tests for the result export (P2 #17): JSON and INSERT join CSV as file
// exports of the current result. JSON rendering is tested for its type shape,
// INSERT for its statement text and its "needs one table" guard, and the edit
// folding for keeping the working copy's view of the rows.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dbcore/dbcore"
)

// exportRows builds a QueryResult like a browse returns.
func exportRows(rows ...[]dbcore.Value) *dbcore.QueryResult {
	return &dbcore.QueryResult{
		Columns: []dbcore.ColumnInfo{
			{Name: "id"},
			{Name: "name"},
			{Name: "salary"},
		},
		Rows: rows,
	}
}

func TestJSONExportTypes(t *testing.T) {
	app := &App{}
	res := exportRows(
		[]dbcore.Value{
			{Kind: dbcore.ValInt, I: 1},
			{Kind: dbcore.ValText, S: "Ada"},
			{Kind: dbcore.ValFloat, F: 190000.5},
		},
		[]dbcore.Value{
			{Kind: dbcore.ValNull},
			{Kind: dbcore.ValText, S: "Alan"},
			{Kind: dbcore.ValNull},
		},
		[]dbcore.Value{
			{Kind: dbcore.ValInt, I: 3},
			{Kind: dbcore.ValBool, Bool: true},
			{Kind: dbcore.ValNull},
		},
	)
	data, err := app.jsonExport(res)
	if err != nil {
		t.Fatalf("jsonExport: %v", err)
	}
	var got []map[string]interface{}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, data)
	}
	if len(got) != 3 {
		t.Fatalf("wrote %d rows, want 3", len(got))
	}
	if v, ok := got[0]["id"].(float64); !ok || v != 1 {
		t.Fatalf("id = %#v, want 1", got[0]["id"])
	}
	if got[0]["name"] != "Ada" {
		t.Fatalf("name = %#v, want Ada", got[0]["name"])
	}
	if v, ok := got[1]["id"]; !ok || v != nil {
		t.Fatalf("NULL id = %#v, want null", v)
	}
	if v, ok := got[2]["salary"]; !ok || v != nil {
		t.Fatalf("NULL salary = %#v, want null", v)
	}
	if b, ok := got[2]["name"].(bool); !ok || !b {
		t.Fatalf("bool name = %#v, want true", got[2]["name"])
	}
}

func TestJSONExportEmpty(t *testing.T) {
	app := &App{}
	data, err := app.jsonExport(exportRows())
	if err != nil {
		t.Fatalf("jsonExport: %v", err)
	}
	if strings.TrimSpace(string(data)) != "[]" {
		t.Fatalf("empty result = %q, want []", data)
	}
}

func TestInsertExport(t *testing.T) {
	app := &App{conn: Conn{Kind: "sqlite"}}
	res := exportRows(
		[]dbcore.Value{{Kind: dbcore.ValInt, I: 1}, {Kind: dbcore.ValText, S: "Ada"}, {Kind: dbcore.ValNull}},
		[]dbcore.Value{{Kind: dbcore.ValInt, I: 2}, {Kind: dbcore.ValText, S: "Alan"}, {Kind: dbcore.ValNull}},
	)
	app.edit.active = true
	app.edit.table = "employees"
	app.edit.schema = "main"
	app.edit.cols = res.Columns

	script, err := app.insertScript(res)
	if err != nil {
		t.Fatalf("insertScript: %v", err)
	}
	upper := strings.ToUpper(script)
	if !strings.Contains(upper, "INSERT INTO") || !strings.Contains(upper, "EMPLOYEES") {
		t.Fatalf("script = %q, want an INSERT INTO employees", script)
	}
	if !strings.Contains(script, "Ada") || !strings.Contains(script, "Alan") {
		t.Fatalf("script = %q, want both rows", script)
	}
	if lines := strings.Count(strings.TrimSpace(script), "\n"); lines != 1 {
		t.Fatalf("script has %d line breaks, want 2 statements (1 break)", lines)
	}
}

func TestInsertExportNeedsTable(t *testing.T) {
	app := &App{}
	res := exportRows([]dbcore.Value{{Kind: dbcore.ValInt, I: 1}})
	// No working copy: an arbitrary SELECT cannot be exported as INSERT.
	if _, err := app.insertScript(res); err == nil {
		t.Fatal("insertScript on a result with no table did not fail")
	}
}

func TestEditResultFoldsWorkingCopy(t *testing.T) {
	app := &App{}
	res := exportRows([]dbcore.Value{{Kind: dbcore.ValInt, I: 1}, {Kind: dbcore.ValText, S: "old"}, {Kind: dbcore.ValNull}})
	app.result = res
	app.edit.active = true
	app.edit.cols = res.Columns
	app.edit.rows = []*editRow{
		{strs: []string{"1", "changed", ""}, origStrs: []string{"1", "old", ""}, kinds: []dbcore.ValueKind{dbcore.ValInt, dbcore.ValText, dbcore.ValNull}, state: rowModified},
	}
	out := app.editResult()
	if len(out.Rows) != 1 {
		t.Fatalf("edit folded to %d rows, want 1", len(out.Rows))
	}
	if got := out.Rows[0][1].String(); got != "changed" {
		t.Fatalf("edited cell = %q, want changed", got)
	}
}

func TestEditResultKeepsTypedRows(t *testing.T) {
	app := &App{}
	res := exportRows([]dbcore.Value{{Kind: dbcore.ValInt, I: 42}, {Kind: dbcore.ValText, S: "x"}, {Kind: dbcore.ValNull}})
	app.result = res
	out := app.editResult()
	if len(out.Rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(out.Rows))
	}
	if out.Rows[0][0].Kind != dbcore.ValInt || out.Rows[0][0].I != 42 {
		t.Fatalf("integer cell lost its type: %+v", out.Rows[0][0])
	}
}
