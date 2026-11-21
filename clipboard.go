package main

import (
	"fmt"
	"time"
	"unicode/utf8"

	"shellgrip/internal/terminal"

	"github.com/egoist/mygo/ui"
)

func terminalCopyOnSelect(enabled bool) terminal.CopyOnSelect {
	if enabled {
		return terminal.CopyOnSelectClipboard
	}
	return terminal.CopyOnSelectNone
}

func (a *App) terminalCopied(text string, source terminal.CopySource) {
	if source != terminal.CopyProgram {
		a.selectionCopied(text)
	}
}

// terminalZoom keeps wheel zoom and terminal keybindings in step with the
// saved font size of every pane, as the app's text-size commands do.
func (a *App) terminalZoom(z terminal.Zoom) bool {
	if z.Reset {
		a.setFontSize(defaultFontSize)
	} else {
		a.setFontSize(termFont.Size + float32(z.Steps))
	}
	return true
}

func (a *App) selectionCopied(text string) {
	if !a.quitting && text != "" {
		a.copiedCharacters = utf8.RuneCountInString(text)
	}
}

func (a *App) showCopyToast(c *ui.Context) {
	count := a.copiedCharacters
	if count == 0 {
		return
	}
	a.copiedCharacters = 0
	unit := "characters"
	if count == 1 {
		unit = "character"
	}
	c.AddToast(ui.Toast{
		ID: "selection-copied", Title: fmt.Sprintf("Copied %d %s to clipboard", count, unit),
		Type: "success", Timeout: 2500 * time.Millisecond,
	})
}

func (a *App) toastViewport(c *ui.Context, k *colors) {
	ui.ToastViewportBase(c, func(viewport ui.Element, toasts []ui.Toast) {
		viewport.Padding(titleH+12, 16, 16, 16).Justify(ui.Start).AlignItems(ui.End).Gap(8)
		for _, notification := range toasts {
			toast := ui.ToastBase(c, notification)
			toast.Root.Row().Padding(10, 14).Gap(9).AlignItems(ui.Center).MaxWidth(360).Radius(10).
				Background(k.panel).Border(1, k.panelBorder).Shadow(0, 6, 20, 0, k.shadowFocused).Children(func() {
				ui.Icon(c, icon("check")).Size(16, 16).TextColor(k.busy).Shrink(0)
				ui.Text(c, notification.Title).FontSize(13).TextColor(k.text).MinWidth(0)
			})
		}
	})
}
