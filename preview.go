package main

import (
	"bytes"
	"fmt"
	"image"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/egoist/mygo/ui"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
	_ "golang.org/x/image/tiff"
	"golang.org/x/text/encoding/charmap"
)

const (
	maxPreviewText     = 2 << 20
	maxPreviewImage    = 32 << 20
	maxPreviewPixels   = 32_000_000
	maxRichPreviewText = 64 << 10
	maxRichPreviewLine = 4096
	previewChunkRunes  = 1024
	previewTabWidth    = 4
)

type filePreview struct {
	path                    string
	pane                    *Pane
	loading                 bool
	content                 previewContent
	initialized             bool
	scroll                  ui.ScrollState
	selection               [2]int
	copySource, copyDisplay string
}

type previewContent struct {
	kind       string
	message    string
	language   string
	source     []byte
	lines      [][]chroma.Token
	spans      [2][]ui.Span
	gutter     string
	document   ast.Node
	code       map[ast.Node][][]chroma.Token
	image      ui.ImageSource
	images     map[ast.Node]ui.ImageSource
	buffer     *ui.TextBuffer
	original   *ui.TextBuffer
	insertions []int
	large      bool
	lexer      chroma.Lexer
	ranges     [2][]ui.TextRange
}

func (a *App) openFilePreview(c *ui.Context, p *Pane, path string) {
	t := a.tab()
	if t == nil || t.Settings {
		return
	}
	preview := &filePreview{path: path, pane: p, loading: true}
	t.preview = preview
	a.focusReq = nil
	services := c.Services()
	go func() {
		content := loadPreview(path)
		initial := content
		a.post(func() {
			if t.preview == preview {
				preview.content, preview.loading = initial, false
			}
		})
		services.Invalidate()
		if content.buffer != nil && content.lexer != nil {
			content.highlightLargeText()
			ranges := content.ranges
			a.post(func() {
				if t.preview == preview {
					preview.content.ranges = ranges
				}
			})
			services.Invalidate()
		}
	}()
	c.Invalidate()
}

func (a *App) closeFilePreview(t *Tab) {
	t.preview = nil
	if a.tab() == t {
		a.focusReq = t.Focus
	}
}

func previewImageType(ext string) bool {
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".tif", ".tiff", ".svg":
		return true
	}
	return false
}

func previewText(data []byte) bool {
	if !utf8.Valid(data) || !strings.HasPrefix(http.DetectContentType(data), "text/") {
		return false
	}
	for _, r := range string(data) {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' && r != '\f' {
			return false
		}
	}
	return true
}

func decodePreviewText(data []byte) ([]byte, bool) {
	if !utf8.Valid(data) {
		// A UTF-16 BOM is not single-byte Windows text.
		if bytes.HasPrefix(data, []byte{0xff, 0xfe}) || bytes.HasPrefix(data, []byte{0xfe, 0xff}) {
			return nil, false
		}
		for _, b := range data {
			if charmap.Windows1252.DecodeByte(b) == utf8.RuneError {
				return nil, false
			}
		}
		var err error
		data, err = charmap.Windows1252.NewDecoder().Bytes(data)
		if err != nil {
			return nil, false
		}
	}
	return data, previewText(data)
}

