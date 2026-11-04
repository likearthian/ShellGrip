package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"gorex/internal/terminal"
)

type vscodeLaunch struct{ editor, directory string }

func captureVSCodeLaunches(t *testing.T) <-chan vscodeLaunch {
	t.Helper()
	previous := runVSCode
	t.Cleanup(func() { runVSCode = previous })
	calls := make(chan vscodeLaunch, 4)
	runVSCode = func(editor, directory string) error {
		calls <- vscodeLaunch{editor: editor, directory: directory}
		return nil
	}
	return calls
}

func TestPaneVSCodeButtonUsesCurrentDirectory(t *testing.T) {
	calls := captureVSCodeLaunches(t)
	dir := filepath.Join(t.TempDir(), "directory with spaces")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	a, tt := newTestApp(t)
	a.vsCodePath, a.vsCodeCheckedAt = "", time.Now()
	tt.Frame()
	if _, found := tt.Find("Open in VS Code"); found {
		t.Fatal("VS Code button is visible without an installed editor")
	}
	a.vsCodePath, a.vsCodeCheckedAt = "test-vscode", time.Now()
	tt.Frame()
	p := a.tab().Focus
	sid := p.SID
	command := "cd '" + dir + "'; echo vscode_directory_ready\r"
	if runtime.GOOS == "windows" {
		command = `cd /d "` + dir + `" & echo vscode_directory_ready` + "\r"
	}
	p.term.Send([]byte(command))
	waitFor(t, tt, "shell directory change", func() bool {
		sessions, _ := a.client.List()
		for _, session := range sessions {
			if session.ID == sid && samePath(session.Dir, dir) {
				return true
			}
		}
		return false
	})
	// The click must refresh directory information, even before the next poll.
	p.info.Dir = os.TempDir()
	if err := tt.Click("Open in VS Code"); err != nil {
		t.Fatal(err)
	}
	var got vscodeLaunch
	waitFor(t, tt, "VS Code launch", func() bool {
		select {
		case got = <-calls:
			return true
		default:
			return false
		}
	})
	if got.editor != "test-vscode" || !samePath(got.directory, dir) {
		t.Fatalf("VS Code launch = %+v, want directory %q", got, dir)
	}
	if p.SID != sid || len(a.tab().panes()) != 1 {
		t.Fatal("opening VS Code changed the terminal session or pane layout")
	}
}

func TestPaneVSCodeButtonUsesClickedPane(t *testing.T) {
	registerFonts()
	calls := captureVSCodeLaunches(t)
	left := &Pane{ID: 1, startDir: t.TempDir()}
	right := &Pane{ID: 2, startDir: t.TempDir()}
	tab := &Tab{ID: 3, Focus: right, Root: &Node{ID: 4, Ratio: 0.5, A: &Node{ID: 5, Pane: left}, B: &Node{ID: 6, Pane: right}}}
	a := &App{tabs: []*Tab{tab}, vsCodePath: "test-vscode", vsCodeCheckedAt: time.Now()}
	tt := ui.NewTester(a.view, 1000, 620)
	tt.Frame()
	tt.Move(left.bounds.X+20, left.bounds.Y+headerH/2)
	tt.Frame()
	if err := tt.Click("Open in VS Code"); err != nil {
		t.Fatal(err)
	}
	var got vscodeLaunch
	waitFor(t, tt, "clicked pane editor launch", func() bool {
		select {
		case got = <-calls:
			return true
		default:
			return false
		}
	})
	if got.directory != left.startDir {
		t.Fatalf("opened %q instead of clicked pane directory %q", got.directory, left.startDir)
	}
}

func TestPaneVSCodeUsesShellReportedWSLDirectory(t *testing.T) {
	calls := captureVSCodeLaunches(t)
	term, err := terminal.New(terminal.Options{Conn: nopConn{}})
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	term.Feed([]byte("\x1b]7;file://wsl.localhost/Ubuntu/home/user/project%20files\x07"))
	p := &Pane{SID: "shell", term: term, profile: terminalProfile{ID: "wsl:Ubuntu", WSL: true}, startDir: "~"}
	a := &App{}
	tt := ui.NewTester(func(c *ui.Context) {
		if ui.Button(c, "Open shell").Clicked() {
			a.openPaneInVSCode(c, p, "test-vscode")
		}
	}, 400, 200)
	if err := tt.Click("Open shell"); err != nil {
		t.Fatal(err)
	}
	var got vscodeLaunch
	waitFor(t, tt, "WSL editor launch", func() bool {
		select {
		case got = <-calls:
			return true
		default:
			return false
		}
	})
	want := "vscode-remote://wsl+Ubuntu/home/user/project%20files"
	if got.directory != want {
		t.Fatalf("VS Code directory = %q, want %q", got.directory, want)
	}
}

