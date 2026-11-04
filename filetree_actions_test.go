package main

import (
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestFileTreeContextMenus(t *testing.T) {
	previous := prefs
	t.Cleanup(func() { prefs = previous })
	prefs.FileTree, prefs.FileTreeWidth = true, 240
	root := t.TempDir()
	folder := filepath.Join(root, "nested folder")
	if err := os.Mkdir(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	page := previewFixture(t, folder, "page #1 & 20%.HTML", []byte("<h1>Hello</h1>"))
	previewFixture(t, root, "notes.txt", []byte("hello"))
	p := &Pane{ID: 1, startDir: root}
	tab := &Tab{ID: 2, Focus: p, Root: &Node{Pane: p}}
	a := &App{tabs: []*Tab{tab}}
	tt := ui.NewTester(a.view, 1000, 620)
	waitFor(t, tt, "file tree", func() bool { return a.fileTree != nil && a.fileTree.loaded })
	if err := tt.RightClick("Preview file notes.txt"); err != nil {
		t.Fatal(err)
	}
	if got := fileTreePopupLabels(tt); !slices.Equal(got, []string{"Copy Full Path", "Copy Relative Path"}) {
		t.Fatalf("text file menu: %q", got)
	}
	if tab.preview != nil {
		t.Fatal("right-clicking opened a file preview")
	}
	if err := tt.Click("Copy Full Path"); err != nil {
		t.Fatal(err)
	}
	if tt.Clipboard() != filepath.Join(root, "notes.txt") {
		t.Fatalf("full path copied %q", tt.Clipboard())
	}
	if err := tt.RightClick("Preview file notes.txt"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Copy Relative Path"); err != nil {
		t.Fatal(err)
	}
	if tt.Clipboard() != "notes.txt" {
		t.Fatalf("relative path copied %q", tt.Clipboard())
	}
	if err := tt.RightClick("Toggle folder nested folder"); err != nil {
		t.Fatal(err)
	}
	if a.fileTree.expanded[folder] {
		t.Fatal("right-clicking expanded a folder")
	}
	if err := tt.Click("Copy Full Path"); err != nil {
		t.Fatal(err)
	}
	if tt.Clipboard() != folder {
		t.Fatalf("folder path copied %q", tt.Clipboard())
	}
	if err := tt.Click("Toggle folder nested folder"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, tt, "expanded folder", func() bool { _, ok := tt.Find("Preview file page #1 & 20%.HTML"); return ok })
	if err := tt.RightClick("Preview file page #1 & 20%.HTML"); err != nil {
		t.Fatal(err)
	}
	if got := fileTreePopupLabels(tt); !slices.Equal(got, []string{"Copy Full Path", "Copy Relative Path", "Open in Default Browser"}) {
		t.Fatalf("HTML file menu: %q", got)
	}
	if err := tt.Click("Open in Default Browser"); err != nil {
		t.Fatal(err)
	}
	wantURL, err := fileTreeURL(page)
	if err != nil || !slices.Equal(tt.OpenedURLs(), []string{wantURL}) {
		t.Fatalf("opened %q, want %q: %v", tt.OpenedURLs(), wantURL, err)
	}
	if tab.preview != nil {
		t.Fatal("browser action opened an internal preview")
	}
	if err := tt.RightClick("Preview file page #1 & 20%.HTML"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Copy Relative Path"); err != nil {
		t.Fatal(err)
	}
	if tt.Clipboard() != filepath.Join("nested folder", filepath.Base(page)) {
		t.Fatalf("nested relative path copied %q", tt.Clipboard())
	}
	if err := tt.RightClick("File Tree Directory"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Copy Relative Path"); err != nil {
		t.Fatal(err)
	}
	if tt.Clipboard() != "." {
		t.Fatalf("root relative path copied %q", tt.Clipboard())
	}
	if err := tt.Click("Preview file notes.txt"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, tt, "file preview", func() bool { return tab.preview != nil && !tab.preview.loading })
	if err := tt.RightClick("Toggle folder nested folder"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyEscape)
	if tab.preview == nil {
		t.Fatal("dismissing the file-tree menu also closed the file preview")
	}
}

func TestFileTreeVSCodeMenuAvailability(t *testing.T) {
	root := t.TempDir()
	for _, editor := range []string{"", filepath.Join(root, "Code.exe")} {
		a := &App{}
		state := fileTreeMenuState{open: true}
		tt := ui.NewTester(func(c *ui.Context) {
			anchor := ui.Text(c, "Folder")
			a.fileTreePopup(c, anchor, &state, a.fileTreeActions(c, root, fileTreeRow{path: root, directory: true}, editor))
		}, 400, 200)
		if got := slices.Contains(fileTreePopupLabels(tt), "Open Folder in VS Code"); got != (editor != "") {
			t.Fatalf("VS Code availability %q: menu %q", editor, fileTreePopupLabels(tt))
		}
		if slices.Contains(fileTreePopupLabels(tt), "Open in Default Browser") {
			t.Fatal("folder has a browser action")
		}
		bounds, hasIcon := tt.Find("VS Code icon")
		if hasIcon != (editor != "") {
			t.Fatal("VS Code icon does not match menu availability")
		}
		if hasIcon {
			if bounds.W != 16 || bounds.H != 16 {
				t.Fatalf("VS Code icon has unexpected bounds: %+v", bounds)
			}
			blue := false
			shot := tt.Image()
			for y := int(bounds.Y); y < int(bounds.Y+bounds.H); y++ {
				for x := int(bounds.X); x < int(bounds.X+bounds.W); x++ {
					p := shot.RGBAAt(x, y)
					blue = blue || int(p.B) > int(p.R)+40 && int(p.B) > int(p.G)+10
				}
			}
			if !blue {
				t.Fatal("VS Code logo was not painted in blue")
			}
		}
	}
}

func fileTreePopupLabels(tt *ui.Tester) []string {
	var labels []string
	for _, text := range tt.Texts() {
		switch text {
		case "Copy Full Path", "Copy Relative Path", "Open in Default Browser", "Open Folder in VS Code":
			if !slices.Contains(labels, text) {
				labels = append(labels, text)
			}
		}
	}
	return labels
}

func TestFileTreePopupKeyboardAndDismissal(t *testing.T) {
	a := &App{}
	root := t.TempDir()
	state := fileTreeMenuState{open: true}
	tt := ui.NewTester(func(c *ui.Context) {
		anchor := ui.Text(c, "Folder")
		a.fileTreePopup(c, anchor, &state, a.fileTreeActions(c, root, fileTreeRow{path: root, directory: true}, ""))
	}, 400, 200)
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyEnter)
	if state.open || tt.Clipboard() != "." {
		t.Fatalf("keyboard menu activation failed: open %v, clipboard %q", state.open, tt.Clipboard())
	}
	state.open = true
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	if state.open {
		t.Fatal("Escape did not close the file-tree menu")
	}
	state.open = true
	tt.Frame()
	tt.Press(390, 190)
	tt.Release(390, 190)
	if state.open {
		t.Fatal("clicking outside did not close the file-tree menu")
	}
}

func TestFileTreeURLs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "page #1 & 20%.html")
	value, err := fileTreeURL(path)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "file" || parsed.Fragment != "" || parsed.RawQuery != "" || !strings.HasSuffix(parsed.Path, "/page #1 & 20%.html") {
		t.Fatalf("incorrect file URL %q: %v", value, err)
	}
	if windowsProfiles {
		value, err := fileTreeURL(`\\wsl.localhost\Ubuntu\home\user\page #1.html`)
		if err != nil || value != "file://wsl.localhost/Ubuntu/home/user/page%20%231.html" {
			t.Fatalf("incorrect WSL file URL %q: %v", value, err)
		}
	}
}

func TestVSCodeCommandAndDiscovery(t *testing.T) {
	root := t.TempDir()
	folder := filepath.Join(root, "folder & spaces # %")
	editor := filepath.Join(root, "Code.exe")
	if args := vscodeCommand(editor, folder).Args; !slices.Equal(args, []string{editor, "--new-window", folder}) {
		t.Fatalf("folder was not passed as a separate argument: %q", args)
	}
	if !windowsProfiles {
		return
	}
	t.Setenv("PATH", root)
	t.Setenv("LOCALAPPDATA", root)
	t.Setenv("ProgramFiles", root)
	t.Setenv("ProgramFiles(x86)", root)
	if got := findVSCode(); got != "" {
		t.Fatalf("detected VS Code when unavailable: %q", got)
	}
	exe := filepath.Join(root, "Programs", "Microsoft VS Code", "Code.exe")
	if err := os.MkdirAll(filepath.Dir(exe), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := findVSCode(); got != exe {
		t.Fatalf("installed VS Code not detected: %q", got)
	}
	launcher := filepath.Join(filepath.Dir(exe), "bin", "code.cmd")
	if got := vscodeBinary(launcher); got != exe {
		t.Fatalf("CLI launcher did not resolve to GUI executable: %q", got)
	}
}
