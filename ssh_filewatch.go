package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type relayFS interface {
	CreateRelay(string) (io.WriteCloser, error)
	RenameRelay(string, string) error
	MkdirAll(string) error
	Chmod(string, os.FileMode) error
	Remove(string) error
	Stat(string) (os.FileInfo, error)
}
type sshRelayInstall struct {
	cancel      context.CancelFunc
	helper, err string
	sent, total int64
	done        bool
}
type sshRemoteWatch struct {
	session *sftpSession
	root    string
	cancel  context.CancelFunc
	ready   bool
	err     string
	pending atomic.Bool
}

func sshRelayKey(c sshConnection) string {
	c.ID, c.Name, c.StartDir = "", "", ""
	c.NoFollow = false
	b, _ := json.Marshal(c)
	return string(b)
}
func sshQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }

func sshRelayCommand(ctx context.Context, c sshConnection, password, command string) (*exec.Cmd, func(), error) {
	args := append(sshOptions(), "-T", "-o", "StrictHostKeyChecking=accept-new", "-o", "NumberOfPasswordPrompts=1")
	if password == "" {
		args = append(args, "-o", "BatchMode=yes")
	}
	args = append(args, c.arguments()...)
	args = append(args, command)
	cmd := exec.CommandContext(ctx, "ssh", args...)
	configureEditorCommand(cmd)
	cmd.Env = environmentWithoutSession()
	cleanup := func() {}
	if password != "" {
		env, remove, err := nativeAskpass(gitCredentials{Username: c.User, Password: password})
		if err != nil {
			return nil, nil, err
		}
		cleanup = remove
		cmd.Env = append(cmd.Env, env...)
		cmd.Env = append(cmd.Env, "DISPLAY=:0")
	}
	return cmd, cleanup, nil
}

func sshRelayOutput(ctx context.Context, c sshConnection, password, command string) (string, error) {
	cmd, cleanup, err := sshRelayCommand(ctx, c, password, command)
	if err != nil {
		return "", err
	}
	defer cleanup()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s", sshMessage(stderr.String(), err))
	}
	return strings.TrimSpace(string(out)), nil
}

func relayBinary(arch string) ([]byte, error) {
	switch arch {
	case "x86_64":
		arch = "amd64"
	case "aarch64":
		arch = "arm64"
	}
	if arch != "amd64" && arch != "arm64" {
		return nil, fmt.Errorf("unsupported Linux architecture %q", arch)
	}
	exe, _ := os.Executable()
	dirs := []string{filepath.Join(filepath.Dir(exe), "wslwatch"), filepath.Join("resources", "wslwatch")}
	if resources, err := mygo.App.Path(mygo.PathResources); err == nil {
		dirs = append([]string{filepath.Join(resources, "wslwatch")}, dirs...)
	}
	for _, dir := range dirs {
		if data, err := os.ReadFile(filepath.Join(dir, "wslwatch-"+arch)); err == nil {
			return data, nil
		}
	}
	return nil, fmt.Errorf("Linux watcher is missing; build with go run ./tools/mkwslwatch")
}

