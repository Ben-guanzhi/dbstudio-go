package main

// The DDL viewer.
//
// A table's DDL is not the SELECT that browses it: it is the statement that
// creates it, which the server keeps and the client asks for in the dialect it
// speaks. Tables come back from the driver's own CreateTableSQL; views are
// built from the definition the extra metadata reader returns, wrapped into a
// CREATE OR REPLACE VIEW by sqltext. The viewer shows the statement in a
// read-only editor, and can copy it or turn the table's columns into an INSERT
// that loads a row of it.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Ben-guanzhi/dbcore"
	"github.com/Ben-guanzhi/dbcore/sqltext"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// openDDL asks for the DDL of a table and shows it in its modal. It is the tree
// row's menu that calls it, and the load it starts is dropped if another table's
// DDL is opened before it answers.
func (app *App) openDDL(n treeNode) {
	if n.kind != nodeTable && n.kind != nodeObject {
		return
	}
	if app.db == nil || app.db.SQL == nil {
		app.errMsg = "Not connected"
		app.invalidate()
		return
	}
	app.ddlOpen = true
	app.ddlBusy = true
	app.ddlErr = ""
	app.ddlSchema = n.parent
	app.ddlTable = n.label
	app.ddlGen++
	app.invalidate()
	app.loadDDL(n, app.ddlGen)
}

// loadDDL reads the statement in the background, so the window stays free while
// the server is asked. A test has no window, so there the read fills the state
// directly: the state is what the test is about.
func (app *App) loadDDL(n treeNode, gen uint64) {
	db := app.db
	if app.win == nil {
		text, err := app.readDDL(context.Background(), db, n)
		if gen == app.ddlGen {
			app.ddlBusy = false
			if err != nil {
				app.ddlErr = err.Error()
				return
			}
			if strings.TrimSpace(text) == "" {
				app.ddlErr = "The server returned no DDL"
				return
			}
			app.ddlBuf.Set(text)
		}
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		text, err := app.readDDL(ctx, db, n)
		app.post(func() {
			if gen != app.ddlGen {
				return
			}
			app.ddlBusy = false
			if err != nil {
				app.ddlErr = err.Error()
				return
			}
			if strings.TrimSpace(text) == "" {
				app.ddlErr = "The server returned no DDL"
				return
			}
			app.ddlBuf.Set(text)
		})
	}()
}

// readDDL is the one read behind the viewer. Tables come from the driver's own
// DDL reader; views and the extended objects (indexes, foreign keys, CHECK
// constraints, partitions, sequences, routines, enums) ask the extra metadata
// and structured readers for their definitions. The window's background load
// and a headless test both go through here.
func (app *App) readDDL(ctx context.Context, db *DB, n treeNode) (string, error) {
	if n.kind == nodeObject {
		return app.objectDDL(ctx, n)
	}
	if app.tableType(n) != "table" {
		extra, ok := dbcore.AsExtraMetadata(db.Driver)
		if !ok {
			return "", fmt.Errorf("view definitions are not available for this driver")
		}
		body, err := extra.ViewDefinition(ctx, n.parent, n.label)
		if err != nil {
			return "", err
		}
		return sqltext.ComposeViewDDL(app.engine(), n.parent, n.label, body), nil
	}
	return db.SQL.CreateTableSQL(ctx, n.parent, n.label)
}

// tableType is the kind of object a tree node names, as the table list calls it.
// An object the list does not know is taken to be a table, which is what the
// driver's own DDL reader answers.
func (app *App) tableType(n treeNode) string {
	return app.TableType(n.parent, n.label)
}

