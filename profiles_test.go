package main

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestProfileLaunch(t *testing.T) {
	profile := terminalProfile{ID: "wsl:Ubuntu", Name: "Ubuntu (WSL)", Command: []string{"wsl.exe", "--distribution", "Ubuntu"}, WSL: true}
	for _, test := range []struct{ dir, want string }{
		{`C:\Users\Test User`, "~"},
		{`C:\Users\Test User\project`, "~"},
		{"C:/Users/Test User", "~"},
		{`\\server\share`, "~"},
		{"//server/share", "~"},
		{"/home/user/project with spaces", "/home/user/project with spaces"},
		{"/mnt/c/project", "/mnt/c/project"},
		{"/", "/"},
		{"~", "~"},
		{"", "~"},
	} {
		command, hostDir := profile.launch(test.dir)
		want := []string{"wsl.exe", "--distribution", "Ubuntu", "--cd", test.want}
		if !reflect.DeepEqual(command, want) {
			t.Fatalf("launch(%q) = %q, want %q", test.dir, command, want)
		}
		if got := profile.startingDir(test.dir); got != test.want {
			t.Fatalf("saved launch directory = %q, want %q", got, test.want)
		}
		home, _ := os.UserHomeDir()
		if hostDir != home {
			t.Fatalf("Windows process directory = %q, want %q", hostDir, home)
		}
	}
	if len(profile.Command) != 3 {
		t.Fatal("launch changed the saved profile command")
	}
	native := terminalProfile{Command: []string{"cmd.exe"}}
	if got := native.startingDir(`C:\Users\Test User`); got != `C:\Users\Test User` {
		t.Fatalf("native shell directory changed to %q", got)
	}
}

func TestTerminalProfilePicker(t *testing.T) {
	a, tt := newTestApp(t)
	previousPrefs := prefs
	t.Cleanup(func() { prefs = previousPrefs })
	profile := terminalProfile{ID: "test-shell", Name: "Test Shell", Command: []string{testShell.path}}
	a.profiles = []terminalProfile{{ID: "auto", Name: "Default shell"}, profile}
	tt.Frame()
	if err := tt.Click("New Tab Menu"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Set default: Test Shell"); err != nil {
		t.Fatal(err)
	}
	if a.defaultProfile().ID != profile.ID {
		t.Fatal("the picker did not change the default")
	}
	data, err := os.ReadFile(settingsPath())
	if err != nil {
		t.Fatal(err)
	}
	var saved settings
	if json.Unmarshal(data, &saved) != nil || saved.DefaultProfile != profile.ID {
		t.Fatalf("default was not saved: %s", data)
	}
	if err := tt.Click("Open Test Shell"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(a.tabs) != 2 || a.tab().Focus.profile.ID != profile.ID || a.profilesOpen {
		t.Fatal("selecting a profile did not open a tab and close the picker")
	}
	if err := tt.Click("New Tab"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(a.tabs) != 3 || a.tab().Focus.profile.ID != profile.ID {
		t.Fatal("+ did not open the selected default")
	}
	a.split(false)
	if a.tab().Focus.profile.ID != profile.ID {
		t.Fatal("split lost its profile")
	}
	a.restartFocused()
	if a.tab().Focus.profile.ID != profile.ID {
		t.Fatal("restart lost its profile")
	}
	a.saveNow()
	b := &App{}
	t.Cleanup(func() { closeTestSessions(b) })
	if !b.restore() || b.tab().Focus.profile.ID != profile.ID || !b.tab().Focus.restored {
		t.Fatal("restored pane lost its profile")
	}
	for _, app := range []*App{a, b} {
		for _, tab := range app.tabs {
			for _, pane := range tab.panes() {
				pane.markClosed()
				if pane.term != nil {
					pane.term.Close()
				}
			}
		}
	}
}
