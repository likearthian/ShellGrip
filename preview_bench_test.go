package main

import (
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
