package main

// Connection info: what this connection can do, in one read-only list.
//
// Every optional capability in the kernel is a type assertion (AsSQLTx,
// AsBatchWriter, AsExtraMetadata, AsTriggerLister, …), and the toolbar's
// behaviour follows from them: the Autocommit switch exists when the driver is
// an SQLTxDriver, the grid's Commit when it is a BatchWriter, the trigger
// section when it is a TriggerLister. When one of those is missing the feature
// is simply absent, which is right for the UI and confusing for a person — "why
// is there no transaction switch on this connection?".
//
// So this panel answers that question by naming the capabilities the connection
// actually has. It reports the capability layer, not a guess at what the engine
// might support: a driver that implements BatchWriter gets a yes, and whether a
// particular table has a CHECK constraint is a question the object tree answers
// when it asks.

import (
	"fmt"

	"github.com/Ben-guanzhi/dbcore"
	"github.com/egoist/mygo/ui"
)

// infoRow is one line of the panel: a capability's name and whether the
// connection has it. ok colors the value and tells the eye where the gaps are.
type infoRow struct {
	label string
	value string
	ok    bool
}

// connInfo is the panel's state: whether it is open, and the rows it shows.
type connInfo struct {
	open bool
	rows []infoRow
}

// openConnInfo builds the panel from the open connection and shows it. The rows
// are read once, when it opens: a capability cannot change without reconnecting,
// and reconnecting closes the panel.
func (app *App) openConnInfo() {
	if !app.open || app.db == nil || app.db.Driver == nil {
		app.errMsg = "Not connected"
		app.status = "Not connected"
		app.invalidate()
		return
	}
	app.connInfo = connInfo{open: true, rows: app.connInfoRows()}
	app.invalidate()
}

// connInfoRows is the capability matrix of the open connection. Each row is one
// assertion the app itself makes elsewhere, so the panel and the behaviour can
// never disagree: the Autocommit switch and the transaction row read the same
// interface.
func (app *App) connInfoRows() []infoRow {
	d := app.db.Driver
	engine := app.engine()
	rows := []infoRow{
		{label: "Connection", value: app.connName, ok: true},
		{label: "Engine", value: engineLabel(engine), ok: true},
	}
	if d == nil {
		// A DB without a driver is a connection mid-setup or a test fixture: the
		// capability rows would all read "no" for the wrong reason, so say so.
		return append(rows, infoRow{label: "Driver", value: "not attached", ok: false})
	}
	// Where it points: a file for SQLite, host:port/database otherwise.
	if app.conn.Kind == "sqlite" || app.conn.File != "" {
		rows = append(rows, infoRow{label: "File", value: app.conn.File, ok: app.conn.File != ""})
	} else {
		where := app.conn.Host
		if app.conn.Port > 0 {
			where = fmt.Sprintf("%s:%d", app.conn.Host, app.conn.Port)
		}
		if app.conn.Database != "" {
			where += "/" + app.conn.Database
		}
		rows = append(rows, infoRow{label: "Address", value: where, ok: where != ""})
	}
	if app.conn.Schema != "" {
		rows = append(rows, infoRow{label: "Schema", value: app.conn.Schema, ok: true})
	}

	// The capability layer, one row per assertion the app makes.
	add := func(label string, has bool) {
		v := "no"
		if has {
			v = "yes"
		}
		rows = append(rows, infoRow{label: label, value: v, ok: has})
	}
	_, sql := d.AsSQL()
	add("SQL statements", sql)
	_, kv := d.AsKV()
	add("Key/value browse (Redis)", kv)
	_, txd := d.(dbcore.SQLTxDriver)
	add("Transactions (Autocommit, Commit Tx)", txd)
	_, bw := dbcore.AsBatchWriter(d)
	add("Batch writes (grid commit, import)", bw)
	_, rw := dbcore.AsRowWriter(d)
	add("Single-row writes", rw)
	_, xm := dbcore.AsExtraMetadata(d)
	add("Extra metadata (views, CHECK, sequences, routines, partitions, enums)", xm)
	_, tl := dbcore.AsTriggerLister(d)
	add("Trigger catalog", tl)
	_, bc := dbcore.AsBatchColumns(d)
	add("Catalog batch read (columns)", bc)
	_, bf := dbcore.AsBatchForeignKeys(d)
	add("Catalog batch read (foreign keys)", bf)
	_, oh := dbcore.AsObjectHealth(d)
	add("Object health (invalid objects)", oh)
	_, ms := dbcore.AsMetricsSource(d)
	add("Server metrics", ms)
	_, sq := dbcore.AsStreamQuerier(d)
	add("Streamed results", sq)
	return rows
}

// connInfoView draws the panel: the rows, a note about why a missing capability
// is not an error, and a close.
func (app *App) connInfoView(c *ui.Context) {
	ci := &app.connInfo
	if !ci.open {
		return
	}
	t := c.Theme()
	ui.Modal(c, &ci.open, func() {
		ui.Column(c).Gap(8).Padding(12).Width(560).Height(520).
			Background(t.Surface).Radius(10).Border(1, t.Border).
			Children(func() {
				ui.Text(c, "Connection Info").FontSize(13).Bold()
				ui.Scroll(c).Grow(1).FillWidth().KeepScrollbars(true).Children(func() {
					ui.Column(c).Gap(3).Children(func() {
						for _, r := range ci.rows {
							ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
								ui.Text(c, r.label).TextColor(t.TextMuted).FontSize(11).SingleLine()
								ui.Spacer(c)
								txt := ui.Text(c, r.value).FontSize(11).SingleLine()
								if r.ok {
									txt.TextColor(t.Success)
								} else {
									txt.TextColor(t.TextMuted)
								}
							})
						}
					})
				})
				ui.Text(c, "A capability a driver does not implement is not a failure: the toolbar and "+
					"the tree simply leave out the features that need it. Object sections also hide when "+
					"an engine has no such concept, and the status line names the concept.").
					TextColor(t.TextMuted).FontSize(10).Wrap()
				ui.Row(c).Gap(8).Children(func() {
					ui.Spacer(c)
					if ui.Button(c, "Close").Clicked() {
						ci.open = false
					}
				})
			})
	})
}

// openConnInfoFromHealthDot is the health dot's menu: the same panel, reachable
// where the user already looks when a connection misbehaves.
func (app *App) openConnInfoFromHealthDot(c *ui.Context) {
	app.openConnInfo()
}
