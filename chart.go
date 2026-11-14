package main

// 结果可视化（Q-05，F4/DFDS 的"动态显示"半边）：把当前结果集画成柱状/折线图。
//
// mygo 的绘制形态与 Fyne 不同：没有常驻 canvas 控件，而是 Element.Draw 回调——
// 每帧拿 Painter 与元素盒子画一次。图的本体因此是一个纯函数 paintChart(p, r,
// data)，由一个固定高度的 element 携带；数据从 dbcore.QueryResult 抽取（与
// fynequery 的 chart.go 同一套语义：数值列自动识别、NULL/非数值跳过、类目上限
// 60、X 标签间隔取样）。
//
// X 列默认第 0 列，Y 列默认第一个数值列（Kind 为 ValInt/ValFloat 的列）。

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/dbcore/dbcore"
	"github.com/egoist/mygo/ui"
)

// chartCategories 上限：超过则取前 N 并提示（避免把千行结果压成一屏噪声）。
const chartCategories = 60

// chartNumericCol 找第一个数值列（ValInt / ValFloat）。
func chartNumericCol(res *dbcore.QueryResult) int {
	for i := range res.Columns {
		if chartColNumeric(res, i) {
			return i
		}
	}
	// 类型标注缺失时退一格：扫前几行有没有数值 Kind。
	for i := range res.Columns {
		for _, row := range res.Rows {
			if row[i].Kind == dbcore.ValInt || row[i].Kind == dbcore.ValFloat {
				return i
			}
		}
	}
	return -1
}

// chartColNumeric 判一列是否数值列：优先看类型名（int/decimal/float/numeric/number/bigint/smallint），
// 类型标注缺失时看前几行的 Kind（ValInt/ValFloat）。
func chartColNumeric(res *dbcore.QueryResult, col int) bool {
	if col < 0 || col >= len(res.Columns) {
		return false
	}
	t := strings.ToLower(res.Columns[col].Type)
	switch {
	case strings.Contains(t, "int"), strings.Contains(t, "float"), strings.Contains(t, "decimal"),
		strings.Contains(t, "numeric"), strings.Contains(t, "number"), strings.Contains(t, "real"),
		strings.Contains(t, "double"):
		return true
	}
	for _, row := range res.Rows {
		if row[col].Kind == dbcore.ValInt || row[col].Kind == dbcore.ValFloat {
			return true
		}
	}
	return false
}

// chartPoints 把结果数据抽成 (label, value) 序列：X 取 labelCol，Y 取 valueCol；
// NULL/不可解析的值跳过；类目超上限截断。
func chartPoints(res *dbcore.QueryResult, labelCol, valueCol int) ([]string, []float64, int) {
	labels := make([]string, 0, len(res.Rows))
	values := make([]float64, 0, len(res.Rows))
	dropped := 0
	for _, row := range res.Rows {
		if labelCol < 0 || labelCol >= len(row) || valueCol < 0 || valueCol >= len(row) {
			continue
		}
		cell := row[valueCol]
		var v float64
		switch cell.Kind {
		case dbcore.ValInt:
			v = float64(cell.I)
		case dbcore.ValFloat:
			v = cell.F
		case dbcore.ValText:
			f, err := strconv.ParseFloat(strings.TrimSpace(cell.S), 64)
			if err != nil {
				dropped++
				continue
			}
			v = f
		default:
			dropped++
			continue
		}
		lbl := "NULL"
		if row[labelCol].Kind != dbcore.ValNull {
			lbl = row[labelCol].String()
		}
		labels = append(labels, lbl)
		values = append(values, v)
		if len(labels) >= chartCategories {
			break
		}
	}
	return labels, values, dropped
}

