package main

import (
	"fmt"
	"math"
	"runtime"

	"github.com/egoist/mygo/ui"
	"gorex/internal/terminal"
)

// zero values preserve the terminal's defaults in older settings files.
func normalizeTerminalSettings() {
	if math.IsNaN(float64(prefs.MinimumContrast)) || prefs.MinimumContrast < 1 || prefs.MinimumContrast > 21 {
		prefs.MinimumContrast = 1
	}
	if math.IsNaN(float64(prefs.ScrollSpeed)) || prefs.ScrollSpeed < 0.25 || prefs.ScrollSpeed > 4 {
		prefs.ScrollSpeed = 1
	}
	if prefs.ScrollbackMiB < 0 || prefs.ScrollbackMiB > 100 {
		prefs.ScrollbackMiB = 10
	}
}

func terminalBehaviorOptions(o terminal.Options) terminal.Options {
	speed := prefs.ScrollSpeed
	if speed == 0 {
		speed = 1
	}
	o.MinimumContrast = prefs.MinimumContrast
	o.ScrollMultiplier = terminal.ScrollMultiplier{Precision: speed, Discrete: 3 * speed}
	o.Scrollback = prefs.ScrollbackMiB << 20
	o.OptionAsAlt = prefs.OptionAsAlt
	return o
}

func newConfiguredTerminal(o terminal.Options) (*terminal.Terminal, error) {
	return terminal.New(terminalBehaviorOptions(o))
}

func (a *App) applyTerminalBehavior() {
	o := terminalBehaviorOptions(terminal.Options{})
	for _, tab := range a.tabs {
		for _, pane := range tab.panes() {
			if pane.term != nil {
				pane.term.SetMinimumContrast(o.MinimumContrast)
				pane.term.SetScrollMultiplier(o.ScrollMultiplier)
			}
		}
	}
	saveSettings()
}

func (a *App) terminalBehaviorSettings(c *ui.Context, k *colors) {
	ui.Column(c).Gap(8).Children(func() {
		ui.Text(c, "Terminal behavior").FontSize(13).FontWeight(600).TextColor(k.text)
		contrast := prefs.MinimumContrast
		if contrast == 0 {
			contrast = 1
		}
		label := fmt.Sprintf("%g:1", contrast)
		if ui.Select(c, &label, []string{"1:1", "3:1", "4.5:1", "7:1", "21:1"}).Label("Minimum text contrast").Changed() {
			var ratio float32
			if _, err := fmt.Sscanf(label, "%f:1", &ratio); err == nil {
				prefs.MinimumContrast = ratio
				a.applyTerminalBehavior()
			}
		}
		ui.Text(c, "Minimum text contrast · 1:1 preserves program colors. Changes apply to open panes.").FontSize(11.5).TextColor(k.textFaint)
		speed := prefs.ScrollSpeed
		if speed == 0 {
			speed = 1
		}
		label = fmt.Sprintf("%gx", speed)
		if ui.Select(c, &label, []string{"0.25x", "0.5x", "1x", "2x", "4x"}).Label("Scroll speed").Changed() {
			var value float32
			if _, err := fmt.Sscanf(label, "%fx", &value); err == nil {
				prefs.ScrollSpeed = value
				a.applyTerminalBehavior()
			}
		}
		ui.Text(c, "Scroll speed · scales mouse-wheel and touchpad scrolling in open panes.").FontSize(11.5).TextColor(k.textFaint)
		capacity := prefs.ScrollbackMiB
		if capacity == 0 {
			capacity = 10
		}
		label = fmt.Sprintf("%d MiB", capacity)
		if ui.Select(c, &label, []string{"10 MiB", "25 MiB", "50 MiB", "100 MiB"}).Label("Scrollback capacity").Changed() {
			var value int
			if _, err := fmt.Sscanf(label, "%d MiB", &value); err == nil {
				prefs.ScrollbackMiB = value
				saveSettings()
			}
		}
		ui.Text(c, "Scrollback capacity · approximate output history per pane. Applies to new panes.").FontSize(11.5).TextColor(k.textFaint)
		if runtime.GOOS == "darwin" {
			if ui.Checkbox(c, &prefs.OptionAsAlt, "Use Option as Alt").Changed() {
				saveSettings()
			}
			ui.Text(c, "Use Option for shell Meta shortcuts. Applies to new panes.").FontSize(11.5).TextColor(k.textFaint)
		}
	})
}
