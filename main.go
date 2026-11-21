// ShellGrip is a terminal of tabs and split panes, forked from GoRex,
// in MyGo's native UI. Its shells run in pseudo-terminals of the window:
// closing the window ends them, as it ends the app.
//
//	go tool mygo dev    the app, rebuilt as the code changes
package main

import (
	_ "embed"
	"encoding/json"
	"log"
	"os"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"shellgrip/internal/session"
)

// manifestData is the app's own mygo.json, embedded so version and
// copyright read the same from source and from a packaged build.
//
//go:embed mygo.json
var manifestData []byte

// manifest is the part of mygo.json the app tells about itself.
var manifest = func() appManifest {
	var m appManifest
	json.Unmarshal(manifestData, &m) // the file ships with the source
	return m
}()

type appManifest struct {
	Version   string `json:"version"`
	Copyright string `json:"copyright"`
}

// appVersion is the packaged app's version, or the manifest's when ShellGrip
// runs from source, where its executable carries none.
func appVersion() string {
	if v := mygo.App.Version(); v != "" {
		return v
	}
	return manifest.Version
}

func main() {
	// Git runs ShellGrip itself for an askpass prompt, with the secret in the
	// environment. Answer it before anything of the app starts.
	if secret := os.Getenv("SHELLGRIP_ASKPASS_SECRET"); secret != "" {
		runAskpass(secret, os.Args[1:])
		return
	}
	// One ShellGrip per user: a taskbar launch while it runs would otherwise
	// start a second GUI over the same data directory. Hand such a launch
	// to the running instance (see OnSecondInstance) and let this process
	// exit.
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}
	// The state of the window lives in the app's data directory, which
	// dev builds take from SHELLGRIP_DIR: "ShellGrip", or "ShellGrip Dev" under mygo
	// dev, which keeps the installed app's state apart.
	if os.Getenv("SHELLGRIP_DIR") == "" {
		if dir, err := mygo.App.Path(mygo.PathUserData); err == nil {
			os.Setenv("SHELLGRIP_DIR", dir)
		}
	}
	registerFonts()
	loadSettings()
	a := &App{}
	mygo.App.SetMenu(a.menu())
	mygo.App.OnSecondInstance(func(_ []string, _ string) {
		a.showWindow()
	})
	mygo.App.WhenReady(func() {
		a.open()
	})
	mygo.App.OnActivate(func(hasVisibleWindows bool) {
		if !hasVisibleWindows {
			a.showWindow()
		}
	})
	mygo.App.OnBeforeQuit(a.beforeQuit)
	mygo.App.OnQuit(func() {
		a.closeSFTPSessions()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// open opens the window, with the tabs of the saved layout.
func (a *App) open() {
	a.reset()
	a.profiles = discoverProfiles()
	a.sshConns = loadSSHConnections()
	opts := mygo.WindowOptions{
		Title:                "ShellGrip",
		Width:                1000,
		Height:               620,
		MinWidth:             560,
		MinHeight:            340,
		StateKey:             "main",
		TitleBarStyle:        mygo.TitleBarHidden,
		TrafficLightPosition: &mygo.Point{X: 16, Y: 15},
		BackgroundColor:      "light-dark(#e9eaec, #252628)",
		Vibrancy:             chromeMaterial(),
		Content:              ui.View(a.view),
	}
	if ownControls {
		// The title bar draws the window's controls (windowControls).
		opts.TitleBarStyle, opts.Frameless = mygo.TitleBarDefault, true
	}
	win := mygo.NewWindow(opts)
	a.win = win
	a.applyVibrancy()
	// The host's details may take a second: the panel shows the name until
	// they are there.
	go func() {
		h := session.HostInfo()
		win.Update(func() { a.host = h })
	}()
	// The file trees read when the window is back, and keep quiet while it
	// is in the background.
	win.OnFocus(func() { a.refreshTrees() })
	win.OnShow(func() { a.refreshTrees() })
	win.OnBlur(func() { a.focusedWin = false })
	if !a.restore() {
		home, _ := os.UserHomeDir()
		a.newTab(home)
	}
	a.changed()
	a.debugHook()
	win.OnClose(a.beforeClose)
	win.OnClosed(func() {
		a.closeFileWatches()
		a.saveNow()
		a.closeSFTPSessions()
		a.quitting = true
		for _, t := range a.tabs {
			for _, p := range t.panes() {
				p.markClosed()
				if p.sess != nil {
					p.sess.Kill() // quitting ends the programs, at once
				}
				if p.term != nil {
					p.term.Close()
				}
			}
		}
		a.win = nil
	})
}

// showWindow brings the window to the front, as a second launch or a click
// on the app asks.
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

// saveNow writes the layout to disk, as the window closes or the app quits.
func (a *App) saveNow() {
	if len(a.tabs) == 0 && !a.quitting {
		return
	}
	b, err := json.Marshal(a.snapshot())
	if err != nil {
		return
	}
	a.writeLayout(b)
}

// reset forgets the state of a window that closed, for the next window.
func (a *App) reset() {
	a.closeSFTPSessions()
	a.sftpSessions = map[int]*sftpSession{}
	a.remoteTree = nil
	a.tabs, a.active, a.focusReq = nil, 0, nil
	a.profilesOpen = false
	a.sshDraft = nil
	a.vsCodeWSLHelpOpen, a.vsCodeWSLMessage = false, ""
	if a.themePreviewActive {
		a.applyTerminalTheme(prefs.TerminalTheme)
	}
	a.quickThemeOpen, a.themePreviewActive = false, false
	a.settingsCategory = ""
	a.ctrlHeldAt, a.tabHints = time.Time{}, false
	a.copiedCharacters = 0
	a.paletteOpen, a.hostOpen, a.aboutOpen, a.renaming = false, false, false, nil
	a.gitMenu, a.gitPrompt, a.gitAuth = gitMenuState{}, gitPromptState{}, gitAuthState{}
	a.scmDir, a.scmMessage, a.scmMessages, a.scmExpanded = "", "", nil, nil
	a.saveDue, a.quitting, a.lastSnapshot, a.title = false, false, "", ""
	a.confirming = false
	a.exiting.Store(false)
	a.cueOn = false
}
