package main

// 参数补全：写 :name / @name / $name 时，列出这条语句里的参数名。
//
// 写查询的人不该记住自己起了什么参数名——这是补全该管的事。这些测试直接
// 调 completionsAt，钉住三件事：前缀认得出来、只列这条语句里的名字、已经
// 输入的部分用来过滤。

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui/editor"
)

func TestParamCompletionOffersTheStatementsOwnNames(t *testing.T) {
	app := newViewApp(t, nil)
	// 参数名在这条语句里已经出现；光标在新的一处 :n 上。
	text := "SELECT :name, :age FROM t WHERE x = :n"
	head := len(text)
	items := app.completionsAt(text, head, head)

	var labels []string
	for _, it := range items {
		labels = append(labels, it.Label)
	}
	joined := strings.Join(labels, ",")
	if !strings.Contains(joined, ":name") {
		t.Errorf("补全没有给出这条语句里的 :name；得到 %v", labels)
	}
	// :age 不匹配已输入的 n，不该出现。
	if strings.Contains(joined, ":age") {
		t.Errorf("补全给出了不匹配已输入前缀的 :age；得到 %v", labels)
	}
}

func TestParamCompletionTakesTheMarker(t *testing.T) {
	app := newViewApp(t, nil)
	// 只认 :name——与 extractParams 支持的一致（$ 后面是位置号）。
	text := "SELECT :id FROM t WHERE x = :"
	items := app.completionsAt(text, len(text), len(text))
	var labels []string
	for _, it := range items {
		labels = append(labels, it.Label)
	}
	if !strings.Contains(strings.Join(labels, ","), ":id") {
		t.Errorf(": 前缀的补全没有给出 :id；得到 %v", labels)
	}
}

func TestParamCompletionAbsentWithoutAPlaceholder(t *testing.T) {
	app := newViewApp(t, nil)
	// 普通标识符不该触发参数补全。
	text := "SELECT nam FROM t"
	items := app.completionsAt(text, len(text), len(text))
	for _, it := range items {
		if it.Kind == editor.CompletionVariable {
			t.Errorf("没有参数前缀却给出了参数补全 %q", it.Label)
		}
	}
}
