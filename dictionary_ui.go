package main

// 数据字典导出（DBeaver 的 Generate Documentation 的文档半边）：内核
// ExportDictionary（dbcore v0.12.0）直驱——当前 schema 的结构导出为
// Markdown，用系统保存对话框落盘。
//
// 与"导出数据"的分工：那是行数据（CSV/JSON），这是**结构文档**（表/列/
// 主键/外键/注释）——交接口/评审/文档站用。

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Ben-guanzhi/dbcore"
	"github.com/egoist/mygo"
)

// exportDictionary 把当前 schema 的结构导出为 Markdown 数据字典。
func (app *App) exportDictionary() {
	if app.db == nil || app.db.SQL == nil {
		app.errMsg = "No connection"
		app.invalidate()
		return
	}
	schema := app.schema
	drv := app.db.SQL
	name := fmt.Sprintf("%s-dictionary-%s.md", schemaFileStem(schema), time.Now().Format("20060102"))
	path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
		Title:       "Export Data Dictionary",
		ButtonLabel: "Export",
		DefaultPath: name,
		Filters: []mygo.FileFilter{
			{Name: "Markdown", Extensions: []string{"md"}},
		},
	})
	if err != nil || path == "" {
		return // cancelled
	}
	if !strings.HasSuffix(strings.ToLower(path), ".md") {
		path += ".md"
	}
	app.status = "Exporting dictionary…"
	app.invalidate()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		doc, err := dbcore.ExportDictionary(ctx, drv, schema, nil)
		app.post(func() {
			if err != nil {
				app.errMsg = "Dictionary export failed: " + err.Error()
				app.status = ""
				app.invalidate()
				return
			}
			if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
				app.errMsg = "Write failed: " + err.Error()
				app.status = ""
				app.invalidate()
				return
			}
			app.status = fmt.Sprintf("Dictionary exported: %d tables → %s",
				strings.Count(doc, "\n## "), path)
			app.invalidate()
		})
	}()
}

// schemaFileStem 把 schema 名变成安全的文件名主干。
func schemaFileStem(schema string) string {
	if schema == "" {
		return "schema"
	}
	var b strings.Builder
	for _, r := range schema {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}
