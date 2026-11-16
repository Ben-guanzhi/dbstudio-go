package main

// The connection-level transaction (P1 #10).
//
// By default every statement the editor runs commits itself — autocommit is
// what databases and every SQL client do. The toolbar's Autocommit switch turns
// that off: the app then keeps one transaction open across statements, so a
// script that writes a few rows is one unit of work the user commits or rolls
// back by hand, instead of a set of changes that landed one at a time.
//
// The transaction is the kernel's SQLTxDriver registry (TxManager), one per
// connection, torn down with the connection. It is deliberately separate from
// the editable grid's working copy: the working copy commits its own batch, and
// with a transaction open that Commit is refused rather than silently committed
// inside it, because the kernel has no way to run a batch through a TxManager
// transaction.

import (
	"context"
	"fmt"
	"time"

	"github.com/Ben-guanzhi/dbcore"
)

// executeStatement runs one statement with optional bind arguments. With
// autocommit off and a transaction open the statement runs inside that
// transaction, so a script's statements share one unit of work the user can
// still commit or roll back; otherwise the driver's own autocommit behavior
// applies. The arguments are taken from the calling goroutine's snapshot rather
// than read from App, so a commit that happens on the view thread while a script
// runs cannot race with the script.
//
// This is the ONE place the "inside the transaction or past it" rule lives: both
// a plain run and a parameterized run call it, so they cannot disagree.
func executeStatement(ctx context.Context, autocommit bool, tx *dbcore.TxManager, txID string, db *DB, query string, args ...dbcore.Value) (*dbcore.QueryResult, error) {
	if db == nil || db.SQL == nil {
		// A connection mid-setup, or a KV-only driver: there is no statement to
		// run, and saying so beats dereferencing a nil interface.
		return nil, fmt.Errorf("this connection cannot run SQL")
	}
	if !autocommit && tx != nil && txID != "" {
		return tx.ExecuteInTx(ctx, txID, query, args...)
	}
	return db.SQL.Execute(ctx, query, args...)
}

// executeStatementSets is executeStatement's multi-set form: the same one rule
// about "inside the transaction or past it", answering with every result set the
// statement emitted.
//
// Only the plain, autocommit path can take the multi route: the kernel's
// ExecuteMulti runs the whole text as one statement (which is how SQL Server's
// batches and MySQL's stored procedures answer with several sets), and inside a
// transaction the registry's ExecuteInTx takes one statement's single set. The
// fallback is therefore the truth, not a loss: a statement that would emit
// several sets inside a transaction still emits them through the driver's own
// single call where the engine supports it.
func executeStatementSets(ctx context.Context, autocommit bool, tx *dbcore.TxManager, txID string, db *DB, query string, args []dbcore.Value, allowMulti bool) ([]*dbcore.QueryResult, error) {
	if db == nil {
		return nil, fmt.Errorf("this connection cannot run SQL")
	}
	inTx := !autocommit && tx != nil && txID != ""
	if allowMulti && !inTx {
		if m, ok := dbcore.AsMultiResult(db.Driver); ok {
			sets, err := m.ExecuteMulti(ctx, query, args...)
			if len(sets) > 0 || err != nil {
				return sets, err
			}
		}
	}
	res, err := executeStatement(ctx, autocommit, tx, txID, db, query, args...)
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, nil
	}
	return []*dbcore.QueryResult{res}, nil
}

// txCapable reports whether the open connection can run a transaction.
func (app *App) txCapable() bool {
	return app.db != nil && app.db.Tx != nil
}

// beginTxNow opens the connection transaction inline and reports its error. The
// UI wrappers call it from a goroutine; tests call it directly. It is a no-op
// when a transaction is already open.
func (app *App) beginTxNow(ctx context.Context) error {
	if app.db == nil || app.db.Tx == nil || app.txActive {
		return nil
	}
	info, err := app.db.Tx.Begin(ctx, dbcore.TxMode{IsolationLevel: app.txIsolation})
	if err != nil {
		return err
	}
	app.txActive, app.txID = true, info.ID
	return nil
}

