package main

// 模型编辑器（Navicat Model / MySQL Workbench 的 EER 模型的最小可用形态）：
// **表单驱动**（非拖拽画布）——内核 dbcore/model（v0.11.0）的 Model 直驱。
//
// 编辑流：实体下拉选中 → 列表（加列/删列/列名/类型/主键勾选）→ 关系列表
// （from/to/card）→ 「生成 DDL」进编辑器（可复制/直接跑）→ 「存/载入 JSON」
// （模型是可版本控制的设计文件——scripts 目录旁的 models/）。
//
// 与画布的边界：拖拽布局是交互升级，**数据模型与 DDL 生成已在内核**；表单
// 先让正向工程可用——「模型文件→可执行的建表脚本」闭环。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Ben-guanzhi/dbcore/model"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// openModelEditor 打开模型编辑器 modal（Tools 菜单）。
func (app *App) openModelEditor() {
	if app.modelEdit == nil {
		app.modelEdit = &model.UIModel{Model: &model.Model{Name: "design", Dialect: "mysql"}}
	}
	app.modelOpen = true
	app.modelMsg = ""
	app.invalidate()
}

// modelEditorView draws the model editor modal.
func (app *App) modelEditorView(c *ui.Context) {
	if !app.modelOpen {
		return
	}
	t := c.Theme()
	m := app.modelEdit
	ui.Modal(c, &app.modelOpen, func() {
		ui.Column(c).Gap(8).Padding(12).Width(820).Height(600).
			Background(t.Surface).Radius(10).Border(1, t.Border).
			Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "Model Designer").FontSize(13).Bold()
					ui.Text(c, app.modelMsg).FontSize(10).TextColor(t.TextMuted).SingleLine()
					ui.Spacer(c)
					if ui.Button(c, "Save JSON").FontSize(10).
						Tooltip("模型存为 models/<名>.json（可版本控制的设计文件）").
						Clicked() {
						app.modelSaveJSON()
					}
					if ui.Button(c, "Load JSON").FontSize(10).
						Tooltip("载入一个模型文件").
						Clicked() {
						app.modelLoadJSON()
					}
					if ui.Button(c, "Close").Clicked() {
						app.modelOpen = false
					}
				})
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "方言").FontSize(11)
					ui.Select(c, &m.Model.Dialect, []string{"mysql", "postgres", "sqlserver"}).Label("方言")
					ui.Text(c, "实体").FontSize(11)
					ui.Select(c, &app.modelSelEntity, m.EntityNames()).Label("当前实体")
					if ui.Button(c, "+ 实体").FontSize(10).Clicked() {
						app.modelAddEntity()
					}
				})
				ui.Scroll(c).Grow(1).FillWidth().KeepScrollbars(true).Children(func() {
					app.modelEntityPane(c, t)
					app.modelRelationsPane(c, t)
				})
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					if ui.Button(c, "生成 DDL → 编辑器").FontSize(11).Clicked() {
						app.modelGenerate()
					}
					ui.Spacer(c)
				})
			})
	})
}

// modelEntityPane 当前实体的编辑：列行（名/类型/可空/主键勾选/删除）。
func (app *App) modelEntityPane(c *ui.Context, t *ui.Theme) {
	m := app.modelEdit
	e, ok := m.Entity(app.modelSelEntity)
	if !ok {
		ui.Text(c, "没有选中实体——「+ 实体」新建。").FontSize(10).TextColor(t.TextMuted).SingleLine()
		return
	}
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "实体 "+e.Name).FontSize(12).Bold()
		if ui.Button(c, "删实体").FontSize(10).Clicked() {
			m.RemoveEntity(app.modelSelEntity)
			app.modelSelEntity = ""
			app.invalidate()
		}
	})
	ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.TextInput(c, &app.modelNewCol).Width(120).Label("新列名")
		ui.Select(c, &app.modelNewColType, modelTypeNames()).Label("类型")
		if ui.Button(c, "+ 列").FontSize(10).Clicked() {
			if strings.TrimSpace(app.modelNewCol) != "" {
				m.AddColumn(app.modelSelEntity, model.Column{
					Name: strings.TrimSpace(app.modelNewCol),
					Type: app.modelNewColType, Nullable: true,
				})
				app.modelNewCol = ""
				app.invalidate()
			}
		}
	})
	for ci := range e.Columns {
		ci := ci
		col := &e.Columns[ci]
		isPK := app.modelEdit.IsPK(app.modelSelEntity, col.Name)
		mark := "  "
		if isPK {
			mark = "★ "
		}
		pkLabel := "设为主键"
		if isPK {
			pkLabel = "取消主键"
		}
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, mark).FontSize(11)
			ui.TextInput(c, &col.Name).Width(120).Label("列名")
			ui.Select(c, &col.Type, modelTypeNames()).Label("列类型")
			if ui.Button(c, pkLabel).FontSize(10).
				Tooltip("切换主键成员").Clicked() {
				m.TogglePK(app.modelSelEntity, col.Name)
				app.invalidate()
			}
			if ui.Button(c, "×").FontSize(10).Clicked() {
				m.RemoveColumn(app.modelSelEntity, ci)
				app.invalidate()
			}
		})
	}
}

