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

// paintEdges draws the foreign keys into r: from the referencing box's right
// edge to the referenced box's left edge, as an elbow (out right, in left) so
// the direction reads.
func paintEdges(p *ui.Painter, r ui.Rect, m *erModel, t *ui.Theme) {
	byName := make(map[string]erTable, len(m.Tables))
	for _, tb := range m.Tables {
		byName[tb.Name] = tb
	}
	for _, fk := range m.FKs {
		from, ok1 := byName[fk.From]
		to, ok2 := byName[fk.To]
		if !ok1 || !ok2 {
			continue
		}
		fx, fy := from.at()
		tx, ty := to.at()
		x0, y0 := fx+from.Box.W, fy+from.Box.H/2 // 引用者右缘中点
		x1, y1 := tx, ty+to.Box.H/2              // 被引用者左缘中点
		mid := (x0 + x1) / 2
		p.Line(r.X+x0, r.Y+y0, r.X+mid, r.Y+y0, 1, t.Border)
		p.Line(r.X+mid, r.Y+y0, r.X+mid, r.Y+y1, 1, t.Border)
		p.Line(r.X+mid, r.Y+y1, r.X+x1, r.Y+y1, 1, t.Border)
	}
}

// erBoxView builds one table's box as real elements, not painted glyphs: the
// table's name and its columns are Text, so assistive technology reads the
// diagram and a test can find a box. The drag offset is the element's own, so
// the box follows the pointer and the edges follow the box.
func erBoxView(c *ui.Context, tb *erTable, t *ui.Theme) ui.Element {
	x, y := tb.at()
	return ui.Box(c).Absolute().Top(y).Left(x).Width(tb.Box.W).Height(tb.Box.H).
		Background(t.Surface).Border(1, t.Border).Radius(6).Padding(4, 6).
		Draggable(&tb.off, ui.DraggableOptions{}).
		Children(func() {
			ui.Text(c, tb.Name).Bold().FontSize(12).SingleLine()
			for _, col := range tb.Cols {
				name := col.Name
				if col.PK {
					name = "• " + name
				}
				ui.Text(c, name).FontSize(10).TextColor(t.TextMuted).SingleLine()
			}
		})
}

// erEnsureLoaded asks for whatever the diagram still needs: the schema's table
// list, then each table's columns. Both loads are asynchronous and end with an
// invalidate, so the next frame builds the view again — by then the answer is
// in and the diagram draws itself.
//
// It reports whether everything the diagram needs is in hand.
func (app *App) erEnsureLoaded() bool {
	if !app.open || app.db == nil {
		return true // nothing to load; erView says so
	}
	schemas := app.SchemaNames()
	if app.schema != "" {
		schemas = []string{app.schema}
	}
	for _, s := range schemas {
		if _, st := app.TablesOf(s); st == TablesNotLoaded {
			app.loadTables(s)
			return false
		}
	}
	for _, s := range schemas {
		tabs, _ := app.TablesOf(s)
		for _, t := range tabs {
			if len(app.erModel.Tables) >= erMaxTb {
				break
			}
			if _, st := app.ColumnsOf(s, t.Name); st == ColsNotLoaded {
				app.loadColumns(s, t.Name)
				return false
			}
		}
	}
	return true
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
	ui.Modal(c, &app.erOpen, func() {
		ui.Column(c).Gap(8).Padding(12).Width(720).Height(560).
			Background(t.Surface).Radius(10).Border(1, t.Border).
			Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "ER Diagram").FontSize(13).Bold()
					ui.Spacer(c)
					if ui.Button(c, "Close").Clicked() {
						app.erOpen = false
					}
				})
				if !app.erEnsureLoaded() {
					ui.Text(c, "Loading the schema…").FontSize(10).TextColor(t.TextMuted).SingleLine()
					return
				}
				// 每帧重取：列的异步加载完成后会 invalidate，这一帧就有数据了。
				app.erModel = app.erFetch()
				m := app.erModel
				w, h := erCanvasSize(m)
				if len(m.Tables) == 0 {
					ui.Text(c, "No tables in this schema").FontSize(10).TextColor(t.TextMuted).SingleLine()
					return
				}
				ui.Scroll(c).Grow(1).FillWidth().KeepScrollbars(true).Children(func() {
					// 每个盒子是独立元素（才能各自拖拽），连线画在它们之下的一层。
					ui.Box(c).Width(w).Height(h).Children(func() {
						ui.Box(c).Absolute().Top(0).Left(0).Width(w).Height(h).
							Draw(func(p *ui.Painter, r ui.Rect) {
								paintEdges(p, r, m, t)
							})
						for i := range m.Tables {
							tb := &m.Tables[i]
							erBoxView(c, tb, t)
						}
					})
				})
			})
	})
}
