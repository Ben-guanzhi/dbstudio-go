package main

// 可视化查询构建器（Navicat View Builder / DBeaver Visual Query Builder 的
// mygo 形态）：SelectSpec（dbcore/sqltext v0.9.0）直驱的表单——选主表 → 勾列
// → 加条件 → 生成 SELECT 进编辑器。
//
// 最小可用范围：主表 + 列多选 + AND 条件（col op value）+ LIMIT；JOIN/聚合/
// OR 嵌套后续按需（spec 模型已支持，UI 先落地主干）。标识符引用经
// sqltext.QuoteIdentLoose（方言由连接 Kind 决定）。

import (
	"strings"

	"github.com/Ben-guanzhi/dbcore/sqltext"
	"github.com/egoist/mygo/ui"
)

// qbCol 是构建器里一列的勾选态。
type qbCol struct {
	name string
	typ  string
	on   bool
}

// qbCond 是一个 AND 条件的表单态。
type qbCond struct {
	col   string
	op    string
	value string
}

// qbJoin 是一个 JOIN 的表单态（ON 左右列允许 "别名.列" 限定形式）。
type qbJoin struct {
	joinType string
	table    string
	onLeft   string
	onRight  string
}

// openBuilder 打开查询构建器 modal（Tools 菜单）。
func (app *App) openBuilder() {
	if app.db == nil || app.db.SQL == nil {
		app.errMsg = "先连接一个数据库"
		app.invalidate()
		return
	}
	app.qbOpen = true
	app.qbMsg = ""
	app.invalidate()
}

// qbDialect 当前连接的方言名（sqltext 的拼写）。
func (app *App) qbDialect() string { return string(app.db.Kind) }

// qbQuote 方言引用的标识符。
func (app *App) qbQuote(name string) string {
	return sqltext.QuoteIdentLoose(app.qbDialect(), name)
}

// qbSpec 从当前表单状态拼 SelectSpec（标识符已引用）。
func (app *App) qbSpec() sqltext.SelectSpec {
	d := app.qbDialect()
	spec := sqltext.SelectSpec{
		Table: sqltext.QuoteIdentLoose(d, app.qbTable),
		Limit: int(app.qbLimit),
	}
	for _, j := range app.qbJoins {
		if j.table == "" {
			continue
		}
		join := sqltext.QBJoin{
			Table: app.qbQuote(j.table),
			Type:  j.joinType,
		}
		if j.onLeft != "" && j.onRight != "" {
			join.On = append(join.On, sqltext.QBCond{
				Kind: "cmp",
				Col:  app.qbQuote(j.onLeft),
				Op:   "=",
				// 右侧是列名（限定形式），不是字面量——原样引用。
				Value: app.qbQuote(j.onRight),
			})
		}
		spec.Joins = append(spec.Joins, join)
	}
	for _, col := range app.qbCols {
		if col.on {
			spec.Columns = append(spec.Columns, sqltext.QBColumn{Col: app.qbQuote(col.name)})
		}
	}
	for _, cond := range app.qbConds {
		if cond.col == "" {
			continue
		}
		spec.Where = append(spec.Where, sqltext.QBCond{
			Kind: "cmp",
			Col:  app.qbQuote(cond.col),
			Op:   cond.op,
			Value: func() string {
				// 文本类操作符的值按字面量渲染；数字裸值。
				switch strings.ToUpper(cond.op) {
				case "IN":
					return cond.value
				default:
					return sqltext.QuotedLiteral(d, cond.value)
				}
			}(),
		})
	}
	return spec
}

// builderView draws the query-builder modal.
func (app *App) builderView(c *ui.Context) {
	if !app.qbOpen {
		return
	}
	t := c.Theme()
	ui.Modal(c, &app.qbOpen, func() {
		// Select 无 onChange 回调——每帧比较主表（history 的 prev 同款手法），
		// 变化即重拉列清单。
		if app.qbTable != app.qbTableSeen {
			app.qbTableSeen = app.qbTable
			app.qbOnTableChanged()
		}
		ui.Column(c).Gap(8).Padding(12).Width(720).Height(540).
			Background(t.Surface).Radius(10).Border(1, t.Border).
			Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "Query Builder").FontSize(13).Bold()
					ui.Text(c, app.qbMsg).FontSize(10).TextColor(t.TextMuted).SingleLine()
					ui.Spacer(c)
					if ui.Button(c, "Close").Clicked() {
						app.qbOpen = false
					}
				})
				// 主表下拉。
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "主表").FontSize(11)
					ui.Select(c, &app.qbTable, app.schemaTables()).Label("主表")
				})
				// 列多选（勾选 = 进投影）。
				ui.Text(c, "列（勾选进投影；全不勾 = SELECT *）").FontSize(10).TextColor(t.TextMuted)
				ui.Scroll(c).Height(140).FillWidth().KeepScrollbars(true).Children(func() {
					app.qbColsList(c)
				})
				// JOIN 行（可选：表/类型/ON 左右列——左列为主表或先连的表）。
				for i := range app.qbJoins {
					app.qbJoinRow(c, i)
				}
				ui.Row(c).Gap(8).Children(func() {
					if ui.Button(c, "+ JOIN").FontSize(10).Clicked() {
						app.qbJoins = append(app.qbJoins, qbJoin{joinType: "INNER"})
						app.invalidate()
					}
				})
				// 条件行。
				ui.Text(c, "条件（AND 连接）").FontSize(10).TextColor(t.TextMuted)
				for i := range app.qbConds {
					app.qbCondRow(c, i)
				}
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "LIMIT").FontSize(11)
					ui.NumberInput(c, &app.qbLimit, 0, 100000, 1)
					ui.Spacer(c)
					if ui.Button(c, "+ 条件").FontSize(10).Clicked() {
						app.qbConds = append(app.qbConds, qbCond{})
						app.invalidate()
					}
					if ui.Button(c, "生成 SELECT → 编辑器").FontSize(11).Clicked() {
						app.qbGenerate()
					}
				})
			})
	})
}

