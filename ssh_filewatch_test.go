package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

type relayTestFS struct {
	files            map[string][]byte
	creates, renames int
}
type relayTestWriter struct {
	bytes.Buffer
	close func([]byte)
}

func (w *relayTestWriter) Close() error { w.close(w.Bytes()); return nil }
func (f *relayTestFS) CreateRelay(name string) (io.WriteCloser, error) {
	f.creates++
	return &relayTestWriter{close: func(data []byte) { f.files[name] = append([]byte{}, data...) }}, nil
}
func (f *relayTestFS) RenameRelay(old, new string) error {
	f.renames++
	f.files[new] = f.files[old]
	delete(f.files, old)
	return nil
}
func (f *relayTestFS) MkdirAll(string) error            { return nil }
func (f *relayTestFS) Chmod(string, os.FileMode) error  { return nil }
func (f *relayTestFS) Remove(name string) error         { delete(f.files, name); return nil }
func (f *relayTestFS) Stat(string) (os.FileInfo, error) { return nil, os.ErrNotExist }

func TestSSHRelayDeployment(t *testing.T) {
	data := bytes.Repeat([]byte("relay"), 30000)
	fs := &relayTestFS{files: map[string][]byte{}}
	cached := func(name, sum string) bool {
		b, ok := fs.files[name]
		return ok && fmt.Sprintf("%x", sha256.Sum256(b)) == sum
	}
	var sent, total int64
	name, err := deploySSHRelay(context.Background(), fs, "/home/u", data, cached, func(s, n int64) { sent, total = s, n })
	if err != nil || !bytes.Equal(fs.files[name], data) || sent != int64(len(data)) || total != sent || fs.renames != 1 {
		t.Fatalf("deployment: %q %v, progress %d/%d", name, err, sent, total)
	}
	if len(fs.files) != 1 {
		t.Fatal("temporary upload was retained")
	}
	_, err = deploySSHRelay(context.Background(), fs, "/home/u", data, cached, func(int64, int64) { t.Fatal("cached relay was uploaded again") })
	if err != nil || fs.creates != 1 {
		t.Fatalf("cache reuse: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = deploySSHRelay(ctx, fs, "/home/u", data, func(string, string) bool { return false }, func(int64, int64) {})
	if err == nil || fs.renames != 1 || len(fs.files) != 1 {
		t.Fatal("canceled upload published a relay or leaked temporary data")
	}
	_, err = deploySSHRelay(context.Background(), fs, "/home/u", data, func(string, string) bool { return false }, func(int64, int64) {})
	if err == nil || fs.renames != 1 || len(fs.files) != 1 {
		t.Fatal("unverified upload was published")
	}
}

func TestSSHRelaySharingAndStatus(t *testing.T) {
	c := sshConnection{ID: "one", Name: "One", Host: "box", User: "u"}
	other := c
	other.ID = "two"
	other.Name = "Two"
	other.StartDir = "/elsewhere"
	if sshRelayKey(c) != sshRelayKey(other) {
		t.Fatal("tabs of the same host/user do not share setup")
	}
	other.User = "another"
	if sshRelayKey(c) == sshRelayKey(other) {
		t.Fatal("different users share a deployment")
	}
	a := &App{sshRelays: map[string]*sshRelayInstall{}}
	p := &Pane{profile: c.profile()}
	s := &sftpSession{app: a, pane: p, fs: &fakeRemote{}}
	install := &sshRelayInstall{sent: 640 * 1024, total: 1700 * 1024, cancel: func() {}}
	a.sshRelays[sshRelayKey(c)] = install
	tt := ui.NewTester(func(ctx *ui.Context) { a.sshWatchStatus(ctx, colorsOf(ctx), s) }, 500, 120)
	if !strings.Contains(strings.Join(tt.Texts(), " "), "640 / 1700 KB") {
		t.Fatalf("upload progress missing: %q", tt.Texts())
	}
	install.done = true
	install.err = "network interrupted"
	tt.Frame()
	if !tt.HasText("Using periodic refresh") {
		t.Fatal("failure did not explain fallback")
	}
	if err := tt.Click("Retry live updates"); err != nil {
		t.Fatal(err)
	}
	if a.sshRelays[sshRelayKey(c)] != nil {
		t.Fatal("Retry did not reset setup")
	}
	w := &sshRemoteWatch{session: s, root: "/home/u", ready: true}
	a.sshWatch = w
	a.remoteTree = &remoteTreeState{session: s, loading: true, next: time.Now().Add(time.Minute)}
	a.remoteFilesChanged(w)
	if !a.remoteTree.loading || !a.remoteTree.dirty {
		t.Fatal("notification started overlapping directory reads")
	}
	if a.remoteWatchInterval(s) != 30*time.Second {
		t.Fatal("live watcher still polls every five seconds")
	}
	w.ready = false
	if a.remoteWatchInterval(s) != 5*time.Second {
		t.Fatal("failed watcher did not restore polling")
	}
}

func TestRemotePreviewCacheIsolatedByHost(t *testing.T) {
	t.Setenv("SHELLGRIP_DIR", t.TempDir())
	p := &Pane{profile: sshConnection{Host: "one", User: "u"}.profile()}
	q := &Pane{profile: sshConnection{Host: "two", User: "u"}.profile()}
	if remotePreviewPath(p, "/home/u/file.txt") == remotePreviewPath(q, "/home/u/file.txt") {
		t.Fatal("different hosts share a preview cache")
	}
	name := remotePreviewPath(p, "/home/u/file.txt")
	if err := writeRemoteCache(name, []byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := writeRemoteCache(name, []byte("second")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(name)
	if err != nil || string(data) != "second" {
		t.Fatalf("replacement cache: %q %v", data, err)
	}
}
