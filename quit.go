package main

import "github.com/egoist/mygo"

// beforeQuit saves the layout and allows explicit quits without a dialog.
// The session server keeps running unless Quit and End All Sessions was chosen.
func (a *App) beforeQuit(_ *mygo.QuitEvent) {
	a.exiting.Store(true)
	a.saveNow()
}

// beforeClose keeps the window and its terminals available from the tray.
// If the platform cannot create a tray, closing still closes the window.
func (a *App) beforeClose(e *mygo.CloseEvent) {
	if a.tray == nil || a.exiting.Load() || a.win == nil {
		return
	}
	e.PreventDefault()
	a.saveNow()
	a.win.Hide()
}
