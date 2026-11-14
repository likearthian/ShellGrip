// GoRex is a terminal of tabs and split panes, after Superlogical's Rex,
// in MyGo's native UI. Its shells run in a session server of their own,
// so that they outlive the app: quit it, open it again, and every tab and
// pane is back as it was, its programs still running.
//
//	go tool mygo dev    the app, rebuilt as the code changes
//	go run . -server    the session server alone, in the foreground
package main

import (
	_ "embed"
	"encoding/json"
	"log"
	"os"
	"runtime"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"gorex/internal/rex"
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

// appVersion is the packaged app's version, or the manifest's when GoRex
// runs from source, where its executable carries none.
func appVersion() string {
	if v := mygo.App.Version(); v != "" {
		return v
	}
	return manifest.Version
}

func main() {
	// Git runs GoRex itself for an askpass prompt, with the secret in the
	// environment. Answer it before anything of the app starts.
	if secret := os.Getenv("GOREX_ASKPASS_SECRET"); secret != "" {
		runAskpass(secret, os.Args[1:])
		return
	}
	// -server runs the session server; other arguments are AppKit's, as
	// -NSWindowResizeTime, which it reads itself.
	if len(os.Args) > 1 && os.Args[1] == "-server" {
		log.SetPrefix("[server] ")
		if err := rex.Serve(); err != nil {
			log.Fatal(err)
		}
		return
	}
	// One GoRex per user: closing the window hides it to the tray, so a
	// taskbar launch while it runs would otherwise start a second GUI,
	// with a second tray icon and a second window over the same
	// sessions. Hand such a launch to the running instance (see
	// OnSecondInstance) and let this process exit.
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}
	// The state of the sessions lives in the app's data directory, which
	// the server it starts takes from GOREX_DIR: "GoRex", or "GoRex Dev"
	// under mygo dev, which keeps the installed app's sessions apart.
	if os.Getenv("GOREX_DIR") == "" {
		if dir, err := mygo.App.Path(mygo.PathUserData); err == nil {
			os.Setenv("GOREX_DIR", dir)
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
		if a.win != nil {
			a.createTray()
		}
	})
	mygo.App.OnActivate(func(hasVisibleWindows bool) {
		if !hasVisibleWindows {
			a.showWindow()
		}
	})
	mygo.App.OnWindowAllClosed(func() {
		if runtime.GOOS != "darwin" && a.tray == nil {
			mygo.App.Quit()
		}
	})
	mygo.App.OnBeforeQuit(a.beforeQuit)
	mygo.App.OnQuit(func() {
		a.closeSFTPSessions()
		if a.tray != nil {
			a.tray.Destroy()
		}
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// open opens the window, with the tabs the server kept.
func (a *App) open() {
	client, err := rex.Connect()
	if err != nil {
		log.Print(err)
		mygo.Dialog.Error("GoRex could not start its session server", err.Error())
		mygo.App.Quit()
		return
	}
	a.client = client
	a.hello, err = client.Hello()
	if err == nil && mygo.IsDev() && staleServer(a.hello) {
		// A server a build before this one started runs that build's
		// code: start one of this build's, ending its sessions.
		if client, err = client.Restart(a.hello.PID); err == nil {
			a.client = client
			a.hello, err = client.Hello()
		} else {
			log.Print(err)
			mygo.Dialog.Error("GoRex could not restart its session server", err.Error())
			mygo.App.Quit()
			return
		}
	}
	if err == nil && a.hello.Version != rex.ProtocolVersion {
		a.err = "The session server is of another version of GoRex: quit and end all sessions to restart it."
	}
	a.reset()
	a.profiles = discoverProfiles()
	a.sshConns = loadSSHConnections()
	opts := mygo.WindowOptions{
		Title:                "GoRex",
		Width:                1000,
		Height:               620,
		MinWidth:             560,
		MinHeight:            340,
		StateKey:             "main",
		TitleBarStyle:        mygo.TitleBarHidden,
		TrafficLightPosition: &mygo.Point{X: 16, Y: 15},
		BackgroundColor:      "light-dark(#efe1e6, #292426)",
		Content:              ui.View(a.view),
	}
	if ownControls {
		// The title bar draws the window's controls (windowControls).
		opts.TitleBarStyle, opts.Frameless = mygo.TitleBarDefault, true
	}
	win := mygo.NewWindow(opts)
	a.win = win
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
	go a.poll(win, client)
	a.debugHook()
	win.OnClose(a.beforeClose)
	win.OnClosed(func() {
		a.saveNow()
		a.closeSFTPSessions()
		a.quitting = true
		for _, t := range a.tabs {
			for _, p := range t.panes() {
				p.closed = true
				if p.term != nil {
					p.term.Close() // detaches: the session goes on
				}
			}
		}
		a.client.Close()
		a.win = nil
	})
}

// saveNow sends the layout to the server and waits for it to be kept.
func (a *App) saveNow() {
	if a.client == nil || len(a.tabs) == 0 && !a.quitting {
		return
	}
	b, err := json.Marshal(a.snapshot())
	if err != nil {
		return
	}
	done := make(chan struct{})
	go func() {
		a.client.SetLayout(b)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
	}
}

// reset forgets the state of a window that closed, keeping the server's
// connection, for the next window.
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
	a.scmDir, a.scmMessage, a.scmExpanded = "", "", nil
	a.saveDue, a.quitting, a.lastSnapshot, a.title = false, false, "", ""
	a.exiting.Store(false)
	a.cueOn = false
}

// staleServer reports whether the server runs another build of the app
// than this one, as after mygo dev rebuilt it.
func staleServer(h rex.Hello) bool {
	exe, at := rex.Executable()
	return h.Exe != exe || !h.ExeTime.Equal(at)
}
