package main

// 数据传输 UI（Navicat 的 Data Transfer 的 mygo 形态）：内核 TransferTable
// （dbcore v0.10.0）直驱——源/目标连接（saved connections，compare 同款双开
// 模式）+ 表名 + 行数上限 + 进度回调驱动的状态行。
//
// **破坏性确认**：目标表已有数据时 INSERT 可能撞主键——开始前提示「目标表
// 需为空或无主键冲突」；传输**只追加**，不清空目标（清表是用户的独立决定）。
// 批量失败即中止并报告已传行数（不回滚已传批次——ApplyBatch 的批内原子性由
// 驱动保证，跨批不承诺）。

import (
	"context"
	"fmt"
	"time"

	"github.com/Ben-guanzhi/dbcore"
	"github.com/egoist/mygo/ui"
)

// transferState 是传输 modal 的全部状态（compare 同款结构体形态——app 字段
// 一个整体赋值，避免逐字段竞争）。
type transferState struct {
	open     bool
	running  bool
	done     bool
	srcName  string
	dstName  string
	table    string
	maxRows  float64
	progress int
	err      string
}

// openTransfer 打开数据传输 modal（Tools 菜单）。
func (app *App) openTransfer() {
	if app.store == nil || len(app.store.List()) < 2 {
		app.errMsg = "数据传输需要至少两个已保存的连接"
		app.invalidate()
		return
	}
	names := app.connNames()
	t := transferState{
		open:    true,
		srcName: app.connName,
		dstName: nextName(names, app.connName),
	}
	if t.srcName == "" {
		t.srcName = pick(names, 0)
	}
	if t.dstName == "" || t.dstName == t.srcName {
		t.dstName = nextName(names, t.srcName)
	}
	app.transfer = t
	app.invalidate()
}

// startTransfer 在后台打开两个连接并跑 TransferTable（进度回调 post 回 UI）。
func (app *App) startTransfer() {
	t := app.transfer
	if t.srcName == "" || t.dstName == "" || t.srcName == t.dstName || t.table == "" {
		t.err = "源/目标连接与表名都要有，且源目标不能相同"
		app.transfer = t
		app.invalidate()
		return
	}
	if t.running {
		return
	}
	t.running = true
	t.err = ""
	t.done = false
	t.progress = 0
	app.transfer = t
	app.invalidate()

	srcConn, ok := app.store.Get(t.srcName)
	if !ok {
		app.finishTransfer("打开源连接失败：没有保存的连接 " + t.srcName)
		return
	}
	dstConn, ok := app.store.Get(t.dstName)
	if !ok {
		app.finishTransfer("打开目标连接失败：没有保存的连接 " + t.dstName)
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		src, err := OpenDB(ctx, srcConn)
		if err != nil {
			app.finishTransfer("打开源连接失败：" + err.Error())
			return
		}
		defer src.Close()
		dst, err := OpenDB(ctx, dstConn)
		if err != nil {
			app.finishTransfer("打开目标连接失败：" + err.Error())
			return
		}
		defer dst.Close()

		spec := dbcore.TransferSpec{Table: t.table, MaxRows: int64(t.maxRows)}
		got, err := dbcore.TransferTable(ctx, src.SQL, dst.SQL, spec,
			func(p dbcore.TransferProgress) {
				app.post(func() {
					t := app.transfer
					t.progress = p.Rows
					app.transfer = t
					app.invalidate()
				})
			})
		app.finishTransfer(func() string {
			if err != nil {
				return err.Error()
			}
			return fmt.Sprintf("传输完成：共 %d 行 → %s.%s", got, t.dstName, t.table)
		}())
	}()
}

// finishTransfer 收尾：结束 running，写结果行。
func (app *App) finishTransfer(msg string) {
	app.post(func() {
		t := app.transfer
		t.running = false
		t.done = true
		t.err = msg
		app.transfer = t
		app.invalidate()
	})
}

// transferView draws the Data Transfer modal.
func (app *App) transferView(c *ui.Context) {
	if !app.transfer.open {
		return
	}
	t := c.Theme()
	names := app.connNames()
	ui.Modal(c, &app.transfer.open, func() {
		ui.Column(c).Gap(8).Padding(12).Width(600).Height(420).
			Background(t.Surface).Radius(10).Border(1, t.Border).
			Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "Data Transfer").FontSize(13).Bold()
					ui.Text(c, app.transfer.err).FontSize(10).TextColor(t.TextMuted).SingleLine()
					ui.Spacer(c)
					if ui.Button(c, "Close").Clicked() {
						app.transfer.open = false
					}
				})
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "源连接").FontSize(11)
					ui.Select(c, &app.transfer.srcName, names).Label("源连接")
				})
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "目标连接").FontSize(11)
					ui.Select(c, &app.transfer.dstName, names).Label("目标连接")
				})
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "表名").FontSize(11)
					ui.TextInput(c, &app.transfer.table).Width(200).Label("表名")
					ui.Text(c, "最多行数").FontSize(11)
					ui.NumberInput(c, &app.transfer.maxRows, 0, 10_000_000, 1000)
				})
				if app.transfer.running {
					ui.Text(c, fmt.Sprintf("传输中… 已传 %d 行", app.transfer.progress)).
						FontSize(11).SingleLine()
				} else if app.transfer.done {
					ui.Text(c, app.transfer.err).FontSize(11).SingleLine()
				} else {
					ui.Text(c, "只追加不清空——目标表需为空或无主键冲突；批量失败即中止（已传批次不回滚）。").
						FontSize(10).TextColor(t.TextMuted)
				}
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Spacer(c)
					if ui.Button(c, "开始传输").FontSize(11).
						Disabled(app.transfer.running).Clicked() {
						app.startTransfer()
					}
				})
			})
	})
}