// Install through SFTP to a temporary file; validate remotely before making it executable.
func deploySSHRelay(ctx context.Context, fs relayFS, home string, data []byte, cached func(string, string) bool, progress func(int64, int64)) (string, error) {
	if !path.IsAbs(home) {
		return "", fmt.Errorf("remote home directory is unavailable")
	}
	sum := fmt.Sprintf("%x", sha256.Sum256(data))
	dir := path.Join(home, ".cache", "shellgrip", "watchers")
	target := path.Join(dir, "watch-"+sum)
	if cached(target, sum) {
		if err := fs.Chmod(target, 0700); err != nil {
			return "", err
		}
		return target, nil
	}
	if err := fs.MkdirAll(dir); err != nil {
		return "", err
	}
	if err := fs.Chmod(dir, 0700); err != nil {
		return "", err
	}
	var nonce [12]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	tmp := path.Join(dir, ".upload-"+hex.EncodeToString(nonce[:]))
	f, err := fs.CreateRelay(tmp)
	if err != nil {
		return "", err
	}
	defer fs.Remove(tmp)
	progress(0, int64(len(data)))
	for sent := 0; sent < len(data); {
		if err := ctx.Err(); err != nil {
			f.Close()
			return "", err
		}
		end := min(sent+32768, len(data))
		n, err := f.Write(data[sent:end])
		sent += n
		if err != nil {
			f.Close()
			return "", err
		}
		if n == 0 {
			f.Close()
			return "", io.ErrShortWrite
		}
		progress(int64(sent), int64(len(data)))
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	if !cached(tmp, sum) {
		return "", fmt.Errorf("remote watcher checksum verification failed")
	}
	if err := fs.Chmod(tmp, 0700); err != nil {
		return "", err
	}
	if err := fs.RenameRelay(tmp, target); err != nil {
		return "", err
	}
	return target, nil
}

func (a *App) ensureSSHRelay(s *sftpSession) *sshRelayInstall {
	c := *s.pane.profile.SSH
	key := sshRelayKey(c)
	if a.sshRelays == nil {
		a.sshRelays = map[string]*sshRelayInstall{}
	}
	if install := a.sshRelays[key]; install != nil {
		return install
	}
	ctx, cancel := context.WithCancel(context.Background())
	install := &sshRelayInstall{cancel: cancel}
	a.sshRelays[key] = install
	fs, ok := s.fs.(relayFS)
	home, password := s.home, s.password
	go func() {
		var helper string
		var err error
		if !ok {
			err = fmt.Errorf("SFTP server does not support watcher uploads")
		} else {
			probeCtx, stop := context.WithTimeout(ctx, 20*time.Second)
			var platform string
			platform, err = sshRelayOutput(probeCtx, c, password, "if ! command -v sha256sum >/dev/null 2>&1; then echo 'sha256sum is required' >&2; exit 1; fi; uname -s; uname -m")
			stop()
			if err == nil {
				parts := strings.Fields(platform)
				if len(parts) != 2 || parts[0] != "Linux" {
					err = fmt.Errorf("live updates require a Linux server")
				} else {
					var data []byte
					data, err = relayBinary(parts[1])
					if err == nil {
						cached := func(name, sum string) bool {
							checkCtx, stop := context.WithTimeout(ctx, 20*time.Second)
							defer stop()
							out, e := sshRelayOutput(checkCtx, c, password, "sha256sum -- "+sshQuote(name))
							fields := strings.Fields(out)
							return e == nil && len(fields) > 0 && fields[0] == sum
						}
						last := time.Time{}
						helper, err = deploySSHRelay(ctx, fs, home, data, cached, func(sent, total int64) {
							if sent != total && time.Since(last) < 100*time.Millisecond {
								return
							}
							last = time.Now()
							a.post(func() { install.sent, install.total = sent, total })
							a.invalidate()
						})
					}
				}
			}
		}
		a.post(func() {
			if a.sshRelays[key] != install {
				return
			}
			if err != nil && s.closed {
				delete(a.sshRelays, key)
				return
			}
			install.done = true
			install.helper = helper
			if err != nil {
				install.err = err.Error()
			}
		})
		a.invalidate()
	}()
	return install
}

func (a *App) syncSSHWatch(cui *ui.Context) {
	if a.win == nil {
		return
	}
	t := a.tab()
	var s *sftpSession
	root := ""
	if t != nil && !t.Settings && a.focusedWin && t.Focus != nil && t.Focus.profile.SSH != nil && (prefs.FileTree || t.preview != nil && t.preview.source != "") {
		s = a.sftpSessions[t.Focus.ID]
		if s != nil && s.fs != nil {
			root = remoteRoot(t.Focus, s)
		}
	}
	if w := a.sshWatch; w != nil && (w.session != s || w.root != root || root == "") {
		w.cancel()
		a.sshWatch = nil
	}
	if s == nil || root == "" || s.fs == nil {
		return
	}
	install := a.ensureSSHRelay(s)
	a.refreshSSHPreview(s, false)
	cui.After(a.remoteWatchInterval(s))
	if !install.done || install.err != "" || a.sshWatch != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	w := &sshRemoteWatch{session: s, root: root, cancel: cancel}
	a.sshWatch = w
	c, password := *s.pane.profile.SSH, s.password
	go func() {
		err := runSSHRelay(ctx, c, password, install.helper, root, func() {
			a.post(func() {
				if a.sshWatch == w {
					w.ready = true
					a.remoteFilesChanged(w)
				}
			})
			a.invalidate()
		}, func() {
			if !w.pending.CompareAndSwap(false, true) {
				return
			}
			a.post(func() {
				w.pending.Store(false)
				if a.sshWatch == w {
					a.remoteFilesChanged(w)
				}
			})
			a.invalidate()
		})
		if ctx.Err() != nil {
			return
		}
		a.post(func() {
			if a.sshWatch == w {
				w.ready = false
				w.err = err.Error()
				a.remoteFilesChanged(w)
			}
		})
		a.invalidate()
	}()
}

func runSSHRelay(ctx context.Context, c sshConnection, password, helper, root string, ready, change func()) error {
	cmd, cleanup, err := sshRelayCommand(ctx, c, password, "exec "+sshQuote(helper)+" "+sshQuote(root)+" tree")
	if err != nil {
		return err
	}
	defer cleanup()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	defer stdin.Close()
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Cancel = func() error { return stdin.Close() }
	cmd.WaitDelay = 2 * time.Second
	if err := cmd.Start(); err != nil {
		return err
	}
	scanner := bufio.NewScanner(out)
	started := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "READY ") && !started {
			started = true
			ready()
		} else if line == "CHANGE" && started {
			change()
		} else {
			break
		}
	}
	stdin.Close()
	err = cmd.Wait()
	if err == nil {
		err = scanner.Err()
	}
	if err == nil {
		err = fmt.Errorf("watcher disconnected")
	}
	return fmt.Errorf("%s", sshMessage(stderr.String(), err))
}

