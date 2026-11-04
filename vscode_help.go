package main

import "github.com/egoist/mygo/ui"

func (a *App) vsCodeWSLHelp(c *ui.Context, k *colors) {
	if !a.vsCodeWSLHelpOpen {
		return
	}
	ui.DialogBase(c, &a.vsCodeWSLHelpOpen, func(backdrop, panel ui.Element) {
		backdrop.Background(k.backdrop)
		panel.Width(500).MaxWidthPercent(90).Padding(20).Gap(12).Radius(12).
			Background(k.panel).Border(1, k.panelBorder).Shadow(0, 16, 40, 0, k.shadowFocused)
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Image(c, icon("brand:vscode")).Size(24, 24)
			ui.Text(c, "Could not open Remote–WSL").FontSize(17).FontWeight(600).TextColor(k.text)
		})
		ui.Text(c, a.vsCodeWSLMessage).FontSize(12.5).TextColor(k.attention).Selectable()
		ui.Text(c, "Remote–WSL requires Microsoft's WSL extension. Install or enable it in your Windows VS Code installation, then try opening the folder again.").FontSize(13).TextColor(k.text)
		ui.Text(c, "Extension: WSL · Publisher: Microsoft\n"+wslExtensionID).FontSize(12).TextColor(k.textMuted).Selectable()
		ui.Row(c).Gap(8).Children(func() {
			if ui.Button(c, "View WSL extension").Clicked() {
				c.OpenURL(wslExtensionURL)
			}
			if ui.Button(c, "Copy install command").Clicked() {
				c.WriteClipboard(wslInstallCommand)
				a.selectionCopied(wslInstallCommand)
			}
			if ui.Button(c, "Dismiss").Clicked() {
				a.vsCodeWSLHelpOpen = false
			}
		})
	})
}
