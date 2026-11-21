package main

import (
	"bytes"
	"fmt"
	"image"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
	_ "golang.org/x/image/tiff"
	"golang.org/x/text/encoding/charmap"
)

const (
	maxPreviewText   = 2 << 20
	maxPreviewImage  = 32 << 20
	maxPreviewPixels = 32_000_000
	// maxRichPreviewText bounds what the rich code view draws as one text.
	// The text system keeps a layout only while it fits its budget, which a
	// rune of this one fills by about 150 bytes: from some 50 KB on, the
	// layout misses the cache and the preview lays the file out again on
	// every frame. Past it, the virtualized viewer draws only the page in
	// view, which scrolls at any length.
	maxRichPreviewText = 40 << 10
	maxRichPreviewLine = 4096
	// maxRichMarkdownBlocks bounds the blocks a Markdown preview renders as
	// one column: past it, a virtualized list keeps scrolling smooth.
	maxRichMarkdownBlocks = 400
	previewChunkRunes     = 1024
	previewTabWidth       = 4
)

type filePreview struct {
	remoteStamp    previewFileStamp
	remoteDigest   [32]byte
	remoteAgain    bool
	fileStamp      previewFileStamp
	nextFileCheck  time.Time
	checkingFile   bool
	reloadRevision int
	reloadBusy     bool
	reloadAgain    bool
	path           string
	// source names where a cached preview came from, when path is a copy:
	// a remote file downloaded for it.
	source      string
	pane        *Pane
	loading     bool
	diff        bool
	subtitle    string
	diffBar     bool
	diffChange  int
	diffDisplay *editor.DiffState
	content     previewContent
	markdown    ui.ListState
	initialized bool
	scroll      ui.ScrollState
	// codeBox and gutterBox pin the syntax text and its line numbers to the
	// size they took when the preview first laid them out. The text system
	// caches a layout only while it fits its budget, so the larger files of
	// the rich view would lay the same text out on every frame otherwise:
	// tens of milliseconds a scroll tick, not one.
	codeBox, gutterBox ui.Rect

	selection               [2]int
	copySource, copyDisplay string
}

type previewContent struct {
	kind     string
	message  string
	language string
	source   []byte
	lines    [][]chroma.Token
	spans    [2][]ui.Span
	gutter   string
	document ast.Node
	// blocks are the document's top-level ones, the rows of its preview.
	blocks []ast.Node
	// inline holds the spans of each block's text, and blockImages the local
	// images those words show, made once: a frame draws them as they are.
	inline      map[ast.Node][]ui.Span
	blockImages map[ast.Node][]ui.ImageSource
	code        map[ast.Node][][]chroma.Token
	// codeSpans are the highlighted lines of each code block, light and dark.
	codeSpans  map[ast.Node][2][]ui.Span
	image      ui.ImageSource
	images     map[ast.Node]ui.ImageSource
	buffer     *ui.TextBuffer
	original   *ui.TextBuffer
	insertions []int
	large      bool
	lexer      chroma.Lexer
	ranges     [2][]ui.TextRange
	diffLines  []diffLine
	diffPatch  *editor.FilePatch
}

// diffLine is one line of a unified diff and what it does.
type diffLine struct {
	text string
	kind byte
}

func (a *App) openFilePreview(c *ui.Context, p *Pane, path string) {
	a.showPreview(p, path, "")
	if c != nil {
		c.Invalidate()
	} else {
		a.invalidate()
	}
}

// showPreview opens path in the tab's preview, naming source when path is
// a cache of a remote file.
func (a *App) showPreview(p *Pane, path, source string, places ...terminalFilePlace) {
	t := a.tab()
	if len(places) > 0 && p.Tab != nil {
		t = p.Tab
	}
	if t == nil || t.Settings {
		return
	}
	preview := &filePreview{path: path, source: source, pane: p, loading: true}
	t.preview = preview
	a.focusReq = nil
	a.loadFilePreview(t, preview, places...)
}

