package main

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"shellgrip/internal/terminal"
)

const defaultTerminalTheme = "ShellGrip (follow appearance)"

var terminalLight, terminalDark = lightTerm, darkTerm

func resolveTerminalTheme(name string) (light, dark *terminal.Theme, err error) {
	if name == "" {
		return lightTerm, darkTerm, nil
	}
	theme, err := terminal.GhosttyTheme(name)
	if err != nil {
		return lightTerm, darkTerm, err
	}
	return theme, theme, nil
}

func (a *App) applyTerminalTheme(name string) bool {
	light, dark, err := resolveTerminalTheme(name)
	if err != nil {
		a.err = err.Error()
		return false
	}
	terminalLight, terminalDark = light, dark
	for _, tab := range a.tabs {
		for _, pane := range tab.panes() {
			if pane.term != nil {
				pane.term.SetTheme(light, dark)
			}
		}
	}
	return true
}

func (a *App) setTerminalTheme(name string) {
	if a.applyTerminalTheme(name) {
		prefs.TerminalTheme = name
		saveSettings()
	}
}

func (a *App) activeTerminalThemeName() string {
	if a.themePreviewActive {
		return a.themePreview
	}
	return prefs.TerminalTheme
}

func terminalThemeFor(dark bool) *terminal.Theme {
	if dark {
		return terminalDark
	}
	return terminalLight
}

func (a *App) terminalThemeSettings(c *ui.Context, k *colors) {
	ui.Column(c).Gap(8).Children(func() {
		ui.Text(c, "Terminal theme").FontSize(13).FontWeight(600).TextColor(k.text)
		name := prefs.TerminalTheme
		if name == "" {
			name = defaultTerminalTheme
		}
		button := ui.Button(c, name).Label("Choose terminal theme")
		opening := false
		if button.Clicked() {
			a.themePickerOpen = !a.themePickerOpen
			a.themeQuery = ""
			opening = a.themePickerOpen
		}
		ui.PopoverBase(c, button, &a.themePickerOpen, func(panel ui.Element) {
			panel.Width(360).Padding(8).Gap(6).Radius(8).Background(k.panel).Border(1, k.panelBorder).
				Shadow(0, 8, 20, 0, k.shadowFocused)
			input := ui.TextInput(c, &a.themeQuery).Placeholder("Search themes…").Label("Search terminal themes").AutoFocus()
			query := strings.ToLower(strings.TrimSpace(a.themeQuery))
			var matches []string
			for _, theme := range a.themeNames {
				if strings.Contains(strings.ToLower(theme), query) {
					matches = append(matches, theme)
				}
			}
			choose := func(theme string) {
				if theme == defaultTerminalTheme {
					theme = ""
				}
				a.setTerminalTheme(theme)
				a.themePickerOpen = false
			}
			if input.Submitted() && len(matches) > 0 {
				choose(matches[0])
			}
			listHeight := float32(180)
			if viewport := c.Root().Bounds().H; viewport > 0 {
				anchor := button.Bounds()
				room := max(anchor.Y-16, viewport-anchor.Y-anchor.H-16)
				listHeight = min(listHeight, max(30, room-64))
			}
			ui.Scroll(c).Height(listHeight).Children(func() {
				if len(matches) == 0 {
					ui.Text(c, "No themes found").FontSize(13).TextColor(k.textFaint).Padding(8)
				}
				for _, theme := range matches {
					row := ui.Button(c.Key(theme), theme).Label("Use theme: " + theme).Height(30).Shrink(0)
					row.Selected(theme == name)
					if theme == name {
						row.Background(k.panelSel).TextColor(k.text).Border(1, k.textFaint)
					}
					if opening && theme == name {
						row.ScrollIntoView()
					}
					if row.Clicked() {
						choose(theme)
					}
				}
			})
		})
		theme := terminalThemeFor(c.Theme().Dark)
		ui.Column(c).Padding(12).Gap(6).Radius(8).Background(theme.Background).Children(func() {
			ui.Text(c, "user@shellgrip ~ $ echo hello").Font(termFont.Family).FontSize(12).TextColor(theme.Foreground).SingleLine()
			ui.Text(c, "hello · ready").Font(termFont.Family).FontSize(12).TextColor(theme.Palette[2]).SingleLine()
			ui.Row(c).Gap(4).Children(func() {
				for _, color := range theme.Palette {
					ui.Box(c).Height(10).Grow(1).Radius(2).Background(color)
				}
			})
		})
		ui.Text(c, "Applies to all panes. App appearance stays independent.").FontSize(11.5).TextColor(k.textFaint)
	})
}

