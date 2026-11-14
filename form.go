package main

// The connection editor: a modal form over the connection list, in the same
// style as the rest of the app.

import (
	"github.com/egoist/mygo/ui"
)

// openForm opens the connection form for a new connection, or for the
// selected one when editing.
func (app *App) openForm() {
	conn := Conn{Kind: "sqlite", File: ":memory:"}
	hostDefaults(&conn)
	if app.connName != "" {
		if c, ok := app.store.Get(app.connName); ok {
			conn = c
		}
	}
	app.form = conn
	app.formKind = conn.Kind
	app.port = float64(conn.Port)
	app.sshPort = float64(conn.SSHPort)
	if app.sshPort == 0 {
		app.sshPort = 22
	}
	app.formErr = ""
	app.modal = true
	app.invalidate()
}

// hostDefaults fills the form's host, port and schema defaults in place.
func (app *App) hostDefaults() { hostDefaults(&app.form) }

// formView draws the modal when one is open.
func (app *App) formView(c *ui.Context) {
	if !app.modal {
		return
	}
	t := c.Theme()
	// A kind change invalidates the schema, so the dialect's default fills in.
	if app.formKind != app.form.Kind {
		app.form.Schema = ""
		app.formKind = app.form.Kind
		app.hostDefaults()
	}
	ui.Modal(c, &app.modal, func() {
		ui.Column(c).Gap(12).Padding(16).Width(440).
			Background(t.Surface).Radius(10).Border(1, t.Border).
			Children(func() {
				ui.Text(c, "Connection").FontSize(15).Bold()
				ui.Form(c, func() {
					ui.Field(c, "Name", func() {
						ui.TextInput(c, &app.form.Name)
					})
					ui.Field(c, "Kind", func() {
						ui.Select(c, &app.form.Kind, kinds)
					})
					ui.Row(c).Gap(8).Children(func() {
						ui.Field(c, "Colour", func() {
							// The picker holds a label; the connection holds the
							// key, so the palette can be relabelled without
							// invalidating saved connections.
							label := colorLabel(app.form.Color)
							sel := ui.Select(c, &label, colorLabels())
							if sel.Changed() {
								app.form.Color = colorOfLabel(label)
							}
						})
						ui.Field(c, "Read-only", func() {
							ui.Checkbox(c, &app.form.ReadOnly, "refuse statements that write")
						})
					})
					if app.form.Kind == "sqlite" {
						ui.Field(c, "File", func() {
							ui.TextInput(c, &app.form.File)
						})
					} else {
						ui.Row(c).Gap(8).Children(func() {
							ui.Field(c, "Host", func() {
								ui.TextInput(c, &app.form.Host)
							})
							ui.Field(c, "Port", func() {
								ui.NumberInput(c, &app.port, 0, 65535, 1)
							})
						})
						ui.Row(c).Gap(8).Children(func() {
							ui.Field(c, "User", func() {
								ui.TextInput(c, &app.form.User)
							})
							ui.Field(c, "Password", func() {
								ui.TextInput(c, &app.form.Password).Password()
							})
						})
						ui.Row(c).Gap(8).Children(func() {
							ui.Field(c, "Database", func() {
								ui.TextInput(c, &app.form.Database)
							})
							ui.Field(c, "Schema", func() {
								ui.TextInput(c, &app.form.Schema)
							})
						})
						if app.form.Kind == "postgres" {
							ui.Field(c, "SSL mode", func() {
								ui.Select(c, &app.form.SSLMode,
									[]string{"", "disable", "require", "verify-ca", "verify-full"})
							})
						}
						// The SSH bastion, when the database is only reachable
						// through one. Empty host means a direct connection.
						ui.Divider(c)
						ui.Text(c, "SSH tunnel (optional)").FontSize(10).TextColor(t.TextMuted)
						ui.Row(c).Gap(8).Children(func() {
							ui.Field(c, "SSH host", func() {
								ui.TextInput(c, &app.form.SSHHost)
							})
							ui.Field(c, "Port", func() {
								ui.NumberInput(c, &app.sshPort, 0, 65535, 1)
							})
						})
						ui.Row(c).Gap(8).Children(func() {
							ui.Field(c, "SSH user", func() {
								ui.TextInput(c, &app.form.SSHUser)
							})
							ui.Field(c, "Password", func() {
								ui.TextInput(c, &app.form.SSHPassword).Password()
							})
						})
						ui.Row(c).Gap(8).Children(func() {
							ui.Field(c, "Key file", func() {
								ui.TextInput(c, &app.form.SSHKeyFile)
							})
							ui.Field(c, "Key passphrase", func() {
								ui.TextInput(c, &app.form.SSHPassphrase).Password()
							})
						})
					}
				})
				if app.formErr != "" {
					ui.Text(c, app.formErr).TextColor(t.Danger).Padding(4, 0)
				}
				app.formTestView(c)
				ui.Row(c).Gap(8).Children(func() {
					ui.Spacer(c)
					if ui.Button(c, "Test").Disabled(app.formTesting).
						Tooltip("Open this connection and close it again").Clicked() {
						app.testForm()
					}
					if ui.Button(c, "Cancel").Clicked() {
						app.modal = false
					}
					if ui.PrimaryButton(c, "Save").Clicked() {
						app.saveForm()
					}
				})
			})
	})
}

// saveForm validates the form, stores the connection and connects to it.
func (app *App) saveForm() {
	app.form.Port = int(app.port)
	app.form.SSHPort = int(app.sshPort)
	normalize(&app.form)

	switch {
	case app.form.Name == "":
		app.formErr = "Name is required"
		return
	case app.form.Kind == "sqlite" && app.form.File == "":
		app.formErr = "File is required (or use :memory:)"
		return
	case app.form.Kind != "sqlite" && app.form.Host == "":
		app.formErr = "Host is required"
		return
	case app.form.Kind != "sqlite" && app.form.Database == "":
		app.formErr = "Database is required"
		return
	}

	if err := app.store.Upsert(app.form); err != nil {
		app.formErr = err.Error()
		return
	}
	app.modal = false
	app.connName = app.form.Name
	app.connect(app.connName)
}
