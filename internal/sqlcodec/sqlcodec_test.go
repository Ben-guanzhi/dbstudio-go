package sqlcodec

// The codec's contract, as the callers rely on it: a value round-trips through
// Text and Cell for every kind the grid edits, the SQL literal never quotes a
// number, the JSON spelling of a blob is the literal spelling without its
// wrapper's disagreement, and a type-name guess keeps its text on a miss.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Ben-guanzhi/dbcore"
)

func TestTextAndCellRoundTrip(t *testing.T) {
	cases := []struct {
		kind dbcore.ValueKind
		v    dbcore.Value
		text string
	}{
		{dbcore.ValInt, dbcore.Value{Kind: dbcore.ValInt, I: 42}, "42"},
		{dbcore.ValInt, dbcore.Value{Kind: dbcore.ValInt, I: -7}, "-7"},
		{dbcore.ValFloat, dbcore.Value{Kind: dbcore.ValFloat, F: 3.5}, "3.5"},
		{dbcore.ValText, dbcore.Value{Kind: dbcore.ValText, S: "Ada"}, "Ada"},
		{dbcore.ValBool, dbcore.Value{Kind: dbcore.ValBool, Bool: true}, "true"},
		{dbcore.ValNull, dbcore.Value{Kind: dbcore.ValNull}, ""},
	}
	for _, c := range cases {
		if got := Text(c.v); got != c.text {
			t.Errorf("Text(%+v) = %q, want %q", c.v, got, c.text)
		}
		back := Cell(c.kind, c.text)
		if back.Kind != c.v.Kind {
			t.Errorf("Cell(%v, %q) kind = %v, want %v", c.kind, c.text, back.Kind, c.v.Kind)
		}
		if got, want := back.String(), c.v.String(); c.kind != dbcore.ValNull && got != want {
			t.Errorf("round trip of %q: back = %q, want %q", c.text, got, want)
		}
	}
	// NULL shows as empty and comes back as NULL.
	if v := Cell(dbcore.ValInt, ""); v.Kind != dbcore.ValNull {
		t.Fatalf("Cell of empty = %+v, want NULL", v)
	}
}

// A number that does not parse keeps its text: a value the user typed must not
// be silently narrowed.
func TestCellKeepsUnparseable(t *testing.T) {
	v := Cell(dbcore.ValInt, "not a number")
	if v.Kind != dbcore.ValText || v.S != "not a number" {
		t.Fatalf("unparseable int = %+v, want the text kept", v)
	}
	v = Cell(dbcore.ValFloat, "1.2.3")
	if v.Kind != dbcore.ValText {
		t.Fatalf("malformed float = %+v, want the text kept", v)
	}
}

// The grid's Cell parses what it can: a leading-zero text becomes the number it
// spells, which is what a cell edit has always done here. (The parameter panel's
// own stricter rule — codes stay text — lives in params.go, a different
// question: it asks what a user meant by a whole input box.)
func TestCellLeadingZeroParses(t *testing.T) {
	v := Cell(dbcore.ValInt, "007")
	if v.Kind != dbcore.ValInt || v.I != 7 {
		t.Fatalf("leading zero = %+v, want int 7", v)
	}
	if v := Cell(dbcore.ValInt, "0"); v.Kind != dbcore.ValInt || v.I != 0 {
		t.Fatalf("plain zero = %+v, want int 0", v)
	}
	if v := Cell(dbcore.ValFloat, "0.5"); v.Kind != dbcore.ValFloat {
		t.Fatalf("0.5 = %+v, want float", v)
	}
}

func TestCellBoolSpellings(t *testing.T) {
	trues := []string{"true", "TRUE", "True", "1", "t", "T", "yes", "YES"}
	falses := []string{"false", "FALSE", "False", "0", "f", "F", "no", "NO"}
	for _, s := range trues {
		if v := Cell(dbcore.ValBool, s); v.Kind != dbcore.ValBool || !v.Bool {
			t.Errorf("Cell(bool, %q) = %+v, want true", s, v)
		}
	}
	for _, s := range falses {
		if v := Cell(dbcore.ValBool, s); v.Kind != dbcore.ValBool || v.Bool {
			t.Errorf("Cell(bool, %q) = %+v, want false", s, v)
		}
	}
	// Anything else keeps its text.
	if v := Cell(dbcore.ValBool, "maybe"); v.Kind != dbcore.ValText {
		t.Fatalf("Cell(bool, maybe) = %+v, want text", v)
	}
}

