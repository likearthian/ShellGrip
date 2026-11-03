package main

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"gorex/internal/rex"
)

func TestBeforeQuitKeepsSessionsAndSavesLayout(t *testing.T) {
	a, _ := newTestApp(t)
	want, err := json.Marshal(a.snapshot())
	if err != nil {
		t.Fatal(err)
	}
	e := &mygo.QuitEvent{}
	a.beforeQuit(e)
	if e.DefaultPrevented() || !a.exiting.Load() {
		t.Fatal("explicit quit should proceed without a dialog")
	}
	got, err := a.client.Layout()
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("saved layout = %s, %v; want %s", got, err, want)
	}
	infos, err := a.client.List()
	if err != nil || len(infos) != 1 || infos[0].Exited {
		t.Fatalf("sessions should keep running: %v, %v", infos, err)
	}
}

func TestBeforeCloseAllowsExitAndTrayFallback(t *testing.T) {
	for _, exiting := range []bool{false, true} {
		a := &App{}
		a.exiting.Store(exiting)
		if exiting {
			a.tray = &mygo.Tray{}
			a.win = &mygo.Window{}
		}
		e := &mygo.CloseEvent{}
		a.beforeClose(e)
		if e.DefaultPrevented() {
			t.Fatalf("close prevented with exiting=%t", exiting)
		}
	}
}

func TestTrayEndAllStopsServer(t *testing.T) {
	a, _ := newTestApp(t)
	items := a.trayMenu().Items()
	items[len(items)-1].Click(nil, nil)
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if _, err := os.Stat(rex.SocketPath()); os.IsNotExist(err) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("tray quit did not stop the session server")
		}
	}
	if !a.exiting.Load() || !a.quitting {
		t.Fatal("tray quit should allow the app and window to exit")
	}
}
