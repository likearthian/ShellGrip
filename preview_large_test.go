package main

import (
	"bytes"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// embedded images make HTML lines much longer than the visible source.
func TestPreviewFullLargeHTML(t *testing.T) {
	source := "<!doctype html>\n<img src=\"data:image/png;base64," + strings.Repeat("AAAA", 110000) + "\">\n" +
		strings.Repeat("<p>hello 世界 😀</p>\n", 18000) + "<!-- end of document -->"
	path := previewFixture(t, t.TempDir(), "large.html", []byte(source))
	checkFullLargePreview(t, path)
}

func TestPreviewLargeLocalDocument(t *testing.T) {
	path := os.Getenv("SHELLGRIP_PREVIEW_TEST_FILE")
	if path == "" {
		t.Skip("set SHELLGRIP_PREVIEW_TEST_FILE to test a local document")
	}
	checkFullLargePreview(t, path)
}

func checkFullLargePreview(t *testing.T, path string) {
	t.Helper()
	registerFonts()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	start := time.Now()
	content := loadPreview(path)
	if content.message != "" || content.buffer == nil || !content.large {
		t.Fatalf("expected virtualized text: %s", content.message)
	}
	if !bytes.Equal(content.source, data) || content.selectedSource(0, content.buffer.Len()) != string(data) {
		t.Fatal("large preview did not preserve the entire document")
	}
	for _, line := range strings.Split(content.buffer.String(), "\n") {
		if len([]rune(line)) > previewChunkRunes+previewTabWidth {
			t.Fatal("unbounded paragraph reached the text layout")
		}
	}
	t.Log("load", time.Since(start))
	p := &filePreview{path: path, content: content}
	a := &App{tabs: []*Tab{{preview: p}}}
	tt := ui.NewTester(a.view, 1000, 620)
	start = time.Now()
	tt.Frame()
	t.Log("first frame", time.Since(start))
	start = time.Now()
	content.highlightLargeText()
	t.Log("highlight", time.Since(start))
	if content.lexer != nil && len(content.ranges[0]) == 0 {
		t.Fatal("large document lost syntax colors")
	}
	p.content.ranges = content.ranges
	for i := 0; i < 5; i++ {
		start = time.Now()
		tt.Scroll(500, 300, 0, 5000)
		tt.Frame()
		t.Log("colored scroll", time.Since(start))
	}
	mods := ui.Ctrl
	if runtime.GOOS == "darwin" {
		mods = ui.Cmd
	}
	r, ok := tt.Find("File preview text")
	if !ok {
		t.Fatal("preview text is missing")
	}
	tt.ClickAt(r.X+20, r.Y+20)
	tt.Key(mods, ui.KeyEnd)
	tt.Key(mods, ui.KeyA)
	tt.Key(mods, ui.KeyC)
	if tt.Clipboard() != string(data) {
		t.Fatal("Select All did not copy the full document")
	}
}
