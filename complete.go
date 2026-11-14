package main

// SQL autocompletion for the editor.
//
// The editor asks for completions as a word is typed or Ctrl+Space is pressed,
// through State.OnCompletion; the app answers with State.ShowCompletions, and
// the editor's own list shows, filters and inserts them.
//
// The words offered come from three places: the SQL keywords, functions and
// types the five engines share, and the schema, table and column names the
// connection's metadata cache holds. The cache is what the schema tree reads,
// so a table or column appears in the list once it has been looked at, as it
// does in DataGrip; nothing here talks to the server.
//
// The context decides what is worth offering. The text before the caret is read
// back to the last clause keyword, as "after FROM" is a table and "after
// WHERE" is a column, and a dot narrows the list to the columns of the table
// or alias on the left of it.

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/ui/editor"
)

// sqlKeywords are the clause and statement words of SQL, whichever engine
// the connection is to.
var sqlKeywords = []string{
	"ADD", "ALL", "ALTER", "AND", "ANY", "AS", "ASC", "BETWEEN", "BY", "CASE",
	"CAST", "CHECK", "COLLATE", "COLUMN", "COMMIT", "CONSTRAINT", "CREATE",
	"CROSS", "CURRENT", "CURRENT_DATE", "CURRENT_TIME", "CURRENT_TIMESTAMP",
	"DATABASE", "DEFAULT", "DELETE", "DESC", "DISTINCT", "DROP", "ELSE", "END",
	"EXCEPT", "EXISTS", "FETCH", "FILTER", "FIRST", "FOREIGN", "FROM", "FULL",
	"GENERATED", "GROUP", "HAVING", "IF", "IN", "INDEX", "INNER", "INSERT",
	"INTO", "IS", "JOIN", "KEY", "LEADING", "LEFT", "LIKE", "LIMIT", "NATURAL",
	"NOT", "NULL", "OFFSET", "ON", "OR", "ORDER", "PRIMARY", "REFERENCES",
	"RETURNING", "RIGHT", "RLIKE", "REGEXP", "SELECT", "SET", "THEN",
	"TRAILING", "TRIGGER", "UNION", "UNIQUE", "UPDATE", "USING", "VALUES",
	"VIEW", "WHEN", "WHERE", "WITH",
}

// sqlTypes are the column types of the engines, offered as a word is typed
// after CAST or in a column definition.
var sqlTypes = []string{
	"INT", "INTEGER", "BIGINT", "SMALLINT", "TINYINT", "MEDIUMINT", "DECIMAL",
	"NUMERIC", "NUMBER", "FLOAT", "REAL", "DOUBLE", "PRECISION", "BOOLEAN",
	"BOOL", "CHAR", "VARCHAR", "TEXT", "LONGTEXT", "BLOB", "CLOB", "DATE",
	"TIME", "DATETIME", "TIMESTAMP", "BINARY", "VARBINARY", "JSON", "UUID",
	"ENUM",
}

// sqlFunctions are the aggregate and scalar functions the engines share.
var sqlFunctions = []string{
	"AVG", "COUNT", "MAX", "MIN", "STDDEV", "STDDEV_POP", "STDDEV_SAMP",
	"SUM", "VAR_POP", "VAR_SAMP", "GROUP_CONCAT", "TOTAL",
	"ABS", "CEIL", "CEILING", "FLOOR", "MOD", "POWER", "POW", "SQRT", "EXP",
	"LN", "LOG", "LOG2", "LOG10", "PI", "RANDOM", "RANDOMBLOB", "SIGN",
	"ROUND",
	"CONCAT", "CONCAT_WS", "SUBSTR", "SUBSTRING", "LENGTH", "CHAR_LENGTH",
	"CHARACTER_LENGTH", "LOWER", "UPPER", "TRIM", "LTRIM", "RTRIM", "REPLACE",
	"LOCATE", "INSTR", "POSITION", "LEFT", "RIGHT", "LPAD", "RPAD", "REVERSE",
	"ASCII", "CHAR", "HEX", "UNHEX", "INSTR",
	"COALESCE", "NULLIF", "IFNULL", "NVL", "NVL2", "DECODE",
	"STRFTIME", "DATE", "TIME", "DATETIME", "JULIANDAY",
	"CAST", "TYPEOF",
	"JSON_EXTRACT", "JSON_TYPE", "JSON_OBJECT", "JSON_ARRAY", "JSON_QUOTE",
	"ROW_NUMBER", "RANK", "DENSE_RANK", "NTILE", "LAG", "LEAD", "FIRST_VALUE",
	"LAST_VALUE", "NTH_VALUE",
}

