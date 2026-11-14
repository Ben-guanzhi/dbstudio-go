package main

// Testing a connection before saving it.
//
// Every mature client has the button: fill the form, press Test, and learn
// whether this host/user/database/tunnel actually answers — before the
// connection is stored and the window switches to it. It is the difference
// between "the form is plausible" and "it works".
//
// The test opens a real connection from the form's values (through the same
// OpenDB the editor uses, so a bastion is exercised too), reports how long it
// took and which engine answered, and closes it again. Nothing is saved.

import (
	"context"
	"time"

	"github.com/egoist/mygo/ui"
)

// testForm tries the form's connection. It runs off the view thread: a wrong
// host sits in the connect timeout, and the form must stay responsive (and
// cancellable by closing it).
func (app *App) testForm() {
	if app.formTesting {
		return
	}
	app.formTesting = true
	app.formTest = ""
	app.formTestOK = false
	app.invalidate()

	conn := app.form
	conn.Port = int(app.port)
	conn.SSHPort = int(app.sshPort)
	normalize(&conn)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		started := time.Now()
		db, err := OpenDB(ctx, conn)
		elapsed := time.Since(started)
		if err != nil {
			app.post(func() {
				app.formTesting = false
				app.formTestOK = false
				app.formTest = err.Error()
				app.invalidate()
			})
			return
		}
		engine := string(db.Kind)
		_ = db.Close()
		app.post(func() {
			app.formTesting = false
			app.formTestOK = true
			app.formTest = "Connected to " + engineLabel(engine) +
				" in " + elapsed.Round(time.Millisecond).String()
			app.invalidate()
		})
	}()
}

// formTestView is the line under the form's buttons: the test's verdict, or the
// spinner while it runs. It is separate from the form's error so a failed test
// does not look like a failed save.
func (app *App) formTestView(c *ui.Context) {
	t := c.Theme()
	if app.formTesting {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Spinner(c)
			ui.Text(c, "Testing…").FontSize(10).TextColor(t.TextMuted).SingleLine()
		})
		return
	}
	if app.formTest == "" {
		return
	}
	ink := t.Success
	prefix := "✓ "
	if !app.formTestOK {
		ink = t.Danger
		prefix = "✗ "
	}
	ui.Text(c, prefix+app.formTest).FontSize(10).TextColor(ink).Wrap()
}