func TestCellTimeLayouts(t *testing.T) {
	midnight := time.Date(2024, 3, 14, 0, 0, 0, 0, time.UTC)
	halfPastTen := time.Date(2024, 3, 14, 10, 30, 0, 0, time.UTC)
	cases := []struct {
		text string
		want time.Time
	}{
		{"2024-03-14 10:30:00", halfPastTen},
		{"2024-03-14", midnight}, // a date alone is that day's midnight
		{"2024-03-14T10:30:00Z", halfPastTen},
	}
	for _, c := range cases {
		v := Cell(dbcore.ValTime, c.text)
		if v.Kind != dbcore.ValTime {
			t.Errorf("Cell(time, %q) = %+v, want a time", c.text, v)
			continue
		}
		if !v.T.Equal(c.want) {
			t.Errorf("Cell(time, %q) = %v, want %v", c.text, v.T, c.want)
		}
	}
}

func TestLiteralPerKind(t *testing.T) {
	cases := []struct {
		v    dbcore.Value
		want string
	}{
		{dbcore.Value{Kind: dbcore.ValNull}, "NULL"},
		{dbcore.Value{Kind: dbcore.ValInt, I: 42}, "42"},
		{dbcore.Value{Kind: dbcore.ValFloat, F: 1.5}, "1.5"},
		{dbcore.Value{Kind: dbcore.ValDecimal, S: "10.25"}, "10.25"},
		{dbcore.Value{Kind: dbcore.ValBool, Bool: true}, "TRUE"},
		{dbcore.Value{Kind: dbcore.ValBool, Bool: false}, "FALSE"},
		{dbcore.Value{Kind: dbcore.ValBlob, B: []byte{0xde, 0xad}}, "X'dead'"},
		{dbcore.Value{Kind: dbcore.ValText, S: "it's"}, `'it''s'`},
	}
	for _, c := range cases {
		if got := Literal("sqlite", c.v); got != c.want {
			t.Errorf("Literal(%+v) = %q, want %q", c.v, got, c.want)
		}
	}
}

// A number as text in the database (a TEXT column holding "42") is quoted: the
// column decides, not the value's digits.
func TestLiteralQuotesText(t *testing.T) {
	got := Literal("sqlite", dbcore.Value{Kind: dbcore.ValText, S: "42"})
	if !strings.HasPrefix(got, "'") {
		t.Fatalf("Literal(text 42) = %q, want it quoted", got)
	}
}

func TestJSONPerKind(t *testing.T) {
	// Numbers and bools keep their JSON types; NULL is null.
	b, err := json.Marshal([]interface{}{
		JSON(dbcore.Value{Kind: dbcore.ValInt, I: 42}),
		JSON(dbcore.Value{Kind: dbcore.ValFloat, F: 1.5}),
		JSON(dbcore.Value{Kind: dbcore.ValBool, Bool: true}),
		JSON(dbcore.Value{Kind: dbcore.ValNull}),
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got := string(b); got != `[42,1.5,true,null]` {
		t.Fatalf("JSON row = %s, want [42,1.5,true,null]", got)
	}
	// A blob spells X'..' in JSON as in a literal, so the two exports agree.
	if got := JSON(dbcore.Value{Kind: dbcore.ValBlob, B: []byte{0x01}}); got != "X'01'" {
		t.Fatalf("JSON blob = %#v, want X'01'", got)
	}
}

func TestKindOf(t *testing.T) {
	cases := []struct {
		typeName string
		want     dbcore.ValueKind
	}{
		{"INTEGER", dbcore.ValInt},
		{"bigint", dbcore.ValInt},
		{"serial", dbcore.ValInt},
		{"varchar(255)", dbcore.ValText},
		{"TEXT", dbcore.ValText},
		{"real", dbcore.ValFloat},
		{"double precision", dbcore.ValFloat},
		{"numeric(10,2)", dbcore.ValDecimal},
		{"decimal", dbcore.ValDecimal},
		{"boolean", dbcore.ValBool},
		{"timestamp", dbcore.ValTime},
		{"date", dbcore.ValTime},
		{"bytea", dbcore.ValBlob},
		{"varbinary(16)", dbcore.ValBlob},
		{"blob", dbcore.ValBlob},
		{"", dbcore.ValText},
	}
	for _, c := range cases {
		if got := KindOf(c.typeName); got != c.want {
			t.Errorf("KindOf(%q) = %v, want %v", c.typeName, got, c.want)
		}
	}
}

func TestHexRoundTrip(t *testing.T) {
	b := []byte{0x00, 0xde, 0xad, 0xbe, 0xef}
	s := EncodeHex(b)
	if s != "00deadbeef" {
		t.Fatalf("EncodeHex = %q, want 00deadbeef", s)
	}
	if back := DecodeHex(s); string(back) != string(b) {
		t.Fatalf("DecodeHex(%q) = %v, want the bytes back", s, back)
	}
	// Odd length and non-hex are refused, so a paste can tell bytes from text.
	if DecodeHex("abc") != nil {
		t.Fatal("an odd-length text decoded")
	}
	if DecodeHex("zz") != nil {
		t.Fatal("a non-hex text decoded")
	}
	if DecodeHex("") == nil {
		t.Fatal("an empty text did not decode to the empty byte slice")
	}
}
