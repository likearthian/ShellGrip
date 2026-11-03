package main

import (
	"os"
	"strings"

	"github.com/egoist/mygo/ui"
)

// terminalProfile keeps the launch command separate from a pane's directory.
type terminalProfile struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Command []string `json:"command,omitempty"`
	WSL     bool     `json:"wsl,omitempty"`
}

func (a *App) defaultProfile() terminalProfile {
	for _, p := range a.profiles {
		if p.ID == prefs.DefaultProfile {
			return p
		}
	}
	return terminalProfile{ID: "auto", Name: "Default shell"}
}

func (p terminalProfile) launch(dir string) ([]string, string) {
	dir = p.startingDir(dir)
	command := append([]string(nil), p.Command...)
	if p.WSL {
		command = append(command, "--cd", dir)
		// CreateProcess needs a Windows directory, even for a Linux path.
		dir, _ = os.UserHomeDir()
	} else if strings.HasPrefix(dir, "/") && windowsProfiles {
		dir, _ = os.UserHomeDir()
	}
	return command, dir
}

// Windows directories belong to the host shell. A WSL profile starts in
// Linux home unless it inherits an absolute Linux directory from WSL.
func (p terminalProfile) startingDir(dir string) string {
	if !p.WSL {
		return nativeDirectory(dir)
	}
	if p.WSL && (dir == "" || !strings.HasPrefix(dir, "/") || strings.HasPrefix(dir, "//")) {
		return "~"
	}
	return dir
}

func (a *App) newTabControls(c *ui.Context, k *colors) {
	if iconButton(c, k, "plus", "New Tab", 30, 19).Tooltip(tip("New Tab", &cmdNewTab)).Clicked() {
		a.newTab(a.currentDir())
	}
	button := iconButton(c, k, "chevron-down", "Terminal Profiles", 30, 14).Tooltip("Choose a terminal profile")
	if button.Clicked() {
		a.profilesOpen = !a.profilesOpen
	}
	ui.PopoverBase(c, button, &a.profilesOpen, func(panel ui.Element) {
		panel.Width(290).Padding(8).Gap(4).Radius(12).Background(k.panel).Border(1, k.panelBorder).
			Shadow(0, 12, 32, 0, k.shadowFocused)
		ui.Text(c, "New tab").FontSize(12).TextColor(k.textFaint).Margin(4, 6)
		profiles := a.profiles
		if len(profiles) == 0 {
			profiles = []terminalProfile{{ID: "auto", Name: "Default shell"}}
		}
		for _, profile := range profiles {
			ui.Row(c.Key(profile.ID)).Gap(6).AlignItems(ui.Center).Children(func() {
				row := ui.Row(c).Grow(1).MinWidth(0).Padding(8).Radius(6).Gap(8).AlignItems(ui.Center).
					Cursor(ui.CursorPointer).Role(ui.RoleButton).Label("Open " + profile.Name)
				if row.Hovered() {
					row.Background(k.hover)
				}
				row.Children(func() {
					ui.Icon(c, icon("terminal")).Size(16, 16).TextColor(k.iconMuted)
					ui.Text(c, profile.Name).FontSize(13).TextColor(k.text).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
				})
				if row.Clicked() {
					a.profilesOpen = false
					a.newTabWithProfile(a.currentDir(), profile)
				}
				isDefault := profile.ID == a.defaultProfile().ID
				glyph := "circle-dot"
				if isDefault {
					glyph = "check"
				}
				if iconButton(c, k, glyph, "Set default: "+profile.Name, 28, 14).Tooltip("Use " + profile.Name + " for new tabs").Clicked() {
					prefs.DefaultProfile = profile.ID
					saveSettings()
				}
			})
		}
		ui.Text(c, "+ opens the default · ✓ marks the default").FontSize(11).TextColor(k.textFaint).Margin(4, 6)
	})
}
