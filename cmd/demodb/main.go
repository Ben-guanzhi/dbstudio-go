// Command demodb creates a small SQLite database at a path, with a couple of
// tables and rows, for trying DB Studio out.
//
//	go run ./cmd/demodb [path]
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/Ben-guanzhi/dbcore/sqlite"
)

func main() {
	path := "demo.db"
	if len(os.Args) > 1 {
		path = os.Args[1]
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	driver, err := sqlite.Open(abs)
	if err != nil {
		log.Fatalf("open %s: %v", abs, err)
	}
	sql, ok := driver.AsSQL()
	if !ok {
		log.Fatal("the driver has no SQL interface")
	}

	// Drop any previous demo tables, then recreate them.
	for _, stmt := range []string{
		`DROP TABLE IF EXISTS employees`,
		`DROP TABLE IF EXISTS departments`,
		`CREATE TABLE departments (
			id   INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			budget REAL
		)`,
		`CREATE TABLE employees (
			id          INTEGER PRIMARY KEY,
			name        TEXT NOT NULL,
			department  INTEGER,
			salary      REAL,
			hired_at    TEXT,
			notes       BLOB
		)`,
	} {
		if _, err := sql.Execute(ctx, stmt); err != nil {
			log.Fatalf("execute: %v", err)
		}
	}

	_, err = sql.Execute(ctx, `INSERT INTO departments (id, name, budget) VALUES
		(1, 'Engineering', 4000000),
		(2, 'Sales',       1800000),
		(3, 'Support',      900000)`)
	if err != nil {
		log.Fatalf("insert departments: %v", err)
	}
	_, err = sql.Execute(ctx, `INSERT INTO employees (id, name, department, salary, hired_at, notes) VALUES
		(1, 'Ada Lovelace',   1, 190000, '2019-03-11', X'48656C6C6F'),
		(2, 'Alan Turing',    1, 185000, '2020-06-01', NULL),
		(3, 'Grace Hopper',   1, 205000, '2018-01-15', NULL),
		(4, 'Linus Torvalds', 1, 175000, '2021-09-02', NULL),
		(5, 'Margaret Hamilton', 2, 140000, '2022-02-20', NULL),
		(6, 'Dennis Ritchie', 1, 199000, '2017-11-05', NULL)`)
	if err != nil {
		log.Fatalf("insert employees: %v", err)
	}

	if _, err := sql.Execute(ctx, `CREATE INDEX IF NOT EXISTS idx_employees_department ON employees(department)`); err != nil {
		log.Fatalf("create index: %v", err)
	}

	// A view, so the DDL viewer has something whose statement is not stored as a
	// table's, and its DDL comes from the extra metadata reader.
	if _, err := sql.Execute(ctx, `DROP VIEW IF EXISTS engineering_payroll`); err != nil {
		log.Fatalf("drop view: %v", err)
	}
	if _, err := sql.Execute(ctx, `CREATE VIEW engineering_payroll AS
		SELECT id, name, salary FROM employees WHERE department = 1 ORDER BY salary DESC`); err != nil {
		log.Fatalf("create view: %v", err)
	}

	_ = driver.Close()
	fmt.Printf("demo database ready: %s\n", abs)
}
