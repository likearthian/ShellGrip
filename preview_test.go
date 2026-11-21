package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/egoist/mygo/ui"
	"github.com/yuin/goldmark/ast"
)

func previewFixture(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func previewPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 40, 20))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var data bytes.Buffer
	if err := png.Encode(&data, img); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func TestLoadPreview(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		name, kind, message string
		data                []byte
	}{
		{name: "main.go", kind: "code", data: []byte("package main\n\nfunc main() { println(42) }\n")},
		{name: "data.json", kind: "code", data: []byte(`{"message":"hello"}`)},
		{name: "notes.txt", kind: "code", data: []byte("hello\r\nworld")},
		{name: "README.md", kind: "markdown", data: []byte("# Heading\n\n**bold** and *italic*.\n\n- item\n\n```go\npackage main\n```\n")},
		{name: "windows-1252.md", kind: "markdown", data: []byte("# ONVIF cameras\r\n\r\nDiscovery \x96 registration.\r\n")},
		{name: "windows-1252.xyzabc", kind: "code", data: []byte("Legacy \x93quoted\x94 text with caf\xe9")},
		{name: "photo.png", kind: "image", data: previewPNG(t)},
		{name: "icon.svg", kind: "image", data: []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="20" height="20"><rect width="20" height="20" fill="red"/></svg>`)},
		{name: "document.pdf", message: "unsupported content", data: []byte("%PDF-1.4")},
		{name: "binary.go", message: "unsupported content", data: []byte{0, 1, 2}},
		{name: "unknown.xyzabc", kind: "code", data: []byte("hello")},
		{name: "extensionless", kind: "code", data: []byte("plain text 世界\nsecond line")},
		{name: "unknown-binary.xyzabc", message: "unsupported content", data: []byte{0, 1, 2}},
		{name: "control-characters.xyzabc", message: "unsupported content", data: []byte{'a', 1, 'b'}},
		{name: "invalid-utf8.xyzabc", message: "unsupported content", data: []byte{0xff, 0xfe, 0xfd}},
		{name: "undefined-windows-1252.md", message: "unsupported content", data: []byte{'a', 0x81, 'b'}},
		{name: "large.txt", message: "File is too large to preview", data: bytes.Repeat([]byte("a"), maxPreviewText+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			content := loadPreview(previewFixture(t, dir, tc.name, tc.data))
			if content.kind != tc.kind || content.message != tc.message {
				t.Fatalf("preview = kind %q, message %q", content.kind, content.message)
			}
			if tc.kind == "image" && content.image == nil {
				t.Fatal("image was not decoded")
			}
			if tc.kind == "code" && len(content.lines) == 0 {
				t.Fatal("code has no lines")
			}
		})
	}
	missing := loadPreview(filepath.Join(dir, "missing.txt"))
	if !strings.HasPrefix(missing.message, "Cannot read file:") {
		t.Fatalf("missing file: %+v", missing)
	}
	broken := loadPreview(previewFixture(t, dir, "broken.png", []byte("bad image")))
	if !strings.HasPrefix(broken.message, "Cannot display image:") {
		t.Fatalf("broken image: %+v", broken)
	}
}

func TestPreviewLegacyWindowsText(t *testing.T) {
	data := []byte("# Cameras\r\n\r\nDiscovery \x96 registration, \x93caf\xe9\x94.\r\n")
	content := loadPreview(previewFixture(t, t.TempDir(), "notes.md", data))
	if content.kind != "markdown" || string(content.source) != "# Cameras\n\nDiscovery – registration, “café”.\n" {
		t.Fatalf("Windows Markdown was not decoded correctly: kind %q, text %q", content.kind, content.source)
	}
	utf8Data := []byte("# Cameras\n\nDiscovery – registration, “café”.\n")
	decoded, ok := decodePreviewText(utf8Data)
	if !ok || !bytes.Equal(decoded, utf8Data) {
		t.Fatal("UTF-8 text changed while decoding")
	}
}

func TestPreviewLocalMarkdownFile(t *testing.T) {
	path := os.Getenv("SHELLGRIP_PREVIEW_TEST_FILE")
	if path == "" {
		t.Skip("set SHELLGRIP_PREVIEW_TEST_FILE to verify a local Markdown document")
	}
	content := loadPreview(path)
	if content.kind != "markdown" || content.document == nil {
		t.Fatalf("Markdown preview failed: %s", content.message)
	}
	tab := &Tab{preview: &filePreview{path: path, content: content}}
	a := &App{tabs: []*Tab{tab}}
	tt := ui.NewTester(a.view, 1000, 620)
	tt.Frame()
	if strings.Contains(strings.Join(tt.Texts(), " "), "unsupported content") {
		t.Fatal("Markdown was shown as unsupported content")
	}
}

// TestLargeMarkdownVirtualizes checks that a long Markdown document builds
// only the blocks in view, lays them out as one column would, and scrolls to
// its end.
func TestLargeMarkdownVirtualizes(t *testing.T) {
	registerFonts()
	const sections = 400
	var b strings.Builder
	for i := range sections {
		fmt.Fprintf(&b, "## Section %d\n\nBody %d with words.\n", i, i)
	}
	path := previewFixture(t, t.TempDir(), "large.md", []byte(b.String()))
	content := loadPreview(path)
	if len(content.blocks) <= maxRichMarkdownBlocks {
		t.Fatalf("%d blocks were not over the limit of %d", len(content.blocks), maxRichMarkdownBlocks)
	}
	tab := &Tab{preview: &filePreview{path: path, content: content}}
	a := &App{tabs: []*Tab{tab}}
	tt := ui.NewTester(a.view, 1000, 620)
	tt.Frame()
	head, ok := tt.Find("Section 0")
	if !ok || head.H <= 0 {
		t.Fatalf("the first heading does not show: %+v", head)
	}
	body, ok := tt.Find("Body 0 with words.")
	if !ok || body.Y < head.Y+head.H {
		t.Fatalf("the paragraph of the first heading is not below it: %+v", body)
	}
	last := fmt.Sprintf("Section %d", sections-1)
	if _, ok := tt.Find(last); ok {
		t.Fatal("a document far longer than the view built its last block")
	}
	// The wheel reaches the blocks that were not built.
	tt.Move(500, 300)
	for range 300 {
		tt.Scroll(500, 300, 0, 600)
		tt.Frame()
	}
	if end, ok := tt.Find(last); !ok || end.H <= 0 {
		t.Fatal("scrolling to the end did not build the last block")
	}
}

func TestPreviewHighlightAndMarkdown(t *testing.T) {
	dir := t.TempDir()
	content := loadPreview(previewFixture(t, dir, "code.go", []byte("package main\n// comment\nvar answer = 42\n")))
	foundColour := false
	for _, line := range content.lines {
		for _, token := range line {
			if token.Type != chroma.Text && token.Type != chroma.Whitespace {
				foundColour = true
			}
		}
		for _, dark := range []bool{false, true} {
			if len(previewCodeSpans(line, dark)) == 0 {
				t.Fatal("missing highlighted spans")
			}
		}
	}
	if !foundColour {
		t.Fatal("Go code was not tokenized")
	}
	previewFixture(t, dir, "photo.png", previewPNG(t))
	markdown := loadPreview(previewFixture(t, dir, "notes.md", []byte("# Title\n\n**bold** and *italic* and `code` &amp; text\n\n![Photo](photo.png)\n\n```go\npackage main\n```\n")))
	if _, ok := markdown.document.FirstChild().(*ast.Heading); !ok || len(markdown.code) != 1 || len(markdown.images) != 1 {
		t.Fatal("Markdown structure, code blocks, or local images were not parsed")
	}
	paragraph := markdown.document.FirstChild().NextSibling()
	spans := previewInline(paragraph, markdown.source, ui.Span{})
	bold, italic, code, entity := false, false, false, false
	var rendered strings.Builder
	for _, span := range spans {
		rendered.WriteString(span.Text)
		bold = bold || span.Text == "bold" && span.Weight == 700
		italic = italic || span.Text == "italic" && span.Italic
		code = code || span.Text == "code" && span.Font != ""
	}
	entity = strings.Contains(rendered.String(), "& text")
	if !bold || !italic || !code || !entity {
		t.Fatalf("Markdown inline styles missing: %+v", spans)
	}
	// What a frame draws is the spans laid out with the document, not a copy
	// made as it draws.
	if cached := markdown.inline[paragraph]; !slices.Equal(cached, spans) {
		t.Fatalf("the cached spans of a paragraph differ: %+v", cached)
	}
	// A code block is one text of all its lines, spaced by its line height.
	fence := markdown.document.LastChild()
	if len(markdown.code[fence]) == 0 || len(markdown.codeSpans[fence][0]) == 0 || len(markdown.codeSpans[fence][1]) == 0 {
		t.Fatalf("a code block has no lines or spans: %+v", markdown.codeSpans[fence])
	}
	var block strings.Builder
	for _, span := range previewCodeLines([][]chroma.Token{{}, {{Type: chroma.Text, Value: "a"}}, {{Type: chroma.Text, Value: "b"}}}, false) {
		block.WriteString(span.Text)
	}
	if block.String() != "\na\nb" {
		t.Fatalf("code lines joined as %q", block.String())
	}
}

func TestFileTreePreviewAndReturnToShell(t *testing.T) {
	previous := prefs
	t.Cleanup(func() { prefs = previous })
	prefs.FileTree, prefs.FileTreeWidth = true, 240
	dir := t.TempDir()
	previewFixture(t, dir, "main.go", []byte("package main\nfunc main() {}\n"))
	previewFixture(t, dir, "notes.md", []byte("# Rendered heading\n\n**Strong text**\n\n| Name | Value |\n| --- | --- |\n| test | 42 |\n\n- item\n\n> quote\n\n```go\npackage main\n```\n"))
	previewFixture(t, dir, "image.png", previewPNG(t))
	previewFixture(t, dir, "document.pdf", []byte("%PDF-1.4"))
	p := &Pane{ID: 1, startDir: dir}
	tab := &Tab{ID: 2, Focus: p, Root: &Node{Pane: p}}
	a := &App{tabs: []*Tab{tab}}
	root := tab.Root
	tt := ui.NewTester(a.view, 1000, 620)
	tt.Frame()
	waitFor(t, tt, "file tree", func() bool { return a.fileTree != nil && a.fileTree.loaded })
	for _, name := range []string{"main.go", "notes.md", "image.png", "document.pdf"} {
		if err := tt.Click("Preview file " + name); err != nil {
			t.Fatal(err)
		}
		waitFor(t, tt, "file preview", func() bool { return tab.preview != nil && !tab.preview.loading })
		tt.Frame()
		if bounds, ok := tt.Find("Close File Preview"); !ok || bounds.W <= 0 || bounds.H <= 0 {
			t.Fatalf("preview not visible for %s", name)
		}
		if tab.Root != root || tab.Focus != p || tab.Zoom != nil {
			t.Fatal("preview changed the shell layout")
		}
		switch name {
		case "notes.md":
			texts := strings.Join(tt.Texts(), " ")
			if !strings.Contains(texts, "Rendered heading") || !strings.Contains(texts, "Strong text") || strings.Contains(texts, "**Strong text**") {
				t.Fatalf("Markdown not rendered: %s", texts)
			}
		case "image.png":
			if bounds, ok := tt.Find("Image preview image.png"); !ok || bounds.W <= 0 || bounds.H <= 0 {
				t.Fatal("image not visible")
			}
		case "document.pdf":
			if !strings.Contains(strings.Join(tt.Texts(), " "), "unsupported content") {
				t.Fatal("missing unsupported content message")
			}
		}
		if name == "notes.md" {
			if err := tt.Click("Close File Preview"); err != nil {
				t.Fatal(err)
			}
		} else {
			tt.Key(0, ui.KeyEscape)
		}
		tt.Frame()
		if tab.preview != nil || a.focusReq != p {
			t.Fatal("preview did not return focus to the shell")
		}
	}
}

func TestPreviewTextSelectionAndCopy(t *testing.T) {
	registerFonts()
	mods := ui.Ctrl
	if runtime.GOOS == "darwin" {
		mods = ui.Cmd
	}
	for _, name := range []string{"main.go", "unknown.xyzabc"} {
		t.Run(name, func(t *testing.T) {
			const source = "package main\n\nfunc main() {\n\tprintln(\"hello 世界\")\n}"
			path := previewFixture(t, t.TempDir(), name, []byte(source))
			content := loadPreview(path)
			tab := &Tab{preview: &filePreview{path: path, content: content}}
			a := &App{tabs: []*Tab{tab}}
			tt := ui.NewTester(a.view, 1000, 620)
			tt.Frame()
			text, ok := tt.Find("File preview text")
			if !ok || text.H <= 0 || text.W <= 0 {
				t.Fatalf("preview text not visible: %+v", text)
			}
			// Drag across several lines, including a blank line.
			tt.Press(text.X+1, text.Y+10)
			tt.Move(text.X+30, text.Y+52)
			tt.Release(text.X+30, text.Y+52)
			tt.Key(mods, ui.KeyC)
			if got := tt.Clipboard(); !strings.HasPrefix(got, "package main\n\nfunc") {
				t.Fatalf("mouse selection copied %q", got)
			}
			// Select All includes the full document and preserves tabs and Unicode.
			tt.Key(mods, ui.KeyA)
			tt.Key(mods, ui.KeyC)
			if got := tt.Clipboard(); got != source {
				t.Fatalf("Select All copied %q, want %q", got, source)
			}
			tt.RightClickAt(text.X+20, text.Y+10)
			if err := tt.ChooseMenuItem("Copy"); err != nil {
				t.Fatal(err)
			}
			if got := tt.Clipboard(); got != source {
				t.Fatalf("context menu copied %q", got)
			}
			tt.Type("replacement")
			tt.Key(mods, ui.KeyC)
			if tt.Clipboard() != source {
				t.Fatal("read-only preview was edited")
			}
		})
	}
}

func TestLargePreviewSelectionAndCopy(t *testing.T) {
	registerFonts()
	mods := ui.Ctrl
	if runtime.GOOS == "darwin" {
		mods = ui.Cmd
	}
	for _, tc := range []struct{ name, source string }{
		{"formatted.html", strings.Repeat("<p>hello 世界</p>\n", 5000)},
		{"minified.html", strings.Repeat("<p>hello 世界</p>", 5000)},
		{"long-line.html", strings.Repeat("abc😀", 3000)},
		{"tabbed.go", strings.Repeat("func main() {\n\tprintln(\"hello 世界\")\n}\n", 2500)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := previewFixture(t, t.TempDir(), tc.name, []byte(tc.source))
			content := loadPreview(path)
			if content.buffer == nil || len(content.spans[0]) != 0 || len(content.spans[1]) != 0 {
				t.Fatal("large file did not use the virtualized viewer")
			}
			if content.selectedSource(0, content.buffer.Len()) != tc.source {
				t.Fatal("display-to-source mapping changed the document")
			}
			if strings.Contains(tc.source, "\n") && !strings.ContainsRune(tc.source, '\t') && len(content.insertions) != 0 {
				t.Fatal("ordinary paragraphs were unnecessarily split")
			}
			for _, at := range content.insertions {
				if content.selectedSource(at, at+1) != "" {
					t.Fatal("display-only character was included in copied text")
				}
			}
			tab := &Tab{preview: &filePreview{path: path, content: content}}
			a := &App{tabs: []*Tab{tab}}
			tt := ui.NewTester(a.view, 1000, 620)
			tt.Frame()
			text, ok := tt.Find("File preview text")
			if !ok || text.H <= 0 || text.W <= 0 {
				t.Fatalf("large preview not visible: %+v", text)
			}
			if tab.preview.scroll.Y != 0 {
				t.Fatal("large file did not open at the beginning")
			}
			tt.Press(text.X+15, text.Y+22)
			tt.Move(text.X+70, text.Y+64)
			tt.Release(text.X+70, text.Y+64)
			tt.Key(mods, ui.KeyC)
			if got := tt.Clipboard(); got == "" || !strings.Contains(tc.source, got) {
				t.Fatal("mouse selection did not copy original source text")
			}
			selection := tt.Clipboard()
			// Background colors must not reset the document or its selection.
			colored := content
			colored.highlightLargeText()
			tab.preview.content.ranges = colored.ranges
			tt.Frame()
			tt.Frame()
			tt.SetClipboard("")
			tt.Key(mods, ui.KeyC)
			if tt.Clipboard() != selection {
				t.Fatal("selection was lost after redrawing the preview")
			}
			tt.Key(mods, ui.KeyA)
			tt.Key(mods, ui.KeyC)
			if tt.Clipboard() != tc.source {
				t.Fatal("Select All did not copy the entire source exactly")
			}
			tt.SetClipboard("sentinel")
			tt.Command("copy")
			if tt.Clipboard() != tc.source {
				t.Fatal("Edit menu Copy did not preserve source text")
			}
			tt.RightClickAt(text.X+30, text.Y+22)
			if err := tt.ChooseMenuItem("Copy"); err != nil {
				t.Fatal(err)
			}
			if tt.Clipboard() != tc.source {
				t.Fatal("context menu Copy did not preserve source text")
			}
			version := content.buffer.Version()
			tt.Type("replacement")
			if content.buffer.Version() != version || content.original.String() != tc.source {
				t.Fatal("large-file preview was edited")
			}
		})
	}
}

func TestLargeHTMLPreviewColors(t *testing.T) {
	registerFonts()
	previous := prefs
	t.Cleanup(func() { prefs = previous })
	prefs.FileTree = true
	dir := t.TempDir()
	source := strings.Repeat(`<section class="item"><p>hello 世界</p></section>`, 6500)
	previewFixture(t, dir, "large.html", []byte(source))
	pane := &Pane{ID: 1, startDir: dir}
	tab := &Tab{ID: 2, Focus: pane, Root: &Node{Pane: pane}}
	a := &App{tabs: []*Tab{tab}}
	tt := ui.NewTester(a.view, 1000, 620)
	waitFor(t, tt, "file tree", func() bool { return a.fileTree != nil && a.fileTree.loaded })
	if err := tt.Click("Preview file large.html"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, tt, "background HTML colors", func() bool {
		return tab.preview != nil && len(tab.preview.content.ranges[0]) > 0 && len(tab.preview.content.ranges[1]) > 0
	})
	content := &tab.preview.content
	if content.language != "HTML" || content.buffer == nil {
		t.Fatal("large HTML lost its language or virtualized buffer")
	}
	for theme, name := range []string{"github", "dracula"} {
		tt.SetDark(theme == 1)
		tt.Frame()
		want := ui.Hex(styles.Get(name).Get(chroma.NameTag).Colour.String())
		bounds, _ := tt.Find("File preview text")
		shot := tt.Image()
		colored := false
		for y := int(bounds.Y); y < min(int(bounds.Y+bounds.H), int(bounds.Y)+100); y++ {
			for x := int(bounds.X); x < int(bounds.X+bounds.W); x++ {
				pixel := shot.RGBAAt(x, y)
				if pixel.R == want.R && pixel.G == want.G && pixel.B == want.B {
					colored = true
				}
			}
		}
		if !colored {
			t.Fatalf("HTML tag colors were not painted in %s", name)
		}
		last := 0
		for _, r := range content.ranges[theme] {
			if r.Start < last || r.End <= r.Start || r.End > content.buffer.Len() {
				t.Fatalf("invalid highlighted range: %+v", r)
			}
			last = r.End
		}
	}
	unknown := loadPreview(previewFixture(t, dir, "unknown.xyzabc", []byte(strings.Repeat("plain text\n", 10000))))
	unknown.highlightLargeText()
	if unknown.language != "Plain text" || len(unknown.ranges[0]) != 0 || len(unknown.ranges[1]) != 0 {
		t.Fatal("unknown text file was incorrectly syntax highlighted")
	}
}

func TestPreviewIndentation(t *testing.T) {
	registerFonts()
	for _, large := range []bool{false, true} {
		source := "X\n    X\n\tX\n  \tX\n"
		if large {
			source += strings.Repeat("padding\n", 10000)
		}
		path := previewFixture(t, t.TempDir(), "indent.txt", []byte(source))
		content := loadPreview(path)
		tab := &Tab{preview: &filePreview{path: path, content: content}}
		a := &App{tabs: []*Tab{tab}}
		tt := ui.NewTester(a.view, 1000, 620)
		tt.Frame()
		r, _ := tt.Find("File preview text")
		top := int(r.Y)
		if large {
			top += 12
		}
		shot := tt.Image()
		starts := [4]int{}
		for line := range starts {
			starts[line] = -1
			for x := int(r.X); x < min(int(r.X+r.W), int(r.X)+200); x++ {
				for y := top + line*21; y < top+(line+1)*21; y++ {
					p := shot.RGBAAt(x, y)
					if p.R < 160 && p.G < 160 && p.B < 160 {
						starts[line] = x
						break
					}
				}
				if starts[line] >= 0 {
					break
				}
			}
		}
		if starts[0] < 0 || starts[1] < starts[0]+24 || starts[2] < starts[0]+24 || starts[3] < starts[0]+24 {
			t.Errorf("large=%v: indentation was not painted, glyph starts %v", large, starts)
		}
		for _, start := range starts[2:] {
			if start < starts[1]-1 || start > starts[1]+1 {
				t.Errorf("large=%v: tabs did not align with four spaces: %v", large, starts)
			}
		}
	}
}

func TestPreviewTabStopsAndSourceMapping(t *testing.T) {
	for _, tc := range []struct{ source, display string }{
		{"\tX", "    X"},
		{" \tX", "    X"},
		{"   \tX", "    X"},
		{"    \tX", "        X"},
		{"\t\tX", "        X"},
		{"\t世界\tX", "    世界  X"},
		{"    X\n\tX\n  \tX", "    X\n    X\n    X"},
	} {
		content := previewContent{source: []byte(tc.source)}
		content.prepareCodeText()
		if got := content.buffer.String(); got != tc.display {
			t.Errorf("display of %q = %q, want %q", tc.source, got, tc.display)
		}
		if got := content.selectedSource(0, content.buffer.Len()); got != tc.source {
			t.Errorf("copied %q, want %q", got, tc.source)
		}
		var rendered strings.Builder
		for i, line := range previewTokens(tc.source, nil) {
			if i > 0 {
				rendered.WriteByte('\n')
			}
			for _, token := range line {
				rendered.WriteString(token.Value)
			}
		}
		if rendered.String() != tc.display {
			t.Errorf("highlighted display of %q = %q, want %q", tc.source, rendered.String(), tc.display)
		}
	}
}
