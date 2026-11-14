package main

// What a column is: the header's tooltip.
//
// A grid shows names and values; it does not show that `amount` is a DECIMAL(12,2)
// that cannot be null, or that `id` is the primary key. Mature clients answer
// that on hover, and it is the question a person has before typing a WHERE or a
// literal — the type decides whether `'12'` needs quotes.
//
// The result's own column metadata carries the type and nullability; for a
// browse of a table the catalog's column list adds the primary key, the default
// and the comment, so the tooltip is richer where the app knows more.

import (
	"strings"

	"github.com/dbcore/dbcore"
	"github.com/egoist/mygo/ui"
)

// columnInfoText is a column's one-line description for its header's tooltip:
// the type, whether it can be null, whether it is the key, its default and its
// comment.
func (app *App) columnInfoText(t *resTab, res *dbcore.QueryResult, col int) string {
	if res == nil || col < 0 || col >= len(res.Columns) {
		return ""
	}
	c := res.Columns[col]
	// A browse knows the table, so the catalog can say what the result set
	// cannot: the primary key, the default, the comment.
	if t != nil && t.table != "" {
		if cols, st := app.ColumnsOf(t.schema, t.table); st == ColsLoaded {
			for _, cat := range cols {
				if strings.EqualFold(cat.Name, c.Name) {
					c.IsPrimaryKey = cat.IsPrimaryKey
					if cat.Default != nil {
						c.Default = cat.Default
					}
					if cat.Comment != "" {
						c.Comment = cat.Comment
					}
					break
				}
			}
		}
	}

	parts := make([]string, 0, 5)
	parts = append(parts, c.Name)
	if c.Type != "" {
		parts = append(parts, c.Type)
	}
	if c.IsPrimaryKey {
		parts = append(parts, "primary key")
	}
	if c.Nullable {
		parts = append(parts, "null")
	} else {
		parts = append(parts, "not null")
	}
	if c.Default != nil && *c.Default != "" {
		parts = append(parts, "default "+*c.Default)
	}
	if c.Comment != "" {
		parts = append(parts, c.Comment)
	}
	return strings.Join(parts, " · ")
}

// headerInfo attaches the tooltip to a column's header cell. It is wired into
// the grid's HeaderCell, which the framework calls as it builds each header.
func (app *App) headerInfo(cell ui.Element, res *dbcore.QueryResult, visible []int, col int) {
	i := t2col(visible, col)
	if i < 0 {
		return
	}
	if text := app.columnInfoText(app.tab(), res, i); text != "" {
		cell.Tooltip(text)
	}
}
