package main

import (
	"encoding/json"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestShortcutParsing(t *testing.T) {
	for _, value := range []string{"Ctrl+Shift+T", "Ctrl+Tab", "Ctrl+Shift+Tab", "Alt+Left", "Ctrl+=", "Ctrl+-", "Ctrl+[", "Shift+PageDown", "F6", "Ctrl+F12", ""} {
		binding, err := parseShortcut(value)
		if err != nil || binding.String() != value {
			t.Fatalf("shortcut %q did not round trip: %q, %v", value, binding.String(), err)
		}
	}
	for _, value := range []string{"Ctrl+", "Ctrl+Ctrl+T", "T", "Enter", "Shift+T", "Shift+1", "Shift+Space", "Shift+NoSuchKey", "F13", "Ctrl+Shift", "Unknown+T"} {
		if _, err := parseShortcut(value); err == nil {
			t.Fatalf("invalid shortcut %q was accepted", value)
		}
	}
	if binding, err := parseShortcut(" control + shift + t "); err != nil || binding != ctrlShift(ui.KeyT) {
		t.Fatalf("shortcut normalization failed: %+v, %v", binding, err)
	}
}

func TestShortcutConflictsAndReset(t *testing.T) {
	t.Setenv("SHELLGRIP_DIR", t.TempDir())
	previous := prefs
	prefs.Shortcuts = map[string]string{}
	t.Cleanup(func() { prefs = previous })
	a := numberedTabs(3)
	if err := a.saveShortcut(&cmdSettings, cmdNewTab.binding().String(), false); err == nil {
		t.Fatal("duplicate shortcut accepted")
	}
	if err := a.saveShortcut(&cmdSettings, tabNumberCommands[0].binding().String(), false); err == nil {
		t.Fatal("conflict with tab navigation accepted")
	}
	copyKeys := "Ctrl+Shift+C"
	if runtime.GOOS == "darwin" {
		copyKeys = "Cmd+C"
	}
	if err := a.saveShortcut(&cmdSettings, copyKeys, false); err == nil {
		t.Fatal("terminal copy shortcut was intercepted")
	}
	if err := a.saveShortcut(&cmdNewTab, "F6", false); err != nil {
		t.Fatal(err)
	}
	if err := a.saveShortcut(&cmdSettings, cmdNewTab.defaultBinding().String(), false); err != nil {
		t.Fatal(err)
	}
	if err := a.saveShortcut(&cmdNewTab, "", true); err == nil {
		t.Fatal("reset silently created a duplicate shortcut")
	}
	if cmdNewTab.binding().Key != ui.KeyF6 {
		t.Fatal("failed reset changed the shortcut")
	}
	if err := a.saveShortcut(&cmdSettings, "", false); err != nil {
		t.Fatal(err)
	}
	if err := a.saveShortcut(&cmdNewTab, "", true); err != nil {
		t.Fatal(err)
	}
	if cmdNewTab.binding() != cmdNewTab.defaultBinding() {
		t.Fatal("reset did not restore the default")
	}
	if _, custom := prefs.Shortcuts[cmdNewTab.shortcutID()]; custom {
		t.Fatal("reset kept an override")
	}
	prefs.Shortcuts[cmdThemes.shortcutID()] = "Not+A+Shortcut"
	if cmdThemes.binding() != cmdThemes.defaultBinding() {
		t.Fatal("malformed stored shortcut did not fall back")
	}
}

func TestKeyboardShortcutSettings(t *testing.T) {
	a, tt := newTestApp(t)
	previous := prefs
	prefs.Shortcuts = map[string]string{}
	t.Cleanup(func() { prefs = previous })
	a.openSettings()
	tt.Frame()
	click := func(label string) {
		t.Helper()
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
	}
	click("Settings category: Keyboard Shortcuts")
	click("Search keyboard shortcuts")
	tt.Type("Themes")
	tt.Frame()
	click("Edit shortcut: Themes")
	click("Shortcut keys")
	tt.Key(ui.Ctrl, ui.KeyA)
	tt.Type("Ctrl+Shift+T")
	click("Save shortcut")
	if a.shortcutError == "" || !strings.Contains(a.shortcutError, "New Tab") {
		t.Fatal("duplicate binding error was not displayed")
	}
	click("Shortcut keys")
	tt.Key(ui.Ctrl, ui.KeyA)
	tt.Type("F6")
	click("Save shortcut")
	if a.shortcutEditing != nil || cmdThemes.binding().Key != ui.KeyF6 || tip("Themes", &cmdThemes) != "Themes  F6" {
		t.Fatal("saving a shortcut did not update its binding and tooltip")
	}
	data, err := os.ReadFile(settingsPath())
	if err != nil {
		t.Fatal(err)
	}
	prefs.Shortcuts = nil
	var saved settings
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	prefs.Shortcuts = saved.Shortcuts
	if cmdThemes.binding().Key != ui.KeyF6 {
		t.Fatal("shortcut did not survive loading saved preferences")
	}
	a.selectTab(0)
	tt.Frame()
	tt.Key(0, ui.KeyF6)
	tt.Frame()
	if !a.quickThemeOpen || a.tab().Settings {
		t.Fatal("custom shortcut did not open Themes from a terminal tab")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	a.openSettings()
	tt.Frame()
	click("Edit shortcut: Themes")
	click("Disable")
	if cmdThemes.binding() != (shortcut{}) {
		t.Fatal("disable did not clear the binding")
	}
	a.selectTab(0)
	tt.Frame()
	tt.Key(0, ui.KeyF6)
	tt.Frame()
	if a.quickThemeOpen {
		t.Fatal("disabled shortcut still ran")
	}
	a.openSettings()
	tt.Frame()
	click("Reset shortcut: Themes")
	if _, ok := prefs.Shortcuts[cmdThemes.shortcutID()]; ok {
		t.Fatal("reset kept the custom shortcut")
	}
	if err := a.saveShortcut(&cmdNextTab, "F7", false); err != nil {
		t.Fatal(err)
	}
	click("Reset all shortcuts")
	if len(prefs.Shortcuts) != 0 {
		t.Fatal("reset all did not clear platform overrides")
	}
	tt.SetSize(560, 340)
	tt.Scroll(400, 200, 0, 800)
	tt.Frame()
	if bounds, ok := tt.Find("Edit shortcut: Themes"); !ok || bounds.W <= 0 || bounds.H <= 0 || bounds.X+bounds.W > 560 || bounds.Y+bounds.H > 340 {
		t.Fatalf("shortcut controls inaccessible at minimum window size: %+v", bounds)
	}
}

func TestReboundTabShortcutDispatch(t *testing.T) {
	t.Setenv("SHELLGRIP_DIR", t.TempDir())
	previous := prefs
	prefs.Shortcuts = map[string]string{}
	t.Cleanup(func() { prefs = previous })
	a := numberedTabs(3)
	if err := a.saveShortcut(tabNumberCommands[1], "F8", false); err != nil {
		t.Fatal(err)
	}
	tt := ui.NewTester(a.view, 1000, 620)
	tt.Key(0, ui.KeyF8)
	tt.Frame()
	if a.active != 1 {
		t.Fatal("rebound tab shortcut did not select its tab")
	}
	a.selectTab(0)
	tt.Frame()
	old := tabNumberCommands[1].defaultBinding()
	tt.Key(old.Mods, old.Key)
	tt.Frame()
	if a.active != 0 {
		t.Fatal("old shortcut remained active after rebinding")
	}
}
