package main

// Editor navigation and hover documentation (#18).
//
// Two questions the metadata cache can answer while the pointer is on a name:
// what is this, and where is it defined. The editor asks the first through
// State.OnHover (a tooltip once the pointer rests) and the second through
// State.OnDefinition (F12, or Cmd/Ctrl+click). Both work on the identifier
// under the offset: a table, a view, or a column — bare, schema-qualified, or
// alias-qualified. The qualifier resolution is the completion engine's
// (aliasFor/findTable), so the hover and the definition agree with what
// completion offered.

import (
	"strconv"
	"strings"

	"github.com/Ben-guanzhi/dbcore"
	"github.com/egoist/mygo/ui/editor"
)

// identPart is one dotted part of a name chain: the text without its quotes, and
// the range it occupies in the SQL, so a hover can underline exactly the word.
type identPart struct {
	name       string
	start, end int
}

// identifierAt reads the dotted name chain at offset: the identifier the pointer
// rests on, split into its parts. It returns nothing when the offset sits in a
// string literal or a comment (a value is not a definition), or on whitespace
// between names.
//
// Quoted identifiers are understood — "employees", `employees` and [employees]
// each name the same object — because a dialect quotes what it must and the
// cache stores the unquoted name.
func identifierAt(text string, offset int) []identPart {
	if offset < 0 || offset > len(text) {
		return nil
	}
	if offset == len(text) {
		return nil
	}
	if inSQLLiteralOrComment(text, offset) {
		return nil
	}
	// A name chain stops at whitespace and at every SQL delimiter; it may span
	// dots, and a quoted part may hold spaces, so the scan skips quote groups.
	mask := quotedMask(text)
	start, end := offset, offset
	for start > 0 && (mask[start-1] || !identDelimiter(text[start-1])) {
		start--
	}
	for end < len(text) && (mask[end] || !identDelimiter(text[end])) {
		end++
	}
	if start == end {
		return nil
	}
	// The offset must fall on a name part, not on a dot or a stray quote.
	chain := text[start:end]
	parts := splitNameChain(chain)
	var out []identPart
	for _, p := range parts {
		out = append(out, identPart{name: p.name, start: start + p.start, end: start + p.end})
	}
	// Keep only when the offset is inside one of the parts.
	for _, p := range out {
		if offset >= p.start && offset <= p.end {
			return out
		}
	}
	return nil
}

// quotedMask marks the bytes of the text that sit inside a quoted identifier:
// "double quotes", `backticks` and [brackets]. A quoted name may hold spaces and
// dots, so the chain scan must not treat them as separators there.
func quotedMask(text string) []bool {
	mask := make([]bool, len(text))
	for i := 0; i < len(text); {
		switch text[i] {
		case '"', '`':
			q := text[i]
			j := i + 1
			for j < len(text) {
				if text[j] == q {
					if j+1 < len(text) && text[j+1] == q {
						j += 2
						continue
					}
					break
				}
				j++
			}
			if j >= len(text) {
				j = len(text) - 1
			}
			for k := i; k <= j; k++ {
				mask[k] = true
			}
			i = j + 1
		case '[':
			j := i + 1
			for j < len(text) && text[j] != ']' {
				j++
			}
			if j >= len(text) {
				j = len(text) - 1
			}
			for k := i; k <= j; k++ {
				mask[k] = true
			}
			i = j + 1
		default:
			i++
		}
	}
	return mask
}

// identDelimiter reports whether the byte at i ends a name chain.
func identDelimiter(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\r', '(', ')', ',', ';', '+', '-', '*', '/',
		'=', '<', '>', '!', '|', '&', '%', '^', '~', '\'', '?', ':':
		return true
	}
	return false
}

