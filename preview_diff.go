package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
)

const largeDiffRows = 128

type diffDisplay struct {
	changes []int
	panes   [2]diffTextPane
}

type diffTextPane struct {
	preview     filePreview
	ranges      [2][]ui.TextRange
	starts      []int
	highlights  []diffHighlight
	initialized bool
}

type diffHighlight struct{ x, width float32 }

// Buffers retain all text for native selection and copying, while the text
// control only lays out visible paragraphs. Gutters and alignment padding
// are display insertions, removed by the existing preview copy mapping.
func (p *filePreview) prepareDiffDisplay(c *ui.Context) {
	if p.diffDisplay != nil {
		return
	}
	display := &diffDisplay{changes: diffChanges(p.content.diffRows)}
	if len(p.content.diffRows) > largeDiffRows {
		for side := range 2 {
			pane := &display.panes[side]
			var shown, source strings.Builder
			position := 0
			write := func(s string, inserted bool) {
				shown.WriteString(s)
				for range s {
					if inserted {
						pane.preview.content.insertions = append(pane.preview.content.insertions, position)
					}
					position++
				}
			}
			lastPresent := -1
			for i, row := range p.content.diffRows {
				cell := row.left
				if side == 1 {
					cell = row.right
				}
				if cell.present {
					lastPresent = i
				}
			}
			for i, row := range p.content.diffRows {
				pane.starts = append(pane.starts, position)
				cell := row.left
				if side == 1 {
					cell = row.right
				}
				marker := " "
				if cell.kind == '-' {
					marker = "−"
				} else if cell.kind == '+' {
					marker = "+"
				}
				prefix := fmt.Sprintf("%5d %s ", cell.number, marker)
				if !cell.present {
					prefix = "        "
				}
				write(prefix, true)
				start := position
				value := cell.text
				if row.separator {
					value = "··· Unchanged lines ···"
				}
				write(value, !cell.present)
				if cell.present {
					source.WriteString(value)
				}
				for theme := range pane.ranges {
					offset := start
					for _, span := range cell.spans[theme] {
						end := offset + utf8.RuneCountInString(span.Text)
						if end > offset {
							pane.ranges[theme] = append(pane.ranges[theme], ui.TextRange{Start: offset, End: end, Color: span.Color, Weight: span.Weight})
						}
						offset = end
					}
				}
				highlight := diffHighlight{}
				if cell.changeEnd > cell.changeStart {
					runes := []rune(cell.text)
					highlight.x, _ = c.MeasureText(0, ui.Span{Text: prefix + string(runes[:cell.changeStart]), Font: defaultFontFamily, Size: 12})
					highlight.width, _ = c.MeasureText(0, ui.Span{Text: string(runes[cell.changeStart:cell.changeEnd]), Font: defaultFontFamily, Size: 12})
				}
				pane.highlights = append(pane.highlights, highlight)
				if i < len(p.content.diffRows)-1 {
					keep := cell.present && i < lastPresent
					write("\n", !keep)
					if keep {
						source.WriteByte('\n')
					}
				}
			}
			pane.preview.content.buffer = ui.NewTextBuffer(shown.String())
			pane.preview.content.original = ui.NewTextBuffer(source.String())
		}
	}
	p.diffDisplay = display
}

func diffChanges(rows []diffRow) []int {
	var changes []int
	changed := false
	for i, row := range rows {
		next := row.left.kind == '-' || row.right.kind == '+'
		if next && !changed {
			changes = append(changes, i)
		}
		changed = next
	}
	return changes
}

func (p *filePreview) syncDiffScroll() {
	large := p.diffDisplay != nil && p.diffDisplay.panes[0].preview.content.buffer != nil
	if large {
		for i := range p.diffScroll {
			p.diffScroll[i].X = p.diffHorizontal[i].X
		}
	}
	x, y := p.diffScrollX, p.diffScrollY
	for _, scroll := range p.diffScroll {
		if scroll.X != p.diffScrollX {
			x = scroll.X
		}
		if scroll.Y != p.diffScrollY {
			y = scroll.Y
		}
	}
	for i := range p.diffScroll {
		p.diffScroll[i].X, p.diffScroll[i].Y = x, y
		p.diffHorizontal[i].X = x
	}
	p.diffScrollX, p.diffScrollY = x, y
}

func (p *filePreview) navigateDiff(changes []int, direction int) {
	if len(changes) == 0 {
		return
	}
	if p.diffChange == 0 && direction < 0 {
		p.diffChange = len(changes)
	} else {
		p.diffChange = (p.diffChange-1+direction+len(changes))%len(changes) + 1
	}
	y := float32(max(0, changes[p.diffChange-1]-3) * diffLineHeight)
	for i := range p.diffScroll {
		p.diffScroll[i].X, p.diffScroll[i].Y = 0, y
		p.diffHorizontal[i].X = 0
	}
	p.diffScrollX, p.diffScrollY = 0, y
}