// IsolationLevels are the levels the dropdown offers: the kernel's TxMode
// spellings, with "" as the engine's own default.
var IsolationLevels = []string{"", "read_uncommitted", "read_committed", "repeatable_read", "serializable"}

// isolationLabel is what the dropdown shows for a level.
func isolationLabel(level string) string {
	if level == "" {
		return "default"
	}
	return level
}

// commitTxNow finalizes the open transaction inline and reports its error, leaving
// no transaction open.
func (app *App) commitTxNow() error {
	if app.db == nil || app.db.Tx == nil || app.txID == "" {
		return nil
	}
	err := app.db.Tx.Commit(app.txID)
	app.txActive, app.txID = false, ""
	return err
}

// rollbackTxNow discards the open transaction inline and reports its error.
func (app *App) rollbackTxNow() error {
	if app.db == nil || app.db.Tx == nil || app.txID == "" {
		return nil
	}
	err := app.db.Tx.Rollback(app.txID)
	app.txActive, app.txID = false, ""
	return err
}

// setAutocommit reacts to the toolbar's switch being flipped. Turning autocommit
// back on while a transaction is open commits it: a client that re-enables
// autocommit has decided the work is done.
func (app *App) setAutocommit() {
	if app.autocommit && app.txActive {
		app.commitTx()
	}
	if app.autocommit {
		app.errMsg = ""
		app.status = "Autocommit on"
	} else {
		app.status = "Autocommit off"
	}
	app.invalidate()
}

// commitTx finalizes the open transaction, off the view thread.
func (app *App) commitTx() {
	if app.db == nil || app.db.Tx == nil || app.txID == "" || app.txBusy {
		return
	}
	app.txBusy = true
	app.errMsg = ""
	app.status = "Committing transaction…"
	app.invalidate()
	// S-01：commit 是"成功/失败"两态任务。
	started := time.Now()

	// An App built for a test has no window, so the call runs here instead of on
	// a goroutine that would have nothing to report back to.
	if app.win == nil {
		err := app.commitTxNow()
		app.txBusy = false
		app.recordOutcome("commit", started, err, nil)
		if err != nil {
			app.errMsg = "Commit transaction: " + err.Error()
			app.status = "Commit failed"
			return
		}
		app.errMsg = ""
		app.status = "Transaction committed"
		app.invalidate()
		return
	}

	go func() {
		err := app.commitTxNow()
		app.post(func() {
			app.txBusy = false
			app.recordOutcome("commit", started, err, nil)
			if err != nil {
				app.errMsg = "Commit transaction: " + err.Error()
				app.status = "Commit failed"
				return
			}
			app.errMsg = ""
			app.status = "Transaction committed"
		})
	}()
}

// rollbackTx discards the open transaction, off the view thread.
func (app *App) rollbackTx() {
	if app.db == nil || app.db.Tx == nil || app.txID == "" || app.txBusy {
		return
	}
	app.txBusy = true
	app.errMsg = ""
	app.status = "Rolling back transaction…"
	app.invalidate()
	// S-01：rollback 成功的 outcome 是 reverted（任务级回退），失败是 error。
	started := time.Now()

	if app.win == nil {
		err := app.rollbackTxNow()
		app.txBusy = false
		app.recordRollback(started, err)
		if err != nil {
			app.errMsg = "Rollback transaction: " + err.Error()
			app.status = "Rollback failed"
			return
		}
		app.errMsg = ""
		app.status = "Transaction rolled back"
		app.invalidate()
		return
	}

	go func() {
		err := app.rollbackTxNow()
		app.post(func() {
			app.txBusy = false
			app.recordRollback(started, err)
			if err != nil {
				app.errMsg = "Rollback transaction: " + err.Error()
				app.status = "Rollback failed"
				return
			}
			app.errMsg = ""
			app.status = "Transaction rolled back"
		})
	}()
}
