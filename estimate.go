package main

// Row estimates on the pager, and column widths that survive.
//
// Two things a mature client does that this one did not:
//
//   - a table too large to count still says how large it is: the catalog's
//     estimate ("~1.2M rows") instead of "Page 1 / ?". The estimate comes from
//     TableInfo.Rows when the table list carried one (PostgreSQL, SQL Server,
//     Oracle, DM fill it), and from the driver's RowEstimator when it did not
//     (MySQL, whose list can afford an estimate but not for every table at once);
//     an engine with no statistics answers nothing, and the pager keeps its "?";
//   - a column dragged wider stays wider: the width lives in the grid's layout
//     and is written to the session, so the next launch opens with the columns
//     the user arranged.

import (
	"context"
	"time"

	"github.com/dbcore/dbcore"
)

// rowEstimate is a cached estimate for one table: the rows, and whether the
// engine could give one at all (a miss is cached too, so a view is not asked
// about on every frame).
type rowEstimate struct {
	rows  int64
	known bool
}

// estimateKey is the cache key of a table's estimate.
func estimateKey(schema, table string) string { return schema + "\x00" + table }

// estimateOf returns the cached estimate for a table.
func (app *App) estimateOf(schema, table string) (int64, bool) {
	e, ok := app.rowEst[schema+"\x00"+table]
	if !ok || !e.known {
		return 0, false
	}
	return e.rows, true
}

// requestEstimate fills in a table's estimate when the pager needs one: from the
// table list when it carried it, else from the driver's own estimator, off the
// view thread. It is a no-op when the answer is already known.
func (app *App) requestEstimate(schema, table string) {
	if table == "" {
		return
	}
	key := estimateKey(schema, table)
	if _, seen := app.rowEst[key]; seen {
		return // asked already (a miss is an answer)
	}
	// The table list's estimate is free to read.
	if tabs, st := app.TablesOf(schema); st == TablesLoaded {
		for _, t := range tabs {
			if t.Name == table {
				if t.Rows >= 0 {
					app.putEstimate(key, rowEstimate{rows: t.Rows, known: true})
					return
				}
				break
			}
		}
	}
	if app.db == nil {
		return
	}
	est, ok := dbcore.AsRowEstimator(app.db.Driver)
	if !ok {
		app.putEstimate(key, rowEstimate{}) // no estimator: remember the miss
		return
	}
	app.putEstimate(key, rowEstimate{}) // claim the key so frames do not ask twice
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		rows, _, known, err := est.EstimatedRows(ctx, schema, table)
		if err != nil || !known {
			return
		}
		app.post(func() {
			app.putEstimate(key, rowEstimate{rows: rows, known: true})
			app.invalidate()
		})
	}()
}

// putEstimate records an estimate.
func (app *App) putEstimate(key string, e rowEstimate) {
	if app.rowEst == nil {
		app.rowEst = map[string]rowEstimate{}
	}
	app.rowEst[key] = e
}

// estimateLabel is the pager's "~1.2M rows" for a tab, or "" when there is no
// estimate to show.
func (app *App) estimateLabel(t *resTab) string {
	if t == nil || t.table == "" {
		return ""
	}
	rows, ok := app.estimateOf(t.schema, t.table)
	if !ok {
		return ""
	}
	return dbcore.EstimateLabel(rows) + " rows"
}

// noteColumnWidths writes the session when the user has resized a column. The
// grid keeps the widths in its own layout; this only notices they changed, which
// is what makes them survive a restart.
func (app *App) noteColumnWidths() {
	widths := app.grid.Columns.Widths
	if len(widths) == len(app.savedWidths) {
		same := true
		for k, v := range widths {
			if app.savedWidths[k] != v {
				same = false
				break
			}
		}
		if same {
			return
		}
	}
	app.savedWidths = make(map[string]float32, len(widths))
	for k, v := range widths {
		app.savedWidths[k] = v
	}
	app.saveSession()
}
