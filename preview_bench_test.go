package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func benchmarkHTML(b *testing.B, minified bool, repetitions int) string {
	b.Helper()
	line := `<section class="item"><h2>Example</h2><p>Some content &amp; more text.</p></section>`
	if !minified {
		line += "\n"
	}
	path := filepath.Join(b.TempDir(), "large.html")
	if err := os.WriteFile(path, []byte(strings.Repeat(line, repetitions)), 0o600); err != nil {
		b.Fatal(err)
	}
	return path
}

func BenchmarkPreviewHTMLLoad(b *testing.B) {
	path := benchmarkHTML(b, false, 2000)
	b.ResetTimer()
	for b.Loop() {
		loadPreview(path)
	}
}

func benchmarkPreviewHTMLFrame(b *testing.B, minified bool, repetitions int) {
	registerFonts()
	path := benchmarkHTML(b, minified, repetitions)
	content := loadPreview(path)
	content.highlightLargeText()
	b.ResetTimer()
	for b.Loop() {
		tab := &Tab{preview: &filePreview{path: path, content: content}}
		a := &App{tabs: []*Tab{tab}}
		tt := ui.NewTester(a.view, 1000, 620)
		tt.Frame()
	}
}

func BenchmarkPreviewHTMLFirstFrame(b *testing.B)         { benchmarkPreviewHTMLFrame(b, false, 2000) }
func BenchmarkPreviewMinifiedHTMLFirstFrame(b *testing.B) { benchmarkPreviewHTMLFrame(b, true, 2000) }
func BenchmarkPreviewNearLimitMinifiedHTMLFirstFrame(b *testing.B) {
	benchmarkPreviewHTMLFrame(b, true, 24000)
}

// BenchmarkPreviewHTMLScrollFrame scrolls a file of the largest size the rich
// view draws as one text. That text's layout is the whole cost of the frame,
// and the text system drops it when it does not fit its cache: pinned to the
// size it took, a frame is a few milliseconds, unpinned it is a hundred.
func BenchmarkPreviewHTMLScrollFrame(b *testing.B) {
	registerFonts()
	path := benchmarkHTML(b, false, 400)
	content := loadPreview(path)
	tab := &Tab{preview: &filePreview{path: path, content: content}}
	a := &App{tabs: []*Tab{tab}}
	tt := ui.NewTester(a.view, 1000, 620)
	tt.Frame()
	tt.Move(500, 300)
	// The frame after the first measures the text, and the one after pins it.
	tt.Scroll(500, 300, 0, 40)
	b.ResetTimer()
	for b.Loop() {
		tt.Scroll(500, 300, 0, 40)
	}
}

// benchmarkMarkdown writes a long Markdown document, of a heading, a
// paragraph, and a list item each section, for the virtualized preview.
func benchmarkMarkdown(b *testing.B, sections int) string {
	b.Helper()
	var text strings.Builder
	for i := range sections {
		fmt.Fprintf(&text, "## Section %d\n\nSome **bold** text with `code` and a [link](https://example.com).\n- item %d\n", i, i)
	}
	path := filepath.Join(b.TempDir(), "large.md")
	if err := os.WriteFile(path, []byte(text.String()), 0o600); err != nil {
		b.Fatal(err)
	}
	return path
}

func BenchmarkPreviewMarkdownFirstFrame(b *testing.B) {
	registerFonts()
	path := benchmarkMarkdown(b, 2000)
	content := loadPreview(path)
	b.ResetTimer()
	for b.Loop() {
		tab := &Tab{preview: &filePreview{path: path, content: content}}
		a := &App{tabs: []*Tab{tab}}
		tt := ui.NewTester(a.view, 1000, 620)
		tt.Frame()
	}
}

func BenchmarkPreviewMarkdownScrollFrame(b *testing.B) {
	registerFonts()
	path := benchmarkMarkdown(b, 2000)
	content := loadPreview(path)
	tab := &Tab{preview: &filePreview{path: path, content: content}}
	a := &App{tabs: []*Tab{tab}}
	tt := ui.NewTester(a.view, 1000, 620)
	tt.Frame()
	tt.Move(500, 300)
	b.ResetTimer()
	for b.Loop() {
		tt.Scroll(500, 300, 0, 120)
	}
}
