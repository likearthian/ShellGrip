package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

const (
	wslExtensionID    = "ms-vscode-remote.remote-wsl"
	wslExtensionURL   = "https://marketplace.visualstudio.com/items?itemName=" + wslExtensionID
	wslInstallCommand = "code --install-extension " + wslExtensionID
)

var errWSLExtensionMissing = errors.New("the Microsoft WSL extension is not installed in Windows VS Code")

var runVSCode = launchVSCode

func launchVSCode(editor, folder string) error {
	if !isVSCodeWSLFolder(folder) {
		return vscodeCommand(editor, folder).Run()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	check, err := vscodeCLICommand(ctx, editor, "--list-extensions")
	if err != nil {
		return err
	}
	output, err := check.Output()
	if err != nil {
		return fmt.Errorf("could not check installed VS Code extensions: %w", err)
	}
	if !hasWSLExtension(output) {
		return errWSLExtensionMissing
	}
	cmd, err := vscodeCLICommand(ctx, editor, "--new-window", "--folder-uri", folder)
	if err != nil {
		return err
	}
	return cmd.Run()
}

func hasWSLExtension(output []byte) bool {
	for _, line := range strings.Split(string(output), "\n") {
		id, _, _ := strings.Cut(strings.TrimSpace(line), "@")
		if strings.EqualFold(id, wslExtensionID) {
			return true
		}
	}
	return false
}

func isVSCodeWSLFolder(folder string) bool {
	return strings.HasPrefix(folder, "vscode-remote://wsl+")
}

func vscodeWSLFolder(distribution, directory string) string {
	if distribution == "" || strings.ContainsAny(distribution, "/\\\x00") || !strings.HasPrefix(directory, "/") || strings.ContainsRune(directory, '\x00') {
		return ""
	}
	return (&url.URL{Scheme: "vscode-remote", Host: "wsl+" + distribution, Path: directory}).String()
}

func vscodeWSLFromUNC(directory string) string {
	parts := strings.SplitN(strings.ReplaceAll(directory, `\`, "/"), "/", 5)
	if len(parts) < 4 || parts[0] != "" || parts[1] != "" || (!strings.EqualFold(parts[2], "wsl.localhost") && !strings.EqualFold(parts[2], "wsl$")) {
		return directory
	}
	path := "/"
	if len(parts) == 5 {
		path += parts[4]
	}
	return vscodeWSLFolder(parts[3], path)
}

func vscodeCLICommand(ctx context.Context, editor string, args ...string) (*exec.Cmd, error) {
	if !windowsProfiles {
		return exec.CommandContext(ctx, editor, args...), nil
	}
	cli, err := vscodeCLIPath(editor)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, editor, append([]string{cli}, args...)...)
	for _, value := range os.Environ() {
		name, _, _ := strings.Cut(value, "=")
		if !strings.EqualFold(name, "ELECTRON_RUN_AS_NODE") && !strings.EqualFold(name, "VSCODE_DEV") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, "ELECTRON_RUN_AS_NODE=1")
	configureEditorCommand(cmd)
	return cmd, nil
}

func vscodeCLIPath(editor string) (string, error) {
	if resolved, err := filepath.EvalSymlinks(editor); err == nil {
		editor = resolved
	}
	base := filepath.Dir(editor)
	// Recent VS Code updates keep cli.js in a versioned installation folder.
	// Read the installed launcher to use the matching version, without cmd.exe.
	if data, err := os.ReadFile(filepath.Join(base, "bin", "code.cmd")); err == nil {
		quoted := strings.Split(string(data), `"`)
		for i := 1; i < len(quoted); i += 2 {
			part := strings.ReplaceAll(quoted[i], `\`, "/")
			if !strings.HasPrefix(part, "%~dp0") || !strings.HasSuffix(part, "/resources/app/out/cli.js") {
				continue
			}
			candidate := filepath.Clean(filepath.Join(base, "bin", filepath.FromSlash(strings.TrimPrefix(part, "%~dp0"))))
			relative, err := filepath.Rel(base, candidate)
			if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
					return candidate, nil
				}
			}
		}
	}
	path := filepath.Join(base, "resources", "app", "out", "cli.js")
	if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
		return path, nil
	}
	return "", fmt.Errorf("could not locate VS Code's command-line launcher")
}

func (a *App) installedVSCode(now time.Time) string {
	if a.vsCodeCheckedAt.IsZero() || now.Sub(a.vsCodeCheckedAt) >= 30*time.Second {
		a.vsCodePath = findVSCode()
		a.vsCodeCheckedAt = now
	}
	return a.vsCodePath
}

func (a *App) openPaneInVSCode(c *ui.Context, p *Pane, editor string) {
	reported := p.reportedDirectory()
	// The lookup runs off the UI thread with its own pane snapshot.
	lookup := &Pane{SID: p.SID, profile: p.profile, info: p.info, startDir: p.directory()}
	client := a.client
	a.openVSCodeTarget(c, editor, lookup.profile.WSL, func() string {
		dir := reported
		if dir == "" {
			dir = (&App{client: client}).splitDirectory(lookup)
		}
		if lookup.profile.WSL {
			return vscodeWSLFolder(lookup.profile.distribution(), dir)
		}
		return dir
	})
}

func (a *App) openVSCodeDirectory(c *ui.Context, editor string, directory func() string) {
	a.openVSCodeTarget(c, editor, false, directory)
}

func (a *App) openVSCodeTarget(c *ui.Context, editor string, wsl bool, directory func() string) {
	services := c.Services()
	launch := runVSCode
	go func() {
		dir := directory()
		if windowsProfiles {
			dir = vscodeWSLFromUNC(dir)
		}
		remote := wsl || isVSCodeWSLFolder(dir)
		var err error
		if dir == "" {
			err = fmt.Errorf("the directory is unavailable")
		} else {
			err = launch(editor, dir)
		}
		if err != nil {
			a.post(func() {
				if remote {
					a.vsCodeWSLMessage, a.vsCodeWSLHelpOpen = err.Error(), true
				} else {
					a.err = fmt.Sprintf("Could not open folder in VS Code: %v", err)
				}
			})
			services.Invalidate()
		}
	}()
}
