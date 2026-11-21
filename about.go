package main

import "github.com/egoist/mygo/ui"

// aboutDialog is the app's face, opened from the host popover: the name,
// the manifest's version and copyright, and what it stands on.
func (a *App) aboutDialog(c *ui.Context, k *colors) {
	if !a.aboutOpen {
		return
	}
	ui.DialogBase(c, &a.aboutOpen, func(backdrop, panel ui.Element) {
		backdrop.Background(k.backdrop)
		panel.Width(440).MaxWidthPercent(90).Padding(20).Gap(12).Radius(12).
			Background(k.panel).Border(1, k.panelBorder).Shadow(0, 16, 40, 0, k.shadowFocused)
		ui.Row(c).Gap(14).AlignItems(ui.Center).Children(func() {
			ui.Image(c, appLogo).Size(46, 46).Fit(ui.Contain)
			ui.Column(c).Grow(1).MinWidth(0).Children(func() {
				ui.Text(c, "ShellGrip").FontSize(17).FontWeight(700).TextColor(k.text)
				ui.Text(c, "Version "+appVersion()).FontSize(12.5).TextColor(k.textMuted).Selectable()
			})
		})
		ui.Text(c, manifest.Copyright).FontSize(13).TextColor(k.text).LineHeight(1.35)
		ui.Text(c, "Built with MyGo's native UI and Ghostty's libghostty-vt.\n"+
			"Icons: Lucide (ISC) and Simple Icons (CC0). Font: JetBrains Mono (OFL).").
			FontSize(12).TextColor(k.textMuted).LineHeight(1.4).Selectable()
		ui.Row(c).Gap(8).Children(func() {
			if ui.Button(c, "GoRex").Clicked() {
				c.OpenURL("https://github.com/egoist/gorex")
			}
			if ui.Button(c, "MyGo").Clicked() {
				c.OpenURL("https://mygo.egoist.dev")
			}
			if ui.Button(c, "Dismiss").Clicked() {
				a.aboutOpen = false
			}
		})
	})
}