func (a *App) remoteFilesChanged(w *sshRemoteWatch) {
	if s := a.remoteTree; s != nil && s.session == w.session {
		if s.loading {
			s.dirty = true
		} else {
			s.revision++
			s.next = time.Time{}
		}
	}
	a.refreshSSHPreview(w.session, true)
}

func (a *App) refreshSSHPreview(s *sftpSession, force bool) {
	t := a.tab()
	if t == nil || t.preview == nil {
		return
	}
	p := t.preview
	if p.pane != s.pane || p.source == "" || p.loading || p.diff {
		return
	}
	if p.checkingFile {
		p.remoteAgain = p.remoteAgain || force
		return
	}
	if !force && time.Now().Before(p.nextFileCheck) {
		return
	}
	stat, ok := s.fs.(interface {
		Stat(string) (os.FileInfo, error)
	})
	if !ok {
		return
	}
	interval := remoteTreeRefresh
	if w := a.sshWatch; w != nil && w.session == s && w.ready && (p.source == w.root || strings.HasPrefix(p.source, strings.TrimSuffix(w.root, "/")+"/")) {
		interval = watchedSafetyRefresh
	}
	p.nextFileCheck = time.Now().Add(interval)
	p.checkingFile = true
	fs := s.fs
	connection, password := *s.pane.profile.SSH, s.password
	parent := s.watchCtx
	if parent == nil {
		parent = context.Background()
	}
	previousDigest, previousStamp := p.remoteDigest, p.remoteStamp
	go func() {
		info, err := stat.Stat(p.source)
		stamp := previewFileStamp{}
		if err == nil {
			stamp = previewFileStamp{info.ModTime(), info.Size(), info.Mode(), true}
		}
		changed := stamp != previousStamp
		// SFTP v3 timestamps have second precision; equal-size rapid saves
		// need a checksum when an actual filesystem notification arrives.
		if err == nil && force && !changed {
			ctx, cancel := context.WithTimeout(parent, 20*time.Second)
			out, hashErr := sshRelayOutput(ctx, connection, password, "sha256sum -- "+sshQuote(p.source))
			cancel()
			fields := strings.Fields(out)
			if hashErr != nil || len(fields) == 0 {
				err = fmt.Errorf("could not verify changed remote file")
			} else {
				changed = fields[0] != fmt.Sprintf("%x", previousDigest)
			}
		}
		digest := previousDigest
		if err == nil && changed {
			var f io.ReadCloser
			f, err = fs.Open(p.source)
			if err == nil {
				var data []byte
				data, err = io.ReadAll(io.LimitReader(f, remotePreviewLimit+1))
				f.Close()
				if len(data) > remotePreviewLimit {
					err = fmt.Errorf("file is too large to preview")
				}
				if err == nil {
					digest = sha256.Sum256(data)
					err = writeRemoteCache(p.path, data)
				}
			}
		}
		a.post(func() {
			p.checkingFile = false
			if t.preview != p || s.closed {
				return
			}
			if err != nil {
				p.content.message = "Could not refresh remote file: " + err.Error()
				p.remoteStamp = previewFileStamp{}
			} else if changed {
				p.remoteStamp = stamp
				p.remoteDigest = digest
				a.loadFilePreview(t, p)
			}
			if p.remoteAgain {
				p.remoteAgain = false
				a.refreshSSHPreview(s, true)
			}
		})
		a.invalidate()
	}()
}

