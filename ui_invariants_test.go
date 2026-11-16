package main

// 界面不变量：主按钮只给**真正的确认动作**。
//
// 「主按钮」（PrimaryButton）在界面上是最重的视觉权重——它该指向用户最可能
// 想要的那一步（Run / Save / Apply / Import / Go）。把 Close / Cancel 做成主
// 按钮会误导：用户扫一眼看到的色块是「关闭」，而不是「执行」。
//
// 这条测试直接读源码（同 sqlgpui 的 invariants.rs、db-dev 的
// layout_invariants_test.js 的做法）：配置会被绕过，源码不会。
//
// 波 101 续五十八。

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestNoRowSParenthetical：行数不用 `row(s)` 这种偷懒写法——正确的单复数由
// pluralRows 给（`1 row` / `3 rows`）。
//
// 这是「措辞打磨」的锁：`row(s)` 曾在 15 处出现，靠人眼不可能保持统一。
func TestNoRowSParenthetical(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	hits := 0
	for _, name := range files {
		if name == "ui_invariants_test.go" {
			continue // 本文件要在错误信息里提到这种写法
		}
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("读 %s: %v", name, err)
		}
		for i, line := range strings.Split(string(raw), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") {
				continue // 注释里提到写法本身是可以的
			}
			if strings.Contains(line, "row(s)") {
				hits++
				t.Errorf("%s:%d: 代码里出现 row(s)——改用 pluralRows（正确单复数）", name, i+1)
			}
		}
	}
	if hits == 0 {
		t.Log("没有 row(s) 的偷懒写法")
	}
}

// writeRE 匹配对 app.<字段> 的赋值；readRE 匹配其它任何出现。
var (
	writeRE = regexp.MustCompile(`app\.([a-zA-Z]\w*)\s*=[^=]`)
	useRE   = regexp.MustCompile(`app\.([a-zA-Z]\w*)`)
)

// TestNoWriteOnlyAppFields：没有「只写不读」的 app 字段。
//
// `txErr` 曾是一个：事务失败的原因写进去，**从不被读**——用户只看到状态条的
// 「Commit failed」，原因丢了。这类缺陷靠读代码很容易漏（每一处赋值都写得对，
// 只是没人用），靠扫描字段的使用次数才抓得准。
func TestNoWriteOnlyAppFields(t *testing.T) {
	writes := map[string]int{}
	uses := map[string]int{}

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		if name == "ui_invariants_test.go" {
			continue
		}
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("读 %s: %v", name, err)
		}
		text := string(raw)
		for _, m := range writeRE.FindAllStringSubmatch(text, -1) {
			writes[m[1]]++
		}
		for _, m := range useRE.FindAllStringSubmatch(text, -1) {
			uses[m[1]]++
		}
	}

	for field, n := range writes {
		// 出现次数 = 写次数 + 读次数；只有写说明没人读它。
		if uses[field] <= n {
			t.Errorf("app.%s 被写了 %d 次，却没有被读过——一个只写不读的字段没有意义（原因/状态丢了）", field, n)
		}
	}
	t.Logf("扫描了 %d 个字段", len(uses))
}

// acceleratorRE 匹配 menu.go 里声明的菜单加速键。
var acceleratorRE = regexp.MustCompile(`Accelerator:\s*"([^"]+)"`)

// shortcutCallRE 匹配 c.Shortcut(修饰键, 键) 的调用。
var shortcutCallRE = regexp.MustCompile(`c\.Shortcut\(([^,]+),\s*(ui\.Key\w+)\)`)

// keyNames 是键常量的显示名。**未知的键要报错**——新增一颗快捷键却忘了列进
// 速查，正是这条测试要抓的。
var keyNames = map[string]string{
	"ui.KeyEnter":  "Enter",
	"ui.KeyT":      "T",
	"ui.KeyW":      "W",
	"ui.KeyTab":    "Tab",
	"ui.KeyZ":      "Z",
	"ui.KeyF5":     "F5",
	"ui.KeyF11":    "F11",
	"ui.KeyLeft":   "Left",
	"ui.KeyRight":  "Right",
	"ui.KeyUp":     "Up",
	"ui.KeyDown":   "Down",
	"ui.KeyEscape": "Escape",
	"ui.KeyH":      "H",
	"ui.KeyG":      "G",
	"ui.KeySlash":  "/",
}

