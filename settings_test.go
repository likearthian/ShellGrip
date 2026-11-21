package main

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestPaneFocusCuesSetting(t *testing.T) {
	a, tt := newTestApp(t)
	previous := prefs
	t.Cleanup(func() { prefs = previous })
	prefs.PaneFocusCues = true
	tt.Frame()
	if _, ok := tt.Find("Active"); !ok {
		t.Fatal("active pane cue is missing")
	}
	a.openSettings()
	tt.Frame()
	if err := tt.Click("Highlight active pane"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if prefs.PaneFocusCues {
		t.Fatal("pane focus cues did not turn off")
	}
	data, err := os.ReadFile(settingsPath())
	if err != nil {
		t.Fatal(err)
	}
	// Seed the enabled default, as loadSettings does, to ensure a saved
	// false survives the next launch rather than reverting to the default.
	saved := settings{PaneFocusCues: true}
	if err := json.Unmarshal(data, &saved); err != nil || saved.PaneFocusCues {
		t.Fatalf("disabled pane focus cues were not persisted: %s, %v", data, err)
	}
	if _, ok := tt.Find("Active"); ok {
		t.Fatal("active label remains visible with cues disabled")
	}
	if err := tt.Click("Highlight active pane"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !prefs.PaneFocusCues {
		t.Fatal("pane focus cues did not turn on")
	}
	a.selectTab(0)
	tt.Frame()
	if _, ok := tt.Find("Active"); !ok {
		t.Fatal("active label did not return")
	}
}

func TestSettingsTab(t *testing.T) {
	a, tt := newTestApp(t)
	previousPrefs, previousFont := prefs, termFont
	previousLight, previousDark := terminalLight, terminalDark
	t.Cleanup(func() {
		prefs, termFont = previousPrefs, previousFont
		terminalLight, terminalDark = previousLight, previousDark
	})
	a.setFontSize(defaultFontSize)
	a.setFontFamily(defaultFontFamily)
	a.fontFamilies = []string{defaultFontFamily, "monospace"}
	a.profiles = []terminalProfile{{ID: "auto", Name: "Default shell"}, {ID: "test-shell", Name: "Test Shell", Command: []string{testShell.path}}}
	click := func(label string) {
		t.Helper()
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
	}
	click("Settings")
	if !a.tab().Settings {
		t.Fatal("settings did not open")
	}
	// Theme changes require MyGo's native event loop; the headless tester
	// verifies the controls while font and profile changes run end to end.
	for _, label := range []string{"System", "Light", "Dark"} {
		if _, ok := tt.Find(label); !ok {
			t.Fatalf("missing appearance choice %q", label)
		}
	}
	click("Choose terminal theme")
	tt.Type("Dracula")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if prefs.TerminalTheme != "Dracula" || terminalLight != terminalDark || a.themePickerOpen {
		t.Fatal("terminal theme was not applied independently of appearance")
	}
	click("Settings category: Terminal")
	click("Choose terminal font")
	click("monospace")
	if a.settingsFont != "monospace" || prefs.FontFamily != defaultFontFamily {
		t.Fatal("font preview changed the applied font")
	}
	click("Apply font")
	if prefs.FontFamily != "monospace" || termFont.Family != terminalFontFamily("monospace") {
		t.Fatal("font family did not apply")
	}

	click("Increase font size")
	if prefs.FontSize != defaultFontSize+1 || termFont.Size != prefs.FontSize {
		t.Fatal("font size did not apply")
	}
	click("Font size")
	tt.Key(ui.Ctrl, ui.KeyA)
	tt.Type("14.25")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if prefs.FontSize != 14.25 || termFont.Size != 14.25 {
		t.Fatal("typed decimal font size did not apply")
	}
	tt.Key(ui.Ctrl, ui.KeyA)
	tt.Type("NaN")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if prefs.FontSize != 14.25 || a.settingsFontSize != "14.25" {
		t.Fatal("invalid font size changed the applied value")
	}
	click("Reset size")
	if prefs.FontSize != defaultFontSize {
		t.Fatal("font size did not reset")
	}
	tt.Scroll(500, 400, 0, 250)
	tt.Frame()
	click("Choose default terminal")
	click("Test Shell")
	if prefs.DefaultProfile != "test-shell" || a.defaultProfile().ID != "test-shell" {
		t.Fatal("default terminal did not change")
	}
	click("Settings category: Clipboard")
	a.setCopyOnSelect(true)
	tt.Frame()
	click("Copy on select")
	if prefs.CopyOnSelect {
		t.Fatal("copy on select setting did not turn off")
	}
	data, err := os.ReadFile(settingsPath())
	if err != nil {
		t.Fatal(err)
	}
	var saved settings
	if err := json.Unmarshal(data, &saved); err != nil || !reflect.DeepEqual(saved, prefs) {
		t.Fatalf("settings were not persisted: %s, %v", data, err)
	}
	if err := tt.Click("Close Settings"); err != nil {
		t.Fatal(err)
	}
	if a.tab().Settings {
		t.Fatal("close button did not close settings")
	}
	cmdSettings.Run(a)
	tt.Frame()
	if a.settingsFont != "monospace" {
		t.Fatal("reopening settings lost the font choice")
	}

	click("Settings category: Terminal")
	click("Reset font")
	if prefs.FontFamily != defaultFontFamily || termFont.Family != terminalFontFamily(defaultFontFamily) {
		t.Fatal("font family did not reset")
	}
	a.closeActiveTab()
	tt.Frame()
	if a.tab().Settings {
		t.Fatal("Close Tab did not close settings")
	}
}

func TestSettingsTabNavigationAndLayout(t *testing.T) {
	t.Setenv("SHELLGRIP_DIR", t.TempDir())
	a := numberedTabs(2)
	a.fontFamilies = []string{defaultFontFamily}
	a.openSettings()
	settingsTab := a.tab()
	if !settingsTab.Settings || len(a.tabs) != 3 || len(settingsTab.panes()) != 0 {
		t.Fatal("settings should open as a tab without a terminal session")
	}
	a.selectTab(0)
	a.openSettings()
	if a.tab() != settingsTab || len(a.tabs) != 3 {
		t.Fatal("opening settings created a duplicate")
	}
	a.startRename()
	a.split(false)
	a.equalize()
	if a.renaming != nil || settingsTab.Root != nil {
		t.Fatal("terminal actions changed the Settings tab")
	}
	for _, active := range []int{0, 1, 2} {
		a.selectTab(active)
		layout := a.snapshot()
		if len(layout.Tabs) != 2 || layout.Active < 0 || layout.Active >= 2 {
			t.Fatalf("settings leaked into the saved layout: %+v", layout)
		}
		if active == 2 && layout.Active != 1 {
			t.Fatal("saved active terminal index shifted")
		}
	}
	a.openSettings()
	tt := ui.NewTester(a.view, 560, 340)
	if err := tt.Click("Settings category: Clipboard"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if bounds, ok := tt.Find("Copy on select"); !ok || bounds.W <= 0 || bounds.H <= 0 || bounds.Y+bounds.H > 340 {
		t.Fatalf("clipboard controls inaccessible at minimum window size: %+v", bounds)
	}
	tt.Key(ui.Ctrl, ui.KeyTab)
	tt.Frame()
	if a.tab().Settings {
		t.Fatal("Ctrl+Tab did not leave Settings")
	}
	a.openSettings()
	a.closeFocused()
	if len(a.tabs) != 2 || a.tab().Settings || a.focusReq != a.tab().Focus {
		t.Fatal("closing Settings did not return focus to a terminal")
	}
}

func TestChromeVibrancySetting(t *testing.T) {
	t.Setenv("SHELLGRIP_DIR", t.TempDir())
	prev := prefs
	t.Cleanup(func() { prefs = prev })
	prefs = settings{CopyOnSelect: true, PaneFocusCues: true}

	a := numberedTabs(1)
	a.fontFamilies = []string{defaultFontFamily}
	a.openSettings()
	tt := ui.NewTester(a.view, 900, 600)
	if err := tt.Click("Translucent window chrome"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !prefs.ChromeVibrancy {
		t.Fatal("clicking Translucent window chrome did not enable ChromeVibrancy")
	}
	raw, err := os.ReadFile(settingsPath())
	if err != nil {
		t.Fatal(err)
	}
	var saved settings
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	if !saved.ChromeVibrancy {
		t.Fatal("ChromeVibrancy was not persisted to settings.json")
	}
}
