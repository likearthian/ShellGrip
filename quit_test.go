package main

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// TestQuitKeepsLayout checks that a quit with the shells at their prompts
// goes ahead, saves the layout, and leaves the sessions for the window to
// close.
func TestQuitKeepsLayout(t *testing.T) {
	a, _ := newTestApp(t)
	want, err := json.Marshal(a.snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if summary := a.workSummary(); summary != "" {
		t.Fatalf("a fresh shell asks %q", summary)
	}
	e := &mygo.QuitEvent{}
	a.beforeQuit(e)
	if e.DefaultPrevented() || !a.exiting.Load() {
		t.Fatal("explicit quit should proceed without a dialog")
	}
	got, err := os.ReadFile(a.layoutPath())
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("saved layout = %s, %v; want %s", got, err, want)
	}
	if p := a.tab().Focus; p.sess == nil || p.term == nil {
		t.Fatal("the session should run until the window closes")
	}
}

// TestQuitAsksWithARunningProgram checks that a program in the foreground
// stops the quit, and a Cancel keeps the app and its programs running.
func TestQuitAsksWithARunningProgram(t *testing.T) {
	a, tt := newTestApp(t)
	p := a.tab().Focus
	waitFor(t, tt, "the shell", func() bool {
		return p.term != nil && p.sess != nil && p.sess.Info().Idle
	})
	tt.Type(testShell.run)
	tt.Key(0, ui.KeyEnter)
	waitFor(t, tt, "the program", func() bool { return !p.sess.Info().Idle })

	asked := make(chan string, 1)
	a.askQuit = func(summary string) bool { asked <- summary; return false }
	e := &mygo.QuitEvent{}
	a.beforeQuit(e)
	if !e.DefaultPrevented() {
		t.Fatal("the quit went ahead with a program running")
	}
	if a.exiting.Load() {
		t.Fatal("a prevented quit leaves the app running")
	}
	select {
	case summary := <-asked:
		if summary == "" {
			t.Fatal("the dialog says nothing of the program")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no dialog")
	}
	tt.Frame() // the answer comes back to the window
	if a.exiting.Load() || a.confirming {
		t.Fatal("Cancel should leave the app running")
	}
	if p.sess == nil || p.term == nil {
		t.Fatal("Cancel ended a session")
	}
}

// TestQuitConfirmedEndsTheApp checks that the dialog's Quit ends it.
func TestQuitConfirmedEndsTheApp(t *testing.T) {
	a, tt := newTestApp(t)
	p := a.tab().Focus
	waitFor(t, tt, "the shell", func() bool {
		return p.term != nil && p.sess != nil && p.sess.Info().Idle
	})
	tt.Type(testShell.run)
	tt.Key(0, ui.KeyEnter)
	waitFor(t, tt, "the program", func() bool { return !p.sess.Info().Idle })

	a.askQuit = func(string) bool { return true }
	e := &mygo.QuitEvent{}
	a.beforeQuit(e)
	if !e.DefaultPrevented() {
		t.Fatal("the quit went ahead before the answer")
	}
	waitFor(t, tt, "the confirmed quit", func() bool { return a.exiting.Load() })
}

// TestCloseEndsTheApp checks that closing the window is finishing work:
// nothing to ask about, and the app quits rather than hiding.
func TestCloseEndsTheApp(t *testing.T) {
	a, tt := newTestApp(t)
	waitFor(t, tt, "the shell", func() bool {
		p := a.tab().Focus
		return p.term != nil && p.sess != nil && p.sess.Info().Idle
	})
	e := &mygo.CloseEvent{}
	a.beforeClose(e)
	if !e.DefaultPrevented() {
		t.Fatal("the close should go through the app's own quit")
	}
	waitFor(t, tt, "the quit", func() bool { return a.exiting.Load() })
}

// TestCloseDuringQuitClosesTheWindow checks that the quit sequence lets
// the window close.
func TestCloseDuringQuitClosesTheWindow(t *testing.T) {
	a := &App{}
	a.exiting.Store(true)
	e := &mygo.CloseEvent{}
	a.beforeClose(e)
	if e.DefaultPrevented() {
		t.Fatal("the quit sequence was blocked")
	}
}