func loadPreview(path string) previewContent {
	unsupported := previewContent{message: "unsupported content"}
	ext := strings.ToLower(filepath.Ext(path))
	isImage := previewImageType(ext)
	isMarkdown := ext == ".md" || ext == ".markdown" || ext == ".mdown"
	lexer := lexers.Match(filepath.Base(path))
	info, err := os.Stat(path)
	if err != nil {
		return previewContent{message: "Cannot read file: " + err.Error()}
	}
	if !info.Mode().IsRegular() {
		return unsupported
	}
	limit := int64(maxPreviewText)
	if isImage {
		limit = maxPreviewImage
	}
	if info.Size() > limit {
		return previewContent{message: "File is too large to preview"}
	}
	f, err := os.Open(path)
	if err != nil {
		return previewContent{message: "Cannot read file: " + err.Error()}
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return previewContent{message: "Cannot read file: " + err.Error()}
	}
	if int64(len(data)) > limit {
		return previewContent{message: "File is too large to preview"}
	}
	if isImage {
		var src ui.ImageSource
		if ext == ".svg" {
			src, err = ui.ParseSVG(data)
		} else {
			var config image.Config
			config, _, err = image.DecodeConfig(bytes.NewReader(data))
			if err == nil && (config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > maxPreviewPixels) {
				return previewContent{message: "Image is too large to preview"}
			}
			if err == nil {
				src, err = ui.DecodeBitmap(data)
			}
		}
		if err != nil {
			return previewContent{message: "Cannot display image: " + err.Error()}
		}
		return previewContent{kind: "image", image: src}
	}
	data, isText := decodePreviewText(data)
	if !isText {
		return unsupported
	}
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	if bytes.Count(data, []byte("\n")) > 20000 {
		return previewContent{message: "File has too many lines to preview"}
	}
	if isMarkdown {
		md := goldmark.New(goldmark.WithExtensions(extension.GFM))
		doc := md.Parser().Parse(text.NewReader(data))
		content := previewContent{kind: "markdown", source: data, document: doc, code: map[ast.Node][][]chroma.Token{}, images: map[ast.Node]ui.ImageSource{}}
		ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
			if !entering {
				return ast.WalkContinue, nil
			}
			switch block := n.(type) {
			case *ast.FencedCodeBlock:
				content.code[n] = previewTokens(string(block.Lines().Value(data)), lexers.Get(string(block.Language(data))))
			case *ast.CodeBlock:
				content.code[n] = previewTokens(string(block.Lines().Value(data)), nil)
			case *ast.Image:
				// Local illustrations are decoded with the document, off the UI thread.
				// Remote images remain labeled placeholders in this offline viewer.
				if len(content.images) < 8 {
					if link, parseErr := url.Parse(string(block.Destination)); parseErr == nil && link.Scheme == "" && link.Host == "" {
						imagePath := filepath.Join(filepath.Dir(path), filepath.FromSlash(link.Path))
						if previewImageType(strings.ToLower(filepath.Ext(imagePath))) {
							content.images[n] = loadPreview(imagePath).image
						}
					}
				}
			}
			return ast.WalkContinue, nil
		})
		return content
	}
	language := "Plain text"
	if lexer != nil {
		language = lexer.Config().Name
	}
	content := previewContent{kind: "code", language: language, source: data, large: largeTextPreview(data)}
	content.prepareCodeText()
	if content.large {
		content.lexer = lexer
		return content
	}
	content.lines = previewTokens(string(data), lexer)
	var gutter strings.Builder
	for i, line := range content.lines {
		if i > 0 {
			gutter.WriteByte('\n')
			for theme := range content.spans {
				content.spans[theme] = append(content.spans[theme], ui.Span{Text: "\n"})
			}
		}
		fmt.Fprint(&gutter, i+1)
		if len(line) > 0 {
			for theme := range content.spans {
				content.spans[theme] = append(content.spans[theme], previewCodeSpans(line, theme == 1)...)
			}
		}
	}
	content.gutter = gutter.String()
	return content
}

func largeTextPreview(data []byte) bool {
	if len(data) > maxRichPreviewText {
		return true
	}
	for len(data) > 0 {
		line, rest, found := bytes.Cut(data, []byte("\n"))
		if len(line) > maxRichPreviewLine {
			return true
		}
		if !found {
			break
		}
		data = rest
	}
	return false
}

// Expand tabs for display and bound paragraphs in the virtualized viewer.
// Extra display characters map back to the original source when copied.
func (content *previewContent) prepareCodeText() {
	source := string(content.source)
	content.original = ui.NewTextBuffer(source)
	var display strings.Builder
	display.Grow(len(source))
	at, column := 0, 0
	for _, r := range source {
		if content.large && column >= previewChunkRunes && r != '\n' {
			content.insertions = append(content.insertions, at)
			display.WriteByte('\n')
			at++
			column = 0
		}
		if r == '\t' {
			spaces := previewTabWidth - column%previewTabWidth
			for i := 0; i < spaces; i++ {
				if i > 0 {
					content.insertions = append(content.insertions, at)
				}
				display.WriteByte(' ')
				at++
				column++
			}
		} else {
			display.WriteRune(r)
			at++
			column++
		}
		if r == '\n' {
			column = 0
		}
	}
	content.buffer = content.original
	if display.String() != source {
		content.buffer = ui.NewTextBuffer(display.String())
	}
}

func (content *previewContent) selectedSource(start, end int) string {
	start -= sort.SearchInts(content.insertions, start)
	end -= sort.SearchInts(content.insertions, end)
	return content.original.Slice(start, end)
}

