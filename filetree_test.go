package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestFileTreeResize(t *testing.T) {
	t.Setenv("GOREX_DIR", t.TempDir())
	previous := prefs
	t.Cleanup(func() { prefs = previous })
	prefs.FileTree, prefs.FileTreeWidth = true, 0
	p := &Pane{ID: 1, startDir: t.TempDir()}
	a := &App{tabs: []*Tab{{ID: 2, Focus: p, Root: &Node{Pane: p}}}}
	tt := ui.NewTester(a.view, 1000, 620)
	tt.Frame()
	divider, ok := tt.Find("Resize File Tree")
	if !ok || divider.H <= 0 || divider.W != gap {
		t.Fatalf("missing resize handle: %+v", divider)
	}
	x, y := divider.X+divider.W/2, divider.Y+divider.H/2
	tt.Press(x, y)
	tt.Frame()
	tt.Move(x+100, y)
	tt.Frame()
	tt.Release(x+100, y)
	tt.Frame()
	resized, _ := tt.Find("Resize File Tree")
	if prefs.FileTreeWidth != 340 || resized.X != divider.X+100 {
		t.Fatalf("drag did not resize sidebar: width %v, divider %+v", prefs.FileTreeWidth, resized)
	}
	data, err := os.ReadFile(settingsPath())
	if err != nil {
		t.Fatal(err)
	}
	var saved settings
	if err := json.Unmarshal(data, &saved); err != nil || saved.FileTreeWidth != 340 {
		t.Fatalf("sidebar width not saved: %s, %v", data, err)
	}
	a.toggleFileTree()
	tt.Frame()
	a.toggleFileTree()
	tt.Frame()
	reopened, _ := tt.Find("Resize File Tree")
	if reopened.X != resized.X {
		t.Fatal("sidebar width changed after toggling")
	}
	// Large saved widths must still leave space for a terminal in a small window.
	prefs.FileTreeWidth = 600
	small := ui.NewTester(a.view, 560, 340)
	small.Frame()
	small.Frame()
	if p.bounds.W < 280 {
		t.Fatalf("terminal too narrow: %+v", p.bounds)
	}
}

func TestReadFileTree(t *testing.T) {
	root := t.TempDir()
	folder := filepath.Join(root, "z-folder")
	if err := os.Mkdir(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(root, "b.txt"), filepath.Join(root, "A.txt"), filepath.Join(folder, "child.txt")} {
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	rows := readFileTree(root, nil)
	if len(rows) != 3 || rows[0].name != "z-folder" || rows[1].name != "A.txt" || rows[2].name != "b.txt" {
		t.Fatalf("unexpected collapsed tree: %+v", rows)
	}
	rows = readFileTree(root, map[string]bool{folder: true})
	if len(rows) != 4 || rows[1].name != "child.txt" || rows[1].depth != 1 {
		t.Fatalf("unexpected expanded tree: %+v", rows)
	}
	rows = readFileTree(filepath.Join(root, "missing"), nil)
	if len(rows) != 1 || !rows[0].message || rows[0].name != "Cannot read folder" {
		t.Fatalf("missing folder: %+v", rows)
	}
}

func TestFileTreeFollowsFocusAndDirectory(t *testing.T) {
	t.Setenv("GOREX_DIR", t.TempDir())
	previous := prefs
	t.Cleanup(func() { prefs = previous })
	prefs.FileTree = false
	first, second := t.TempDir(), t.TempDir()
	folder := filepath.Join(first, "nested")
	if err := os.Mkdir(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "child.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for dir, name := range map[string]string{first: "first.txt", second: "second.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	p := &Pane{ID: 1, startDir: first}
	tab := &Tab{ID: 2, Focus: p, Root: &Node{Pane: p}}
	a := &App{tabs: []*Tab{tab}}
	tt := ui.NewTester(a.view, 1000, 620)
	tt.Frame()
	if err := tt.Click("Toggle File Tree"); err != nil {
		t.Fatal(err)
	}
	loaded := func(name string) bool {
		return a.fileTree != nil && a.fileTree.loaded && strings.Contains(strings.Join(tt.Texts(), " "), name)
	}
	waitFor(t, tt, "first directory", func() bool { return loaded("first.txt") })
	if err := tt.Click("Toggle folder nested"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, tt, "expanded folder", func() bool { return loaded("child.txt") })
	if err := tt.Click("Toggle folder nested"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, tt, "collapsed folder", func() bool { return !loaded("child.txt") })
	p.startDir = second
	tt.Frame()
	waitFor(t, tt, "changed directory", func() bool { return loaded("second.txt") })
	other := &Pane{ID: 3, startDir: first}
	tab.Focus, tab.Root.Pane = other, other
	tt.Frame()
	waitFor(t, tt, "focused pane directory", func() bool { return loaded("first.txt") })
	if err := os.WriteFile(filepath.Join(first, "new.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	a.fileTree.next = time.Time{}
	tt.Frame()
	waitFor(t, tt, "refreshed files", func() bool { return loaded("new.txt") })
	if err := tt.Click("Hide File Tree"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if prefs.FileTree || a.fileTree != nil {
		t.Fatal("file tree did not hide")
	}
	data, err := os.ReadFile(settingsPath())
	if err != nil || strings.Contains(string(data), `"fileTree": true`) {
		t.Fatalf("hidden state was not saved: %s, %v", data, err)
	}
}

func TestWSLFileTreeDirectory(t *testing.T) {
	if !windowsProfiles {
		t.Skip("Windows WSL path mapping")
	}
	p := &Pane{profile: terminalProfile{ID: "wsl:Ubuntu", WSL: true}}
	if got := fileTreeDirectory(p, "/home/user"); got != `\\wsl.localhost\Ubuntu\home\user` {
		t.Fatalf("WSL directory = %q", got)
	}
}