func (a *App) sideBySideDiff(c *ui.Context, k *colors, p *filePreview, dark bool) {
	rows := p.content.diffRows
	p.prepareDiffDisplay(c)
	p.syncDiffScroll()
	changes := p.diffDisplay.changes
	ui.Row(c).Height(30).Padding(0, 8).Gap(4).AlignItems(ui.Center).BorderColor(k.panelBorder).BorderWidth(1, 0, 1, 0).Children(func() {
		ui.Text(c, fmt.Sprintf("%d changes", len(changes))).FontSize(11).TextColor(k.textMuted).Grow(1)
		if p.diffChange > 0 {
			ui.Text(c, fmt.Sprintf("%d of %d", p.diffChange, len(changes))).FontSize(11).TextColor(k.textFaint)
		}
		previous := iconButton(c, k, "arrow-up", "Previous Change", 24, 14).Disabled(len(changes) == 0).Tooltip("Previous change · Shift+F7").Clicked()
		next := iconButton(c, k, "chevron-down", "Next Change", 24, 14).Disabled(len(changes) == 0).Tooltip("Next change · F7").Clicked()
		if !a.paletteOpen && a.renaming == nil {
			previous = c.Shortcut(ui.Shift, ui.KeyF7) || previous
			next = c.Shortcut(0, ui.KeyF7) || next
		}
		if previous {
			p.navigateDiff(changes, -1)
		}
		if next {
			p.navigateDiff(changes, 1)
		}
	})
	if p.diffWidth == 0 {
		var text strings.Builder
		for _, row := range rows {
			text.WriteString(row.left.text)
			text.WriteByte('\n')
			text.WriteString(row.right.text)
			text.WriteByte('\n')
		}
		width, _ := c.MeasureText(0, ui.Span{Text: text.String(), Font: defaultFontFamily, Size: 12})
		p.diffWidth = max(diffMinCodeWidth, width+16)
	}
	ui.Row(c).Grow(1).MinHeight(0).MinWidth(0).AlignItems(ui.Stretch).Children(func() {
		for side := range 2 {
			pane := ui.Column(c.Key(fmt.Sprintf("diff-side-%d", side))).Basis(0).Grow(1).MinWidth(0).MinHeight(0).Clip()
			pane.Children(func() {
				label := "Original · Index"
				if side == 1 {
					label = "Modified · Working Tree"
				}
				if p.subtitle == "Staged changes" {
					label = "Original · HEAD"
					if side == 1 {
						label = "Modified · Index"
					}
				} else if p.subtitle == "Untracked file" && side == 0 {
					label = "Original · Empty"
				}
				ui.Text(c, label).Height(28).Padding(5, 10).FontSize(11).TextColor(k.textMuted).NoWrap().Unselectable()
				width := max(p.diffWidth, pane.Bounds().W-diffGutterWidth)
				if p.diffDisplay.panes[side].preview.content.buffer != nil {
					a.diffTextPaneView(c, k, p, side, label, width, dark)
					return
				}
				ui.ScrollBoth(c).Label(label + " diff pane").TrackScroll(&p.diffScroll[side]).Grow(1).MinHeight(0).MinWidth(0).Children(func() {
					ui.Column(c).Width(width + diffGutterWidth).Gap(0).Selectable().Children(func() {
						for _, row := range rows {
							cell := row.left
							if side == 1 {
								cell = row.right
							}
							a.diffCellView(c, k, cell, row.separator, width, dark)
						}
					})
				})
			})
			if side == 0 {
				ui.Box(c).Width(1).Shrink(0).Background(k.panelBorder)
			}
		}
	})
}

