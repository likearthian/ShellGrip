package main

import "github.com/egoist/mygo/ui"

type fileTreeAction struct {
	label, icon string
	disabled    bool
	run         func()
}

type fileTreeMenuState struct {
	tree     *fileTreeState
	open     bool
	selected int
	editor   string
}

func (a *App) fileTreePopup(c *ui.Context, anchor ui.Element, state *fileTreeMenuState, actions []fileTreeAction) {
	k := colorsOf(c)
	state.selected = min(max(state.selected, 0), len(actions)-1)
	ui.PopoverBase(c, anchor, &state.open, func(panel ui.Element) {
		panel.Role(ui.RoleMenu).Label("File Tree Menu").Width(252).Padding(5).Gap(1).Margin(2, 0).
			Radius(9).Background(k.panel).Border(1, k.panelBorder).Shadow(0, 8, 24, 0, k.shadowFocused)
		move := func(direction int) {
			for range len(actions) {
				state.selected = (state.selected + direction + len(actions)) % len(actions)
				if actions[state.selected].label != "" && !actions[state.selected].disabled {
					return
				}
			}
		}
		focus := false
		if panel.OverlayShortcut(0, ui.KeyDown) {
			move(1)
			focus = true
		}
		if panel.OverlayShortcut(0, ui.KeyUp) {
			move(-1)
			focus = true
		}
		if panel.OverlayShortcut(0, ui.KeyEnter) {
			if action := actions[state.selected]; !action.disabled && action.run != nil {
				state.open = false
				action.run()
			}
		}
		target := state.selected
		for i, action := range actions {
			if action.label == "" {
				ui.Box(c.Key(i)).Height(1).Margin(4, 5).Background(k.panelBorder)
				continue
			}
			button := ui.ButtonBase(c.Key(i)).Row().Height(30).Padding(0, 8).Gap(8).AlignItems(ui.Center).Justify(ui.Start).
				Radius(5).Role(ui.RoleMenuItem).Label(action.label).Disabled(action.disabled)
			if i == 0 {
				button.AutoFocus()
			}
			if focus && i == target {
				button.Focus()
			}
			if !focus && button.Focused() {
				state.selected = i
			}
			if button.Hovered() || button.FocusVisible() {
				button.Background(k.hover)
			}
			button.Children(func() {
				if action.icon != "" {
					ui.Image(c, icon(action.icon)).Size(16, 16).Label("VS Code icon")
				} else {
					ui.Box(c).Size(16, 16)
				}
				ui.Text(c, action.label).FontSize(12.5).TextColor(k.text).SingleLine()
			})
			if button.Clicked() {
				state.open = false
				action.run()
			}
		}
	})
}