func (a *App) loadFilePreview(t *Tab, preview *filePreview, places ...terminalFilePlace) {
	if preview.reloadBusy {
		preview.reloadAgain = true
		return
	}
	preview.reloadBusy = true
	preview.reloadRevision++
	revision := preview.reloadRevision
	go func() {
		defer func() {
			a.post(func() {
				preview.reloadBusy = false
				if preview.reloadAgain {
					preview.reloadAgain = false
					if t.preview == preview && !a.quitting && !a.exiting.Load() {
						a.loadFilePreview(t, preview)
					}
				}
			})
			a.invalidate()
		}()
		stamp := statPreviewFile(preview.path)
		content := loadPreview(preview.path)
		initial := content
		a.post(func() {
			if t.preview == preview && preview.reloadRevision == revision {
				preview.content, preview.loading = initial, false
				preview.fileStamp = stamp
				preview.initialized = false
				preview.codeBox, preview.gutterBox = ui.Rect{}, ui.Rect{}
				if len(places) > 0 && places[0].line > 0 {
					line := min(places[0].line, strings.Count(string(initial.source), "\n")+1)
					preview.scroll.Y = float32(max(0, line-3)) * 21
				}
			}
		})
		a.invalidate()
		if content.buffer != nil && content.lexer != nil {
			content.highlightLargeText()
			ranges := content.ranges
			a.post(func() {
				if t.preview == preview && preview.reloadRevision == revision {
					preview.content.ranges = ranges
				}
			})
			a.invalidate()
		}
	}()
}

