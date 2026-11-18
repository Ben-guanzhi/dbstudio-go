package main

// The structure viewer: a table's columns as a grid — name, type, key,
// nullability, default, comment — the "Structure" page TablePlus and DataGrip
// put beside the data. The DDL viewer shows the CREATE statement; this shows
// the same facts as rows, which is what "what columns does it have" wants.
//
// 波 100：树右键加 Structure；数据从 app 的列缓存拿（树展开过就是热的），没
// 加载过就异步读一次，生成守卫与 DDL 的相同。

import (
	"context"
	"strings"
	"strconv"
	"time"

	"github.com/Ben-guanzhi/dbcore"
	"github.com/Ben-guanzhi/dbcore/ddl"
	"github.com/egoist/mygo/ui"
)

// openStructure opens the structure modal for a table. It is the tree row's
// menu that calls it; the load is dropped when another table's structure is
// opened before it answers (the same generation guard the DDL viewer uses).
func (app *App) openStructure(n treeNode) {
	if n.kind != nodeTable {
		return
	}
	if app.db == nil || app.db.SQL == nil {
		app.errMsg = "Not connected"
		app.invalidate()
		return
	}
	app.stOpen = true
	app.stBusy = true
	app.stErr = ""
	app.stTable = n.parent + "." + n.label
	app.stGen++
	app.invalidate()
	app.loadStructure(n, app.stGen)
}

// loadStructure reads the table's columns off the main thread and applies them
// on it, dropping a stale answer by generation.
func (app *App) loadStructure(n treeNode, gen uint64) {
	db := app.db
	loadAsync(app, metadataTimeout, func(ctx context.Context) ([]dbcore.ColumnInfo, error) {
		return db.SQL.ListColumns(ctx, n.parent, n.label)
	}, func(cols []dbcore.ColumnInfo, err error) {
		if gen != app.stGen {
			return
		}
		app.stBusy = false
		if err != nil {
			app.stErr = firstLine(err.Error())
			app.invalidate()
			return
		}
		app.stCols = cols
		// 索引区与列同源拉取（引擎不支持 ListIndexes 时空——查看仍是全的）。
		loadAsync(app, metadataTimeout, func(ctx context.Context) ([]dbcore.IndexInfo, error) {
			return db.SQL.ListIndexes(ctx, n.parent, n.label)
		}, func(idxs []dbcore.IndexInfo, ierr error) {
			if gen != app.stGen {
				return
			}
			if ierr == nil {
				app.stIdxs = idxs
			}
			app.invalidate()
		})
		app.invalidate()
	})
}

// structureView draws the modal: the columns' grid and the actions beside it.
func (app *App) structureView(c *ui.Context) {
	if !app.stOpen {
		return
	}
	t := c.Theme()
	ui.Modal(c, &app.stOpen, func() {
		ui.Column(c).Gap(10).Padding(12).Width(720).Height(480).
			Background(t.Surface).Radius(10).Border(1, t.Border).
			Children(func() {
				ui.Row(c).Gap(6).Children(func() {
					ui.Text(c, "Structure · "+app.stTable).FontSize(13).Bold().SingleLine()
					ui.Spacer(c)
					if app.stBusy {
						ui.Spinner(c)
					}
				})
				if app.stErr != "" {
					ui.Text(c, app.stErr).TextColor(t.Danger).Padding(2, 0).Wrap()
				}
				app.structureGrid(c, t)
				app.structureIndexRow(c, t)
				ui.Row(c).Gap(8).Children(func() {
					ui.Spacer(c)
					if ui.Button(c, "View DDL").Clicked() {
						app.openDDL(treeNode{label: app.stTableOnly(), parent: app.stSchema(), kind: nodeTable})
					}
					if ui.Button(c, "Close").Clicked() {
						app.stOpen = false
					}
				})
			})
	})
}

