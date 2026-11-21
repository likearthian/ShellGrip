package main

import (
	"os"
	"path/filepath"
	"strings"
)

func (p *Pane) directory() string {
	if dir := p.reportedDirectory(); dir != "" {
		return dir
	}
	if !p.profile.WSL && p.info.Dir != "" {
		return p.info.Dir
	}
	return p.startDir
}

// splitDirectory returns the directory a split of the pane opens in: what
// the shell last reported, or the one its program works in now.
func (p *Pane) splitDirectory() string {
	if dir := p.reportedDirectory(); dir != "" {
		return dir
	}
	if p.profile.WSL {
		if dir := wslDirectory(p); dir != "" {
			p.startDir = dir
			return dir
		}
	} else if p.sess != nil {
		// Read now rather than waiting for the next half-second look.
		if dir := p.sess.Info().Dir; dir != "" {
			return dir
		}
	}
	return p.directory()
}

func (p *Pane) reportedDirectory() string {
	// A remote shell reports its own directory over OSC 7: it belongs to
	// the remote tree, not to the local file tree or the saved layout.
	if p.profile.SSH != nil {
		return ""
	}
	if p.term == nil {
		return ""
	}
	dir := p.term.Dir()
	if !p.profile.WSL {
		dir = nativeDirectory(dir)
		if windowsProfiles && !filepath.IsAbs(dir) {
			return ""
		}
		return dir
	}
	// OSC 7 may report file://wsl.localhost/Ubuntu/home/user. MyGo's
	// Dir removes the URI host, leaving /Ubuntu/home/user. That first
	// component names the distribution, not a directory inside Linux.
	distro := p.profile.distribution()
	if distro != "" {
		prefix := "/" + distro
		if strings.EqualFold(dir, prefix) || len(dir) > len(prefix) && strings.EqualFold(dir[:len(prefix)], prefix) && dir[len(prefix)] == '/' {
			dir = strings.TrimPrefix(dir[len(prefix):], "/")
			return "/" + dir
		}
	}
	return dir
}

func nativeDirectory(dir string) string {
	if !windowsProfiles {
		return dir
	}
	if len(dir) >= 2 && dir[0] == '"' && dir[len(dir)-1] == '"' {
		dir = dir[1 : len(dir)-1]
	}
	// A Windows file URI (file:///C:/work) leaves a leading slash
	// after MyGo decodes OSC 7. CreateProcess needs C:\work instead.
	if len(dir) >= 4 && dir[0] == '/' && dir[2] == ':' && dir[3] == '/' &&
		(dir[1] >= 'A' && dir[1] <= 'Z' || dir[1] >= 'a' && dir[1] <= 'z') {
		return filepath.FromSlash(dir[1:])
	}
	return dir
}

func (p terminalProfile) distribution() string {
	for i, arg := range p.Command {
		if (arg == "--distribution" || arg == "-d") && i+1 < len(p.Command) {
			return p.Command[i+1]
		}
	}
	if name, ok := strings.CutPrefix(p.ID, "wsl:"); ok {
		return name
	}
	return ""
}

func (p terminalProfile) environment() []string {
	if !p.WSL {
		return nil
	}
	var forwarded []string
	for _, value := range strings.Split(os.Getenv("WSLENV"), ":") {
		name, _, _ := strings.Cut(value, "/")
		if value != "" && !strings.EqualFold(name, "GOREX_SESSION") {
			forwarded = append(forwarded, value)
		}
	}
	forwarded = append(forwarded, "GOREX_SESSION")
	return []string{"WSLENV=" + strings.Join(forwarded, ":")}
}
