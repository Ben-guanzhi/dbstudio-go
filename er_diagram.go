package main

// The ER diagram: tables as boxes, foreign keys as edges, laid out by
// layoutER and drawn by the same Element.Draw callback the chart uses.
//
// mygo has no canvas widget; an element's Draw callback is handed the painter
// and its own box every frame. So the diagram is one box the size of the
// laid-out graph, and the drawing is a pure function of the model.
//
// 波 101 续四十六.

import (
	"context"
	"fmt"

	"github.com/egoist/mygo/ui"
)

// erFetch builds the diagram from the connection's catalog: the tables of the
// working schema with their columns, and the foreign keys between them.
func (app *App) erFetch() *erModel {
	m := &erModel{}
	if !app.open {
		return m
	}
	schemas := app.SchemaNames()
	if app.schema != "" {
		schemas = []string{app.schema}
	}
	for _, schema := range schemas {
		tabs, _ := app.TablesOf(schema)
		for _, t := range tabs {
			if len(m.Tables) >= erMaxTb {
				break
			}
			if t.Type != "" && t.Type != "table" {
				continue // 视图不是节点：它没有自己的列
			}
			// 列未加载时表照画（盒子上只有表名）：一张表在目录里而它的列
			// 还没取到，不是不画它的理由。
			cols, _ := app.ColumnsOf(schema, t.Name)
			et := erTable{Name: t.Name}
			for _, c := range cols {
				et.Cols = append(et.Cols, erCol{Name: c.Name, PK: c.IsPrimaryKey})
			}
			m.Tables = append(m.Tables, et)
		}
	}
	// 外键：每张表问一次目录。只保留两端都在图里的（跨 schema 的引用画不出来）。
	// 没有会话时（测试里手工构造的 app）就只画表。
	if app.db == nil {
		m.layoutER()
		return m
	}
	inDiagram := make(map[string]bool, len(m.Tables))
	for _, t := range m.Tables {
		inDiagram[t.Name] = true
	}
	for _, schema := range schemas {
		tabs, _ := app.TablesOf(schema)
		for _, t := range tabs {
			if !inDiagram[t.Name] || len(m.FKs) >= erMaxTb*4 {
				continue
			}
			fks, err := app.db.SQL.ListForeignKeys(context.Background(), schema, t.Name)
			if err != nil {
				continue
			}
			for _, f := range fks {
				if !inDiagram[f.RefTable] {
					continue
				}
				for i, col := range f.Columns {
					ref := ""
					if i < len(f.RefColumns) {
						ref = f.RefColumns[i]
					}
					m.FKs = append(m.FKs, erFK{From: t.Name, FromCol: col, To: f.RefTable, ToCol: ref})
				}
			}
		}
	}
	m.layoutER()
	return m
}

// erCanvasSize is how big a box the laid-out graph needs, plus its margin.
func erCanvasSize(m *erModel) (w, h float32) {
	w, h = erGap, erGap
	for _, t := range m.Tables {
		if t.Box.X+t.Box.W > w {
			w = t.Box.X + t.Box.W
		}
		if t.Box.Y+t.Box.H > h {
			h = t.Box.Y + t.Box.H
		}
	}
	return w + erGap, h + erGap
}

// paintER draws the graph into r: edges first (under the boxes), then a box
// per table with its title and columns.
func paintER(p *ui.Painter, r ui.Rect, m *erModel, t *ui.Theme) {
	byName := make(map[string]erTable, len(m.Tables))
	for _, tb := range m.Tables {
		byName[tb.Name] = tb
	}
	// 连线：从引用者盒子的右缘到被引用者盒子的左缘，走一条折线（右出、左进），
	// 让方向可读。
	for _, fk := range m.FKs {
		from, ok1 := byName[fk.From]
		to, ok2 := byName[fk.To]
		if !ok1 || !ok2 {
			continue
		}
		x0 := from.Box.X + from.Box.W
		y0 := from.Box.Y + from.Box.H/2
		x1 := to.Box.X
		y1 := to.Box.Y + to.Box.H/2
		mid := (x0 + x1) / 2
		p.Line(x0, y0, mid, y0, 1, t.Border)
		p.Line(mid, y0, mid, y1, 1, t.Border)
		p.Line(mid, y1, x1, y1, 1, t.Border)
	}
	// 盒子：表面底 + 边框 + 标题 + 每列一行，主键列标出来。
	for _, tb := range m.Tables {
		b := tb.Box
		p.Fill(ui.Rect{X: r.X + b.X, Y: r.Y + b.Y, W: b.W, H: b.H}, t.Surface, 6)
		p.Stroke(ui.Rect{X: r.X + b.X, Y: r.Y + b.Y, W: b.W, H: b.H}, t.Border, 6, 1)
		p.Text(r.X+b.X+8, r.Y+b.Y+6, tb.Name, 12, t.Text)
		y := b.Y + erTitleH
		for _, c := range tb.Cols {
			name := c.Name
			if c.PK {
				name = "• " + name
			}
			p.Text(r.X+b.X+8, r.Y+y, name, 10, t.TextMuted)
			y += erRowH
		}
	}
}

// openER builds the diagram and shows it.
func (app *App) openER() {
	app.erModel = app.erFetch()
	app.erOpen = true
	app.invalidate()
}

// erView draws the diagram modal: the graph in a scrolling box, with the count
// of tables it holds.
func (app *App) erView(c *ui.Context) {
	if !app.erOpen {
		return
	}
	t := c.Theme()
	m := app.erModel
	w, h := erCanvasSize(m)
	ui.Modal(c, &app.erOpen, func() {
		ui.Column(c).Gap(8).Padding(12).Width(720).Height(560).
			Background(t.Surface).Radius(10).Border(1, t.Border).
			Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "ER Diagram").FontSize(13).Bold()
					ui.Spacer(c)
					if ui.PrimaryButton(c, "Close").Clicked() {
						app.erOpen = false
					}
				})
				if len(m.Tables) == 0 {
					ui.Text(c, "No tables in this schema.").FontSize(10).TextColor(t.TextMuted).SingleLine()
					return
				}
				ui.Text(c, fmt.Sprintf("%d table(s)", len(m.Tables))+" · "+fmt.Sprintf("%d foreign key(s)", len(m.FKs))).
					FontSize(10).TextColor(t.TextMuted).SingleLine()
				ui.Scroll(c).Grow(1).FillWidth().KeepScrollbars(true).Children(func() {
					ui.Box(c).Width(w).Height(h).Draw(func(p *ui.Painter, r ui.Rect) {
						paintER(p, r, m, t)
					})
				})
			})
	})
}
