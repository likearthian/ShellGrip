package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

type terminalFilePlace struct {
	name         string
	line, column int
}

var terminalPlacePattern = regexp.MustCompile(`:(\d+)(?::(\d+))?$`)

func parseTerminalFile(link string) (terminalFilePlace, bool) {
	p := terminalFilePlace{name: link}
	if m := terminalPlacePattern.FindStringSubmatchIndex(link); m != nil {
		p.name = link[:m[0]]
		p.line, _ = strconv.Atoi(link[m[2]:m[3]])
		if m[4] >= 0 {
			p.column, _ = strconv.Atoi(link[m[4]:m[5]])
		}
	}
	if strings.HasPrefix(p.name, "file://") {
		u, err := url.Parse(p.name)
		if err != nil {
			return p, false
		}
		p.name = u.Path
		if u.Host != "" && u.Host != "localhost" {
			p.name = "//" + u.Host + p.name
		}
	} else if u, err := url.Parse(p.name); err == nil && u.Scheme != "" && !(len(u.Scheme) == 1 && len(p.name) > 2 && (p.name[2] == '\\' || p.name[2] == '/')) {
		return p, false
	}
	return p, p.name != "" && !strings.ContainsAny(p.name, "\x00\r\n")
}

func terminalFilePath(name, directory, home string, posix bool) (string, error) {
	if name == "~" || strings.HasPrefix(name, "~/") || !posix && strings.HasPrefix(name, `~\`) {
		if home == "" {
			return "", fmt.Errorf("the shell's home directory is unavailable")
		}
		name = home + "/" + strings.TrimLeft(name[1:], `/\`)
	}
	if posix {
		if !path.IsAbs(name) {
			if !path.IsAbs(directory) {
				return "", fmt.Errorf("the shell's directory is unavailable")
			}
			name = path.Join(directory, name)
		}
		return path.Clean(name), nil
	}
	name = nativeDirectory(name)
	if !filepath.IsAbs(name) {
		if !filepath.IsAbs(directory) {
			return "", fmt.Errorf("the shell's directory is unavailable")
		}
		name = filepath.Join(directory, name)
	}
	return filepath.Clean(name), nil
}

type pendingTerminalFile struct {
	pane  *Pane
	place terminalFilePlace
}

func (a *App) openTerminalLink(p *Pane, link string) bool {
	place, ok := parseTerminalFile(link)
	if !ok {
		return false
	} // ordinary URLs keep the terminal's system opener.
	if p == nil || p.closed {
		return true
	}
	if p.profile.SSH != nil {
		a.pendingTerminalFile = &pendingTerminalFile{p, place}
		a.invalidate()
		return true
	}
	dir := p.directory()
	home, _ := os.UserHomeDir()
	if p.profile.WSL && strings.HasPrefix(place.name, "~") {
		distro := p.profile.distribution()
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "wsl.exe", "-d", distro, "--", "printenv", "HOME")
			configureEditorCommand(cmd)
			out, err := cmd.Output()
			a.post(func() {
				if err != nil {
					a.err = "Could not resolve WSL home: " + err.Error()
					return
				}
				a.openLocalTerminalFile(p, place, dir, strings.TrimSpace(string(out)))
			})
			a.invalidate()
		}()
	} else {
		a.openLocalTerminalFile(p, place, dir, home)
	}
	return true
}

func (a *App) openLocalTerminalFile(p *Pane, place terminalFilePlace, dir, home string) {
	if p.closed {
		return
	}
	name, err := terminalFilePath(place.name, dir, home, p.profile.WSL)
	if err != nil {
		a.err = err.Error()
		return
	}
	name = fileTreeDirectory(p, name)
	a.showPreview(p, name, "", place)
	a.invalidate()
}

// wait for SFTP authentication without blocking a frame or opening remote paths locally.
func (a *App) openPendingTerminalFile(c *ui.Context) {
	pending := a.pendingTerminalFile
	if pending == nil {
		return
	}
	p := pending.pane
	if p.closed {
		a.pendingTerminalFile = nil
		return
	}
	s := a.sftpFor(p)
	s.ensure()
	if s.fs == nil {
		if s.err != "" {
			a.err = s.err
			a.pendingTerminalFile = nil
			return
		}
		if s.needPassword {
			prefs.FileTree, prefs.FileTreeView = true, "files"
		}
		c.After(100 * time.Millisecond)
		return
	}
	a.pendingTerminalFile = nil
	name, err := terminalFilePath(pending.place.name, remoteRoot(p, s), s.home, true)
	if err != nil {
		a.err = err.Error()
		return
	}
	a.openRemotePreview(p, &remoteTreeState{session: s}, name, pending.place)
}