// treeNodeMenu gives a schema tree row its context menu: what the row names can
// be looked at and acted on.
func (app *App) treeNodeMenu(m *ui.Menu, n treeNode) {
	switch n.kind {
	case nodeTable:
		if m.Item("Browse Data").Chosen() {
			app.browseTable(n)
		}
		if m.Item("Structure").Chosen() {
			// 波 100：表结构查看——列/类型/键/默认/注释的表格（TablePlus 的
			// Structure 页），与 View DDL 并列：一个是事实表，一个是语句。
			app.openStructure(n)
		}
		if m.Item("View DDL").Chosen() {
			app.openDDL(n)
		}
		if m.Item("Import Data…").Chosen() {
			app.beginImport(n)
		}
		if m.Item("Find Usages").Chosen() {
			app.findUsages(n)
		}
		if m.Item("Rename…").Chosen() {
			app.startRename(n)
		}
		if m.Item("Empty Table…").Chosen() {
			app.emptyTable(n)
		}
	case nodeColumn:
		if m.Item("Copy Name").Chosen() {
			mygo.Clipboard.WriteText(n.label)
			app.status = "Copied " + n.label
			app.invalidate()
		}
		if m.Item("Find Usages").Chosen() {
			// A column's usages are the keys that point at it, in the table the
			// column belongs to.
			app.findUsages(treeNode{label: n.table, parent: n.parent, kind: nodeTable})
		}
		if m.Item("Rename Column…").Chosen() {
			app.startColumnRename(n)
		}
	}
}

// ddlView draws the DDL viewer's modal, and the buttons that act on the
// statement it holds.
func (app *App) ddlView(c *ui.Context) {
	if !app.ddlOpen {
		return
	}
	t := c.Theme()
	ui.Modal(c, &app.ddlOpen, func() {
		ui.Column(c).Gap(10).Padding(12).Width(780).Height(520).
			Background(t.Surface).Radius(10).Border(1, t.Border).
			Children(func() {
				ui.Row(c).Gap(6).Children(func() {
					ui.Text(c, "DDL · "+app.ddlTable).FontSize(13).Bold().SingleLine()
					ui.Spacer(c)
					if app.ddlBusy {
						ui.Spinner(c)
					}
				})
				if app.ddlErr != "" {
					ui.Text(c, app.ddlErr).TextColor(t.Danger).Padding(2, 0).Wrap()
				}
				editor.View(c, app.ddlState).Grow(1).FillWidth()
				ui.Row(c).Gap(8).Children(func() {
					ui.Spacer(c)
					if ui.Button(c, "Generate INSERTs").Disabled(app.ddlBusy).Clicked() {
						app.ddlInserts()
					}
					if ui.Button(c, "Copy DDL").Disabled(app.ddlBusy).Clicked() {
						mygo.Clipboard.WriteText(app.ddlBuf.String())
						app.status = "Copied the DDL"
						app.invalidate()
					}
					if ui.Button(c, "Close").Clicked() {
						app.ddlOpen = false
					}
				})
			})
	})
}

// ddlInserts turns the table's columns into an INSERT the statement can load, and
// puts it after the DDL in the viewer. When the grid holds the table as a working
// copy, the rows it shows are what is rendered; otherwise a single template names
// every column.
func (app *App) ddlInserts() {
	var text string
	var err error
	if app.edit.active && app.edit.table == app.ddlTable && app.edit.schema == app.ddlSchema {
		text, err = app.copyText(copyInsert, app.result, nil)
	} else {
		text, err = insertStatement(app.engine(), app.ddlSchema, app.ddlTable, app.ddlColumns(), nil)
	}
	if err != nil {
		app.ddlErr = err.Error()
		app.invalidate()
		return
	}
	app.ddlErr = ""
	app.ddlBuf.Set(app.ddlBuf.String() + "\n\n" + text)
	app.status = "Generated an INSERT"
	app.invalidate()
}

// ddlColumns is the table's columns, from the working copy when it holds the
// table, else from the columns the tree loaded.
func (app *App) ddlColumns() []dbcore.ColumnInfo {
	if app.edit.active && app.edit.table == app.ddlTable && app.edit.schema == app.ddlSchema {
		return app.edit.cols
	}
	cols, _ := app.ColumnsOf(app.ddlSchema, app.ddlTable)
	return cols
}