// inSQLLiteralOrComment reports whether offset rests inside a '...' literal, a
// -- line comment or a /* ... */ block comment. A "..." group is a quoted
// identifier here, not a literal, so it stays readable.
func inSQLLiteralOrComment(text string, offset int) bool {
	lim := offset
	if lim > len(text) {
		lim = len(text)
	}
	for i := 0; i < lim; {
		switch {
		case text[i] == '-' && i+1 < len(text) && text[i+1] == '-':
			j := strings.IndexByte(text[i:], '\n')
			if j < 0 {
				return true // the line runs to the end, past offset
			}
			if i+j >= lim {
				return true
			}
			i += j + 1
		case text[i] == '/' && i+1 < len(text) && text[i+1] == '*':
			j := strings.Index(text[i+2:], "*/")
			if j < 0 {
				return true
			}
			if i+2+j+1 >= lim {
				return true
			}
			i += 2 + j + 2
		case text[i] == '\'':
			j := i + 1
			for j < len(text) {
				if text[j] == '\'' {
					if j+1 < len(text) && text[j+1] == '\'' {
						j += 2
						continue
					}
					break
				}
				j++
			}
			if lim > i && lim <= j {
				return true
			}
			if j >= len(text) {
				return true
			}
			i = j + 1
		default:
			i++
		}
	}
	return false
}

// rawPart is a name part as splitNameChain finds it: its unquoted text and its
// range within the chain.
type rawPart struct {
	name       string
	start, end int
}

// splitNameChain splits a dotted chain into parts, unquoting each. A part quoted
// with "..."/`...`/[...] keeps its inside text, so `"my table"` and `[my table]`
// both read `my table`.
func splitNameChain(chain string) []rawPart {
	var out []rawPart
	i := 0
	for i < len(chain) {
		// Skip a leading dot left by a previous part.
		if chain[i] == '.' {
			i++
			continue
		}
		start := i
		var name string
		switch chain[i] {
		case '"', '`':
			q := chain[i]
			i++
			from := i
			for i < len(chain) {
				if chain[i] == q {
					if i+1 < len(chain) && chain[i+1] == q {
						i += 2
						continue
					}
					break
				}
				i++
			}
			name = strings.ReplaceAll(chain[from:i], string(q)+string(q), string(q))
			if i < len(chain) {
				i++
			}
		case '[':
			i++
			from := i
			for i < len(chain) && chain[i] != ']' {
				i++
			}
			name = chain[from:i]
			if i < len(chain) {
				i++
			}
		default:
			from := i
			for i < len(chain) && chain[i] != '.' {
				i++
			}
			name = strings.TrimSpace(chain[from:i])
		}
		if name != "" {
			out = append(out, rawPart{name: name, start: start, end: i})
		}
	}
	return out
}

// refInfo is what a name chain resolved to: a table/view, or a column of one.
type refInfo struct {
	ref   tableRef
	table bool // the chain names the table itself, not one of its columns
	view  bool
	col   *dbcore.ColumnInfo
	ti    *tableInfo
}

// resolveName resolves a dotted chain to what it names, using the completion
// engine's table and alias lookups. The last part is the object; the earlier
// parts are its qualifiers: a schema, a table, or an alias of one. It reports
// false when the cache does not know the name, which is what keeps a hover off
// a word that is not an object.
func (app *App) resolveName(text string, parts []identPart) (refInfo, bool) {
	if len(parts) == 0 {
		return refInfo{}, false
	}
	names := make([]string, len(parts))
	for i, p := range parts {
		names[i] = p.name
	}
	last := names[len(names)-1]

	// One part: a table, or a column of the current statement's tables.
	if len(names) == 1 {
		if ref, ok := app.findTable(last); ok {
			return app.tableInfoOf(ref)
		}
		return app.columnOfTables(app.fromTables(text), last)
	}

	// schema.table
	if len(names) == 2 {
		if ref, ok := app.findTable(names[0] + "." + names[1]); ok {
			return app.tableInfoOf(ref)
		}
		// alias.column (or table.column)
		if ref, ok := app.aliasFor(text, names[0]); ok {
			if info, ok := app.columnOfTable(ref, names[1]); ok {
				return info, true
			}
		}
		if ref, ok := app.findTable(names[0]); ok {
			if info, ok := app.columnOfTable(ref, names[1]); ok {
				return info, true
			}
		}
		return refInfo{}, false
	}

	// schema.table.column
	if len(names) >= 3 {
		ref := tableRef{schema: names[len(names)-3], table: names[len(names)-2]}
		if ref, ok := app.findTable(ref.schema + "." + ref.table); ok {
			if info, ok := app.columnOfTable(ref, last); ok {
				return info, true
			}
		}
	}
	return refInfo{}, false
}