// Tokenize the original document, retaining lexer state across long-line
// display chunks (including HTML's embedded JavaScript and CSS).
func (content *previewContent) highlightLargeText() {
	if content.lexer == nil {
		return
	}
	content.ranges = [2][]ui.TextRange{}
	iterator, err := chroma.Coalesce(content.lexer).Tokenise(nil, string(content.source))
	if err != nil {
		return
	}
	stylesByTheme := [2]*chroma.Style{styles.Get("github"), styles.Get("dracula")}
	cache := [2]map[chroma.TokenType]ui.TextRange{{}, {}}
	position, remaining := 0, len(content.source)
	displayPosition := func(runeOffset int) int {
		return runeOffset + sort.Search(len(content.insertions), func(i int) bool { return content.insertions[i]-i >= runeOffset })
	}
	for token := iterator(); token != chroma.EOF; token = iterator() {
		if len(token.Value) > remaining {
			token.Value = token.Value[:remaining]
		}
		remaining -= len(token.Value)
		end := position + utf8.RuneCountInString(token.Value)
		if end > position && token.Type != chroma.Text && token.Type != chroma.Whitespace {
			for theme, style := range stylesByTheme {
				r, ok := cache[theme][token.Type]
				if !ok {
					entry := style.Get(token.Type)
					if entry.Colour.IsSet() {
						r.Color = ui.Hex(entry.Colour.String())
					}
					if entry.Bold == chroma.Yes {
						r.Weight = 700
					}
					cache[theme][token.Type] = r
				}
				r.Start, r.End = displayPosition(position), displayPosition(end)
				ranges := &content.ranges[theme]
				if n := len(*ranges); n > 0 && (*ranges)[n-1].End == r.Start && (*ranges)[n-1].Color == r.Color && (*ranges)[n-1].Weight == r.Weight {
					(*ranges)[n-1].End = r.End
				} else {
					*ranges = append(*ranges, r)
				}
			}
		}
		position = end
	}
}

func (a *App) largeTextPreview(c *ui.Context, k *colors, p *filePreview) {
	area := ui.TextAreaBufferBase(c.Key("large-file-text"), p.content.buffer).Label("File preview text").TrackScroll(&p.scroll).
		Grow(1).MinWidth(0).MinHeight(0).Padding(12, 14).Font(defaultFontFamily).
		FontSize(12).FixedLineHeight(21).TextColor(k.text).ReadOnly(true)
	theme := 0
	if c.Theme().Dark {
		theme = 1
	}
	area.TextRanges(p.content.ranges[theme]...)
	if !p.initialized {
		area.SetTextSelection(0, 0)
		p.initialized = true
	}
	a.previewCopyMapping(c, p, area)
}

func (a *App) previewCopyMapping(c *ui.Context, p *filePreview, area ui.Element) {
	if p.content.buffer == p.content.original {
		return
	}
	services := c.Services()
	// Native copy commands copy the display buffer. Correct only an
	// exact match for this selection, preserving the original source.
	start, end := area.TextSelection()
	if area.Focused() && end > start {
		if p.selection != [2]int{start, end} {
			p.selection = [2]int{start, end}
			p.copyDisplay = p.content.buffer.Slice(start, end)
			p.copySource = p.content.selectedSource(start, end)
		}
		area.Draw(func(_ *ui.Painter, _ ui.Rect) {
			if p.copyDisplay != p.copySource && services.ReadClipboard() == p.copyDisplay {
				services.WriteClipboard(p.copySource)
			}
		})
	}
}

func previewTokens(source string, lexer chroma.Lexer) [][]chroma.Token {
	if lexer == nil {
		lexer = lexers.Fallback
	}
	iterator, err := chroma.Coalesce(lexer).Tokenise(nil, source)
	if err != nil {
		iterator = chroma.Literator(chroma.Token{Type: chroma.Text, Value: source})
	}
	lines := [][]chroma.Token{{}}
	column := 0
	remaining := len(source)
	for token := iterator(); token != chroma.EOF; token = iterator() {
		// Some lexers append a newline for matching. Keep copied text exact.
		if len(token.Value) > remaining {
			token.Value = token.Value[:remaining]
		}
		remaining -= len(token.Value)
		for i, part := range strings.Split(token.Value, "\n") {
			if i > 0 {
				lines = append(lines, nil)
				column = 0
			}
			if part != "" {
				lines[len(lines)-1] = append(lines[len(lines)-1], chroma.Token{Type: token.Type, Value: previewExpandTabs(part, &column)})
			}
		}
	}
	return lines
}

