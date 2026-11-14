package main

// 可及性基线（S-04）：GPU 端没有 ARIA，可及性的最低要求是「每个可交互控件都有
// 可读名字」——按钮的可见文本、输入/下拉的 Label（或外层 Field 的标题）。
//
// 这是静态检查：直接读本包的视图代码。规则来源是工作区根的
// spec/a11y-baseline.json（rules[].applies_to 含 gpu），本测试同时断言该契约存在，
// 于是"基线被删掉"也会红。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

type a11yContract struct {
	Rules []struct {
		ID        string   `json:"id"`
		AppliesTo []string `json:"applies_to"`
		MinRatio  float64  `json:"min_ratio"`
	} `json:"rules"`
}

func loadA11yContract(t *testing.T) a11yContract {
	t.Helper()
	path := filepath.Join("..", "dbcore", "spec", "a11y-baseline.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读可及性契约 %s: %v", path, err)
	}
	var c a11yContract
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatalf("解析可及性契约: %v", err)
	}
	if len(c.Rules) == 0 {
		t.Fatal("契约里没有规则")
	}
	return c
}

func appliesTo(c a11yContract, rule, surface string) bool {
	for _, r := range c.Rules {
		if r.ID != rule {
			continue
		}
		for _, s := range r.AppliesTo {
			if s == surface {
				return true
			}
		}
	}
	return false
}

func readLines(path string) ([]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n"), nil
}

var (
	a11yButtonCall = regexp.MustCompile(`ui\.(?:Button|PrimaryButton)\(c,\s*(.+?)\)`)
	a11yInputCall  = regexp.MustCompile(`ui\.(?:Select|TextInput)\(`)
	a11yLabelChain = regexp.MustCompile(`\.Label\(([^)]+)\)`)
	a11yPlacehold  = regexp.MustCompile(`\.Placeholder\(([^)]+)\)`)
	a11yFieldOpen  = regexp.MustCompile(`ui\.Field\(c,\s*([^,)]+)`)
	a11yIdent      = regexp.MustCompile(`^[A-Za-z_]\w*$`)
)

func TestInteractiveControlsHaveAccessibleNames(t *testing.T) {
	contract := loadA11yContract(t)
	if !appliesTo(contract, "interactive-name", "gpu") {
		t.Fatal("契约的 interactive-name 规则必须适用于 gpu（dbstudio 是 GPU 端）")
	}

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		lines, err := readLines(f)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		for i, line := range lines {
			// 按钮：可见文本必须非空（字面量非空，或一个提供文本的标识符）。
			// 图标按钮的例外：可见文本为空但同一行链了 .Label("非空")——可及
			// 名字由 Label 给出（波 100 的工具条图标按钮），契约的精神是
			// 「控件要有名字」，不是「控件要有可见字」。
			if m := a11yButtonCall.FindStringSubmatch(line); m != nil {
				arg := strings.TrimSpace(m[1])
				switch {
				case strings.HasPrefix(arg, `"`):
					if strings.TrimSpace(strings.Trim(arg, `"`)) == "" {
						if lm := a11yLabelChain.FindStringSubmatch(line); lm != nil &&
							strings.TrimSpace(strings.Trim(lm[1], `"`)) != "" {
							checked++
							continue
						}
						t.Errorf("%s:%d 按钮的可见文本为空: %s", f, i+1, strings.TrimSpace(line))
					}
				case a11yIdent.MatchString(arg):
					// 变量标签（如分页的页码文本）可接受：名字由运行期给出。
				default:
					t.Errorf("%s:%d 按钮的标签既不是非空字面量也不是标识符: %s",
						f, i+1, strings.TrimSpace(line))
				}
				checked++
			}
			// 输入/下拉：行内 .Label("…") 或 .Placeholder("…")，或上方三行内有
			// ui.Field(名字, …)（名字可以是表达式，如参数名）。
			if a11yInputCall.MatchString(line) {
				if m := a11yLabelChain.FindStringSubmatch(line); m != nil && strings.TrimSpace(m[1]) != "" {
					checked++
					continue
				}
				if m := a11yPlacehold.FindStringSubmatch(line); m != nil && strings.TrimSpace(m[1]) != "" {
					checked++
					continue
				}
				inField := false
				for j := i - 1; j >= 0 && j >= i-8; j-- {
					if m := a11yFieldOpen.FindStringSubmatch(lines[j]); m != nil && strings.TrimSpace(m[1]) != "" {
						inField = true
						break
					}
				}
				if !inField {
					t.Errorf("%s:%d 输入/下拉缺少可读名字（.Label / .Placeholder / 外层 ui.Field）: %s",
						f, i+1, strings.TrimSpace(line))
				}
				checked++
			}
		}
	}
	if checked < 50 {
		t.Fatalf("只检查了 %d 个控件，静态扫描可能失效（视图代码应有上百个）", checked)
	}
}