// tableRef names a table in a schema, as the metadata cache does.
type tableRef struct {
	schema string
	table  string
}

// clauseCtx is what the text before the caret asks for.
type clauseCtx int

const (
	ctxGeneral clauseCtx = iota
	ctxTable
	ctxColumn
)

// onCompletion answers the editor's request for completions at the caret with
// the list built for the statement there, as State.OnCompletion does.
func (app *App) onCompletion(req editor.CompletionRequest) {
	if app.edState == nil || app.edBuf == nil {
		return
	}
	text := app.edBuf.String()
	head := req.Offset
	if head > len(text) {
		head = len(text)
	}
	start := head - len(req.Prefix)
	if start < 0 {
		start = 0
	}
	app.edState.ShowCompletions(start, app.completionsAt(text, head, start))
}

// completionsAt builds the completions for the caret at head, which is the
// word starting at start.
func (app *App) completionsAt(text string, head, start int) []editor.Completion {
	// What is before the caret gives the context; the whole statement gives
	// the aliases, as an alias may be written after the place that uses it.
	clean := stripSQLLiterals(text)
	before := clean[:head]

	// The word being completed is left out: what is before it is the context.
	i := skipBackSpace(before, start)
	if i > 0 && runeBefore(before, i) == '.' {
		return app.qualifiedItems(clean, qualifierOf(before, i))
	}

	// Walk back to the last clause keyword. The first one found is the most
	// recent: an identifier, a number or a function name is not one, so a
	// table or column between it and the caret does not hide it.
	ctx := ctxGeneral
	for {
		w, j := readBackWord(before, i)
		if w == "" {
			break
		}
		if isClauseKeyword(w) {
			ctx = clauseCtxOf(w)
			break
		}
		i = skipBackSpace(before, j)
	}

	if ctx == ctxTable {
		return app.tableContextItems()
	}
	return app.generalItems(clean, head)
}

// tableContextItems is the list after FROM and JOIN: the tables of the
// connection, its schemas, and the keywords a clause there takes, as INNER,
// LEFT or ON.
func (app *App) tableContextItems() []editor.Completion {
	var set itemSet
	for _, it := range keywordItems() {
		set.add(it)
	}
	for _, it := range schemaItems(app) {
		set.add(it)
	}
	for _, it := range tableItems(app, "") {
		set.add(it)
	}
	return set.list()
}

// qualifiedItems builds the completions for the name after a dot: the columns
// of the table or alias the dot follows, or the tables of a schema.
func (app *App) qualifiedItems(text string, qualifier string) []editor.Completion {
	qualifier = strings.TrimSpace(qualifier)
	if ref, ok := app.aliasFor(text, qualifier); ok {
		return columnItems(app, ref)
	}
	if app.schemaNamed(qualifier) {
		return tableItems(app, qualifier)
	}
	if ref, ok := app.findTable(qualifier); ok {
		return columnItems(app, ref)
	}
	if refs := app.fromTables(text); len(refs) > 0 {
		return columnItemsFrom(app, refs)
	}
	return nil
}

// generalItems builds the whole list: keywords, functions, types, schemas,
// tables and the columns of the tables the statement names.
func (app *App) generalItems(text string, head int) []editor.Completion {
	var set itemSet
	for _, it := range keywordItems() {
		set.add(it)
	}
	for _, it := range functionItems() {
		set.add(it)
	}
	for _, it := range typeItems() {
		set.add(it)
	}
	for _, it := range schemaItems(app) {
		set.add(it)
	}
	for _, it := range tableItems(app, "") {
		set.add(it)
	}

	refs := app.fromTables(text[:head])
	if len(refs) == 0 {
		if ref, ok := app.currentTableRef(); ok {
			refs = []tableRef{ref}
		}
	}
	for _, it := range columnItemsFrom(app, refs) {
		set.add(it)
	}
	return set.list()
}

