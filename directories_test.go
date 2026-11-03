package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gorex/internal/terminal"
)

func TestSplitUsesCurrentDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "directory with spaces")
	a, tt := newTestApp(t)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, vertical := range []bool{false, true} {
		source := a.tab().Focus
		command := "cd '" + dir + "'; echo split_directory_ready\r"
		if runtime.GOOS == "windows" {
			command = `cd /d "` + dir + `" & echo split_directory_ready` + "\r"
		}
		source.term.Send([]byte(command))
		waitFor(t, tt, "directory change", func() bool {
			sessions, _ := a.client.List()
			for _, info := range sessions {
				if info.ID == source.SID && samePath(info.Dir, dir) {
					return true
				}
			}
			return false
		})
		// Deliberately stale poll data: splitting must refresh it itself.
		source.info.Dir = os.TempDir()
		a.split(vertical)
		pane := a.tab().Focus
		if !samePath(pane.startDir, dir) || !samePath(pane.info.Dir, dir) {
			t.Fatalf("split opened in %q (%q), want %q", pane.startDir, pane.info.Dir, dir)
		}
	}
}

func TestShellReportedDirectory(t *testing.T) {
	term, err := terminal.New(terminal.Options{Conn: nopConn{}})
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	term.Feed([]byte("\x1b]7;file://localhost/home/user/project%20files\x07"))
	pane := &Pane{term: term, profile: terminalProfile{WSL: true}, startDir: "~"}
	app := &App{}
	if got := app.splitDirectory(pane); got != "/home/user/project files" {
		t.Fatalf("shell-reported directory = %q", got)
	}
}

func TestWSLReportedDistributionPath(t *testing.T) {
	for _, test := range []struct{ reported, want string }{
		{"file://wsl.localhost/Ubuntu/home/ziska/Projects/ai/omnivis", "/home/ziska/Projects/ai/omnivis"},
		{"file://wsl$/Ubuntu/home/user/project%20files", "/home/user/project files"},
		{"file://wsl.localhost/Ubuntu/", "/"},
		{"file://localhost/home/user/Ubuntu/project", "/home/user/Ubuntu/project"},
		{"file://localhost/Ubuntu-data/project", "/Ubuntu-data/project"},
		{"file://localhost/mnt/c/Users/ziska", "/mnt/c/Users/ziska"},
	} {
		t.Run(test.reported, func(t *testing.T) {
			term, err := terminal.New(terminal.Options{Conn: nopConn{}})
			if err != nil {
				t.Fatal(err)
			}
			defer term.Close()
			term.Feed([]byte("\x1b]7;" + test.reported + "\x07"))
			profile := terminalProfile{ID: "wsl:Ubuntu", WSL: true, Command: []string{"wsl.exe", "--distribution", "Ubuntu"}}
			pane := &Pane{term: term, profile: profile, startDir: "~"}
			if got := (&App{}).splitDirectory(pane); got != test.want {
				t.Fatalf("split directory = %q, want %q", got, test.want)
			}
			if got := pane.directory(); got != test.want {
				t.Fatalf("saved directory = %q, want %q", got, test.want)
			}
			command, _ := profile.launch(pane.directory())
			if got := command[len(command)-1]; got != test.want {
				t.Fatalf("WSL --cd argument = %q, want %q", got, test.want)
			}
		})
	}
}

func TestWSLSessionEnvironment(t *testing.T) {
	t.Setenv("WSLENV", "EDITOR/u:GOREX_SESSION/w:MY_VAR/p")
	profile := terminalProfile{WSL: true}
	want := "WSLENV=EDITOR/u:MY_VAR/p:GOREX_SESSION"
	if env := profile.environment(); len(env) != 1 || env[0] != want {
		t.Fatalf("WSL forwarding = %q, want %q", env, want)
	}
	if env := (terminalProfile{}).environment(); len(env) != 0 {
		t.Fatalf("native profile changed environment: %s", strings.Join(env, ", "))
	}
}

func TestWindowsReportedDirectory(t *testing.T) {
	if !windowsProfiles {
		t.Skip("Windows file URI normalization")
	}
	for _, reported := range []string{
		"file:///C:/Users/ziska/Project%20Files",
		`"C:\Users\ziska\Project Files"`,
	} {
		t.Run(reported, func(t *testing.T) {
			term, err := terminal.New(terminal.Options{Conn: nopConn{}})
			if err != nil {
				t.Fatal(err)
			}
			defer term.Close()
			term.Feed([]byte("\x1b]7;" + reported + "\x07"))
			profile := terminalProfile{Command: []string{"powershell.exe"}}
			pane := &Pane{term: term, profile: profile}
			dir := (&App{}).splitDirectory(pane)
			if dir != `C:\Users\ziska\Project Files` {
				t.Fatalf("PowerShell directory = %q", dir)
			}
			_, launchDir := profile.launch(dir)
			if launchDir != dir {
				t.Fatalf("PowerShell launch fell back to %q", launchDir)
			}
			if restored := profile.startingDir(`"` + dir + `"`); restored != dir {
				t.Fatalf("saved quoted path was not repaired: %q", restored)
			}
		})
	}
}

func TestMalformedDirectoryFallsBackToServer(t *testing.T) {
	if !windowsProfiles {
		t.Skip("Windows path validation")
	}
	dir := t.TempDir()
	a, tt := newTestApp(t)
	pane := a.newTab(dir).Focus
	waitFor(t, tt, "shell startup", func() bool { return strings.Contains(pane.term.Text(), testShell.prompt) })
	pane.term.Feed([]byte("\x1b]7;invalid-relative-path\x07"))
	if got := a.splitDirectory(pane); !samePath(got, dir) {
		t.Fatalf("malformed report overrode server path: %q, want %q", got, dir)
	}
}

func TestPowerShellSplitDirectory(t *testing.T) {
	if !windowsProfiles {
		t.Skip("PowerShell on Windows")
	}
	for _, shell := range []string{"pwsh.exe", "powershell.exe"} {
		t.Run(shell, func(t *testing.T) {
			path, err := exec.LookPath(shell)
			if err != nil {
				t.Skip("shell is not installed")
			}
			dir := filepath.Join(t.TempDir(), "PowerShell project with spaces")
			a, tt := newTestApp(t)
			if err := os.Mkdir(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			profile := terminalProfile{ID: shell, Name: shell, Command: []string{path}}
			pane := a.newTabWithProfile(os.TempDir(), profile).Focus
			waitFor(t, tt, "PowerShell startup", func() bool { return pane.reportedDirectory() != "" })
			pane.term.Send([]byte("Set-Location -LiteralPath '" + strings.ReplaceAll(dir, "'", "''") + "'\r"))
			waitFor(t, tt, "PowerShell directory report", func() bool { return samePath(pane.reportedDirectory(), dir) })
			for _, vertical := range []bool{false, true} {
				// Some PowerShell prompts report a quoted Windows path
				// directly rather than a file URI, as in the live session.
				pane.term.Feed([]byte("\x1b]7;\"" + dir + "\"\x07"))
				a.split(vertical)
				pane = a.tab().Focus
				waitFor(t, tt, "PowerShell split directory", func() bool { return samePath(pane.reportedDirectory(), dir) })
				if !samePath(pane.startDir, dir) {
					t.Fatalf("PowerShell split launched in %q, want %q", pane.startDir, dir)
				}
			}
		})
	}
}
