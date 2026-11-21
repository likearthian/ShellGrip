package main

import (
	"fmt"
	"html"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/egoist/mygo/ui"
	"github.com/yuin/goldmark/ast"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/util"
)

func previewInline(n ast.Node, source []byte, style ui.Span) []ui.Span {
	var spans []ui.Span
	var walk func(ast.Node, ui.Span)
	walk = func(node ast.Node, s ui.Span) {
		switch v := node.(type) {
		case *ast.Text:
			s.Text = string(v.Segment.Value(source))
			if !v.IsRaw() {
				s.Text = html.UnescapeString(string(util.UnescapePunctuations([]byte(s.Text))))
			}
			if v.HardLineBreak() {
				s.Text += "\n"
			} else if v.SoftLineBreak() {
				s.Text += " "
			}
			spans = append(spans, s)
			return
		case *ast.String:
			s.Text = string(v.Value)
			spans = append(spans, s)
			return
		case *ast.Emphasis:
			if v.Level == 2 {
				s.Weight = 700
			} else {
				s.Italic = true
			}
		case *ast.CodeSpan:
			s.Font = defaultFontFamily
			s.Background = ui.Hex("#808080").Alpha(0.15)
		case *extast.Strikethrough:
			s.Strikethrough = true
		case *ast.Link:
			s.Underline = true
			s.Color = ui.Hex("#508ad5")
		case *ast.AutoLink:
			s.Text, s.Underline, s.Color = string(v.Label(source)), true, ui.Hex("#508ad5")
			spans = append(spans, s)
			return
		case *extast.TaskCheckBox:
			s.Text = "☐ "
			if v.IsChecked {
				s.Text = "☑ "
			}
			spans = append(spans, s)
			return
		case *ast.Image:
			s.Text = "[Image: " + string(v.Text(source)) + "]"
			s.Italic = true
			spans = append(spans, s)
			return
		}
		for child := node.FirstChild(); child != nil; child = child.NextSibling() {
			walk(child, s)
		}
	}
	walk(n, style)
	return spans
}

// previewCodeLineHeight is a code block's line, in DIPs: the lines of its
// block are one text, whose fixed line height spaces them.
const previewCodeLineHeight = 20

// previewCodeLines returns the spans of a code block's highlighted lines,
// as the one text that shows them, each line ending in a newline.
func previewCodeLines(lines [][]chroma.Token, dark bool) []ui.Span {
	var spans []ui.Span
	for i, line := range lines {
		if i > 0 {
			spans = append(spans, ui.Span{Text: "\n"})
		}
		if len(line) == 0 {
			continue
		}
		spans = append(spans, previewCodeSpans(line, dark)...)
	}
	if len(spans) == 0 {
		spans = append(spans, ui.Span{Text: " "})
	}
	return spans
}

// previewMarkdown renders the blocks inside parent: a blockquote's or a list
// item's, which are built whole.
func previewMarkdown(c *ui.Context, k *colors, content *previewContent, parent ast.Node) {
	for n := parent.FirstChild(); n != nil; n = n.NextSibling() {
		previewMarkdownBlock(c, k, content, n)
	}
}