// aliasFor resolves the table an alias before a dot names, from the FROM and
// JOIN clauses of the statement.
func (app *App) aliasFor(text string, qualifier string) (tableRef, bool) {
	ref, ok := app.aliasMap(text)[strings.ToLower(strings.TrimSpace(qualifier))]
	return ref, ok
}

// fromTables lists the tables the statement names in FROM and JOIN clauses.
func (app *App) fromTables(text string) []tableRef {
	seen := map[string]bool{}
	var out []tableRef
	for _, ref := range app.aliasMap(text) {
		k := strings.ToLower(ref.schema + "\x00" + ref.table)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, ref)
	}
	return out
}

// aliasMap reads the FROM and JOIN clauses of text and maps the name and the
// alias of each table to the table. A name the cache does not know is left
// out; the table's columns cannot be offered for it anyway.
func (app *App) aliasMap(text string) map[string]tableRef {
	out := map[string]tableRef{}
	clean := stripSQLLiterals(text)
	lower := strings.ToLower(clean)
	for _, kw := range []string{"from", "join"} {
		idx := 0
		for {
			j := strings.Index(lower[idx:], kw)
			if j < 0 {
				break
			}
			p := idx + j
			idx = p + len(kw)
			if p > 0 && isWordByte(lower[p-1]) {
				continue
			}
			end := p + len(kw)
			if end < len(lower) && isWordByte(lower[end]) {
				continue
			}
			q := skipSpacesForward(clean, end)
			s := q
			for q < len(clean) && isWordByte(clean[q]) {
				q++
			}
			if q == s {
				continue
			}
			ident := clean[s:q]
			if q < len(clean) && clean[q] == '.' {
				r := q + 1
				s2 := r
				for r < len(clean) && isWordByte(clean[r]) {
					r++
				}
				if r > s2 {
					ident += "." + clean[s2:r]
					q = r
				}
			}
			ref, ok := app.findTable(ident)
			if !ok {
				continue
			}
			out[strings.ToLower(ident)] = ref

			// The alias follows the table, after an optional AS.
			q2 := skipSpacesForward(clean, q)
			if q2+2 <= len(clean) && strings.EqualFold(clean[q2:q2+2], "as") &&
				(q2+2 == len(clean) || !isWordByte(clean[q2+2])) {
				w := q2
				for w < len(clean) && isWordByte(clean[w]) {
					w++
				}
				q2 = skipSpacesForward(clean, w)
			}
			s3 := q2
			for q2 < len(clean) && isWordByte(clean[q2]) {
				q2++
			}
			if q2 > s3 {
				out[strings.ToLower(clean[s3:q2])] = ref
			}
		}
	}
	return out
}

// findTable looks a name up in the cache: a schema-qualified name, or a bare
// name that is the only table of its name, as the outline does.
func (app *App) findTable(name string) (tableRef, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return tableRef{}, false
	}
	if schema, table, ok := strings.Cut(name, "."); ok {
		schema = strings.TrimSpace(schema)
		table = strings.TrimSpace(table)
		if app.hasColumns(schema, table) || app.hasTables(schema) {
			return tableRef{schema: schema, table: table}, true
		}
		return tableRef{}, false
	}
	if ref, ok := app.FindTableByName(name); ok {
		return ref, true
	}
	return tableRef{}, false
}

func (app *App) hasTables(schema string) bool {
	return app.HasTables(schema)
}

func (app *App) hasColumns(schema, table string) bool {
	return app.HasColumns(schema, table)
}

func (app *App) schemaNamed(name string) bool {
	for _, s := range app.SchemaNames() {
		if strings.EqualFold(s, name) {
			return true
		}
	}
	return false
}

