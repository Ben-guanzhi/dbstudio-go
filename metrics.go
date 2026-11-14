package main

// 可用性指标（S-01）：契约在工作区根 `spec/usability-metrics.json`，实现共用
// `dbcore/usability`（默认关闭，POLYDB_USABILITY=1/true 才落盘到连接目录旁的
// usability.ndjson）。
//
// 这一层只做一件事：把「一次任务尝试」翻译成一个事件。Sullivan 的三项指标由此
// 可得——成功/失败看 outcome，耗时看 time.Since(started)，回退看 rollback 的
// outcome=reverted。关闭时 record 是 no-op，因此调用点不需要判断。

import (
	"strings"
	"time"
	"unicode"
)

// record 记一次任务尝试。
func (app *App) record(task, outcome string, started time.Time, fields map[string]string) {
	if app == nil || app.um == nil {
		return
	}
	app.um.Record(task, outcome, time.Since(started), fields)
}

// recordOutcome 是「成功/失败」两态任务的简写（connect / commit）。
func (app *App) recordOutcome(task string, started time.Time, err error, fields map[string]string) {
	if err != nil {
		app.record(task, "error", started, fields)
		return
	}
	app.record(task, "ok", started, fields)
}

// recordRollback：成功的回滚是任务级回退（outcome=reverted），失败是 error。
func (app *App) recordRollback(started time.Time, err error) {
	if err != nil {
		app.record("rollback", "error", started, nil)
		return
	}
	app.record("rollback", "reverted", started, nil)
}

// stmtType 取语句的首个关键字作为 statement_type（SELECT/INSERT/…）。
// 比 "batch" 有用，且不需要引入词法器：首个字母序列即足够归类。
func stmtType(sql string) string {
	s := strings.TrimLeftFunc(sql, func(r rune) bool {
		return unicode.IsSpace(r) || r == '(' || r == '/' || r == '*' || r == '-'
	})
	i := 0
	for i < len(s) && (s[i] == '_' || s[i] >= 'A' && s[i] <= 'Z' || s[i] >= 'a' && s[i] <= 'z') {
		i++
	}
	if i == 0 {
		return "unknown"
	}
	if i > 12 {
		i = 12
	}
	return strings.ToUpper(s[:i])
}
