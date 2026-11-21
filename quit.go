package main

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo"
)

// beforeQuit saves the layout and lets the quit end the sessions. When a
// program still runs or an SSH connection is open, it asks first.
func (a *App) beforeQuit(e *mygo.QuitEvent) {
	a.saveNow()
	if a.exiting.Load() {
		return
	}
	if a.workSummary() == "" {
		a.exiting.Store(true)
		return
	}
	e.PreventDefault()
	a.confirmQuit()
}

// beforeClose ends the app with the window: closing GoRex is finishing
// work, not hiding it. What still runs asks first.
func (a *App) beforeClose(e *mygo.CloseEvent) {
	if a.exiting.Load() || a.quitting {
		return
	}
	e.PreventDefault()
	a.confirmQuit()
}

// confirmQuit ends the app, asking first when work still runs.
func (a *App) confirmQuit() {
	if a.confirming {
		return
	}
	summary := a.workSummary()
	if summary == "" {
		a.quitNow()
		return
	}
	ask := a.askQuit
	if ask == nil {
		ask = a.quitDialog
	}
	a.confirming = true
	go func() {
		quit := ask(summary)
		a.post(func() {
			a.confirming = false
			if quit {
				a.quitNow()
			}
		})
		a.invalidate()
	}()
}

// quitDialog asks whether to end the sessions a quit would end.
func (a *App) quitDialog(summary string) bool {
	res, err := mygo.Dialog.Message(mygo.MessageOptions{
		Parent:        a.win,
		Type:          mygo.MessageWarning,
		Title:         "Quit GoRex?",
		Message:       summary,
		Detail:        "Quitting ends every session, with the programs in them.",
		Buttons:       []string{"Quit", "Cancel"},
		DefaultButton: 1,
		CancelButton:  1,
	})
	return err == nil && res.Button == 0
}

// quitNow ends every session, keeps the layout, and quits.
func (a *App) quitNow() {
	if !a.exiting.CompareAndSwap(false, true) {
		return
	}
	a.saveNow()
	mygo.App.Quit()
}

// workSummary names what a quit would end, or "" when nothing runs: the
// programs in the panes' foregrounds, and the SSH connections.
func (a *App) workSummary() string {
	programs, ssh := 0, 0
	for _, t := range a.tabs {
		for _, p := range t.panes() {
			if p.profile.SSH != nil {
				ssh++
			}
			if p.sess == nil {
				continue
			}
			in := p.sess.Info()
			p.info = in
			if in.Idle || in.Program == "" {
				continue
			}
			programs++
			if strings.EqualFold(in.Program, "ssh") {
				ssh++
			}
		}
	}
	if n := len(a.sftpSessions); n > ssh {
		ssh = n
	}
	switch {
	case programs == 0 && ssh == 0:
		return ""
	case programs == 0:
		return "An SSH connection is open."
	case programs == 1 && ssh > 0:
		return "An SSH connection is still running."
	case programs == 1:
		return "A program is still running."
	case ssh == 0:
		return fmt.Sprintf("%d programs are still running.", programs)
	case ssh == programs:
		return fmt.Sprintf("%d programs are still running, over SSH.", programs)
	default:
		return fmt.Sprintf("%d programs are still running, %d of them over SSH.", programs, ssh)
	}
}