// currentTableRef is the table the editor is showing the browse of, if any.
func (app *App) currentTableRef() (tableRef, bool) {
	if app.cached != nil {
		if s, t := app.cached.currentSchema(), app.cached.currentTable(); s != "" && t != "" {
			return tableRef{schema: s, table: t}, true
		}
	}
	if app.edit.active {
		return tableRef{schema: app.edit.schema, table: app.edit.table}, true
	}
	return tableRef{}, false
}

// keywordItems, functionItems and typeItems are the words the engines share.
func keywordItems() []editor.Completion {
	out := make([]editor.Completion, 0, len(sqlKeywords))
	for _, w := range sqlKeywords {
		out = append(out, editor.Completion{Label: w, Kind: editor.CompletionKeyword})
	}
	return out
}

func functionItems() []editor.Completion {
	out := make([]editor.Completion, 0, len(sqlFunctions))
	for _, w := range sqlFunctions {
		out = append(out, editor.Completion{
			Label:  w,
			Insert: w + "(",
			Kind:   editor.CompletionFunction,
			Detail: "()",
		})
	}
	return out
}

func typeItems() []editor.Completion {
	out := make([]editor.Completion, 0, len(sqlTypes))
	for _, w := range sqlTypes {
		out = append(out, editor.Completion{Label: w, Kind: editor.CompletionType})
	}
	return out
}

// schemaItems lists the namespaces the outline shows at its top.
func schemaItems(app *App) []editor.Completion {
	names := app.SchemaNames()
	out := make([]editor.Completion, 0, len(names))
	for _, s := range names {
		out = append(out, editor.Completion{
			Label:         s,
			Kind:          editor.CompletionModule,
			Detail:        "schema",
			Documentation: "A schema of " + app.conn.Kind,
		})
	}
	return out
}