// TestHandledShortcutsAreListed：view.go / pager.go 里处理的每一颗快捷键，都
// 必须出现在速查表里（workbenchShortcuts 或菜单声明）。
//
// 速查此前只从菜单生成，而编辑器与分页的九颗键由 view.go / pager.go 直接处理
// ——用户看不到它们。这条测试让「加一颗键」不可能漏掉速查。
func TestHandledShortcutsAreListed(t *testing.T) {
	// 速查里的键：Workbench 组 + **菜单声明的 Accelerator**（直接从 menu.go
	// 源码扫——菜单是运行期构造的，扫源码才不用造一个 App）。
	listed := map[string]bool{}
	for _, r := range workbenchShortcuts {
		listed[r.keys] = true
	}
	if raw, err := os.ReadFile("menu.go"); err == nil {
		for _, m := range acceleratorRE.FindAllStringSubmatch(string(raw), -1) {
			listed[m[1]] = true
		}
	} else {
		t.Fatalf("读 menu.go: %v", err)
	}

	for _, name := range []string{"view.go", "pager.go", "form.go", "layout.go"} {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("读 %s: %v", name, err)
		}
		for _, m := range shortcutCallRE.FindAllStringSubmatch(string(raw), -1) {
			mods, keyConst := m[1], m[2]
			key, ok := keyNames[keyConst]
			if !ok {
				t.Errorf("%s: 键 %s 不在 keyNames 里——补上显示名，并确认它列进速查", name, keyConst)
				continue
			}
			got := renderShortcutKeys(mods) + key
			if !listed[got] {
				t.Errorf("%s: 处理的快捷键 %q 没有列进速查表", name, got)
			}
		}
	}
}

// renderShortcutKeys 把修饰键表达式渲染成速查里的写法。
func renderShortcutKeys(expr string) string {
	expr = strings.TrimSpace(expr)
	if expr == "0" || expr == "" {
		return ""
	}
	var parts []string
	if strings.Contains(expr, "ui.Cmd") {
		parts = append(parts, "CmdOrCtrl")
	}
	if strings.Contains(expr, "ui.Shift") {
		parts = append(parts, "Shift")
	}
	if strings.Contains(expr, "ui.Alt") {
		parts = append(parts, "Alt")
	}
	return strings.Join(parts, "+") + "+"
}

// emptyStateRE 匹配界面上的空状态文案（ui.Text 里以 No / Nothing / Loading 开头）。
var emptyStateRE = regexp.MustCompile(`ui\.Text\(c,\s*"((?:No |Nothing |Loading )[^"]*)"`)

// TestEmptyStatesArePhrases：空状态是**短语**，不是句子——句号只留给带指令的
// 完整句（如「No favorites yet — select a statement…」，其中的 — 是标志）。
//
// 这条与主按钮那条同源：措辞风格的分歧靠人眼看不全（9 处空状态散在 8 个文件
// 里），靠源码扫描才抓得准。
func TestEmptyStatesArePhrases(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, name := range files {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("读 %s: %v", name, err)
		}
		for _, m := range emptyStateRE.FindAllStringSubmatch(string(raw), -1) {
			text := m[1]
			checked++
			if strings.HasSuffix(text, ".") && !strings.Contains(text, "—") {
				t.Errorf("%s: 空状态 %q 以句号结尾——短语不该有句号（带指令的完整句除外）", name, text)
			}
			if strings.HasSuffix(text, " ") {
				t.Errorf("%s: 空状态 %q 以空格结尾", name, text)
			}
		}
	}
	if checked == 0 {
		t.Fatal("一个空状态都没找到——正则或代码结构变了？")
	}
	t.Logf("检查了 %d 处空状态", checked)
}

// primaryButtonRE 匹配 ui.PrimaryButton(c, "…") 的文案。
var primaryButtonRE = regexp.MustCompile(`ui\.PrimaryButton\(c,\s*"([^"]*)"`)

// secondaryLabels 是不该做成主按钮的文案（关闭/放弃类）。
var secondaryLabels = map[string]bool{
	"Close":   true,
	"Cancel":  true,
	"Dismiss": true,
	"×":       true,
}

// TestPrimaryButtonsAreConfirmActions 扫描本包的所有 .go 文件：任何
// PrimaryButton 的文案都不该是关闭/放弃类。
func TestPrimaryButtonsAreConfirmActions(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("没找到 .go 文件（工作目录不对？）")
	}

	found := 0
	for _, name := range files {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("读 %s: %v", name, err)
		}
		for _, m := range primaryButtonRE.FindAllStringSubmatch(string(raw), -1) {
			label := m[1]
			found++
			if secondaryLabels[label] {
				t.Errorf("%s: %q 被做成了主按钮——关闭/放弃类动作该用次要按钮 ui.Button", name, label)
			}
			if strings.TrimSpace(label) == "" {
				t.Errorf("%s: 有一个没有文案的主按钮", name)
			}
		}
	}
	if found == 0 {
		t.Fatal("一个主按钮都没找到——正则或代码结构变了？")
	}
	t.Logf("检查了 %d 个文件里的 %d 个主按钮", len(files), found)
}
