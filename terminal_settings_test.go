package main

import (
	"encoding/json"
	"os"
	"testing"

	"gorex/internal/terminal"
)

func TestTerminalBehaviorSettings(t *testing.T) {
	previous := prefs
	t.Cleanup(func() { prefs = previous })
	t.Setenv("GOREX_DIR", t.TempDir())
	prefs = settings{}
	normalizeTerminalSettings()
	o := terminalBehaviorOptions(terminal.Options{})
	if o.MinimumContrast != 1 || o.ScrollMultiplier.Precision != 1 || o.ScrollMultiplier.Discrete != 3 || o.Scrollback != 0 {
		t.Fatalf("default options: %+v", o)
	}
	prefs.MinimumContrast, prefs.ScrollSpeed, prefs.ScrollbackMiB, prefs.OptionAsAlt = 4.5, 2, 50, true
	saveSettings()
	prefs = settings{}
	raw, err := os.ReadFile(settingsPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &prefs); err != nil {
		t.Fatal(err)
	}
	normalizeTerminalSettings()
	o = terminalBehaviorOptions(terminal.Options{})
	if o.MinimumContrast != 4.5 || o.ScrollMultiplier.Precision != 2 || o.ScrollMultiplier.Discrete != 6 || o.Scrollback != 50<<20 || !o.OptionAsAlt {
		t.Fatalf("settings did not round trip: %s", raw)
	}
	prefs.MinimumContrast, prefs.ScrollSpeed, prefs.ScrollbackMiB = 30, -2, 1000
	normalizeTerminalSettings()
	if prefs.MinimumContrast != 1 || prefs.ScrollSpeed != 1 || prefs.ScrollbackMiB != 10 {
		t.Fatalf("invalid preferences not reset: %+v", prefs)
	}
}