// tableItems lists the tables of every schema, or of one when it is given.
func tableItems(app *App, schema string) []editor.Completion {
	var out []editor.Completion
	for _, s := range app.SchemaNames() {
		tabs, st := app.TablesOf(s)
		if st == TablesNotLoaded {
			continue
		}
		if schema != "" && !strings.EqualFold(s, schema) {
			continue
		}
		for _, t := range tabs {
			out = append(out, editor.Completion{
				Label:         t.Name,
				Kind:          editor.CompletionType,
				Detail:        s,
				Documentation: "Table " + s + "." + t.Name,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out
}

// columnItems lists the columns of one table.
func columnItems(app *App, ref tableRef) []editor.Completion {
	cols, _ := app.ColumnsOf(ref.schema, ref.table)
	out := make([]editor.Completion, 0, len(cols))
	for _, c := range cols {
		out = append(out, editor.Completion{
			Label:         c.Name,
			Kind:          editor.CompletionField,
			Detail:        c.Type,
			Documentation: ref.schema + "." + ref.table,
		})
	}
	return out
}

// maxColumnItems caps the columns of a general list, so a wide schema does
// not make every keystroke build thousands of completions.
const maxColumnItems = 200

// columnItemsFrom lists the columns of several tables by name once, with the
// first type seen for each, as a JOIN has one column of each name.
func columnItemsFrom(app *App, refs []tableRef) []editor.Completion {
	seen := map[string]bool{}
	var out []editor.Completion
	for _, ref := range refs {
		cols, _ := app.ColumnsOf(ref.schema, ref.table)
		for _, c := range cols {
			k := strings.ToLower(c.Name)
			if seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, editor.Completion{
				Label:         c.Name,
				Kind:          editor.CompletionField,
				Detail:        c.Type,
				Documentation: ref.schema + "." + ref.table,
			})
			if len(out) >= maxColumnItems {
				sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
				return out
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out
}

// itemSet keeps a list of completions free of two alike, as a keyword and a
// column may well share a name and should both stay.
type itemSet struct {
	items []editor.Completion
	seen  map[string]bool
}

func (s *itemSet) add(it editor.Completion) {
	if s.seen == nil {
		s.seen = map[string]bool{}
	}
	k := string(it.Kind) + "\x00" + strings.ToLower(it.Label)
	if s.seen[k] {
		return
	}
	s.seen[k] = true
	s.items = append(s.items, it)
}

func (s *itemSet) list() []editor.Completion { return s.items }

// stripSQLLiterals blanks the string, quoted-identifier and comment spans of
// s, keeping its length so offsets into it are offsets into the original. A
// keyword inside a literal is then not taken for a clause.
func stripSQLLiterals(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == '-' && i+1 < len(s) && s[i+1] == '-':
			for i < len(s) && s[i] != '\n' {
				b.WriteByte(' ')
				i++
			}
		case c == '/' && i+1 < len(s) && s[i+1] == '*':
			for i < len(s) {
				if s[i] == '*' && i+1 < len(s) && s[i+1] == '/' {
					b.WriteByte(' ')
					b.WriteByte(' ')
					i += 2
					break
				}
				b.WriteByte(' ')
				i++
			}
		case c == '\'' || c == '"':
			q := c
			b.WriteByte(' ')
			i++
			for i < len(s) {
				if s[i] == q {
					if i+1 < len(s) && s[i+1] == q {
						b.WriteByte(' ')
						b.WriteByte(' ')
						i += 2
						continue
					}
					b.WriteByte(' ')
					i++
					break
				}
				b.WriteByte(' ')
				i++
			}
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

// readBackWord reads the word ending at i, and where it starts.
func readBackWord(s string, i int) (string, int) {
	j := i
	for j > 0 {
		r, size := utf8.DecodeLastRuneInString(s[:j])
		if !isWordRune(r) {
			break
		}
		j -= size
	}
	return s[j:i], j
}

// skipBackSpace moves i back over whitespace.
func skipBackSpace(s string, i int) int {
	for i > 0 {
		r, size := utf8.DecodeLastRuneInString(s[:i])
		if unicode.IsSpace(r) {
			i -= size
			continue
		}
		break
	}
	return i
}

// skipSpacesForward moves i forward over whitespace.
func skipSpacesForward(s string, i int) int {
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		if unicode.IsSpace(r) {
			i += size
			continue
		}
		break
	}
	return i
}

// runeBefore returns the rune ending at i.
func runeBefore(s string, i int) rune {
	r, _ := utf8.DecodeLastRuneInString(s[:i])
	return r
}

// qualifierOf reads the name before the dot at i, which ends at i-1.
func qualifierOf(s string, i int) string {
	w, j := readBackWord(s, i-1)
	j = skipBackSpace(s, j)
	if j > 0 && runeBefore(s, j) == '.' {
		w2, _ := readBackWord(s, j-1)
		if w2 != "" {
			return w2 + "." + w
		}
	}
	return w
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

func isWordByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9') || b == '_' || b >= 0x80
}

// isClauseKeyword reports whether a word ends a clause and so says what a
// name after it is.
func isClauseKeyword(w string) bool {
	switch strings.ToUpper(w) {
	case "SELECT", "FROM", "WHERE", "GROUP", "ORDER", "BY", "HAVING", "LIMIT",
		"OFFSET", "FETCH", "JOIN", "INNER", "LEFT", "RIGHT", "FULL", "CROSS",
		"NATURAL", "ON", "USING", "AS", "AND", "OR", "NOT", "IN", "IS", "NULL",
		"BETWEEN", "LIKE", "RLIKE", "REGEXP", "SET", "VALUES", "INTO", "UPDATE",
		"DELETE", "INSERT", "DISTINCT", "ALL", "UNION", "EXCEPT", "INTERSECT",
		"RETURNING", "WINDOW", "WITH", "CASE", "WHEN", "THEN", "ELSE", "END",
		"EXISTS":
		return true
	}
	return false
}

// clauseCtxOf says what a name after a clause keyword is.
func clauseCtxOf(w string) clauseCtx {
	switch strings.ToUpper(w) {
	case "FROM", "JOIN", "UPDATE", "INTO", "DELETE":
		return ctxTable
	}
	return ctxColumn
}
