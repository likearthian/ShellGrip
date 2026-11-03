package main

import (
	"bytes"
	"github.com/egoist/mygo/ui"
	"os"
	"testing"

	"gorex/internal/terminal"
)

func TestTerminalThemes(t *testing.T) {
	light, dark, err := resolveTerminalTheme("")
	if err != nil || light != lightTerm || dark != darkTerm {
		t.Fatal("default theme no longer follows appearance")
	}
	for _, name := range []string{"Dracula", "Catppuccin Mocha", "Catppuccin Latte", "Nord"} {
		light, dark, err = resolveTerminalTheme(name)
		want, wantErr := terminal.GhosttyTheme(name)
		if err != nil || wantErr != nil || light != dark || *light != *want {
			t.Fatalf("theme %q did not load: %v, %v", name, err, wantErr)
		}
	}
	light, dark, err = resolveTerminalTheme("GoRex nonexistent theme")
	if err == nil || light != lightTerm || dark != darkTerm {
		t.Fatal("unavailable theme did not fall back safely")
	}
}

func TestThemeChangeKeepsSession(t *testing.T) {
	a, tt := newTestApp(t)
	previousPrefs := prefs
	previousLight, previousDark := terminalLight, terminalDark
	t.Cleanup(func() {
		prefs = previousPrefs
		terminalLight, terminalDark = previousLight, previousDark
	})
	pane := a.tab().Focus
	sid := pane.SID
	a.setTerminalTheme("Catppuccin Mocha")
	tt.Frame()
	if pane.SID != sid || pane.term == nil || pane.closed || prefs.TerminalTheme != "Catppuccin Mocha" {
		t.Fatal("changing the theme disturbed the running session")
	}
	a.setTerminalTheme("GoRex nonexistent theme")
	if prefs.TerminalTheme != "Catppuccin Mocha" {
		t.Fatal("invalid theme replaced the saved choice")
	}
	a.setTerminalTheme("")
	tt.Frame()
	if terminalLight != lightTerm || terminalDark != darkTerm || prefs.TerminalTheme != "" {
		t.Fatal("reset did not restore appearance-aware themes")
	}
}

func TestQuickThemePreviewAndCommit(t *testing.T) {
	a, tt := newTestApp(t)
	previousPrefs := prefs
	previousLight, previousDark := terminalLight, terminalDark
	t.Cleanup(func() { prefs = previousPrefs; terminalLight, terminalDark = previousLight, previousDark })
	a.setTerminalTheme("Nord")
	saved, err := os.ReadFile(settingsPath())
	if err != nil {
		t.Fatal(err)
	}
	pane := a.tab().Focus
	click := func(label string) {
		t.Helper()
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
	}
	preview := func(name string) {
		t.Helper()
		click("Search themes")
		tt.Key(ui.Ctrl, ui.KeyA)
		tt.Type(name)
		tt.Frame()
		rect, ok := tt.Find("Preview theme: " + name)
		if !ok || rect.W <= 0 || rect.H <= 0 {
			t.Fatalf("theme row missing: %q", name)
		}
		tt.Move(rect.X+rect.W/2, rect.Y+rect.H/2)
		tt.Frame()
		want, _ := terminal.GhosttyTheme(name)
		if *terminalLight != *want || terminalLight != terminalDark {
			t.Fatalf("hover did not preview %q", name)
		}
	}
	click("Themes")
	if !a.quickThemeOpen || len(a.themeNames) != len(terminal.GhosttyThemes())+1 {
		t.Fatal("quick picker did not list all themes")
	}
	preview("Dracula")
	current, _ := os.ReadFile(settingsPath())
	if prefs.TerminalTheme != "Nord" || !bytes.Equal(saved, current) {
		t.Fatal("hover persisted a theme")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	nord, _ := terminal.GhosttyTheme("Nord")
	if a.quickThemeOpen || *terminalLight != *nord {
		t.Fatal("dismissal did not restore the saved theme")
	}
	click("Themes")
	preview("Dracula")
	click("Preview theme: Dracula")
	if a.quickThemeOpen || prefs.TerminalTheme != "Dracula" || a.themePreviewActive {
		t.Fatal("click did not keep the theme and close the picker")
	}
	current, _ = os.ReadFile(settingsPath())
	if !bytes.Contains(current, []byte("Dracula")) {
		t.Fatal("clicked theme was not saved")
	}
	if pane.closed || pane != a.tab().Focus || pane.term == nil {
		t.Fatal("theme preview disturbed the terminal session")
	}
	click("Themes")
	preview("Nord")
	tt.ClickAt(400, 580)
	tt.Frame()
	dracula, _ := terminal.GhosttyTheme("Dracula")
	if a.quickThemeOpen || *terminalLight != *dracula {
		t.Fatal("outside click did not cancel preview")
	}
}