func previewExpandTabs(value string, column *int) string {
	if !strings.ContainsRune(value, '\t') {
		*column += utf8.RuneCountInString(value)
		return value
	}
	var expanded strings.Builder
	for _, r := range value {
		if r == '\t' {
			spaces := previewTabWidth - *column%previewTabWidth
			expanded.WriteString(strings.Repeat(" ", spaces))
			*column += spaces
		} else {
			expanded.WriteRune(r)
			*column += 1
		}
	}
	return expanded.String()
}

func previewCodeSpans(tokens []chroma.Token, dark bool) []ui.Span {
	style := styles.Get("github")
	if dark {
		style = styles.Get("dracula")
	}
	spans := make([]ui.Span, 0, len(tokens))
	for _, token := range tokens {
		entry := style.Get(token.Type)
		span := ui.Span{Text: token.Value, Italic: entry.Italic == chroma.Yes}
		if entry.Colour.IsSet() {
			span.Color = ui.Hex(entry.Colour.String())
		}
		if entry.Bold == chroma.Yes {
			span.Weight = 700
		}
		spans = append(spans, span)
	}
	if len(spans) == 0 {
		spans = append(spans, ui.Span{Text: " "})
	}
	return spans
}

func (a *App) filePreviewPanel(c *ui.Context, k *colors, t *Tab) {
	p := t.preview
	if !a.paletteOpen && a.renaming == nil && c.Shortcut(0, ui.KeyEscape) {
		a.later(c, func() { a.closeFilePreview(t) })
	}
	ui.Column(c.Key(fmt.Sprintf("file-preview-%p", p))).Grow(1).MinHeight(0).MinWidth(0).Radius(cardR).Clip().
		Background(k.panel).Border(1, k.panelBorder).Children(func() {
		ui.Row(c).Height(headerH).Padding(0, 10).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icon("file-code")).Size(14, 14).TextColor(k.textMuted)
			ui.Text(c, filepath.Base(p.path)).FontSize(12.5).FontWeight(600).TextColor(k.text).SingleLine().Ellipsis("…").MinWidth(0).Grow(1).Tooltip(p.path)
			label := "Read only"
			if p.content.language != "" {
				label += " · " + p.content.language
			}
			ui.Text(c, label).FontSize(11).TextColor(k.textFaint).Shrink(0)
			if iconButton(c, k, "x", "Close File Preview", 26, 16).Tooltip("Back to shell · Esc").Clicked() {
				a.later(c, func() { a.closeFilePreview(t) })
			}
		})
		if p.loading {
			ui.Text(c, "Loading…").FontSize(13).TextColor(k.textMuted).Padding(20)
			return
		}
		if p.content.message != "" {
			ui.Box(c).Grow(1).MinHeight(0).Center().Padding(20).Children(func() {
				ui.Text(c, p.content.message).FontSize(13).TextColor(k.textMuted)
			})
			return
		}
		switch p.content.kind {
		case "image":
			ui.Image(c, p.content.image).Grow(1).MinHeight(0).MinWidth(0).Fit(ui.Contain).Label("Image preview " + filepath.Base(p.path))
		case "code":
			if p.content.large {
				a.largeTextPreview(c, k, p)
				return
			}
			theme := 0
			if c.Theme().Dark {
				theme = 1
			}
			ui.ScrollBoth(c).Grow(1).MinHeight(0).MinWidth(0).Padding(12, 14).Children(func() {
				ui.Row(c).Gap(14).AlignItems(ui.Start).Children(func() {
					ui.Text(c, p.content.gutter).MinWidth(44).Shrink(0).Font(defaultFontFamily).
						FontSize(12).FixedLineHeight(21).TextColor(k.textFaint).NoWrap().Unselectable()
					text := ui.RichText(c, p.content.spans[theme]...).Label("File preview text").
						Font(defaultFontFamily).FontSize(12).FixedLineHeight(21).TextColor(k.text).NoWrap().Selectable().Shrink(0)
					a.previewCopyMapping(c, p, text)
				})
			})
		case "markdown":
			ui.Scroll(c).Grow(1).MinHeight(0).Padding(24).Children(func() {
				ui.Column(c).Gap(10).MinWidth(0).Selectable().Children(func() {
					previewMarkdown(c, k, &p.content, p.content.document)
				})
			})
		}
	})
}
