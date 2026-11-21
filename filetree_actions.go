package main

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/egoist/mygo/ui"
)

// this menu holds a live tree, whose pane leads back to the tab's preview.
// a non-string key keeps MyGo dev from serializing that graph every 300 ms.
type fileTreeMenuKey struct{}

func (a *App) fileTreeContextMenu(c *ui.Context, row ui.Element, root string, entry fileTreeRow) {
	if entry.message {
		return
	}
	state := ui.Local(row, fileTreeMenuKey{}, func() fileTreeMenuState { return fileTreeMenuState{} })
	if state.tree != a.fileTree {
		state.open, state.tree = false, a.fileTree
	}
	if state.open && row.Clicked() {
		state.open = false
	}
	if row.RightClicked() {
		state.open, state.selected, state.editor = true, 0, ""
		if entry.directory {
			state.editor = findVSCode()
		}
	}
	if state.open {
		a.fileTreePopup(c, row, state, a.fileTreeActions(c, root, entry, state.editor))
	}
}

func (a *App) fileTreeActions(c *ui.Context, root string, entry fileTreeRow, editor string) []fileTreeAction {
	relative, err := filepath.Rel(root, entry.path)
	actions := []fileTreeAction{
		{label: "Copy Full Path", run: func() {
			c.WriteClipboard(entry.path)
			a.selectionCopied(entry.path)
		}},
		{label: "Copy Relative Path", disabled: err != nil, run: func() {
			c.WriteClipboard(relative)
			a.selectionCopied(relative)
		}},
	}
	if !entry.directory && htmlFile(entry.path) {
		actions = append(actions, fileTreeAction{}, fileTreeAction{label: "Open in Default Browser", run: func() {
			fileURL, err := fileTreeURL(entry.path)
			if err != nil {
				a.err = fmt.Sprintf("Could not open HTML file: %v", err)
				return
			}
			if a.win == nil {
				c.OpenURL(fileURL)
			} else {
				services := c.Services()
				go func() {
					if err := openHTMLInDefaultBrowser(fileURL); err != nil {
						a.post(func() { a.err = fmt.Sprintf("Could not open HTML file in the browser: %v", err) })
						services.Invalidate()
					}
				}()
			}
		}})
	}
	if entry.directory && editor != "" {
		actions = append(actions, fileTreeAction{}, fileTreeAction{label: "Open Folder in VS Code", icon: "brand:vscode", run: func() {
			a.openVSCodeDirectory(c, editor, func() string { return entry.path })
		}})
	}
	return actions
}

func htmlFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".html", ".htm", ".xhtml":
		return true
	}
	return false
}

func fileTreeURL(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	path = filepath.ToSlash(abs)
	u := url.URL{Scheme: "file", Path: path}
	if strings.HasPrefix(path, "//") {
		host, rest, _ := strings.Cut(path[2:], "/")
		u.Host, u.Path = host, "/"+rest
	} else if windowsProfiles {
		u.Path = "/" + path
	}
	return u.String(), nil
}

func findVSCode() string {
	for _, name := range []string{"Code.exe", "code"} {
		if path, err := exec.LookPath(name); err == nil {
			if exe := vscodeBinary(path); exe != "" {
				return exe
			}
		}
	}
	var candidates []string
	switch runtime.GOOS {
	case "windows":
		for _, base := range []string{os.Getenv("LOCALAPPDATA"), os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")} {
			if base == "" {
				continue
			}
			candidates = append(candidates,
				filepath.Join(base, "Microsoft VS Code", "Code.exe"),
				filepath.Join(base, "Programs", "Microsoft VS Code", "Code.exe"))
		}
	case "darwin":
		const app = "Visual Studio Code.app/Contents/Resources/app/bin/code"
		candidates = append(candidates, filepath.Join("/Applications", app))
		if home, err := os.UserHomeDir(); err == nil {
			candidates = append(candidates, filepath.Join(home, "Applications", app))
		}
	}
	for _, path := range candidates {
		if exe := vscodeBinary(path); exe != "" {
			return exe
		}
	}
	return ""
}

func vscodeBinary(path string) string {
	if windowsProfiles {
		switch strings.ToLower(filepath.Ext(path)) {
		case ".cmd", ".bat":
			// Launch the GUI executable directly, without a command shell.
			path = filepath.Join(filepath.Dir(filepath.Dir(path)), "Code.exe")
		case ".exe":
		default:
			return ""
		}
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || !windowsProfiles && info.Mode().Perm()&0o111 == 0 {
		return ""
	}
	return path
}

func vscodeCommand(editor, folder string) *exec.Cmd {
	args := []string{"--new-window", folder}
	if isVSCodeWSLFolder(folder) {
		args = []string{"--new-window", "--folder-uri", folder}
	}
	cmd := exec.Command(editor, args...)
	configureEditorCommand(cmd)
	return cmd
}