func (a *App) diffTextPaneView(c *ui.Context, k *colors, preview *filePreview, side int, label string, width float32, dark bool) {
	pane := &preview.diffDisplay.panes[side]
	body := ui.Box(c).Grow(1).MinHeight(0).MinWidth(0).Clip()
	if body.Bounds().H == 0 {
		c.Invalidate()
	}
	body.Children(func() {
		ui.ScrollHorizontal(c).Label(label + " diff pane").TrackScroll(&preview.diffHorizontal[side]).Grow(1).MinWidth(0).MinHeight(0).Children(func() {
			ui.Box(c).Width(width + diffGutterWidth).Height(body.Bounds().H).Clip().Draw(func(p *ui.Painter, r ui.Rect) {
				y := preview.diffScroll[side].Y
				first := max(0, int(y/diffLineHeight))
				last := min(len(preview.content.diffRows), int((y+r.H)/diffLineHeight)+1)
				for i := first; i < last; i++ {
					row := preview.content.diffRows[i]
					cell := row.left
					if side == 1 {
						cell = row.right
					}
					background, accent := ui.Color{}, ui.Color{}
					switch {
					case cell.kind == '-':
						accent, background = diffRemoved(dark)
					case cell.kind == '+':
						accent, background = diffAdded(dark)
					case !cell.present || row.separator:
						background = k.panelBorder.Alpha(0.3)
					}
					top := r.Y + float32(i*diffLineHeight) - y
					if background.A != 0 {
						p.Fill(ui.Rect{X: r.X, Y: top, W: r.W, H: diffLineHeight}, background, 0)
					}
					if h := pane.highlights[i]; h.width > 0 {
						p.Fill(ui.Rect{X: r.X + 8 + h.x, Y: top, W: h.width, H: diffLineHeight}, accent.Alpha(0.28), 0)
					}
				}
			}).Children(func() {
				area := ui.TextAreaBufferBase(c, pane.preview.content.buffer).Label(label+" diff text").TrackScroll(&preview.diffScroll[side]).Grow(1).MinHeight(0).MinWidth(0).Padding(0, 8).Font(defaultFontFamily).FontSize(12).FixedLineHeight(diffLineHeight).TextColor(k.text).NoWrap().ReadOnly(true)
				theme := 0
				if dark {
					theme = 1
				}
				// Supplying only nearby ranges avoids copying and sorting every token.
				first := max(0, int(preview.diffScroll[side].Y/diffLineHeight)-2)
				last := min(len(pane.starts)-1, first+int(body.Bounds().H/diffLineHeight)+6)
				if first < len(pane.starts) {
					ranges := pane.ranges[theme]
					start := sort.Search(len(ranges), func(i int) bool { return ranges[i].End > pane.starts[first] })
					end := len(ranges)
					if last+1 < len(pane.starts) {
						end = sort.Search(len(ranges), func(i int) bool { return ranges[i].Start >= pane.starts[last+1] })
					}
					area.TextRanges(ranges[start:end]...)
				}
				if !pane.initialized {
					area.SetTextSelection(0, 0)
					pane.initialized = true
				}
				a.previewCopyMapping(c, &pane.preview, area)
			})
		})
	})
}

func (a *App) diffCellView(c *ui.Context, k *colors, cell diffCell, separator bool, width float32, dark bool) {
	if separator {
		ui.Text(c, "···  Unchanged lines  ···").Width(width + diffGutterWidth).Height(diffLineHeight).FontSize(11).TextColor(k.textFaint).Background(k.panelBorder.Alpha(0.3)).TextAlign(ui.Center).Unselectable()
		return
	}
	background := ui.Color{}
	marker := ""
	if cell.kind == '-' {
		_, background = diffRemoved(dark)
		marker = "−"
	} else if cell.kind == '+' {
		_, background = diffAdded(dark)
		marker = "+"
	} else if !cell.present {
		background = k.panelBorder.Alpha(0.3)
	}
	ui.Row(c).Height(diffLineHeight).Background(background).Children(func() {
		number := ""
		if cell.present {
			number = strconv.Itoa(cell.number)
		}
		ui.Text(c, number).Width(diffGutterWidth - diffMarkerWidth).Height(diffLineHeight).TextAlign(ui.End).Font(defaultFontFamily).FontSize(12).FixedLineHeight(diffLineHeight).TextColor(k.textFaint).Unselectable()
		ui.Text(c, marker).Width(diffMarkerWidth).Height(diffLineHeight).TextAlign(ui.Center).Font(defaultFontFamily).FontSize(12).FixedLineHeight(diffLineHeight).TextColor(k.textMuted).Unselectable()
		if !cell.present {
			ui.Box(c).Width(width).Height(diffLineHeight).Unselectable()
			return
		}
		ui.RichText(c, diffCellSpans(cell, k, dark)...).Width(width).Height(diffLineHeight).Padding(0, 8).Font(defaultFontFamily).FontSize(12).FixedLineHeight(diffLineHeight).TextColor(k.text).NoWrap()
	})
}

func diffCellSpans(cell diffCell, k *colors, dark bool) []ui.Span {
	theme := 0
	if dark {
		theme = 1
	}
	spans := cell.spans[theme]
	if len(spans) == 0 {
		spans = []ui.Span{{Text: cell.text, Color: k.text}}
	}
	if cell.changeEnd <= cell.changeStart {
		return spans
	}
	color, _ := diffRemoved(dark)
	if cell.kind == '+' {
		color, _ = diffAdded(dark)
	}
	var result []ui.Span
	offset := 0
	for _, span := range spans {
		runes := []rune(span.Text)
		start := max(0, min(len(runes), cell.changeStart-offset))
		end := max(start, min(len(runes), cell.changeEnd-offset))
		for _, part := range [][2]int{{0, start}, {start, end}, {end, len(runes)}} {
			if part[0] == part[1] {
				continue
			}
			piece := span
			piece.Text = string(runes[part[0]:part[1]])
			if part[0] == start && part[1] == end && start < end {
				piece.Background = color.Alpha(0.28)
			}
			result = append(result, piece)
		}
		offset += len(runes)
	}
	return result
}
