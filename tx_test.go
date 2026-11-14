package main

// Tests for the connection-level transaction (#10): the autocommit switch, the
// transaction the editor's statements run in, and the commit and rollback that
// finish it.
//
// The transaction is the kernel's SQLTxDriver registry, so these tests drive a
// real SQLite file through OpenDB — the same path the UI takes — and read the
// rows back with the same executeStatement the script loop uses.

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/dbcore/dbcore"
)

// txApp opens the seeded SQLite file and gives the App its connection, the way
// a connected window does.
func txApp(t *testing.T) *App {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "tx.db")
	seed(t, path)
	db, err := OpenDB(ctx, Conn{Name: "tx", Kind: "sqlite", File: path})
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return &App{db: db, conn: Conn{Kind: "sqlite"},
		cols: map[string][]dbcore.ColumnInfo{}, tables: map[string][]tableInfo{}}
}

// countEmployees reads the table through the given path and reports the rows.
func countEmployees(t *testing.T, ctx context.Context, autocommit bool, tx *dbcore.TxManager, txID string, db *DB) int {
	t.Helper()
	res, err := executeStatement(ctx, autocommit, tx, txID, db, "SELECT COUNT(*) FROM employees")
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if len(res.Rows) == 0 {
		t.Fatal("count returned no rows")
	}
	return int(res.Rows[0][0].I)
}

func TestOpenDBBuildsTransactionRegistry(t *testing.T) {
	app := txApp(t)
	if !app.txCapable() {
		t.Fatal("SQLite is a SQLTxDriver, so the connection must have a transaction registry")
	}
	if app.db.Tx == nil {
		t.Fatal("db.Tx is nil")
	}
	// An App with no connection, or one whose driver has no transactions, cannot
	// run one.
	if (&App{}).txCapable() {
		t.Fatal("an App with no database reported transaction support")
	}
	if (&App{db: &DB{}}).txCapable() {
		t.Fatal("a connection with no registry reported transaction support")
	}
}

func TestBeginTxIsIdempotent(t *testing.T) {
	app := txApp(t)
	ctx := context.Background()
	if err := app.beginTxNow(ctx); err != nil {
		t.Fatalf("beginTxNow: %v", err)
	}
	first := app.txID
	if !app.txActive || first == "" {
		t.Fatalf("beginTxNow left txActive=%v txID=%q", app.txActive, app.txID)
	}
	// A second begin must not open a second transaction on the same connection.
	if err := app.beginTxNow(ctx); err != nil {
		t.Fatalf("second beginTxNow: %v", err)
	}
	if app.txID != first {
		t.Fatalf("a second begin opened a new transaction: %q then %q", first, app.txID)
	}
}

func TestCommitTxMakesWorkVisible(t *testing.T) {
	app := txApp(t)
	ctx := context.Background()
	if err := app.beginTxNow(ctx); err != nil {
		t.Fatalf("beginTxNow: %v", err)
	}
	tx, id := app.db.Tx, app.txID

	if _, err := executeStatement(ctx, false, tx, id, app.db,
		`INSERT INTO employees (name, salary) VALUES ('Grace', 170000)`); err != nil {
		t.Fatalf("insert in transaction: %v", err)
	}
	// The insert is visible inside the transaction, and not outside it, until it
	// is committed.
	if got := countEmployees(t, ctx, false, tx, id, app.db); got != 3 {
		t.Fatalf("rows inside the transaction = %d, want 3", got)
	}
	if got := countEmployees(t, ctx, true, tx, id, app.db); got != 2 {
		t.Fatalf("rows outside the transaction = %d, want 2", got)
	}

	if err := app.commitTxNow(); err != nil {
		t.Fatalf("commitTxNow: %v", err)
	}
	if app.txActive || app.txID != "" {
		t.Fatalf("commit left a transaction open: active=%v id=%q", app.txActive, app.txID)
	}
	if got := countEmployees(t, ctx, true, tx, "", app.db); got != 3 {
		t.Fatalf("rows after commit = %d, want 3", got)
	}
}

func TestRollbackTxDiscardsWork(t *testing.T) {
	app := txApp(t)
	ctx := context.Background()
	if err := app.beginTxNow(ctx); err != nil {
		t.Fatalf("beginTxNow: %v", err)
	}
	tx, id := app.db.Tx, app.txID

	if _, err := executeStatement(ctx, false, tx, id, app.db,
		`INSERT INTO employees (name, salary) VALUES ('Alan M', 170000)`); err != nil {
		t.Fatalf("insert in transaction: %v", err)
	}
	if err := app.rollbackTxNow(); err != nil {
		t.Fatalf("rollbackTxNow: %v", err)
	}
	if app.txActive || app.txID != "" {
		t.Fatalf("rollback left a transaction open: active=%v id=%q", app.txActive, app.txID)
	}
	if got := countEmployees(t, ctx, true, tx, "", app.db); got != 2 {
		t.Fatalf("rows after rollback = %d, want 2", got)
	}
}

func TestExecuteStatementAutocommitOnBypassesTx(t *testing.T) {
	app := txApp(t)
	ctx := context.Background()
	// With autocommit on, the statement runs straight through the driver even
	// though a transaction is open: it belongs to no transaction.
	if _, err := executeStatement(ctx, true, app.db.Tx, "txn-whatever", app.db,
		`INSERT INTO employees (name, salary) VALUES ('Turing', 1)`); err != nil {
		t.Fatalf("execute outside the transaction: %v", err)
	}
	if got := countEmployees(t, ctx, true, app.db.Tx, "", app.db); got != 3 {
		t.Fatalf("rows = %d, want 3", got)
	}
}

func TestCommitTxUnknownIDReportsError(t *testing.T) {
	app := txApp(t)
	app.txActive, app.txID = true, "txn-not-there"
	err := app.commitTxNow()
	if err == nil {
		t.Fatal("committing an unknown transaction succeeded")
	}
	if app.txActive {
		t.Fatal("a failed commit left the transaction marked active")
	}
}

func TestSetAutocommitCommitsOpenTx(t *testing.T) {
	app := txApp(t)
	ctx := context.Background()
	if err := app.beginTxNow(ctx); err != nil {
		t.Fatalf("beginTxNow: %v", err)
	}
	tx, id := app.db.Tx, app.txID
	if _, err := executeStatement(ctx, false, tx, id, app.db,
		`INSERT INTO employees (name, salary) VALUES ('Edsger', 1)`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	// Turning autocommit back on finishes the open transaction: a client that
	// re-enables autocommit has decided the work is done.
	app.autocommit = true
	app.setAutocommit()
	if app.txActive {
		t.Fatal("setAutocommit did not commit the open transaction")
	}
	if got := countEmployees(t, ctx, true, tx, "", app.db); got != 3 {
		t.Fatalf("rows after switching autocommit on = %d, want 3", got)
	}
}

func TestCommitWorkingCopyRefusedWhileTxOpen(t *testing.T) {
	app := editApp(t)
	// Make the working copy dirty without running a query.
	app.edit.rows[0].strs[1] = "Changed"
	app.edit.rows[0].state = rowModified
	if !app.edit.dirty() {
		t.Fatal("the working copy is not dirty")
	}
	app.txActive = true
	app.commit()
	if app.errMsg == "" {
		t.Fatal("committing the working copy with a transaction open did not report why")
	}
	if app.busy {
		t.Fatal("the refused commit left the app busy")
	}
}
