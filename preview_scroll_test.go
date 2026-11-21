package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestPreviewWheelGlides(t *testing.T) {
	registerFonts()
	for _, kind := range []string{"code", "large code", "markdown", "large markdown"} {
		t.Run(kind, func(t *testing.T) {
			name, source := "source.go", strings.Repeat("// some source text\n", 200)
			switch kind {
			case "large code":
				source = strings.Repeat("// some source text\n", 4000)
			case "markdown", "large markdown":
				name = "notes.md"
				blocks := 30
				if kind == "large markdown" {
					blocks = 200
				}
				var doc strings.Builder
				for i := range blocks {
					fmt.Fprintf(&doc, "Body %d with words.\n\n", i)
				}
				source = doc.String()
			}
			path := previewFixture(t, t.TempDir(), name, []byte(source))
			p := &filePreview{path: path, content: loadPreview(path)}
			tab := &Tab{preview: p}
			a := &App{tabs: []*Tab{tab}}
			tt := ui.NewTester(a.view, 1000, 400)
			tt.Frame()
			position := func() float32 {
				if kind == "large markdown" {
					r, ok := tt.Find("Body 3 with words.")
					if !ok {
						t.Fatal("visible Markdown block is missing")
					}
					return -r.Y
				}
				return p.scroll.Y
			}
			start := position()
			tt.Wheel(500, 240, 0, 60)
			if position() != start {
				t.Fatal("wheel jumped the entire distance immediately")
			}
			tt.Advance(30 * time.Millisecond)
			mid := position() - start
			if mid <= 0 || mid >= 60 {
				t.Fatalf("expected an intermediate offset, got %v", mid)
			}
			tt.Advance(250 * time.Millisecond)
			if end := position() - start; end < 59 || end > 61 {
				t.Fatalf("wheel ended at %v, want 60", end)
			}
			at := position()
			tt.Scroll(500, 240, 0, 10)
			if got := position() - at; got < 9 || got > 11 {
				t.Fatalf("trackpad did not move directly: %v", got)
			}
			tt.SetPreferences(ui.Preferences{ReduceMotion: true})
			at = position()
			tt.Wheel(500, 240, 0, -10)
			if got := at - position(); got < 9 || got > 11 {
				t.Fatalf("reduced motion did not move directly: %v", got)
			}
		})
	}
}