// modelRelationsPane 关系列表（from/to/基数/删除）+ 新关系行。
func (app *App) modelRelationsPane(c *ui.Context, t *ui.Theme) {
	m := app.modelEdit
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "关系").FontSize(12).Bold()
		if ui.Button(c, "+ 1:N（外键落在 to 侧）").FontSize(10).Clicked() {
			m.AddRelation(model.Relation{Card: "1:N", From: firstModelEntity(m), To: firstModelEntity(m)})
			app.invalidate()
		}
		if ui.Button(c, "+ N:M（自动关联表）").FontSize(10).Clicked() {
			m.AddRelation(model.Relation{Card: "N:M", From: firstModelEntity(m), To: firstModelEntity(m)})
			app.invalidate()
		}
	})
	for ri := range m.Model.Relations {
		ri := ri
		r := &m.Model.Relations[ri]
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Select(c, &r.From, m.EntityNames()).Label("关系 from")
			ui.Select(c, &r.To, m.EntityNames()).Label("关系 to")
			ui.Select(c, &r.Card, []string{"1:N", "N:M"}).Label("基数")
			if ui.Button(c, "×").FontSize(10).Clicked() {
				m.RemoveRelation(ri)
				app.invalidate()
			}
		})
	}
}

// modelGenerate 生成 DDL 放进编辑器（不自动执行——建模产物通常还要手改）。
func (app *App) modelGenerate() {
	ddl, err := app.modelEdit.GenerateDDL()
	if err != nil {
		app.modelMsg = "生成失败：" + err.Error()
		app.invalidate()
		return
	}
	app.edBuf.Set(ddl)
	app.modelOpen = false
	app.status = "已生成模型 DDL（未执行——先看一眼再跑）"
	app.invalidate()
}

// modelSaveJSON 模型存为 models/<名>.json（设计文件可版本控制）。
func (app *App) modelSaveJSON() {
	m := app.modelEdit
	raw, err := json.MarshalIndent(m.Model, "", "  ")
	if err != nil {
		app.modelMsg = "序列化失败：" + err.Error()
		app.invalidate()
		return
	}
	dir := filepath.Join(app.store.Dir(), "models")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		app.modelMsg = err.Error()
		app.invalidate()
		return
	}
	name := strings.TrimSpace(m.Model.Name)
	if name == "" {
		name = "design"
	}
	path := filepath.Join(dir, name+".json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		app.modelMsg = err.Error()
		app.invalidate()
		return
	}
	app.modelMsg = "已存 " + path
	app.invalidate()
}

// modelLoadJSON 载入一个模型文件（系统对话框）。
func (app *App) modelLoadJSON() {
	paths, err := mygoDialogOpenJSON()
	if err != nil || len(paths) == 0 || paths[0] == "" {
		return
	}
	raw, err := os.ReadFile(paths[0])
	if err != nil {
		app.modelMsg = err.Error()
		app.invalidate()
		return
	}
	var m model.Model
	if err := json.Unmarshal(raw, &m); err != nil {
		app.modelMsg = "不是合法的模型文件：" + err.Error()
		app.invalidate()
		return
	}
	app.modelEdit = &model.UIModel{Model: &m}
	app.modelSelEntity = firstModelEntity(app.modelEdit)
	app.modelMsg = "已载入 " + filepath.Base(paths[0])
	app.invalidate()
}

// firstModelEntity 第一个实体名（新关系/载入后的默认选中）。
func firstModelEntity(m *model.UIModel) string {
	for _, e := range m.Entities {
		return e.Name
	}
	return ""
}

// modelTypeNames 建模的类型清单（方言中性的拼写——内核映射到目标方言）。
func modelTypeNames() []string {
	return []string{"int", "long", "text", "decimal", "bool", "datetime", "uuid", "blob"}
}

// modelAddEntity 用行内输入的新名建实体并选中（缺省名 = 实体 N）。
func (app *App) modelAddEntity() {
	m := app.modelEdit
	name := strings.TrimSpace(app.modelNewEntity)
	if name == "" {
		name = fmt.Sprintf("entity_%d", len(m.Entities)+1)
	}
	m.AddEntity(name)
	app.modelSelEntity = name
	app.modelNewEntity = ""
	app.invalidate()
}

// mygoDialogOpenJSON 打开 .json 文件选择对话框（模型的载入）。
func mygoDialogOpenJSON() ([]string, error) {
	return mygo.Dialog.Open(mygo.OpenDialogOptions{
		Title:       "Load Model JSON",
		ButtonLabel: "Load",
		Filters: []mygo.FileFilter{
			{Name: "Model files", Extensions: []string{"json"}},
		},
	})
}
