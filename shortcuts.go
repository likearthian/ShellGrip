package main

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

var tabNumberCommands, tabAlternateCommands = makeTabShortcutCommands()

var tabCycleCommands = []*command{
	{Title: "Cycle to Next Tab", Other: shortcut{ui.Ctrl, ui.KeyTab}, Run: func(a *App) { a.cycleTab(1) }},
	{Title: "Cycle to Previous Tab", Other: shortcut{ui.Ctrl | ui.Shift, ui.KeyTab}, Run: func(a *App) { a.cycleTab(-1) }},
}

func makeTabShortcutCommands() (primary, alternate []*command) {
	count := 10
	if runtime.GOOS == "darwin" {
		count = 9
	}
	for i := 1; i <= count; i++ {
		n := i % 10
		cmd := &command{Title: fmt.Sprintf("Show Tab %d", i), Other: shortcut{ui.Ctrl, ui.Key0 + ui.Key(n)}}
		if runtime.GOOS == "darwin" {
			cmd.Accel = fmt.Sprintf("Cmd+%d", i)
		}
		cmd.Run = func(a *App) {
			if runtime.GOOS == "darwin" {
				a.showTab(i)
			} else {
				a.selectTabNumber(n)
			}
		}
		primary = append(primary, cmd)
		if i <= 9 && runtime.GOOS == "windows" {
			alternate = append(alternate, &command{Title: fmt.Sprintf("Show Tab %d (alternate)", i), Other: showTabKey(i), Run: func(a *App) { a.showTab(i) }})
		}
	}
	return
}

func shortcutCommands() []*command {
	all := append([]*command{}, commands...)
	all = append(all, tabCycleCommands...)
	all = append(all, tabNumberCommands...)
	return append(all, tabAlternateCommands...)
}

var shortcutKeyNames = map[ui.Key]string{
	ui.KeyEnter: "Enter", ui.KeyTab: "Tab", ui.KeySpace: "Space", ui.KeyBackspace: "Backspace",
	ui.KeyDelete: "Delete", ui.KeyInsert: "Insert", ui.KeyHome: "Home", ui.KeyEnd: "End",
	ui.KeyPageUp: "PageUp", ui.KeyPageDown: "PageDown", ui.KeyLeft: "Left", ui.KeyRight: "Right", ui.KeyUp: "Up", ui.KeyDown: "Down",
	ui.KeyEqual: "=", ui.KeyMinus: "-", ui.KeyComma: ",", ui.KeyPeriod: ".", ui.KeySlash: "/", ui.KeySemicolon: ";",
	ui.KeyQuote: "'", ui.KeyBracketLeft: "[", ui.KeyBracketRight: "]", ui.KeyBackslash: "\\", ui.KeyBackquote: "`",
}

func (s shortcut) String() string {
	if s.Key == ui.KeyUnknown {
		return ""
	}
	var parts []string
	for _, m := range []struct {
		mod  ui.Modifiers
		name string
	}{{ui.Ctrl, "Ctrl"}, {ui.Super, "Cmd"}, {ui.Alt, "Alt"}, {ui.Shift, "Shift"}} {
		if s.Mods&m.mod != 0 {
			parts = append(parts, m.name)
		}
	}
	key := shortcutKeyNames[s.Key]
	switch {
	case s.Key >= ui.KeyA && s.Key <= ui.KeyZ:
		key = string(rune('A' + s.Key - ui.KeyA))
	case s.Key >= ui.Key0 && s.Key <= ui.Key9:
		key = string(rune('0' + s.Key - ui.Key0))
	case s.Key >= ui.KeyF1 && s.Key <= ui.KeyF12:
		key = fmt.Sprintf("F%d", s.Key-ui.KeyF1+1)
	}
	return strings.Join(append(parts, key), "+")
}

