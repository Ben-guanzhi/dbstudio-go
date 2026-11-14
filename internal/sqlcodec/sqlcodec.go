// Package sqlcodec is the one place a dbcore.Value meets text: how a value is
// shown in a cell, how edited text becomes a value again, how it is spelled as
// a SQL literal or a JSON value, and which kind a column's type name carries.
//
// It exists because these conversions used to live beside their callers — four
// files, eight entry points — and two callers could then disagree. A value that
// copied as X'ff' but exported as "ff" is not two features, it is one encoding
// with a bug; keeping the conversions beside the callers made that possible.
// Here, one spelling per question, testable without an App.
//
// The package never touches a connection or the UI: pure functions over
// dbcore.Value and strings.
package sqlcodec

import (
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"github.com/dbcore/dbcore"
	"github.com/dbcore/dbcore/sqltext"
)

// Literal renders a value as the literal of its own kind in the dialect dbType.
// A number is never quoted: an engine would read a quoted number as text.
// NULL is the keyword; a blob is X'..' with hex digits; text and time are
// single-quoted with the dialect's escaping.
func Literal(dbType string, v dbcore.Value) string {
	switch v.Kind {
	case dbcore.ValNull:
		return "NULL"
	case dbcore.ValInt:
		return strconv.FormatInt(v.I, 10)
	case dbcore.ValFloat:
		return strconv.FormatFloat(v.F, 'g', -1, 64)
	case dbcore.ValDecimal:
		// The digit text IS the value; quoting it would make the engine parse a
		// string into the decimal column, which some engines refuse.
		return v.S
	case dbcore.ValBool:
		if v.Bool {
			return "TRUE"
		}
		return "FALSE"
	case dbcore.ValBlob:
		return "X'" + hex.EncodeToString(v.B) + "'"
	default:
		// Text and time are single-quoted; a time keeps its text form.
		return sqltext.QuotedLiteral(dbType, v.String())
	}
}

// Text is what a cell shows: the value's text, or nothing for NULL. An empty
// cell is how NULL is edited — typing over it gives the column a value.
func Text(v dbcore.Value) string {
	if v.Kind == dbcore.ValNull {
		return ""
	}
	return v.String()
}

// Cell turns edited text back into a Value of the cell's own kind. A blank cell
// is NULL; a number that does not parse keeps its text rather than being
// silently turned into something else.
func Cell(kind dbcore.ValueKind, s string) dbcore.Value {
	if s == "" {
		return dbcore.Value{Kind: dbcore.ValNull}
	}
	switch kind {
	case dbcore.ValInt:
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return dbcore.Value{Kind: dbcore.ValInt, I: n}
		}
	case dbcore.ValFloat:
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return dbcore.Value{Kind: dbcore.ValFloat, F: f}
		}
	case dbcore.ValDecimal:
		return dbcore.DecimalValue(s)
	case dbcore.ValBool:
		switch s {
		case "true", "TRUE", "True", "1", "t", "T", "yes", "YES":
			return dbcore.Value{Kind: dbcore.ValBool, Bool: true}
		case "false", "FALSE", "False", "0", "f", "F", "no", "NO":
			return dbcore.Value{Kind: dbcore.ValBool, Bool: false}
		}
	case dbcore.ValTime:
		for _, layout := range []string{
			time.RFC3339Nano, time.RFC3339,
			"2006-01-02 15:04:05.999999999-07:00",
			"2006-01-02 15:04:05", "2006-01-02",
		} {
			if t, err := time.Parse(layout, s); err == nil {
				return dbcore.Value{Kind: dbcore.ValTime, T: t}
			}
		}
	case dbcore.ValBlob:
		// A blob is shown as text and edited as text; the driver sends it back
		// as a string. Large blobs are not loaded into the grid in full.
		return dbcore.Value{Kind: dbcore.ValText, S: s}
	}
	return dbcore.Value{Kind: dbcore.ValText, S: s}
}

// JSON converts a value to the JSON value a file export writes: numbers and
// bools keep their type, NULL becomes null, blobs become X'..' with hex digits,
// and everything else becomes its text. The X'..' form is the same spelling
// Literal uses, so a JSON export and an INSERT export of one cell agree.
func JSON(v dbcore.Value) interface{} {
	switch v.Kind {
	case dbcore.ValNull:
		return nil
	case dbcore.ValInt:
		return v.I
	case dbcore.ValFloat:
		return v.F
	case dbcore.ValBool:
		return v.Bool
	case dbcore.ValBlob:
		return "X'" + hex.EncodeToString(v.B) + "'"
	default:
		return v.String()
	}
}

// KindOf guesses a ValueKind from a column's type name, for the cells of a row
// that was inserted rather than read. A guess that is wrong costs nothing: the
// value keeps its text when it does not parse as the guessed kind.
func KindOf(typeName string) dbcore.ValueKind {
	s := strings.ToUpper(strings.TrimSpace(typeName))
	if i := strings.IndexByte(s, '('); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	switch {
	case strings.Contains(s, "BOOL"):
		return dbcore.ValBool
	case strings.Contains(s, "TIME"), strings.Contains(s, "DATE"):
		return dbcore.ValTime
	case strings.Contains(s, "BLOB"), strings.Contains(s, "BYTEA"), strings.Contains(s, "BINARY"), strings.Contains(s, "RAW"):
		return dbcore.ValBlob
	case strings.Contains(s, "DEC"), strings.Contains(s, "NUMERIC"), strings.Contains(s, "NUMBER"):
		return dbcore.ValDecimal
	case strings.Contains(s, "FLOAT"), strings.Contains(s, "DOUBLE"), strings.Contains(s, "REAL"):
		return dbcore.ValFloat
	case strings.Contains(s, "INT"), strings.Contains(s, "SERIAL"):
		return dbcore.ValInt
	}
	return dbcore.ValText
}

// DecodeHex turns a hex text back into the bytes it was read from, answering
// nil when the text is not hex or is not even in length — a value editor and a
// paste both need to tell "the bytes" from "something a user typed".
func DecodeHex(s string) []byte {
	if len(s)%2 != 0 {
		return nil
	}
	out, err := hex.DecodeString(s)
	if err != nil {
		return nil
	}
	return out
}

// EncodeHex is the hex text a blob's bytes are shown as — the same digits
// Literal and JSON wrap in X'..', without the wrapper, as a cell edits it.
func EncodeHex(b []byte) string {
	return hex.EncodeToString(b)
}
