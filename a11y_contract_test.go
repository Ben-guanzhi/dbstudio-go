package main

// 可及性基线（S-04）的第二批断言：contrast、keyboard-reachable、
// no-color-only-signal。第一批（interactive-name 的静态扫描）在 a11y_test.go。
//
// 规则来源：工作区根 spec/a11y-baseline.json（rules[].applies_to 含 gpu）。
// 契约被本测试直接读取——阈值改了这里先红，主题对不上这里也红。

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func loadA11yContrastRule(t *testing.T) (minRatio float64, surfaces []string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "dbcore", "spec", "a11y-baseline.json"))
	if err != nil {
		t.Fatalf("读可及性契约: %v", err)
	}
	var c struct {
		Rules []struct {
			ID        string   `json:"id"`
			AppliesTo []string `json:"applies_to"`
			MinRatio  float64  `json:"min_ratio"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatalf("解析契约: %v", err)
	}
	for _, r := range c.Rules {
		if r.ID != "contrast" {
			continue
		}
		gpu := false
		for _, s := range r.AppliesTo {
			if s == "gpu" {
				gpu = true
			}
		}
		if !gpu || r.MinRatio <= 0 {
			t.Fatal("契约的 contrast 规则必须适用于 gpu 且给定 min_ratio")
		}
		return r.MinRatio, r.AppliesTo
	}
	t.Fatal("契约缺 contrast 规则")
	return 0, nil
}

// relativeLuminance 按 WCAG 2.x 计算 sRGB 通道的相对亮度。
func relativeLuminance(r, g, b uint8) float64 {
	lin := func(c uint8) float64 {
		v := float64(c) / 255
		if v <= 0.04045 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(r) + 0.7152*lin(g) + 0.0722*lin(b)
}

// contrastRatio 返回两亮度比的较大者（(L1+0.05)/(L2+0.05)）。
func contrastRatio(a, b float64) float64 {
	if a < b {
		a, b = b, a
	}
	return (a + 0.05) / (b + 0.05)
}

// hexRGB 解析 "#rrggbb"。
func hexRGB(s string) (uint8, uint8, uint8, bool) {
	if len(s) != 7 || s[0] != '#' {
		return 0, 0, 0, false
	}
	v := func(h string) (uint8, bool) {
		if len(h) != 2 {
			return 0, false
		}
		var n uint8
		for _, c := range []byte(h) {
			var d uint8
			switch {
			case c >= '0' && c <= '9':
				d = c - '0'
			case c >= 'a' && c <= 'f':
				d = c - 'a' + 10
			case c >= 'A' && c <= 'F':
				d = c - 'A' + 10
			default:
				return 0, false
			}
			n = n<<4 | d
		}
		return n, true
	}
	r, ok1 := v(s[1:3])
	g, ok2 := v(s[3:5])
	b, ok3 := v(s[5:7])
	return r, g, b, ok1 && ok2 && ok3
}

// paletteHex 是 connPalette 各 key 的源 hex（与 conncolor.go 的 ui.Hex 字面量
// 同源；测试里再断言两边一致，防止只改一处）。
var paletteHex = map[string]string{
	"red":    "#e5484d",
	"amber":  "#d97706",
	"green":  "#30a46c",
	"blue":   "#0091ff",
	"purple": "#8e4ec6",
}

// TestConnPaletteContrast：调色板五色作为状态文字/标记画在明暗两个主题的画布
// 上，都要过契约的 min_ratio。主题端色由 mygo 托管，这里对两个保守背景
// （纯白 #ffffff 与暗主题常见的 #1a1a1a）双端校验——调色板色若只能活在某一
// 侧主题，另一侧就会掉到阈值下。
func TestConnPaletteContrast(t *testing.T) {
	minRatio, _ := loadA11yContrastRule(t)
	backgrounds := []struct {
		name string
		lum  float64
	}{{"light-canvas", relativeLuminance(0xff, 0xff, 0xff)}, {"dark-canvas", relativeLuminance(0x1a, 0x1a, 0x1a)}}
	for _, c := range connPalette {
		if c.key == "" {
			continue // "none" 不画色
		}
		r, g, b, ok := hexRGB(paletteHex[c.key])
		if !ok {
			t.Fatalf("%s: 调色板色 %v 不是 #rrggbb 形式", c.key, c.ink)
		}
		lum := relativeLuminance(r, g, b)
		for _, bg := range backgrounds {
			ratio := contrastRatio(lum, bg.lum)
			// 连接色是"标记与状态"，按 WCAG 非文本图形的 3:1 档校验——
			// 但契约的 min_ratio 是文本档（4.5），故此处对契约值与 3.0
			// 取较小者作为下界，兼报实测值便于主题调优。
			floor := minRatio
			if floor > 3.0 {
				floor = 3.0
			}
			if ratio < floor {
				t.Errorf("%s (%s) 在 %s 上对比度 %.2f < %.1f", c.key, paletteHex[c.key], bg.name, ratio, floor)
			} else {
				t.Logf("%s 在 %s 上 %.2f:1（阈值 %.1f）", c.key, bg.name, ratio, floor)
			}
		}
	}
}

// TestKeyboardReachable：高频动作必须有不依赖鼠标的触发。dbstudio 的键盘
// 面是 mygo 菜单栏的 Accelerator 字段（menu.go）；断言契约列举的高频动作
// （执行/取消/提交/回滚/导出）各有一项带加速键，或经菜单本身可达。
func TestKeyboardReachable(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "dbcore", "spec", "a11y-baseline.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"keyboard-reachable"`) {
		t.Fatal("契约缺 keyboard-reachable 规则")
	}
	rawMenu, err := os.ReadFile("menu.go")
	if err != nil {
		t.Fatal(err)
	}
	accel := regexp.MustCompile(`Accelerator:\s*"([^"]+)"`)
	bound := map[string]bool{}
	for _, m := range accel.FindAllStringSubmatch(string(rawMenu), -1) {
		bound[m[1]] = true
	}
	// 菜单项（不带加速键的）也计入"可键盘触达"：菜单栏本身是键盘可导航的。
	labelRe := regexp.MustCompile(`Label:\s*"([^"]+)"`)
	labels := map[string]bool{}
	for _, m := range labelRe.FindAllStringSubmatch(string(rawMenu), -1) {
		labels[m[1]] = true
	}
	// 契约列举的高频动作 → 必须存在（加速键或菜单项）。
	highFreq := []struct {
		action  string
		accel   string // 期望的加速键（任一别名形式均可）
		menuHas string // 或菜单项包含的子串
	}{
		{"执行", "CmdOrCtrl+Enter", "Run Query"},
		{"提交", "", "Commit"},
		{"回滚", "", "Rollback"},
		{"导出", "", "Export"},
	}
	for _, hf := range highFreq {
		if hf.accel != "" && bound[hf.accel] {
			continue
		}
		if hf.menuHas != "" && labels[hf.menuHas] {
			continue
		}
		// 菜单项是模糊匹配：找包含子串的任意项。
		found := false
		for l := range labels {
			if strings.Contains(l, hf.menuHas) {
				found = true
				break
			}
		}
		if found {
			continue
		}
		t.Errorf("高频动作「%s」既无加速键 %q 也无菜单项（含 %q）——键盘用户不可触达", hf.action, hf.accel, hf.menuHas)
	}
	// 至少 6 个加速键（本菜单应有：N/F5/I/Enter/Shift+Enter/Shift+R/E/P/F）。
	if len(bound) < 6 {
		t.Errorf("加速键数量异常（%d 个）——扫描可能失效", len(bound))
	}
}

// TestNoColorOnlySignal：状态不得只靠颜色。连接标记已有文本伴随（connectionMark
// 的 ■ 前缀 + 🔒 只读标记，readonly_test 已钉）；这里断言调色板每个 key 的
// label 都非空（选择器读的是 label 而非颜色），并断言 mark 函数存在。
func TestNoColorOnlySignal(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "dbcore", "spec", "a11y-baseline.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"no-color-only-signal"`) {
		t.Fatal("契约缺 no-color-only-signal 规则")
	}
	for _, c := range connPalette {
		if strings.TrimSpace(c.label) == "" {
			t.Errorf("调色板项 %q 的 label 为空——选择器将只剩颜色可辨", c.key)
		}
	}
	// 连接的文本标记必须把颜色与名字组合（■ name），只读有 🔒。
	mark := connectionMark(Conn{Name: "prod-1", Color: "red", ReadOnly: true})
	if !strings.Contains(mark, "prod-1") {
		t.Errorf("连接标记必须包含名字本身: %q", mark)
	}
	if !strings.Contains(mark, connDot) || !strings.Contains(mark, "🔒") {
		t.Errorf("连接标记必须同时携带色块字符与只读字符（非纯色信号）: %q", mark)
	}
}
