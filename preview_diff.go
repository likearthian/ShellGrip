package main

import (
	"fmt"
	"runtime"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// diffBindings leave app shortcuts and AltGr combinations out of the editor.
func diffBindings() []editor.Binding {
	var bindings []editor.Binding
	for _, b := range editor.DefaultBindings() {
		if b.Mods&(ui.Ctrl|ui.Alt) == ui.Ctrl|ui.Alt || b.ThenKey != 0 || b.Key == ui.KeyEscape {
			b.Command = editor.NoCommand
			bindings = append(bindings, b)
		} else if runtime.GOOS != "darwin" && b.Mods&ui.Ctrl != 0 && b.Key >= ui.KeyA && b.Key <= ui.KeyZ {
			b.Command = editor.NoCommand
			bindings = append(bindings, b)
		}
	}
	if runtime.GOOS != "darwin" {
		bindings = append([]editor.Binding{
			{Mods: ui.Ctrl | ui.Shift, Key: ui.KeyA, Command: editor.SelectAll},
			{Mods: ui.Ctrl | ui.Shift, Key: ui.KeyC, Command: editor.Copy},
			{Mods: ui.Ctrl | ui.Shift, Key: ui.KeyF, Command: editor.Find},
			{Mods: ui.Ctrl | ui.Shift, Key: ui.KeyG, Command: editor.GoToLine},
		}, bindings...)
	}
	return bindings
}

func (p *filePreview) prepareDiffDisplay() {
	if p.diffDisplay != nil || p.content.diffPatch == nil {
		return
	}
	d := &editor.DiffState{
		Layout: editor.DiffSplit, Indicators: editor.DiffClassic,
		ScrollPastEnd: true, LineHeight: 1.75, TabSize: previewTabWidth,
		Font:     ui.Font{Family: defaultFontFamily, Size: 12},
		Bindings: diffBindings(),
	}
	d.RenderFindBar = func(c *ui.Context, bar ui.Element, f *editor.FindBar) {
		p.diffBar = true
		editor.DefaultFindBar(c, bar, f)
	}
	d.RenderGoToLine = func(c *ui.Context, bar ui.Element, g *editor.GoToLineBar) {
		p.diffBar = true
		editor.DefaultGoToLine(c, bar, g)
	}
	d.SetPatch(*p.content.diffPatch)
	p.diffDisplay = d
}

func (p *filePreview) navigateDiff(direction int) {
	d := p.diffDisplay
	count := len(d.Hunks())
	if count == 0 {
		return
	}
	if p.diffChange == 0 && direction < 0 {
		p.diffChange = count
	} else {
		p.diffChange = (p.diffChange-1+direction+count)%count + 1
	}
	d.GoToHunk(p.diffChange - 1)
	d.Focus()
}

func (a *App) sideBySideDiff(c *ui.Context, k *colors, p *filePreview, dark bool) {
	p.prepareDiffDisplay()
	d := p.diffDisplay
	if d == nil {
		return
	}
	theme := editor.DefaultTheme(c.Theme())
	theme.Background, theme.Text, theme.LineNumber = k.panel, k.text, k.textFaint
	theme.Added, _ = diffAdded(dark)
	theme.Deleted, _ = diffRemoved(dark)
	d.Theme = theme
	count := len(d.Hunks())
	ui.Row(c).Height(30).Padding(0, 8).Gap(4).AlignItems(ui.Center).BorderColor(k.panelBorder).BorderWidth(1, 0, 1, 0).Children(func() {
		ui.Text(c, fmt.Sprintf("%d changes", count)).FontSize(11).TextColor(k.textMuted).Grow(1)
		if p.diffChange > 0 {
			ui.Text(c, fmt.Sprintf("%d of %d", p.diffChange, count)).FontSize(11).TextColor(k.textFaint)
		}
		previous := iconButton(c, k, "arrow-up", "Previous Change", 24, 14).Disabled(count == 0).Tooltip("Previous change · Shift+F7").Clicked()
		next := iconButton(c, k, "chevron-down", "Next Change", 24, 14).Disabled(count == 0).Tooltip("Next change · F7").Clicked()
		if !a.paletteOpen && a.renaming == nil {
			previous = c.Shortcut(ui.Shift, ui.KeyF7) || previous
			next = c.Shortcut(0, ui.KeyF7) || next
		}
		if previous {
			p.navigateDiff(-1)
		}
		if next {
			p.navigateDiff(1)
		}
	})
	left, right := "Original · Index", "Modified · Working Tree"
	if p.subtitle == "Staged changes" {
		left, right = "Original · HEAD", "Modified · Index"
	}
	if p.subtitle == "Untracked file" {
		left = "Original · Empty"
	}
	share := d.SplitPercent
	if share == 0 {
		share = 50
	}
	ui.Row(c).Height(28).Children(func() {
		ui.Text(c, left).Basis(0).Grow(share).MinWidth(0).SingleLine().Ellipsis("…").Padding(5, 10).FontSize(11).TextColor(k.textMuted)
		ui.Text(c, right).Basis(0).Grow(100-share).MinWidth(0).SingleLine().Ellipsis("…").Padding(5, 10).FontSize(11).TextColor(k.textMuted)
	})
	p.diffBar = false
	editor.DiffView(c.Key("diff-view"), d).Label("Diff preview").Grow(1).MinWidth(0).MinHeight(0)
}
