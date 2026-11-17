package main

// Review Changes 面板的数据测试：从工作副本推导的 BatchRequest 的差异统计
// 与面板展示一致（+inserts ~updates -deletes）。

import (
	"context"
	"testing"

	"github.com/Ben-guanzhi/dbcore"
)

// reviewStats 是面板头部统计的最小形状（reviewView 渲染它）。
type reviewStats struct {
	ins, upd, del int
}

func statsOf(req dbcore.BatchRequest) reviewStats {
	return reviewStats{ins: len(req.Inserts), upd: len(req.Updates), del: len(req.Deletes)}
}

// 同 TestCommitRoundTrip 的场景：改 Ada、删 Alan、插 Grace → 统计 1/1/1。
func TestReviewStatsMatchWorkingCopy(t *testing.T) {
	ctx := context.Background()
	db, err := OpenDB(ctx, Conn{Name: "edit", Kind: "sqlite"})
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()

	for _, stmt := range []string{
		`CREATE TABLE employees (id INTEGER PRIMARY KEY, name TEXT NOT NULL, salary REAL)`,
		`INSERT INTO employees (name, salary) VALUES ('Ada', 190000), ('Alan', 185000)`,
	} {
		if _, err := db.SQL.Execute(ctx, stmt); err != nil {
			t.Fatalf("execute %q: %v", stmt, err)
		}
	}

	cached := newCached(db)
	if err := cached.LoadSchemas(ctx); err != nil {
		t.Fatalf("LoadSchemas: %v", err)
	}
	schemas := cached.SchemaNames()
	schema := schemas[0]
	if err := cached.setTables(ctx, schema); err != nil {
		t.Fatal(err)
	}
	if err := cached.setColumns(ctx, schema, "employees"); err != nil {
		t.Fatal(err)
	}
	cols := cached.columnsOf(schema, "employees")

	res, err := db.SQL.Execute(ctx, "SELECT * FROM employees ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}

	var e editState
	e.init(res, schema, "employees", cols, func() {})

	// Edit Ada, delete Alan, insert Grace.
	e.rows[0].strs[1] = "Ada Lovelace"
	e.rows[0].state = rowModified
	e.rows[1].state = rowDeleted
	e.addRow([]string{"", "Grace", "150000"}, rowInserted)

	req, why := e.request()
	if why != "" {
		t.Fatalf("request = %q", why)
	}

	st := statsOf(req)
	if st != (reviewStats{ins: 1, upd: 1, del: 1}) {
		t.Fatalf("统计 = %+v, want +1 ~1 -1", st)
	}

	// 修改行的列级 diff：PK 是 id（Ada 的 id=1），Changes 是 name。
	if len(req.Updates) != 1 {
		t.Fatalf("updates = %d", len(req.Updates))
	}
	u := req.Updates[0]
	if v, ok := u.PK["id"]; !ok || v.String() != "1" {
		t.Errorf("PK = %+v, want id=1", u.PK)
	}
	if v, ok := u.Changes["name"]; !ok || v.String() != "Ada Lovelace" {
		t.Errorf("Changes = %+v, want name=Ada Lovelace", u.Changes)
	}
}
