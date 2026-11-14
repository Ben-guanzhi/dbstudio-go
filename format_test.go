package main

// Tests for the Format SQL action (P2 #14). The formatter itself is unit-tested
// in internal/sqlformat; here the wrapper that drives the editor buffer — whole
// buffer vs selection, and keeping surrounding text when only a selection is
// formatted — is what matters.

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui/editor"
)

func appWithBuffer(t *testing.T, text string) *App {
	t.Helper()
	buf := editor.NewBuffer(text)
	buf.SetLanguage(editor.LanguageFor("query.sql"))
	return &App{
		edBuf:   buf,
		edState: &editor.State{Buffer: buf},
	}
}

func TestFormatWholeBuffer(t *testing.T) {
	app := appWithBuffer(t, "select id from t where x=1")
	app.formatSQL()
	if !strings.Contains(app.edBuf.String(), "\n") {
		t.Fatalf("whole-buffer format = %q, want a multi-line statement", app.edBuf.String())
	}
	if !strings.Contains(app.edBuf.String(), "SELECT") {
		t.Fatalf("format = %q, keywords not uppercased", app.edBuf.String())
	}
}

func TestFormatSelectionKeepsSurrounding(t *testing.T) {
	app := appWithBuffer(t, "-- header\nselect id from t -- tail")
	// Select just the statement (from 'select' to 'from t').
	buf := app.edBuf.String()
	a := strings.Index(buf, "select")
	b := strings.Index(buf, " from t") + len(" from t")
	app.edState.Select(a, b)
	app.formatSQL()
	got := app.edBuf.String()
	// The comment before and after the selection is preserved.
	if !strings.HasPrefix(got, "-- header") || !strings.HasSuffix(got, "-- tail") {
		t.Fatalf("selection format lost surrounding text: %q", got)
	}
	// The selected part is now formatted (keyword uppercased + line break).
	if !strings.Contains(strings.ToUpper(got), "SELECT") {
		t.Fatalf("selection format = %q, keyword not uppercased", got)
	}
}

func TestFormatIdempotent(t *testing.T) {
	app := appWithBuffer(t, "SELECT id\nFROM t\nWHERE x = 1")
	first := app.formatBuffer()
	app.edBuf.Set(first)
	second := app.formatBuffer()
	if first != second {
		t.Fatalf("formatting is not idempotent:\n%q\n%q", first, second)
	}
}
