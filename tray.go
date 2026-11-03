package main

import (
	_ "embed"
	"log"

	"github.com/egoist/mygo"
)

//go:embed resources/icon.png
var trayIcon []byte

func (a *App) createTray() {
	tray, err := mygo.NewTray(mygo.TrayOptions{
		Icon:    trayIcon,
		ToolTip: "GoRex — terminal sessions",
		Menu:    a.trayMenu(),
	})
	if err != nil {
		log.Printf("GoRex tray unavailable: %v", err)
		return
	}
	a.tray = tray
	tray.OnClick(a.showWindow)
}

func (a *App) trayMenu() *mygo.Menu {
	return mygo.NewMenu([]*mygo.MenuItem{
		{Label: "Show GoRex", Click: func(*mygo.MenuItem, *mygo.Window) { a.showWindow() }},
		mygo.Separator(),
		{Label: "Quit GoRex (Keep Sessions)", Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() }},
		{Label: cmdEndAll.Title, Click: func(*mygo.MenuItem, *mygo.Window) { a.quitAndEnd() }},
	})
}

func (a *App) showWindow() {
	if a.exiting.Load() {
		return
	}
	if a.win == nil {
		a.open()
		return
	}
	if a.win.IsMinimized() {
		a.win.Restore()
	}
	a.win.Show()
}