func writeRemoteCache(name string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(name), ".preview-")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	_, err = f.Write(data)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(tmp, name)
}
func (a *App) remoteWatchInterval(s *sftpSession) time.Duration {
	if w := a.sshWatch; w != nil && w.session == s && w.ready {
		return watchedSafetyRefresh
	}
	return remoteTreeRefresh
}

func (a *App) closeSSHWatchFor(s *sftpSession) {
	if w := a.sshWatch; w != nil && w.session == s {
		w.cancel()
		a.sshWatch = nil
	}
	if s.pane.profile.SSH == nil {
		return
	}
	key := sshRelayKey(*s.pane.profile.SSH)
	for _, other := range a.sftpSessions {
		if other != s && !other.closed && other.pane.profile.SSH != nil && sshRelayKey(*other.pane.profile.SSH) == key {
			return
		}
	}
	if install := a.sshRelays[key]; install != nil && !install.done {
		install.cancel()
		delete(a.sshRelays, key)
	}
}

func (a *App) sshWatchStatus(c *ui.Context, k *colors, s *sftpSession) {
	if s == nil || s.fs == nil || s.pane.profile.SSH == nil {
		return
	}
	install := a.sshRelays[sshRelayKey(*s.pane.profile.SSH)]
	if install == nil {
		return
	}
	label := "Enabling live updates…"
	if install.total > 0 && !install.done {
		label = fmt.Sprintf("Enabling live updates… %d / %d KB", install.sent/1024, install.total/1024)
	}
	reason := install.err
	if w := a.sshWatch; w != nil && w.session == s {
		if w.ready {
			label = "Live updates"
		}
		if w.err != "" {
			reason = w.err
		}
	}
	if reason != "" {
		label = "Using periodic refresh"
	}
	ui.Row(c).Padding(0, 10, 4, 10).Gap(6).Children(func() {
		ui.Text(c, label).FontSize(11.5).TextColor(k.textFaint).Tooltip(reason)
		if reason != "" && ui.Button(c, "Retry").Label("Retry live updates").Clicked() {
			if w := a.sshWatch; w != nil && w.session == s {
				w.cancel()
				a.sshWatch = nil
			}
			install.cancel()
			delete(a.sshRelays, sshRelayKey(*s.pane.profile.SSH))
		}
	})
}