// qbColsList 列选择列表（点击切换选中态；mygo 无 checkbox 控件——用
// 「●/○ 前缀按钮」表达选中，与 schema menu 的 selected 行同一手法）。
func (app *App) qbColsList(c *ui.Context) {
	for i := range app.qbCols {
		i := i
		col := app.qbCols[i]
		mark := "○"
		if col.on {
			mark = "●"
		}
		label := mark + " " + col.name + "  ·  " + col.typ
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			if ui.Button(c, label).FontSize(11).Clicked() {
				app.qbCols[i].on = !app.qbCols[i].on
			}
		})
	}
}

// qbJoinRow 一个 JOIN 行：连接类型 + 表 + ON 左右列（ON 右列允许
// "别名.列" 的限定形式——TextInput 自由填写）。
func (app *App) qbJoinRow(c *ui.Context, i int) {
	j := &app.qbJoins[i]
	joinTypes := []string{"INNER", "LEFT", "RIGHT", "CROSS"}
	ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Select(c, &j.joinType, joinTypes).Label("JOIN 类型")
		ui.Select(c, &j.table, app.schemaTables()).Label("JOIN 表")
		ui.TextInput(c, &j.onLeft).Width(120).Label("ON 左列")
		ui.Text(c, "=").FontSize(11)
		ui.TextInput(c, &j.onRight).Width(120).Label("ON 右列")
		if ui.Button(c, "×").FontSize(10).Clicked() {
			app.qbJoins = append(app.qbJoins[:i], app.qbJoins[i+1:]...)
			app.invalidate()
		}
	})
}

// qbCondRow 一个条件行：列下拉 + 操作符下拉 + 值输入。
func (app *App) qbCondRow(c *ui.Context, i int) {
	cond := &app.qbConds[i]
	names := app.qbColNames()
	ops := []string{"=", "<>", ">", "<", ">=", "<=", "LIKE", "NOT LIKE"}
	ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Select(c, &cond.col, names).Label("条件列")
		ui.Select(c, &cond.op, ops).Label("操作符")
		ui.TextInput(c, &cond.value).Width(160).Placeholder("值")
		if ui.Button(c, "×").FontSize(10).Clicked() {
			app.qbConds = append(app.qbConds[:i], app.qbConds[i+1:]...)
			app.invalidate()
		}
	})
}

// qbGenerate 生成 SQL 并放进编辑器（不自动执行——用户先看一眼）。
func (app *App) qbGenerate() {
	spec := app.qbSpec()
	if spec.Table == "" || spec.Table == "``" {
		app.qbMsg = "先选主表"
		app.invalidate()
		return
	}
	sql := sqltext.BuildSelect(spec)
	app.edBuf.Set(sql)
	app.qbOpen = false
	app.status = "已生成 SELECT（未执行——先看一眼再跑）"
	app.invalidate()
}

// qbOnTableChanged 主表变化：重拉该表的列（树已缓存则直接用）。
func (app *App) qbOnTableChanged() {
	app.qbCols = nil
	app.qbConds = nil
	cols, _ := app.ColumnsOf(app.schema, app.qbTable)
	for _, c := range cols {
		app.qbCols = append(app.qbCols, qbCol{name: c.Name, typ: c.Type})
	}
	app.loadColumns(app.schema, app.qbTable)
	app.invalidate()
}

// qbColNames 当前表的列名（供条件下拉）。
func (app *App) qbColNames() []string {
	names := make([]string, 0, len(app.qbCols))
	for _, c := range app.qbCols {
		names = append(names, c.name)
	}
	return names
}

// schemaTables 当前 schema 已加载的表名列表（供主表下拉）。
func (app *App) schemaTables() []string {
	out := make([]string, 0, 32)
	tabs, _ := app.TablesOf(app.schema)
	for _, t := range tabs {
		out = append(out, t.Name)
	}
	return out
}