func parseShortcut(value string) (shortcut, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return shortcut{}, nil
	}
	var binding shortcut
	parts := strings.Split(value, "+")
	for _, part := range parts[:len(parts)-1] {
		var mod ui.Modifiers
		switch strings.ToLower(strings.TrimSpace(part)) {
		case "ctrl", "control":
			mod = ui.Ctrl
		case "cmd", "command", "super":
			mod = ui.Super
		case "cmdorctrl":
			mod = ui.Cmd
		case "alt", "option":
			mod = ui.Alt
		case "shift":
			mod = ui.Shift
		default:
			return shortcut{}, fmt.Errorf("Unknown modifier %q.", part)
		}
		if binding.Mods&mod != 0 {
			return shortcut{}, fmt.Errorf("Modifier %q is repeated.", part)
		}
		binding.Mods |= mod
	}
	key := strings.ToUpper(strings.TrimSpace(parts[len(parts)-1]))
	switch {
	case len(key) == 1 && key[0] >= 'A' && key[0] <= 'Z':
		binding.Key = ui.KeyA + ui.Key(key[0]-'A')
	case len(key) == 1 && key[0] >= '0' && key[0] <= '9':
		binding.Key = ui.Key0 + ui.Key(key[0]-'0')
	case strings.HasPrefix(key, "F"):
		if n, err := strconv.Atoi(key[1:]); err == nil && n >= 1 && n <= 12 {
			binding.Key = ui.KeyF1 + ui.Key(n-1)
		}
	}
	if binding.Key == ui.KeyUnknown {
		for code, name := range shortcutKeyNames {
			if strings.EqualFold(key, name) {
				binding.Key = code
				break
			}
		}
	}
	if binding.Key == ui.KeyUnknown {
		return shortcut{}, fmt.Errorf("Unknown key %q. Use a letter, digit, F1–F12, or a named key such as Enter.", key)
	}
	if binding.Mods == 0 && (binding.Key < ui.KeyF1 || binding.Key > ui.KeyF12) {
		return shortcut{}, fmt.Errorf("Add Ctrl, Alt, Shift, or Cmd so typing still works in the terminal.")
	}
	if binding.Mods == ui.Shift && ((binding.Key >= ui.KeyA && binding.Key <= ui.KeyZ) ||
		(binding.Key >= ui.Key0 && binding.Key <= ui.Key9) || binding.Key == ui.KeySpace ||
		(len(shortcutKeyNames[binding.Key]) == 1)) {
		return shortcut{}, fmt.Errorf("Add Ctrl, Alt, or Cmd to keep ordinary typing available.")
	}
	if runtime.GOOS != "darwin" && binding.Mods&ui.Super != 0 {
		return shortcut{}, fmt.Errorf("Use Ctrl instead of Cmd on this platform.")
	}
	return binding, nil
}

func (cmd *command) shortcutID() string { return runtime.GOOS + "/" + cmd.Title }

func (cmd *command) defaultBinding() shortcut {
	if runtime.GOOS == "darwin" && cmd.Accel != "" {
		binding, _ := parseShortcut(cmd.Accel)
		return binding
	}
	if runtime.GOOS == "darwin" {
		// These shortcuts are handled by the view on every platform.
		for _, cycle := range tabCycleCommands {
			if cycle == cmd {
				return cmd.Other
			}
		}
		return shortcut{}
	}
	return cmd.Other
}

func (cmd *command) binding() shortcut {
	if value, ok := prefs.Shortcuts[cmd.shortcutID()]; ok {
		if binding, err := parseShortcut(value); err == nil {
			return binding
		}
	}
	return cmd.defaultBinding()
}

func (cmd *command) accelerator() string   { return cmd.binding().String() }
func (cmd *command) shortcutLabel() string { return cmd.binding().String() }

func shortcutConflict(cmd *command, binding shortcut) error {
	if binding == (shortcut{}) {
		return nil
	}
	for _, other := range shortcutCommands() {
		if cmd != other && binding == other.binding() {
			return fmt.Errorf("%s is already assigned to %s.", binding.String(), other.Title)
		}
	}
	copyMods := ui.Ctrl | ui.Shift
	if runtime.GOOS == "darwin" {
		copyMods = ui.Super
	}
	if (binding.Mods == copyMods && (binding.Key == ui.KeyC || binding.Key == ui.KeyV)) ||
		(runtime.GOOS != "darwin" && binding.Mods == ui.Ctrl && binding.Key == ui.KeyV) {
		return fmt.Errorf("%s is used for terminal copy/paste.", binding.String())
	}
	return nil
}

