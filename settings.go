package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"gorex/internal/terminal"

	"gorex/internal/rex"
)

// settings are the user's choices, kept in settings.json beside the
// server's state.
type settings struct {
	FileTree       bool              `json:"fileTree,omitempty"`
	FileTreeWidth  float32           `json:"fileTreeWidth,omitempty"`
	Shortcuts      map[string]string `json:"shortcuts,omitempty"`
	DefaultProfile string            `json:"defaultProfile,omitempty"`
	FontFamily     string            `json:"fontFamily,omitempty"`
	TerminalTheme  string            `json:"terminalTheme,omitempty"`
	CopyOnSelect   bool              `json:"copyOnSelect"`
	PaneFocusCues  bool              `json:"paneFocusCues"`
	// Appearance is "light", "dark", or "" to follow the system.
	Appearance string  `json:"appearance,omitempty"`
	FontSize   float32 `json:"fontSize,omitempty"`
}

const defaultFontSize = 11.6

var prefs = settings{CopyOnSelect: true, PaneFocusCues: true}

func settingsPath() string { return filepath.Join(rex.Dir(), "settings.json") }

func loadSettings() {
	prefs = settings{CopyOnSelect: true, PaneFocusCues: true}
	if b, err := os.ReadFile(settingsPath()); err == nil {
		json.Unmarshal(b, &prefs)
	}
	if prefs.FontSize < 6 || prefs.FontSize > 40 {
		prefs.FontSize = defaultFontSize
	}
	termFont.Size = prefs.FontSize
	termFont.Family = terminalFontFamily(prefs.FontFamily)
	var err error
	terminalLight, terminalDark, err = resolveTerminalTheme(prefs.TerminalTheme)
	if err != nil {
		prefs.TerminalTheme = ""
	}
	applyAppearance()
}

func saveSettings() {
	b, _ := json.MarshalIndent(prefs, "", "  ")
	os.MkdirAll(rex.Dir(), 0o700)
	os.WriteFile(settingsPath(), b, 0o600)
}

func applyAppearance() {
	switch prefs.Appearance {
	case "light":
		mygo.Theme.SetSource(mygo.ThemeLight)
	case "dark":
		mygo.Theme.SetSource(mygo.ThemeDark)
	default:
		mygo.Theme.SetSource(mygo.ThemeSystem)
	}
}

func (a *App) setAppearance(v string) {
	prefs.Appearance = v
	applyAppearance()
	saveSettings()
}

// setFontSize changes the size of the terminals' text, in every pane.
func (a *App) setFontSize(size float32) {
	size = min(max(size, 8), 32)
	prefs.FontSize = size
	termFont.Size = size
	a.applyTerminalFont()
	saveSettings()
}

func (a *App) setFontFamily(family string) {
	prefs.FontFamily = family
	termFont.Family = terminalFontFamily(family)
	a.applyTerminalFont()
	saveSettings()
}

func (a *App) setCopyOnSelect(enabled bool) {
	prefs.CopyOnSelect = enabled
	for _, tab := range a.tabs {
		for _, pane := range tab.panes() {
			if pane.term != nil {
				pane.term.SetCopyOnSelect(enabled)
			}
		}
	}
	saveSettings()
}

func (a *App) applyTerminalFont() {
	for _, t := range a.tabs {
		for _, p := range t.panes() {
			if p.term != nil {
				p.term.SetFont(termFont)
			}
		}
	}
}

func (a *App) openSettings() {
	a.paletteOpen, a.hostOpen, a.profilesOpen = false, false, false
	for i, tab := range a.tabs {
		if tab.Settings {
			a.selectTab(i)
			return
		}
	}
	tab := &Tab{ID: a.id(), Settings: true}
	at := min(a.active+1, len(a.tabs))
	a.tabs = slices.Insert(a.tabs, at, tab)
	a.selectTab(at)
	if a.settingsCategory == "" {
		a.settingsCategory = "Appearance"
	}
	a.settingsProfile = a.defaultProfile().Name
	a.themePickerOpen = false
	if len(a.themeNames) == 0 {
		a.themeNames = append([]string{defaultTerminalTheme}, terminal.GhosttyThemes()...)
	}
	if len(a.fontFamilies) == 0 {
		a.fontFamilies = discoverFontFamilies()
	}
	a.settingsFont = prefs.FontFamily
	a.settingsFontSize = formatFontSize(termFont.Size)
	a.fontHighlight = ""
	if a.settingsFont == "" {
		a.settingsFont = defaultFontFamily
	}
	a.focusReq = nil
}

