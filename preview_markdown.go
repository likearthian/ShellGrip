package main

import (
	"fmt"
	"html"
	"strings"

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

func previewMarkdown(c *ui.Context, k *colors, content *previewContent, parent ast.Node) {
	for n := parent.FirstChild(); n != nil; n = n.NextSibling() {
		switch block := n.(type) {
		case *ast.Heading:
			size := float32(30 - (block.Level-1)*3)
			ui.RichText(c, previewInline(n, content.source, ui.Span{})...).FontSize(size).FontWeight(700).TextColor(k.text).Margin(8, 0, 4, 0)
		case *ast.Paragraph, *ast.TextBlock:
			ui.RichText(c, previewInline(n, content.source, ui.Span{})...).FontSize(14).TextColor(k.text).MinWidth(0)
			ast.Walk(n, func(child ast.Node, entering bool) (ast.WalkStatus, error) {
				if entering {
					if src := content.images[child]; src != nil {
						ui.Image(c, src).Height(260).MaxWidthPercent(100).Fit(ui.Contain)
					}
				}
				return ast.WalkContinue, nil
			})
		case *ast.FencedCodeBlock, *ast.CodeBlock:
			ui.Column(c).Padding(12).Radius(8).Background(k.track).Gap(2).MinWidth(0).Children(func() {
				for _, line := range content.code[n] {
					ui.RichText(c, previewCodeSpans(line, c.Theme().Dark)...).Font(defaultFontFamily).FontSize(12).TextColor(k.text).MinHeight(18)
				}
			})
		case *ast.Blockquote:
			ui.Column(c).Padding(4, 12).Gap(8).BorderWidth(0, 0, 0, 3).BorderColor(k.panelBorder).Children(func() {
				previewMarkdown(c, k, content, n)
			})
		case *ast.List:
			ui.Column(c).Gap(6).Children(func() {
				i := block.Start
				for item := n.FirstChild(); item != nil; item = item.NextSibling() {
					prefix := "•"
					if block.IsOrdered() {
						prefix = fmt.Sprintf("%d.", i)
					}
					i++
					ui.Row(c).Gap(8).AlignItems(ui.Start).Children(func() {
						ui.Text(c, prefix).FontSize(14).TextColor(k.textMuted).MinWidth(20).Shrink(0)
						ui.Column(c).Gap(6).Grow(1).MinWidth(0).Children(func() { previewMarkdown(c, k, content, item) })
					})
				}
			})
		case *ast.ThematicBreak:
			ui.Box(c).Height(1).Background(k.panelBorder).Margin(8, 0)
		case *extast.Table:
			ui.Column(c).Border(1, k.panelBorder).Radius(6).Clip().Children(func() {
				for row := n.FirstChild(); row != nil; row = row.NextSibling() {
					_, header := row.(*extast.TableHeader)
					line := ui.Row(c).AlignItems(ui.Stretch)
					if header {
						line.Background(k.track)
					}
					line.Children(func() {
						for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
							weight := 400
							if header {
								weight = 600
							}
							ui.RichText(c, previewInline(cell, content.source, ui.Span{})...).FontSize(13).FontWeight(weight).TextColor(k.text).
								Grow(1).Basis(0).MinWidth(0).Padding(8).Border(0.5, k.panelBorder)
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
}