// chartLabel 截断长标签（runes），带省略号。
func chartLabel(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// paintChart 把图绘制进 r（Draw 回调）：网格线 + Y 刻度 + 柱/折线 + X 标签。
// 与 fynequery 的 rebuild 同一套几何，只是落到 mygo 的 Painter 上。
func paintChart(p *ui.Painter, r ui.Rect, t *ui.Theme, kind string, labels []string, values []float64, title, note string) {
	left, bottom, top := float32(48), float32(26), float32(36)
	w := r.W - left - 8
	h := r.H - top - bottom
	p.Text(r.X+left, r.Y+4, title, 12, t.Text)
	p.Text(r.X+left, r.Y+20, note, 10, t.TextMuted)
	if w < 40 || h < 30 || len(values) == 0 {
		return
	}
	maxV, minV := values[0], values[0]
	for _, v := range values {
		if v > maxV {
			maxV = v
		}
		if v < minV {
			minV = v
		}
	}
	if kind == "bar" {
		minV = 0
	}
	if maxV == minV {
		maxV = minV + 1
	}
	span := maxV - minV
	yFor := func(v float64) float32 { return r.Y + top + h*float32(1-(v-minV)/span) }
	n := float32(len(values))
	// 网格线 + Y 刻度（5 条，含基线）。
	for i := 0; i <= 4; i++ {
		frac := float32(i) / 4
		y := r.Y + top + h*frac
		p.Line(r.X+left, y, r.X+left+w, y, 1, t.Border)
		label := trimChartFloat(maxV - span*float64(frac))
		p.Text(r.X+2, y-6, label, 10, t.TextMuted)
	}
	slot := w / n
	bw := slot * 0.7
	if kind == "line" {
		for i := 0; i < len(values)-1; i++ {
			x0 := r.X + left + slot*float32(i) + slot/2
			x1 := r.X + left + slot*float32(i+1) + slot/2
			p.Line(x0, yFor(values[i]), x1, yFor(values[i+1]), 2, t.Accent)
		}
	} else {
		for i, v := range values {
			x := r.X + left + slot*float32(i) + (slot-bw)/2
			y := yFor(v)
			p.FillRadii(ui.Rect{X: x, Y: y, W: bw, H: top + h - y}, [4]float32{}, t.Accent)
		}
	}
	// X 轴标签：最多 12 个（间隔取样）。
	step := 1
	if n > 12 {
		step = int(n/12) + 1
	}
	for i := 0; i < len(labels); i += step {
		p.Text(r.X+left+slot*float32(i), r.Y+top+h+6, chartLabel(labels[i], 12), 10, t.TextMuted)
	}
}

func trimChartFloat(v float64) string {
	s := strconv.FormatFloat(v, 'f', -1, 64)
	if len(s) > 8 {
		s = strings.TrimRight(strings.TrimRight(s[:8], "0"), ".")
	}
	return s
}

// chartNumericColExists 报当前结果集是否至少有一列数值（决定 Chart 页是否出现）。
func (app *App) chartNumericColExists() bool {
	res := app.tabResult()
	if res == nil {
		return false
	}
	return chartNumericCol(res) >= 0
}

// chartElement 组装图表面板的 element：类型切换 + 图本体（固定 240 高）。
// 数据从当前结果集抽取；X 列默认第 0 列，Y 列默认第一个数值列。
func (app *App) chartElement(c *ui.Context, t *ui.Theme) {
	res := app.tabResult()
	if res == nil || len(res.Rows) == 0 {
		ui.Text(c, "无结果可绘制").TextColor(t.TextMuted).Padding(8)
		return
	}
	valueCol := chartNumericCol(res)
	if valueCol < 0 {
		ui.Text(c, "结果没有数值列可绘制（图表需要一列数值）").TextColor(t.TextMuted).Padding(8)
		return
	}
	kind := "bar"
	labels, values, dropped := chartPoints(res, 0, valueCol)
	if len(labels) == 0 {
		ui.Text(c, "选定列没有可解析的数值").TextColor(t.TextMuted).Padding(8)
		return
	}
	title := fmt.Sprintf("%s × %s", res.Columns[0].Name, res.Columns[valueCol].Name)
	note := fmt.Sprintf("%d 类目", len(labels))
	if dropped > 0 {
		note += fmt.Sprintf("（%d 行 NULL/非数值已跳过）", dropped)
	}
	if len(labels) >= chartCategories {
		note += fmt.Sprintf("（仅前 %d 行）", chartCategories)
	}
	ui.Row(c).Gap(6).Padding(2, 6).Children(func() {
		if ui.Button(c, "柱状图").FontSize(10).Clicked() {
			kind = "bar"
		}
		if ui.Button(c, "折线图").FontSize(10).Clicked() {
			kind = "line"
		}
		ui.Spacer(c)
	})
	ui.Column(c).Grow(1).Height(240).
		Draw(func(p *ui.Painter, r ui.Rect) {
			paintChart(p, r, t, kind, labels, values, title, note)
		})
}
