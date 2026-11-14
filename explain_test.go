package main

// Tests for EXPLAIN (P2 #13): the sessionless engines build a single EXECUTE-able
// plan statement that runs through the ordinary query pipeline, while an engine
// that needs a Prepare/Cleanup session is reported as unsupported rather than run
// wrongly. The statement-to-explain is picked the same way Run cares for it.

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dbcore/dbcore"
	"github.com/dbcore/dbcore/sqltext"
)

func TestExplainable(t *testing.T) {
	cases := []struct {
		engine string
		sql    string
		ok     bool // true means this build can explain it
	}{
		{"sqlite", "SELECT * FROM employees", true},
		{"postgres", "SELECT * FROM employees", true},
		{"postgres", "SELECT 1", true},
		{"mysql", "SELECT * FROM employees", true},
		// SQL Server and Oracle need a sessioned Prepare/Cleanup bracket.
		{"sqlserver", "SELECT * FROM employees", false},
		{"oracle", "SELECT * FROM employees", false},
		// An engine with no plan concept errors.
		{"redis", "GET k", false},
	}
	for _, c := range cases {
		ok, err := explainable(c.engine, c.sql)
		if c.ok && err != nil {
			t.Errorf("%s explain %q: unexpected error %v", c.engine, c.sql, err)
			continue
		}
		if ok != c.ok {
			t.Errorf("%s explain %q: ok = %v, want %v (err %v)", c.engine, c.sql, ok, c.ok, err)
		}
	}
}

func TestExplainPlanIsStatelessSQLite(t *testing.T) {
	plan, _, _, err := sqltext.ExplainPlanStructured("sqlite", "SELECT * FROM employees")
	if err != nil {
		t.Fatalf("ExplainPlanStructured: %v", err)
	}
	if plan.Prepare != "" || plan.Cleanup != "" || plan.Query == "" {
		t.Fatalf("SQLite plan should be one stateless statement: %+v", plan)
	}
	if wants := "EXPLAIN QUERY PLAN"; !strings.Contains(strings.ToUpper(plan.Query), wants) {
		t.Fatalf("SQLite plan = %q, want it to contain %q", plan.Query, wants)
	}
}

func TestExplainPlanIsSessionlessPG(t *testing.T) {
	plan, fmtKind, ok, err := sqltext.ExplainPlanStructured("postgres", "SELECT * FROM employees")
	if err != nil || !ok {
		t.Fatalf("ExplainPlanStructured: ok=%v err=%v", ok, err)
	}
	if plan.Prepare != "" || plan.Cleanup != "" || plan.Query == "" {
		t.Fatalf("PG plan should be one stateless statement: %+v", plan)
	}
	if fmtKind != "json" {
		t.Fatalf("PG structured plan format = %q, want json", fmtKind)
	}
}

func TestExplainPlanOnServerNeedsSession(t *testing.T) {
	plan, _, ok, err := sqltext.ExplainPlanStructured("sqlserver", "SELECT * FROM employees")
	if err != nil || !ok {
		t.Fatalf("ExplainPlanStructured sqlserver: ok=%v err=%v", ok, err)
	}
	if plan.Prepare == "" || plan.Cleanup == "" {
		t.Fatalf("SQL Server plan should carry prepare/cleanup: %+v", plan)
	}
}

// The full path a user's Explain takes on a real SQLite file: build the plan,
// run it through the statement executor, and read the plan rows back.
func TestExplainRunsOnRealSQLite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "explain.db")
	seed(t, path)
	ctx := context.Background()
	db, err := OpenDB(ctx, Conn{Name: "explain", Kind: "sqlite", File: path})
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	plan, _, ok, err := sqltext.ExplainPlanStructured("sqlite", "SELECT * FROM employees ORDER BY name")
	if err != nil || !ok {
		t.Fatalf("ExplainPlanStructured: ok=%v err=%v", ok, err)
	}
	res, err := db.SQL.Execute(ctx, plan.Query)
	if err != nil {
		t.Fatalf("execute plan: %v", err)
	}
	if len(res.Rows) == 0 {
		t.Fatal("the plan returned no rows")
	}
	// SQLite's EXPLAIN QUERY PLAN reports SEARCH employees ... in its detail
	// column; assert the plan mentions the table we asked about.
	sawEmployees := false
	for _, r := range res.Rows {
		for _, v := range r {
			if v.Kind == dbcore.ValText && strings.Contains(strings.ToLower(v.S), "employees") {
				sawEmployees = true
			}
		}
	}
	if !sawEmployees {
		t.Fatalf("plan did not mention employees; first row detail %q", res.Rows[0][3].S)
	}
}