func TestVSCodeWSLTargets(t *testing.T) {
	for _, tc := range []struct{ distro, directory, want string }{
		{"Ubuntu", "/home/user/a #?.html", "vscode-remote://wsl+Ubuntu/home/user/a%20%23%3F.html"},
		{"Debian", "/", "vscode-remote://wsl+Debian/"},
		{"", "/home/user", ""},
		{"Ubuntu", "~", ""},
		{"bad/distro", "/home/user", ""},
	} {
		if got := vscodeWSLFolder(tc.distro, tc.directory); got != tc.want {
			t.Errorf("WSL target (%q, %q) = %q, want %q", tc.distro, tc.directory, got, tc.want)
		}
	}
	for _, path := range []string{`\\wsl.localhost\Ubuntu\home\user\project files`, `\\wsl$\Ubuntu\home\user\project files`} {
		if got := vscodeWSLFromUNC(path); got != "vscode-remote://wsl+Ubuntu/home/user/project%20files" {
			t.Errorf("UNC target = %q", got)
		}
	}
	if got := vscodeWSLFromUNC(`C:\project`); got != `C:\project` {
		t.Fatalf("native path changed: %q", got)
	}
	if !hasWSLExtension([]byte("other.extension\r\nMS-VSCODE-REMOTE.REMOTE-WSL@1.0.0\r\n")) || hasWSLExtension([]byte("other.extension\n")) {
		t.Fatal("WSL extension detection failed")
	}
}

func TestVSCodeCLILauncher(t *testing.T) {
	base := t.TempDir()
	editor := filepath.Join(base, "Code.exe")
	cli := filepath.Join(base, "version123", "resources", "app", "out", "cli.js")
	if err := os.MkdirAll(filepath.Dir(cli), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cli, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(base, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "bin", "code.cmd"), []byte(`"%~dp0..\Code.exe" "%~dp0..\version123\resources\app\out\cli.js" %*`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := vscodeCLIPath(editor); err != nil || got != cli {
		t.Fatalf("launcher = %q, %v", got, err)
	}
	if !windowsProfiles {
		return
	}
	t.Setenv("ELECTRON_RUN_AS_NODE", "bad")
	t.Setenv("VSCODE_DEV", "bad")
	uri := vscodeWSLFolder("Ubuntu", "/home/user/project files")
	cmd, err := vscodeCLICommand(context.Background(), editor, "--new-window", "--folder-uri", uri)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cmd.Args, []string{editor, cli, "--new-window", "--folder-uri", uri}) {
		t.Fatalf("args = %q", cmd.Args)
	}
	for _, env := range cmd.Env {
		if strings.HasPrefix(env, "VSCODE_DEV=") || (strings.HasPrefix(env, "ELECTRON_RUN_AS_NODE=") && env != "ELECTRON_RUN_AS_NODE=1") {
			t.Fatalf("unexpected environment %q", env)
		}
	}
}

func TestVSCodeCLILegacyLauncher(t *testing.T) {
	base := t.TempDir()
	editor := filepath.Join(base, "Code.exe")
	if _, err := vscodeCLIPath(editor); err == nil {
		t.Fatal("missing launcher was accepted")
	}
	cli := filepath.Join(base, "resources", "app", "out", "cli.js")
	if err := os.MkdirAll(filepath.Dir(cli), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cli, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := vscodeCLIPath(editor); err != nil || got != cli {
		t.Fatalf("legacy launcher = %q, %v", got, err)
	}
}

func TestVSCodeWSLFailureHelp(t *testing.T) {
	registerFonts()
	previous := runVSCode
	t.Cleanup(func() { runVSCode = previous })
	runVSCode = func(string, string) error { return errWSLExtensionMissing }
	p := &Pane{ID: 1, profile: terminalProfile{ID: "wsl:Ubuntu", WSL: true}, startDir: "/home/user/project"}
	tab := &Tab{ID: 2, Focus: p, Root: &Node{Pane: p}}
	a := &App{tabs: []*Tab{tab}, vsCodePath: "test-vscode", vsCodeCheckedAt: time.Now()}
	tt := ui.NewTester(a.view, 800, 600)
	if err := tt.Click("Open in VS Code"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, tt, "WSL failure guidance", func() bool { return a.vsCodeWSLHelpOpen })
	if err := tt.Click("View WSL extension"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(tt.OpenedURLs(), []string{wslExtensionURL}) {
		t.Fatalf("URLs = %q", tt.OpenedURLs())
	}
	if err := tt.Click("Copy install command"); err != nil {
		t.Fatal(err)
	}
	if tt.Clipboard() != wslInstallCommand {
		t.Fatalf("clipboard = %q", tt.Clipboard())
	}
	if err := tt.Click("Dismiss"); err != nil {
		t.Fatal(err)
	}
	if a.vsCodeWSLHelpOpen {
		t.Fatal("failure dialog still open")
	}
}