func (a *App) saveShortcut(cmd *command, value string, reset bool) error {
	binding, err := parseShortcut(value)
	if reset {
		binding, err = cmd.defaultBinding(), nil
	}
	if err != nil {
		return err
	}
	if err := shortcutConflict(cmd, binding); err != nil {
		return err
	}
	if prefs.Shortcuts == nil {
		prefs.Shortcuts = map[string]string{}
	}
	if reset {
		delete(prefs.Shortcuts, cmd.shortcutID())
	} else {
		prefs.Shortcuts[cmd.shortcutID()] = binding.String()
	}
	a.shortcutsChanged()
	return nil
}

func (a *App) shortcutsChanged() {
	saveSettings()
	if a.win != nil {
		mygo.App.SetMenu(a.menu())
	}
}

func (a *App) keyboardShortcutSettings(c *ui.Context, k *colors) {
	ui.Text(c, "Customize shortcuts").FontSize(14).FontWeight(600).TextColor(k.text)
	ui.Text(c, "Edit a shortcut using keys such as Ctrl+Shift+T or F6. Leave it empty to disable it. Changes apply when saved.").FontSize(12).TextColor(k.textMuted)
	ui.Text(c, "Terminal copy and paste keep their built-in shortcuts.").FontSize(11.5).TextColor(k.textFaint)
	ui.TextInput(c, &a.shortcutQuery).Label("Search keyboard shortcuts").Placeholder("Search actions…")
	if ui.Button(c, "Reset all shortcuts").Clicked() {
		for key := range prefs.Shortcuts {
			if strings.HasPrefix(key, runtime.GOOS+"/") {
				delete(prefs.Shortcuts, key)
			}
		}
		a.shortcutEditing, a.shortcutError = nil, ""
		a.shortcutsChanged()
	}
	query := strings.ToLower(strings.TrimSpace(a.shortcutQuery))
	count := 0
	for _, cmd := range shortcutCommands() {
		if !strings.Contains(strings.ToLower(cmd.Title+" "+cmd.shortcutLabel()), query) {
			continue
		}
		count++
		ui.Column(c.Key(cmd.Title)).Gap(8).Padding(12, 0).BorderWidth(0, 0, 1, 0).BorderColor(k.panelBorder).Children(func() {
			ui.Text(c, cmd.Title).FontSize(13).FontWeight(600).TextColor(k.text)
			ui.Row(c).Gap(8).Children(func() {
				label := cmd.shortcutLabel()
				if label == "" {
					label = "Unassigned"
				}
				ui.Text(c, label).FontSize(12).TextColor(k.textMuted).Grow(1)
				if ui.Button(c, "Edit").Label("Edit shortcut: " + cmd.Title).Clicked() {
					a.shortcutEditing, a.shortcutDraft, a.shortcutError = cmd, cmd.shortcutLabel(), ""
				}
				_, custom := prefs.Shortcuts[cmd.shortcutID()]
				if ui.Button(c, "Reset").Label("Reset shortcut: " + cmd.Title).Disabled(!custom).Clicked() {
					if err := a.saveShortcut(cmd, "", true); err != nil {
						a.shortcutEditing, a.shortcutDraft, a.shortcutError = cmd, cmd.shortcutLabel(), err.Error()
					} else {
						a.shortcutEditing, a.shortcutError = nil, ""
					}
				}
			})
			if a.shortcutEditing == cmd {
				input := ui.TextInput(c, &a.shortcutDraft).Label("Shortcut keys").Placeholder("Ctrl+Shift+T").AutoFocus()
				if input.Changed() {
					a.shortcutError = ""
				}
				if a.shortcutError != "" {
					ui.Text(c, a.shortcutError).FontSize(12).TextColor(k.attention)
				}
				ui.Row(c).Gap(8).Children(func() {
					save := ui.Button(c, "Save shortcut").Clicked() || input.Submitted()
					if ui.Button(c, "Disable").Clicked() {
						a.shortcutDraft = ""
						save = true
					}
					if save {
						if err := a.saveShortcut(cmd, a.shortcutDraft, false); err != nil {
							a.shortcutError = err.Error()
						} else {
							a.shortcutEditing, a.shortcutError = nil, ""
						}
					}
					if ui.Button(c, "Cancel").Clicked() {
						a.shortcutEditing, a.shortcutError = nil, ""
					}
				})
			}
		})
	}
	if count == 0 {
		ui.Text(c, "No shortcuts found").FontSize(12).TextColor(k.textFaint)
	}
}
