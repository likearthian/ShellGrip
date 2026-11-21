package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"shellgrip/internal/appdir"
)

// sshConnection is an SSH target the user saved. Aliases name a Host of
// the user's ssh config, which then resolves the address, user and keys;
// the other fields describe a connection of the app's own.
type sshConnection struct {
	ID       string   `json:"id"`
	Name     string   `json:"name,omitempty"`
	Alias    string   `json:"alias,omitempty"`
	Host     string   `json:"host,omitempty"`
	Port     int      `json:"port,omitempty"`
	User     string   `json:"user,omitempty"`
	Key      string   `json:"key,omitempty"`
	Jump     string   `json:"jump,omitempty"`
	Options  []string `json:"options,omitempty"`
	StartDir string   `json:"startDir,omitempty"`
	// NoFollow keeps the shell as it is, for hosts that are not POSIX and
	// would not take the bootstrap that reports its folder.
	NoFollow bool `json:"noFollow,omitempty"`
}

// sshFile is ssh.json: the connections, in the order the menu shows them.
type sshFile struct {
	Connections []sshConnection `json:"connections"`
}

// sshPath returns the connections' file, in the app's data directory.
func sshPath() string { return filepath.Join(appdir.Dir(), "ssh.json") }

// loadSSHConnections reads the saved connections, and repairs what an
// older or hand-edited file may lack.
func loadSSHConnections() []sshConnection {
	b, err := os.ReadFile(sshPath())
	if err != nil {
		return nil
	}
	var f sshFile
	if json.Unmarshal(b, &f) != nil {
		return nil
	}
	out := f.Connections[:0]
	for _, c := range f.Connections {
		c.Name = strings.TrimSpace(c.Name)
		c.Alias = strings.TrimSpace(c.Alias)
		c.Host = strings.TrimSpace(c.Host)
		c.User = strings.TrimSpace(c.User)
		if c.ID == "" || c.dest() == "" {
			continue
		}
		if c.Port == 0 {
			c.Port = 22
		}
		if c.Name == "" {
			c.Name = c.label()
		}
		out = append(out, c)
	}
	return out
}