func (a *App) settingsPanel(c *ui.Context, k *colors) {
	ui.Row(c).Grow(1).MinHeight(0).MinWidth(0).AlignItems(ui.Stretch).Radius(cardR).Background(k.panel).Border(1, k.panelBorder).Clip().Children(func() {
		ui.Column(c).Width(180).Shrink(0).Padding(16, 10).Gap(6).Background(k.track).Children(func() {
			ui.Text(c, "Settings").FontSize(18).FontWeight(700).TextColor(k.text).Padding(4, 8, 14, 8)
			for _, category := range []string{"Appearance", "Terminal", "Clipboard", "Keyboard Shortcuts"} {
				button := ui.Button(c.Key(category), category).Label("Settings category: " + category).Selected(a.settingsCategory == category)
				if a.settingsCategory == category {
					button.Background(k.panelSel).TextColor(k.text).Border(1, k.textFaint)
				}
				if button.Clicked() {
					a.settingsCategory = category
					a.themePickerOpen = false
					a.shortcutEditing, a.shortcutError = nil, ""
				}
			}
		})
		ui.Column(c).Grow(1).MinWidth(0).MinHeight(0).Children(func() {
			ui.Row(c).Padding(16, 20).AlignItems(ui.Center).Shrink(0).Children(func() {
				ui.Text(c, a.settingsCategory).FontSize(20).FontWeight(700).TextColor(k.text).Grow(1)
				if iconButton(c, k, "x", "Close Settings", 28, 16).Clicked() {
					tab := a.tab()
					a.later(c, func() { a.closeTab(tab) })
				}
			})
			ui.Divider(c)
			ui.Scroll(c.Key(a.settingsCategory)).Grow(1).MinHeight(0).Children(func() {
				ui.Column(c).Padding(20).Gap(24).MaxWidth(720).Children(func() {
					switch a.settingsCategory {
					case "Appearance":
						ui.Column(c).Gap(8).Children(func() {
							ui.Text(c, "Appearance").FontSize(13).FontWeight(600).TextColor(k.text)
							ui.Row(c).Gap(6).Children(func() {
								for _, choice := range []struct{ label, value string }{{"System", ""}, {"Light", "light"}, {"Dark", "dark"}} {
									button := ui.Button(c, choice.label).Grow(1).Selected(prefs.Appearance == choice.value)
									if prefs.Appearance == choice.value {
										button.Background(k.panelSel).TextColor(k.text)
										button.Border(1, k.textFaint)
									}
									if button.Clicked() {
										a.setAppearance(choice.value)
									}
								}
							})
						})
						ui.Column(c).Gap(8).Children(func() {
							ui.Text(c, "Split panes").FontSize(13).FontWeight(600).TextColor(k.text)
							if ui.Checkbox(c, &prefs.PaneFocusCues, "Highlight active pane").Changed() {
								saveSettings()
							}
							ui.Text(c, "Mark the active pane with a stronger border and an Active label. Inactive terminals blend into their headers.").FontSize(11.5).TextColor(k.textFaint)
						})
						a.terminalThemeSettings(c, k)

					case "Terminal":
						ui.Column(c).Gap(8).Children(func() {
							ui.Text(c, "Terminal font").FontSize(13).FontWeight(600).TextColor(k.text)
							a.fontSelector(c, k)
							ui.Column(c).Padding(10).Radius(8).Background(k.hover).Gap(4).Children(func() {
								ui.Text(c, "Aa Bb 0O 1Il {} [] =>").Font(terminalFontFamily(a.settingsFont)).FontSize(termFont.Size).TextColor(k.text).SingleLine()
								ui.Text(c, "The quick brown fox jumps over the lazy dog.").Font(terminalFontFamily(a.settingsFont)).FontSize(termFont.Size).TextColor(k.text).SingleLine().Ellipsis("…")
							})
							ui.Row(c).Gap(8).Children(func() {
								applied := prefs.FontFamily
								if applied == "" {
									applied = defaultFontFamily
								}
								if ui.Button(c, "Apply font").Disabled(a.settingsFont == applied).Clicked() {
									a.setFontFamily(a.settingsFont)
								}
								if ui.Button(c, "Reset font").Clicked() {
									a.settingsFont = defaultFontFamily
									a.setFontFamily(defaultFontFamily)
								}
							})
							ui.Text(c, "Installed monospace fonts · JetBrains Mono is the fallback.").FontSize(11.5).TextColor(k.textFaint)
						})
						ui.Column(c).Gap(8).Children(func() {
							ui.Text(c, "Terminal font size").FontSize(13).FontWeight(600).TextColor(k.text)
							ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
								if ui.Button(c, "−").Label("Decrease font size").Disabled(termFont.Size <= 8).Clicked() {
									a.setFontSize(termFont.Size - 1)
								}
								a.fontSizeInput(c, k)
								if ui.Button(c, "+").Label("Increase font size").Disabled(termFont.Size >= 32).Clicked() {
									a.setFontSize(termFont.Size + 1)
								}
								ui.Spacer(c)
								if ui.Button(c, "Reset size").Clicked() {
									a.setFontSize(defaultFontSize)
								}
							})
							ui.Text(c, "8–32 pt · decimals allowed").FontSize(11.5).TextColor(k.textFaint)
						})
						ui.Column(c).Gap(8).Children(func() {
							ui.Text(c, "Default terminal").FontSize(13).FontWeight(600).TextColor(k.text)
							profiles := a.profiles
							if len(profiles) == 0 {
								profiles = []terminalProfile{{ID: "auto", Name: "Default shell"}}
							}
							names := make([]string, len(profiles))
							for i, profile := range profiles {
								names[i] = profile.Name
							}
							if ui.Select(c, &a.settingsProfile, names).Label("Choose default terminal").Changed() {
								for _, profile := range profiles {
									if profile.Name == a.settingsProfile {
										prefs.DefaultProfile = profile.ID
										saveSettings()
										break
									}
								}
							}
							ui.Text(c, "Used by + and New Tab. Existing sessions keep their shell.").FontSize(11.5).TextColor(k.textFaint)
						})

					case "Keyboard Shortcuts":
						a.keyboardShortcutSettings(c, k)
					case "Clipboard":
						ui.Column(c).Gap(8).Children(func() {
							ui.Text(c, "Clipboard").FontSize(13).FontWeight(600).TextColor(k.text)
							if ui.Checkbox(c, &prefs.CopyOnSelect, "Copy on select").Changed() {
								a.setCopyOnSelect(prefs.CopyOnSelect)
							}
							ui.Text(c, "Copy highlighted terminal text when you release the mouse.").FontSize(11.5).TextColor(k.textFaint)
						})
					}
				})
			})
			ui.Divider(c)
			footer := "Font: preview, then Apply. Other changes save automatically."
			if a.settingsCategory == "Keyboard Shortcuts" {
				footer = "Save applies shortcut edits. Reset restores default keys."
			}
			ui.Text(c, footer).FontSize(11).TextColor(k.textFaint).Padding(12, 20).Shrink(0)
		})
	})
}

func formatFontSize(size float32) string {
	return strconv.FormatFloat(float64(size), 'f', -1, 32)
}

func (a *App) fontSizeInput(c *ui.Context, k *colors) {
	input := ui.TextInput(c, &a.settingsFontSize).Label("Font size").Width(84).FontSize(13)
	if input.Changed() {
		if size, err := strconv.ParseFloat(a.settingsFontSize, 32); err == nil && size >= 8 && size <= 32 {
			a.setFontSize(float32(size))
		}
	}
	// Keep incomplete decimal input while editing, and restore the applied
	// value when an invalid value is submitted or the field loses focus.
	if input.Submitted() || !input.Focused() {
		a.settingsFontSize = formatFontSize(termFont.Size)
	}
	ui.Text(c, "pt").FontSize(12).TextColor(k.textFaint)
}