func (a *App) openThemes() {
	a.quickThemeOpen = true
	a.quickThemeQuery = ""
	a.themePickerOpen = false
	a.focusReq = nil
	if len(a.themeNames) == 0 {
		a.themeNames = append([]string{defaultTerminalTheme}, terminal.GhosttyThemes()...)
	}
}

// quickThemeButton previews without changing preferences, so dismissal can
// restore the saved theme and unrelated settings saves cannot keep a preview.
func (a *App) quickThemeButton(c *ui.Context, k *colors) {
	button := iconButton(c, k, "palette", "Themes", 30, 17).Tooltip("Themes")
	opening := false
	if button.Clicked() {
		if !a.quickThemeOpen {
			a.openThemes()
			opening = true
		} else {
			a.quickThemeOpen = false
		}
	}
	ui.PopoverBase(c, button, &a.quickThemeOpen, func(panel ui.Element) {
		panel.Width(360).Padding(10).Gap(8).Radius(12).Background(k.panel).Border(1, k.panelBorder).
			Shadow(0, 8, 20, 0, k.shadowFocused)
		ui.Text(c, "Themes").FontSize(16).FontWeight(700).TextColor(k.text)
		ui.Text(c, "Hover to preview · click to keep").FontSize(11.5).TextColor(k.textFaint)
		input := ui.TextInput(c, &a.quickThemeQuery).Placeholder("Search themes…").Label("Search themes").AutoFocus()
		query := strings.ToLower(strings.TrimSpace(a.quickThemeQuery))
		var matches []string
		for _, name := range a.themeNames {
			if strings.Contains(strings.ToLower(name), query) {
				matches = append(matches, name)
			}
		}
		choose := func(name string) {
			if name == defaultTerminalTheme {
				name = ""
			}
			a.setTerminalTheme(name)
			a.quickThemeOpen = false
		}
		if input.Submitted() && len(matches) > 0 {
			choose(matches[0])
		}
		height := min(float32(320), max(float32(60), c.Root().Bounds().H-180))
		ui.Scroll(c).Height(height).Children(func() {
			if len(matches) == 0 {
				ui.Text(c, "No themes found").FontSize(13).TextColor(k.textFaint).Padding(8)
			}
			for _, name := range matches {
				value := name
				if value == defaultTerminalTheme {
					value = ""
				}
				row := ui.Button(c.Key(name), name).Label("Preview theme: " + name).Height(30).Shrink(0).Selected(value == prefs.TerminalTheme)
				if value == prefs.TerminalTheme {
					row.Background(k.panelSel).TextColor(k.text).Border(1, k.textFaint)
					if opening {
						row.ScrollIntoView()
					}
				}
				if row.Hovered() && (!a.themePreviewActive || a.themePreview != value) {
					if a.applyTerminalTheme(value) {
						a.themePreview, a.themePreviewActive = value, true
						c.Invalidate()
					}
				}
				if row.Clicked() {
					choose(name)
				}
			}
		})
	})
	if !a.quickThemeOpen && a.themePreviewActive {
		a.applyTerminalTheme(prefs.TerminalTheme)
		a.themePreviewActive = false
		c.Invalidate()
		if tab := a.tab(); tab != nil {
			a.focusReq = tab.Focus
		}
	}
}