// saveSSHConnections keeps the connections on disk, atomically.
func saveSSHConnections(conns []sshConnection) error {
	if err := os.MkdirAll(appdir.Dir(), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(sshFile{Connections: conns}, "", "  ")
	if err != nil {
		return err
	}
	path := sshPath()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func newSSHID() string {
	var b [6]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// dest is what ssh is told to connect to: the config alias, or user@host.
func (c sshConnection) dest() string {
	if c.Alias != "" {
		return c.Alias
	}
	if c.User != "" && c.Host != "" {
		return c.User + "@" + c.Host
	}
	return c.Host
}

// label names the connection in the menu and the tab.
func (c sshConnection) label() string {
	if c.Name != "" {
		return c.Name
	}
	return c.dest()
}

// detail shows where the connection goes, when its name says something
// else.
func (c sshConnection) detail() string {
	if c.Alias != "" && c.Alias != c.label() {
		return c.Alias
	}
	if c.User != "" && c.Host != "" {
		return c.User + "@" + c.Host
	}
	return c.Host
}

// controlPath is where ssh keeps the master connection of a host: on
// macOS and Linux, the remote tree joins the terminal's authenticated
// connection rather than asking again. Windows's OpenSSH has no
// multiplexing.
func controlPath() string { return filepath.Join(appdir.Dir(), "ssh-%C") }

// multiplexed reports whether ssh connections are shared, as unix ssh
// does.
func multiplexed() bool { return runtime.GOOS != "windows" }

// options are the flags every connection starts with.
func sshOptions() []string {
	opts := []string{
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=3",
		"-o", "ConnectTimeout=15",
	}
	if multiplexed() {
		opts = append(opts,
			"-o", "ControlMaster=auto",
			"-o", "ControlPath="+controlPath(),
			"-o", "ControlPersist=300",
		)
	}
	return opts
}

// arguments are the destination and the flags before it: the alias alone
// when the ssh config describes the host, otherwise the fields of the app.
func (c sshConnection) arguments() []string {
	if c.Alias != "" {
		return []string{c.Alias}
	}
	var args []string
	if c.User != "" {
		args = append(args, "-l", c.User)
	}
	if c.Port > 0 && c.Port != 22 {
		args = append(args, "-p", strconv.Itoa(c.Port))
	}
	if c.Key != "" {
		args = append(args, "-i", c.Key)
	}
	if c.Jump != "" {
		args = append(args, "-J", c.Jump)
	}
	for _, opt := range c.Options {
		if opt = strings.TrimSpace(opt); opt != "" {
			args = append(args, "-o", opt)
		}
	}
	return append(args, c.dest())
}

// argv starts a terminal on the host, as a shell tab would.
func (c sshConnection) argv() []string {
	args := append([]string{"ssh"}, sshOptions()...)
	follow := c.followCommand()
	if follow != nil {
		args = append(args, "-t")
	}
	args = append(args, c.arguments()...)
	return append(args, follow...)
}

// sftpArgv asks the same host for its sftp subsystem, whose bytes the
// tree speaks the SFTP protocol over.
func (c sshConnection) sftpArgv() []string { return c.sftpArgvExtra(nil) }

// sftpArgvExtra is sftpArgv with options of the tree's own, such as the
// host key policy that a connection without a terminal needs.
func (c sshConnection) sftpArgvExtra(extra []string) []string {
	args := append([]string{"ssh"}, sshOptions()...)
	args = append(args, extra...)
	args = append(args, c.arguments()...)
	dest := args[len(args)-1]
	args = args[:len(args)-1]
	return append(args, "-s", dest, "sftp")
}

// profile turns the connection into the profile of a tab's pane.
func (c sshConnection) profile() terminalProfile {
	conn := c
	return terminalProfile{ID: "ssh:" + c.ID, Name: c.label(), Command: c.argv(), SSH: &conn}
}

// valid reports what keeps a connection from being saved.
func (c sshConnection) valid() error {
	if c.dest() == "" {
		return errors.New("a host or an ssh config alias is required")
	}
	for name, value := range map[string]string{
		"alias": c.Alias, "host": c.Host, "user": c.User, "jump": c.Jump,
	} {
		if strings.HasPrefix(value, "-") {
			return fmt.Errorf("the %s cannot start with a dash", name)
		}
		if strings.ContainsAny(value, " \t\r\n") {
			return fmt.Errorf("the %s cannot contain spaces", name)
		}
	}
	if strings.HasPrefix(c.Name, "-") {
		return errors.New("the name cannot start with a dash")
	}
	if c.Port < 0 || c.Port > 65535 {
		return errors.New("the port must be between 1 and 65535")
	}
	return nil
}

// sshByID returns a saved connection.
func (a *App) sshByID(id string) (sshConnection, bool) {
	for _, c := range a.sshConns {
		if c.ID == id {
			return c, true
		}
	}
	return sshConnection{}, false
}

// saveSSH keeps the connections after a change.
func (a *App) saveSSH() {
	if err := saveSSHConnections(a.sshConns); err != nil {
		log.Printf("saving ssh connections: %v", err)
		a.err = err.Error()
	}
}

// upsertSSH saves a new connection, or the changes to a saved one.
func (a *App) upsertSSH(c sshConnection) {
	if i := slices.IndexFunc(a.sshConns, func(x sshConnection) bool { return x.ID == c.ID }); i >= 0 {
		a.sshConns[i] = c
	} else {
		a.sshConns = append(a.sshConns, c)
	}
	a.saveSSH()
}

// removeSSH forgets a connection. Tabs already open keep their own copy,
// and go on.
func (a *App) removeSSH(id string) {
	a.sshConns = slices.DeleteFunc(a.sshConns, func(c sshConnection) bool { return c.ID == id })
	a.saveSSH()
}

// newSSHTab opens a normal tab whose shell is ssh on the host.
func (a *App) newSSHTab(c sshConnection) {
	home, _ := os.UserHomeDir()
	a.newTabWithProfile(home, c.profile())
}

// importSSHConfig adds the hosts of the user's ssh config, once, without
// touching its file: the aliases it finds let ssh resolve the rest.
func (a *App) importSSHConfig() (int, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return 0, err
	}
	return a.importSSHConfigDir(filepath.Join(home, ".ssh"))
}

// importSSHConfigDir imports the config of a .ssh directory.
func (a *App) importSSHConfigDir(dir string) (int, error) {
	data, err := os.ReadFile(filepath.Join(dir, "config"))
	if err != nil {
		return 0, err
	}
	hosts := parseSSHConfig(data, dir, map[string]bool{})
	known := map[string]bool{}
	for _, c := range a.sshConns {
		if c.Alias != "" {
			known[strings.ToLower(c.Alias)] = true
		}
	}
	added := 0
	for _, h := range hosts {
		key := strings.ToLower(h.Alias)
		if key == "" || h.Alias == "" || known[key] {
			continue
		}
		known[key] = true
		a.sshConns = append(a.sshConns, h.connection())
		added++
	}
	if added > 0 {
		a.saveSSH()
	}
	return added, nil
}

// sshConfigHost is one Host block of an ssh config, as the importer reads
// it: the fields ssh itself would apply.
type sshConfigHost struct {
	Alias    string
	HostName string
	User     string
	Key      string
	Jump     string
	Port     int
}

func (h sshConfigHost) connection() sshConnection {
	return sshConnection{
		ID: newSSHID(), Alias: h.Alias, Name: h.Alias,
		Host: h.HostName, User: h.User, Port: h.Port, Key: h.Key, Jump: h.Jump,
	}
}

// parseSSHConfig reads the Host blocks of an ssh config. Blocks of
// patterns, as Host *.example, name no one host and are skipped; Match
// blocks apply to whatever ssh decides and are skipped too. Include
// brings the files it names, as ssh reads them.
func parseSSHConfig(data []byte, base string, seen map[string]bool) []sshConfigHost {
	var hosts []sshConfigHost
	var current []int
	flush := func() { current = nil }
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value := splitConfigLine(line)
		switch strings.ToLower(key) {
		case "host":
			flush()
			for _, pattern := range strings.Fields(value) {
				if pattern == "" || strings.ContainsAny(pattern, "*?!") {
					continue
				}
				hosts = append(hosts, sshConfigHost{Alias: pattern, HostName: pattern, Port: 22})
				current = append(current, len(hosts)-1)
			}
		case "match":
			flush()
		case "include":
			for _, pattern := range strings.Fields(value) {
				for _, path := range includePaths(pattern, base, seen) {
					b, err := os.ReadFile(path)
					if err != nil {
						continue
					}
					seen[path] = true
					hosts = append(hosts, parseSSHConfig(b, filepath.Dir(path), seen)...)
				}
			}
		case "hostname":
			for _, i := range current {
				hosts[i].HostName = unquote(value)
			}
		case "user":
			for _, i := range current {
				hosts[i].User = unquote(value)
			}
		case "port":
			if port, err := strconv.Atoi(unquote(value)); err == nil {
				for _, i := range current {
					hosts[i].Port = port
				}
			}
		case "identityfile":
			for _, i := range current {
				if hosts[i].Key == "" {
					hosts[i].Key = expandHome(unquote(value))
				}
			}
		case "proxyjump":
			for _, i := range current {
				hosts[i].Jump = unquote(value)
			}
		}
	}
	return hosts
}

// splitConfigLine splits "Keyword value" at the first space or "=".
func splitConfigLine(line string) (string, string) {
	i := strings.IndexAny(line, " \t=")
	if i < 0 {
		return line, ""
	}
	return line[:i], strings.TrimSpace(line[i+1:])
}

// unquote removes the quotes of a config value.
func unquote(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && (value[0] == '"' && value[len(value)-1] == '"' || value[0] == '\'' && value[len(value)-1] == '\'') {
		return value[1 : len(value)-1]
	}
	return value
}

// expandHome resolves the ~ a config writes.
func expandHome(value string) string {
	if value == "~" || strings.HasPrefix(value, "~/") || strings.HasPrefix(value, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, value[1:])
		}
	}
	return value
}

// includePaths returns the files an Include names, relative to ~/.ssh as
// ssh reads them.
func includePaths(pattern, base string, seen map[string]bool) []string {
	pattern = expandHome(unquote(pattern))
	if !filepath.IsAbs(pattern) {
		pattern = filepath.Join(base, pattern)
	}
	matches, _ := filepath.Glob(pattern)
	var out []string
	for _, m := range matches {
		if !seen[m] {
			out = append(out, m)
		}
	}
	return out
}
