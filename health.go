package main

// The connection's health indicator (P2-1 of the gap analysis).
//
// FNTDB ch02: an idle connection can be dropped silently by the OS or the
// peer — a laptop suspended, a network changed, a server reaped the session —
// and without a probe the failure only surfaces at the next statement, as a
// confusing error. The kernel's Driver has Ping(ctx) for exactly this. So the
// toolbar shows a dot: green when the last probe answered, red when it did not,
// grey before any probe, with the time of the last check on hover.
//
// The probe is cheap and never in a statement's way: it runs every
// pingInterval, carries a generation so a slow answer cannot overrule a newer
// one, and a probe on a connection that has since closed is dropped.

import (
	"context"
	"time"
)

// pingInterval is how often the toolbar probes an open connection. Half a
// minute is frequent enough that a dropped link is noticed while the user is
// still looking at the window, and rare enough that it is noise on the wire.
const pingInterval = 30 * time.Second

// pingTimeout bounds one probe: a connection that cannot answer in this long is
// down as far as the indicator is concerned.
const pingTimeout = 5 * time.Second

// pingOnce probes the open connection and records the verdict. It is safe to
// call from the view thread: the probe itself runs in a goroutine, and the
// answer reaches App through post. A test drives it directly.
func (app *App) pingOnce() {
	if !app.open || app.db == nil || app.db.Driver == nil {
		return
	}
	db := app.db
	app.healthMu.Lock()
	app.healthGen++
	gen := app.healthGen
	app.healthMu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), pingTimeout)
		defer cancel()
		err := db.Driver.Ping(ctx)
		app.post(func() {
			app.healthMu.Lock()
			defer app.healthMu.Unlock()
			if gen != app.healthGen || app.db != db {
				return // a newer probe, or another connection, owns the dot
			}
			if err != nil {
				app.health = "down"
			} else {
				app.health = "ok"
			}
			app.invalidate()
		})
	}()
}

// pingLoop probes until the window closes. It is started where the window is
// built; a test never starts it (so no test pays for the ticker).
func (app *App) pingLoop(stop <-chan struct{}) {
	t := time.NewTicker(pingInterval)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			app.pingOnce()
		}
	}
}

// healthState 是 health/healthGen 的带锁读（渲染与测试用）。
func (app *App) healthState() (string, uint64) {
	app.healthMu.Lock()
	defer app.healthMu.Unlock()
	return app.health, app.healthGen
}

// setHealthState 是带锁写（测试直接构造状态用）。
func (app *App) setHealthState(h string, gen uint64) {
	app.healthMu.Lock()
	defer app.healthMu.Unlock()
	app.health, app.healthGen = h, gen
}

// healthLabel is the dot's tooltip and the word beside it.
func (app *App) healthLabel() string {
	h, _ := app.healthState()
	switch h {
	case "ok":
		return "Connected"
	case "down":
		return "Connection lost"
	default:
		return "Not checked"
	}
}