// openDiffPreview shows what changed in a path instead of its content.
func (a *App) openDiffPreview(c *ui.Context, p *Pane, repo *gitRepo, file gitFile, staged bool) {
	t := a.tab()
	if t == nil || t.Settings {
		return
	}
	subtitle := "Changes"
	if staged {
		subtitle = "Staged changes"
	} else if file.Y == '?' {
		subtitle = "Untracked file"
	}
	preview := &filePreview{path: file.Path, pane: p, loading: true, diff: true, subtitle: subtitle}
	t.preview = preview
	a.focusReq = nil
	services := c.Services()
	go func() {
		content := loadDiff(p, repo, file, staged)
		a.post(func() {
			if t.preview == preview {
				preview.content, preview.loading = content, false
			}
		})
		services.Invalidate()
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
		content := previewContent{
			kind: "markdown", source: data, document: doc,
			code:        map[ast.Node][][]chroma.Token{},
			codeSpans:   map[ast.Node][2][]ui.Span{},
			images:      map[ast.Node]ui.ImageSource{},
			inline:      map[ast.Node][]ui.Span{},
			blockImages: map[ast.Node][]ui.ImageSource{},
		}
		for n := doc.FirstChild(); n != nil; n = n.NextSibling() {
			content.blocks = append(content.blocks, n)
		}
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
		// The spans of each block's words, the images they show, and the
		// colors of its code are made once: a frame draws a page of a long
		// document many times over, and need not walk the tree again.
		ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
			if !entering {
				return ast.WalkContinue, nil
			}
			switch n.(type) {
			case *ast.Heading, *ast.Paragraph, *ast.TextBlock, *extast.TableCell:
				content.inline[n] = previewInline(n, data, ui.Span{})
				ast.Walk(n, func(child ast.Node, entering bool) (ast.WalkStatus, error) {
					if entering {
						if src := content.images[child]; src != nil {
							content.blockImages[n] = append(content.blockImages[n], src)
						}
					}
					return ast.WalkContinue, nil
				})
			case *ast.FencedCodeBlock, *ast.CodeBlock:
				lines := content.code[n]
				content.codeSpans[n] = [2][]ui.Span{previewCodeLines(lines, false), previewCodeLines(lines, true)}
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

// loadDiff reads what changed in a path: the index against HEAD for a
// staged file, the work tree against the index for an unstaged one, and
// the work tree against nothing for an untracked one.
func loadDiff(p *Pane, repo *gitRepo, file gitFile, staged bool) previewContent {
	args := []string{"diff", "--no-color", "--no-ext-diff", "--", file.Path}
	switch {
	case staged:
		args = []string{"diff", "--cached", "--no-color", "--no-ext-diff", "--", file.Path}
	case file.Y == '?':
		args = []string{"diff", "--no-index", "--no-color", "--no-ext-diff", "--", "/dev/null", file.Path}
	}
	out, err := gitOutput(p, repo.Dir, args...)
	// --no-index exits with 1 when the files differ, which is the usual case.
	if err != nil && len(out) == 0 {
		return previewContent{kind: "diff", message: "Cannot read diff: " + err.Error()}
	}
	if len(out) > maxPreviewText {
		return previewContent{kind: "diff", message: "Diff is too large to preview"}
	}
	text := strings.TrimRight(string(out), "\n")
	if strings.TrimSpace(text) == "" {
		return previewContent{kind: "diff", message: "No changes"}
	}
	lines := strings.Split(text, "\n")
	if len(lines) > 20000 {
		return previewContent{kind: "diff", message: "Diff has too many lines to preview"}
	}
	content := previewContent{kind: "diff", source: out}
	for _, line := range lines {
		content.diffLines = append(content.diffLines, diffLine{text: line, kind: diffLineKind(line)})
	}
	// Keep Git's hunks rather than comparing the texts again in the viewer.
	patches, err := editor.ParsePatch(string(out))
	if err == nil && len(patches) == 1 && !patches[0].Binary && len(patches[0].Hunks) > 0 {
		content.diffPatch = &patches[0]
	}

	return content
}

// diffLineKind tells a diff line's role from its first characters.
func diffLineKind(line string) byte {
	switch {
	case strings.HasPrefix(line, "diff --git"), strings.HasPrefix(line, "index "),
		strings.HasPrefix(line, "new file"), strings.HasPrefix(line, "deleted file"),
		strings.HasPrefix(line, "old mode"), strings.HasPrefix(line, "new mode"),
		strings.HasPrefix(line, "similarity index"), strings.HasPrefix(line, "rename from"),
		strings.HasPrefix(line, "rename to"), strings.HasPrefix(line, "copy from"),
		strings.HasPrefix(line, "copy to"), strings.HasPrefix(line, "Binary files"),
		strings.HasPrefix(line, "--- "), strings.HasPrefix(line, "+++ "),
		strings.HasPrefix(line, "\\"):
		return 'm'
	case strings.HasPrefix(line, "@@"):
		return '@'
	case strings.HasPrefix(line, "+"):
		return '+'
	case strings.HasPrefix(line, "-"):
		return '-'
	}
	return ' '
}

// diffSpans colors a unified diff for the preview.
func diffSpans(lines []diffLine, k *colors, dark bool) []ui.Span {
	spans := make([]ui.Span, 0, len(lines)*2)
	for i, line := range lines {
		if i > 0 {
			spans = append(spans, ui.Span{Text: "\n"})
		}
		span := ui.Span{Text: line.text, Color: k.text}
		switch line.kind {
		case '+':
			span.Color, span.Background = diffAdded(dark)
		case '-':
			span.Color, span.Background = diffRemoved(dark)
		case '@':
			span.Color, span.Background = diffHunk(dark)
		case 'm':
			span.Color = k.textFaint
		}
		spans = append(spans, span)
	}
	return spans
}

func diffAdded(dark bool) (ui.Color, ui.Color) {
	if dark {
		return ui.Hex("#73c991"), ui.Hex("#73c991").Alpha(0.12)
	}
	return ui.Hex("#1a7f37"), ui.Hex("#1a7f37").Alpha(0.12)
}

func diffRemoved(dark bool) (ui.Color, ui.Color) {
	if dark {
		return ui.Hex("#f14c4c"), ui.Hex("#f14c4c").Alpha(0.12)
	}
	return ui.Hex("#cf222e"), ui.Hex("#cf222e").Alpha(0.12)
}

func diffHunk(dark bool) (ui.Color, ui.Color) {
	if dark {
		return ui.Hex("#7aa2f7"), ui.Hex("#3b9eff").Alpha(0.10)
	}
	return ui.Hex("#0969da"), ui.Hex("#0969da").Alpha(0.10)
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

// markdownPreview renders the document's blocks in the preview's scrolling
// area. A List builds only the blocks in view, so a long document costs what
// its visible page does, not what all of it does; a shorter one scrolls as
// one column, which lets Select All take all of it.
func (a *App) markdownPreview(c *ui.Context, k *colors, p *filePreview) {
	if len(p.content.blocks) <= maxRichMarkdownBlocks {
		ui.Scroll(c).TrackScroll(&p.scroll).Grow(1).MinHeight(0).Padding(24).Children(func() {
			ui.Column(c).Gap(10).MinWidth(0).Selectable().Children(func() {
				previewMarkdown(c, k, &p.content, p.content.document)
			})
		})
		return
	}
	ui.Column(c).Grow(1).MinHeight(0).MinWidth(0).Selectable().Children(func() {
		ui.List(c, &p.markdown, len(p.content.blocks), func(i int) {
			ui.Column(c).Gap(10).MinWidth(0).Children(func() {
				previewMarkdownBlock(c, k, &p.content, p.content.blocks[i])
			})
		}).Grow(1).MinHeight(0).MinWidth(0).Padding(24).Gap(10).Label("Markdown preview")
	})
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

// pinPreviewBox fixes an element at the size it took in the previous frame,
// once it has one. The syntax text and its line numbers are layouts the text
// system keeps only while both fit its budget together; left to measure
// themselves, the larger ones miss it on every frame, and scrolling pays
// their layout again each time. Their size never changes: they do not wrap.
func pinPreviewBox(e ui.Element, box *ui.Rect) {
	if box.W <= 0 {
		*box = e.Bounds()
	}
	if box.W > 0 {
		e.Width(box.W).Height(box.H)
	}
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
	name, source := filepath.Base(p.path), p.path
	if p.source != "" {
		name, source = path.Base(p.source), p.source
	}
	if !a.paletteOpen && a.renaming == nil && !p.diffBar && c.Shortcut(0, ui.KeyEscape) {
		a.later(c, func() { a.closeFilePreview(t) })
	}
	ui.Column(c.Key(fmt.Sprintf("file-preview-%p", p))).Grow(1).MinHeight(0).MinWidth(0).Radius(cardR).Clip().
		Background(k.panel).Border(1, k.panelBorder).Children(func() {
		ui.Row(c).Height(headerH).Padding(0, 10).Gap(8).AlignItems(ui.Center).Children(func() {
			glyph := "file-code"
			if p.diff {
				glyph = "diff"
			}
			ui.Icon(c, icon(glyph)).Size(14, 14).TextColor(k.textMuted)
			ui.Text(c, name).FontSize(12.5).FontWeight(600).TextColor(k.text).SingleLine().Ellipsis("…").MinWidth(0).Grow(1).Tooltip(source)
			label := "Read only"
			if p.source != "" {
				label = "Remote · Read only"
			}
			if p.diff {
				label = p.subtitle
			} else if p.content.language != "" {
				label += " · " + p.content.language
			}
			ui.Text(c, label).FontSize(11).TextColor(k.textFaint).Shrink(0)
			closeLabel := "Close File Preview"
			if p.diff {
				closeLabel = "Close Diff Preview"
			}
			if iconButton(c, k, "x", closeLabel, 26, 16).Tooltip("Back to shell · Esc").Clicked() {
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
			ui.Image(c, p.content.image).Grow(1).MinHeight(0).MinWidth(0).Fit(ui.Contain).Label("Image preview " + name)
		case "code":
			if p.content.large {
				a.largeTextPreview(c, k, p)
				return
			}
			theme := 0
			if c.Theme().Dark {
				theme = 1
			}
			ui.ScrollBoth(c).TrackScroll(&p.scroll).Grow(1).MinHeight(0).MinWidth(0).Padding(12, 14).Children(func() {
				ui.Row(c).Gap(14).AlignItems(ui.Start).Children(func() {
					gutter := ui.Text(c, p.content.gutter).MinWidth(44).Shrink(0).Font(defaultFontFamily).
						FontSize(12).FixedLineHeight(21).TextColor(k.textFaint).NoWrap().Unselectable()
					pinPreviewBox(gutter, &p.gutterBox)
					text := ui.RichText(c, p.content.spans[theme]...).Label("File preview text").
						Font(defaultFontFamily).FontSize(12).FixedLineHeight(21).TextColor(k.text).NoWrap().Selectable().Shrink(0)
					pinPreviewBox(text, &p.codeBox)
					a.previewCopyMapping(c, p, text)
				})
			})
		case "diff":
			if p.content.diffPatch != nil {
				a.sideBySideDiff(c, k, p, c.Theme().Dark)
				return
			}
			ui.ScrollBoth(c).Grow(1).MinHeight(0).MinWidth(0).Padding(12, 14).Children(func() {
				ui.RichText(c, diffSpans(p.content.diffLines, k, c.Theme().Dark)...).Label("Diff preview text").
					Font(defaultFontFamily).FontSize(12).FixedLineHeight(21).TextColor(k.text).NoWrap().Selectable()
			})
		case "markdown":
			a.markdownPreview(c, k, p)
		}
	})
}
