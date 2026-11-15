package main

// The window's canvas is laid out at the size the window was given, but the menu
// bar takes its height out of the client area afterwards: the window is created
// 1320x800, the bar appears, and the client area becomes 1306x731 while the
// canvas keeps being laid out at 800. Whatever the layout puts last — the status
// bar, the tree's last rows — is then below the client area.
//
// mygo computes the outer size from the content size only when the menu bar
// already exists (its outerSize checks the window's menu), so a menu set after
// the window was created shrinks the client area without the window growing to
// match. Asking for the content size again, with the bar in place, settles it.
//
// 波 101 续二十七.

// fitContentToWindow sizes the window so its content area is the size the layout
// was given, after the menu bar has taken its share.
func (app *App) fitContentToWindow() {
	if app.win == nil {
		return
	}
	iw, ih := app.layoutSize()
	if iw <= 0 || ih <= 0 {
		return
	}
	app.win.SetContentSize(iw, ih)
}

// layoutSize is the size the window was created with: what the layout was
// given, before the platform's chrome and the menu bar took their share.
func (app *App) layoutSize() (int, int) {
	if app.wantWidth > 0 && app.wantHeight > 0 {
		return app.wantWidth, app.wantHeight
	}
	return app.win.Size()
}