// structureGrid draws the columns as a table: # / name / type / key / null /
// default / comment. A cell is one line; the comment truncates with a tooltip
// from the grid's own machinery.
func (app *App) structureGrid(c *ui.Context, t *ui.Theme) {
	cols := []ui.TableColumn{
		{Title: "#", RowHeader: true, Fixed: true, Width: 40},
		{Title: "Name", Width: 150},
		{Title: "Type", Width: 120},
		{Title: "Key", Width: 64, Align: ui.Center},
		{Title: "Null", Width: 64, Align: ui.Center},
		{Title: "Default", Width: 120},
		{Title: "Comment", Width: 220},
	}
	n := len(app.stCols)
	ui.Table(c, &app.stGrid, cols, n, func(row, col int) {
		info := app.stCols[row]
		switch col {
		case 0:
			ui.Text(c, strconv.Itoa(row+1)).TextColor(t.TextMuted).SingleLine()
		case 1:
			ui.Text(c, info.Name).Bold().SingleLine()
		case 2:
			ui.Text(c, info.Type).SingleLine()
		case 3:
			switch {
			case info.IsPrimaryKey:
				ui.Text(c, "PK").TextColor(t.Accent).Bold().SingleLine()
			case info.IsAutoIncrement:
				ui.Text(c, "auto").TextColor(t.TextMuted).SingleLine()
			}
		case 4:
			if info.Nullable {
				ui.Text(c, "NULL").TextColor(t.TextMuted).SingleLine()
			} else {
				ui.Text(c, "NOT NULL").SingleLine()
			}
		case 5:
			if info.Default != nil {
				ui.Text(c, *info.Default).SingleLine()
			} else {
				ui.Text(c, "—").TextColor(t.TextMuted).SingleLine()
			}
		case 6:
			ui.Text(c, info.Comment).TextColor(t.TextMuted).SingleLine()
		}
	}).Grow(1).KeepScrollbars(true)
}

// stTableOnly splits the "schema.table" heading back into its parts for the
// DDL hand-off.
func (app *App) stTableOnly() string {
	for i := 0; i < len(app.stTable); i++ {
		if app.stTable[i] == '.' {
			return app.stTable[i+1:]
		}
	}
	return app.stTable
}

func (app *App) stSchema() string {
	for i := 0; i < len(app.stTable); i++ {
		if app.stTable[i] == '.' {
			return app.stTable[:i]
		}
	}
	return ""
}

// structureIndexRow 把该表的索引画成一行行「名字（列） + Drop」——Drop 走
// 内核 DropIndex（refuseWrite 闸门 + 直接执行 + refreshSchema，与列/表改名
// 的同一闸门纪律）。
func (app *App) structureIndexRow(c *ui.Context, t *ui.Theme) {
	if len(app.stIdxs) == 0 {
		return
	}
	ui.Text(c, "Indexes").FontSize(11).Bold()
	for _, ix := range app.stIdxs {
		if ix.Primary {
			continue // 主键删除是表级操作（DropPrimaryKey），不混在索引区
		}
		ix := ix
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, ix.Name).SingleLine()
			ui.Text(c, "("+strings.Join(ix.Columns, ", ")+")").
				FontSize(10).TextColor(t.TextMuted).SingleLine()
			ui.Spacer(c)
			if ui.Button(c, "Drop").FontSize(10).Clicked() {
				app.dropTableIndex(ix.Name)
			}
		})
	}
}

// dropTableIndex 删一个索引：只读闸门 → 内核 DDL → 执行 → 全量刷新。
func (app *App) dropTableIndex(name string) {
	if app.readOnly() {
		app.refuseWrite("DROP INDEX " + name)
		return
	}
	stmt := ddl.DropIndex(app.qbDialectForDDL(), app.stSchema(), app.stTableOnly(), name)
	app.status = "Dropping index…"
	app.invalidate()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, err := app.db.SQL.Execute(ctx, stmt)
		app.post(func() {
			if err != nil {
				app.errMsg = "Drop index failed: " + err.Error()
				app.status = ""
			} else {
				app.status = "Dropped index " + name
				app.refreshSchema()
			}
			app.invalidate()
		})
	}()
}