// previewMarkdownBlock renders one block of a document as a widget of its
// own: a paragraph's rich text, a table's rows, a code block's highlighted
// lines. A long document's blocks are the rows of its virtualized list, so
// only those in view are built each frame; nested blocks come all at once.
func previewMarkdownBlock(c *ui.Context, k *colors, content *previewContent, n ast.Node) {
	switch block := n.(type) {
	case *ast.Heading:
		sizes := [...]float32{26, 21, 17.5, 15.5, 14, 13}
		level := max(1, min(block.Level, len(sizes)))
		size := sizes[level-1]
		weight := 600
		if level <= 2 {
			weight = 700
		}
		color := k.text
		if level == 6 {
			color = k.textMuted
		}
		top := float32(10)
		if level <= 2 {
			top = 16
		} else if level == 3 {
			top = 12
		}
		h := ui.RichText(c, content.inline[n]...).FontSize(size).FontWeight(weight).TextColor(color).Margin(top, 0, 4, 0)
		if level == 1 {
			h.Padding(0, 0, 8, 0).BorderWidth(0, 0, 1, 0).BorderColor(k.panelBorder)
		} else if level == 2 {
			h.Padding(0, 0, 6, 0).BorderWidth(0, 0, 1, 0).BorderColor(k.panelBorder.Alpha(0.6))
		}
	case *ast.Paragraph, *ast.TextBlock:
		ui.RichText(c, content.inline[n]...).FontSize(14.5).FixedLineHeight(24).TextColor(k.text).MinWidth(0)
		for _, src := range content.blockImages[n] {
			ui.Image(c, src).Height(260).MaxWidthPercent(100).Fit(ui.Contain).Margin(6, 0)
		}
	case *ast.FencedCodeBlock, *ast.CodeBlock:
		// One text of the block's lines: a text per line lays out and draws
		// each on its own, where one lays out their lines together.
		theme := 0
		if c.Theme().Dark {
			theme = 1
		}
		lang := ""
		if fenced, ok := block.(*ast.FencedCodeBlock); ok {
			lang = strings.TrimSpace(string(fenced.Language(content.source)))
		}
		ui.Column(c).Padding(12, 16).Gap(6).Radius(8).Background(k.track).Border(1, k.panelBorder.Alpha(0.7)).MinWidth(0).Children(func() {
			if lang != "" {
				ui.Text(c, lang).Font(defaultFontFamily).FontSize(11).TextColor(k.textFaint).Unselectable()
			}
			ui.RichText(c, content.codeSpans[n][theme]...).Font(defaultFontFamily).FontSize(12.5).
				FixedLineHeight(previewCodeLineHeight).TextColor(k.text).MinWidth(0)
		})
	case *ast.Blockquote:
		muted := *k
		muted.text = k.textMuted
		ui.Column(c).Padding(8, 14).Gap(8).Radius(4).Background(k.track.Alpha(0.45)).
			BorderWidth(0, 0, 0, 3).BorderColor(k.panelBorder).Children(func() {
			previewMarkdown(c, &muted, content, n)
		})
	case *ast.List:
		ui.Column(c).Gap(8).Children(func() {
			i := block.Start
			for item := n.FirstChild(); item != nil; item = item.NextSibling() {
				prefix := "•"
				if block.IsOrdered() {
					prefix = fmt.Sprintf("%d.", i)
				}
				i++
				ui.Row(c).Gap(8).AlignItems(ui.Start).Children(func() {
					ui.Text(c, prefix).FontSize(14.5).FixedLineHeight(24).TextColor(k.textMuted).MinWidth(22).Shrink(0)
					ui.Column(c).Gap(6).Grow(1).MinWidth(0).Children(func() { previewMarkdown(c, k, content, item) })
				})
			}
		})
	case *ast.ThematicBreak:
		ui.Box(c).Height(1).Background(k.panelBorder).Margin(14, 0)
	case *extast.Table:
		ui.Column(c).Border(1, k.panelBorder).Radius(6).Clip().Children(func() {
			bodyRow := 0
			for row := n.FirstChild(); row != nil; row = row.NextSibling() {
				_, header := row.(*extast.TableHeader)
				line := ui.Row(c).AlignItems(ui.Stretch)
				if header {
					line.Background(k.track)
				} else {
					if bodyRow%2 == 1 {
						line.Background(k.track.Alpha(0.35))
					}
					bodyRow++
				}
				line.Children(func() {
					for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
						weight := 400
						if header {
							weight = 600
						}
						ui.RichText(c, content.inline[cell]...).FontSize(13.5).FixedLineHeight(20).FontWeight(weight).TextColor(k.text).
							Grow(1).Basis(0).MinWidth(0).Padding(8, 12).Border(0.5, k.panelBorder)
					}
				})
			}
		})
	case *ast.HTMLBlock:
		// Raw HTML stays inert in the native preview.
		value := strings.TrimSpace(string(block.Lines().Value(content.source)))
		if value != "" {
			ui.Text(c, value).Font(defaultFontFamily).FontSize(12).TextColor(k.textMuted)
		}
	default:
		previewMarkdown(c, k, content, n)
	}
}
