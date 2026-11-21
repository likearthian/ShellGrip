package main

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// run alone with MYGO_DEV=1 and SHELLGRIP_PREVIEW_NATIVE_TEST=1. The tester
// doesn't attach MyGo's periodic dev saver, which caused the real hang.
func TestNativePreviewDevState(t *testing.T) {
	if os.Getenv("SHELLGRIP_PREVIEW_NATIVE_TEST") != "1" {
		t.Skip("opt-in native window; run alone with MYGO_DEV=1")
	}
	cache := t.TempDir()
	t.Setenv("LOCALAPPDATA", cache)
	t.Setenv("APPDATA", cache)
	t.Setenv("SHELLGRIP_DIR", cache)
	registerFonts()
	path := os.Getenv("SHELLGRIP_PREVIEW_TEST_FILE")
	if path == "" {
		t.Fatal("set SHELLGRIP_PREVIEW_TEST_FILE to a large source file")
	}
	content := loadPreview(path)
	if content.buffer == nil || !content.large {
		t.Fatal("expected a large text document")
	}
	p := &filePreview{path: path, content: content}
	pane := &Pane{}
	tab := &Tab{preview: p, Focus: pane, Root: &Node{Pane: pane}}
	pane.Tab = tab
	a := &App{tabs: []*Tab{tab}, fileTree: &fileTreeState{pane: pane}}
	updates := 0
	frames := 0
	var worstDelay time.Duration
	closed := make(chan struct{})
	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title: "ShellGrip preview Dev regression", Width: 1000, Height: 620,
			Content: ui.View(func(c *ui.Context) {
				frames++
				ui.Row(c).Fill().Children(func() {
					ui.Column(c).Width(220).Children(func() {
						for i := 0; i < 20; i++ {
							row := ui.Text(c, "File "+strconv.Itoa(i)).Height(24)
							a.fileTreeContextMenu(c, row, filepath.Dir(path), fileTreeRow{path: path})
						}
					})
					a.largeTextPreview(c, colorsOf(c), p)
				})
			}),
		})
		win.OnClosed(func() { close(closed) })
		go func() {
			// the first Dev snapshot can precede background syntax colors.
			time.Sleep(500 * time.Millisecond)
			content.highlightLargeText()
			win.Update(func() { p.content.ranges = content.ranges })
		}()
		go func() {
			for range 10 {
				time.Sleep(250 * time.Millisecond)
				posted := time.Now()
				win.Update(func() {
					updates++
					worstDelay = max(worstDelay, time.Since(posted))
				})
			}
			win.Close()
		}()
	})
	<-closed
	if updates != 10 {
		t.Fatalf("UI handled %d of 10 updates", updates)
	}
	if frames == 0 {
		t.Fatal("native window never rendered the preview")
	}
	if worstDelay > time.Second {
		t.Fatalf("Dev saving blocked UI updates for %v", worstDelay)
	}
	exe, _ := os.Executable()
	state := filepath.Join(cache, "mygo-dev-state", filepath.Base(exe)+"-1.json")
	info, err := os.Stat(state)
	if err != nil {
		t.Fatalf("Dev saver did not run: %v", err)
	}
	if info.Size() > 32<<10 {
		t.Fatalf("temporary menu state serialized the document: %d bytes", info.Size())
	}
	t.Logf("UI rendered %d frames; longest update delay: %v; Dev snapshot: %d bytes", frames, worstDelay, info.Size())
}