// tableInfoOf fills a refInfo for a table the cache knows, marking a view.
func (app *App) tableInfoOf(ref tableRef) (refInfo, bool) {
	info := refInfo{ref: ref, table: true}
	tabs, st := app.TablesOf(ref.schema)
	if st != TablesNotLoaded {
		for _, t := range tabs {
			if strings.EqualFold(t.Name, ref.table) {
				cp := t
				info.ti = &cp
				info.view = t.Type != "table"
				break
			}
		}
	}
	return info, true
}

// columnOfTable looks one named column up in a table's cached columns.
func (app *App) columnOfTable(ref tableRef, name string) (refInfo, bool) {
	cols, st := app.ColumnsOf(ref.schema, ref.table)
	if st == ColsNotLoaded {
		return refInfo{}, false
	}
	for i := range cols {
		if strings.EqualFold(cols[i].Name, name) {
			cp := cols[i]
			info, _ := app.tableInfoOf(ref)
			info.table = false
			info.col = &cp
			return info, true
		}
	}
	return refInfo{}, false
}

// columnOfTables looks a bare column up across the tables a statement reads,
// accepting it only when it is unique: a bare `id` in a join could be either.
func (app *App) columnOfTables(refs []tableRef, name string) (refInfo, bool) {
	var found refInfo
	seen := 0
	for _, ref := range refs {
		if info, ok := app.columnOfTable(ref, name); ok {
			found = info
			seen++
		}
	}
	if seen != 1 {
		return refInfo{}, false
	}
	return found, true
}

// hoverIdentifier answers the editor's hover request with the object the name
// under the pointer resolves to: its kind, its schema, a column's type and
// constraints, and the table's comment when the server gave one.
func (app *App) hoverIdentifier(req editor.HoverRequest) {
	if app.edBuf == nil {
		return
	}
	text := app.edBuf.String()
	parts := identifierAt(text, req.Offset)
	if len(parts) == 0 {
		return
	}
	info, ok := app.resolveName(text, parts)
	if !ok {
		return
	}
	from, to := parts[0].start, parts[len(parts)-1].end
	app.edState.ShowHover(from, to, app.hoverText(info), nil)
}

// hoverText is the one-line-per-fact description a hover shows.
func (app *App) hoverText(info refInfo) string {
	var b strings.Builder
	switch {
	case info.col != nil:
		b.WriteString("column ")
		b.WriteString(info.ref.schema)
		b.WriteString(".")
		b.WriteString(info.ref.table)
		b.WriteString(".")
		b.WriteString(info.col.Name)
		b.WriteString("\n")
		b.WriteString(info.col.Type)
		if info.col.Nullable {
			b.WriteString("  NULL")
		} else {
			b.WriteString("  NOT NULL")
		}
		if info.col.IsPrimaryKey {
			b.WriteString("  PRIMARY KEY")
		}
		if info.col.IsAutoIncrement {
			b.WriteString("  AUTO INCREMENT")
		}
		if info.col.Default != nil && strings.TrimSpace(*info.col.Default) != "" {
			b.WriteString("\nDEFAULT ")
			b.WriteString(strings.TrimSpace(*info.col.Default))
		}
		if info.col.Comment != "" {
			b.WriteString("\n")
			b.WriteString(info.col.Comment)
		}
	case info.table:
		if info.view {
			b.WriteString("view ")
		} else {
			b.WriteString("table ")
		}
		b.WriteString(info.ref.schema)
		b.WriteString(".")
		b.WriteString(info.ref.table)
		if info.ti != nil && info.ti.Comment != "" {
			b.WriteString("\n")
			b.WriteString(info.ti.Comment)
		}
		if cols, st := app.ColumnsOf(info.ref.schema, info.ref.table); st != ColsNotLoaded && len(cols) > 0 {
			b.WriteString("\n")
			b.WriteString(strconv.Itoa(len(cols)))
			b.WriteString(" column(s)")
		}
	default:
		b.WriteString(info.ref.table)
	}
	return b.String()
}

// goToDefinition opens the DDL of the table or view a name resolves to, or of
// the table a column belongs to. It is what F12 and Cmd/Ctrl+click call.
func (app *App) goToDefinition(offset int) {
	if app.edBuf == nil {
		return
	}
	text := app.edBuf.String()
	parts := identifierAt(text, offset)
	if len(parts) == 0 {
		return
	}
	info, ok := app.resolveName(text, parts)
	if !ok {
		return
	}
	app.openDDL(treeNode{label: info.ref.table, parent: info.ref.schema, kind: nodeTable})
}
