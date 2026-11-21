package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// fakeInfo is a remote entry of the tests.
type fakeInfo struct {
	name string
	dir  bool
	size int64
}

func (f fakeInfo) Name() string       { return f.name }
func (f fakeInfo) Size() int64        { return f.size }
func (f fakeInfo) Mode() os.FileMode  { return 0 }
func (f fakeInfo) ModTime() time.Time { return time.Time{} }
func (f fakeInfo) IsDir() bool        { return f.dir }
func (f fakeInfo) Sys() any           { return nil }

// fakeRemote is a remote filesystem of the tests.
type fakeRemote struct {
	dirs   map[string][]os.FileInfo
	files  map[string][]byte
	home   string
	closed bool
	reads  atomic.Int64
}

func (f *fakeRemote) ReadDir(dir string) ([]os.FileInfo, error) {
	f.reads.Add(1)
	if infos, ok := f.dirs[dir]; ok {
		return infos, nil
	}
	return nil, os.ErrNotExist
}

func (f *fakeRemote) Open(name string) (io.ReadCloser, error) {
	data, ok := f.files[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (f *fakeRemote) Getwd() (string, error) {
	if f.home == "" {
		return "/home/u", nil
	}
	return f.home, nil
}

func (f *fakeRemote) Close() error {
	f.closed = true
	return nil
}

func TestRemotePaths(t *testing.T) {
	if got := remoteJoin("/home/u", "src"); got != "/home/u/src" {
		t.Errorf("join = %q", got)
	}
	if got := remoteJoin("", "src"); got != "/src" {
		t.Errorf("join at root = %q", got)
	}
}

func TestReadRemoteFileTree(t *testing.T) {
	fs := &fakeRemote{dirs: map[string][]os.FileInfo{
		"/home/u": {
			fakeInfo{name: "srv", dir: true},
			fakeInfo{name: "notes.txt"},
		},
		"/home/u/srv": {
			fakeInfo{name: "app.log"},
		},
	}}
	open, err := readRemoteFileTree(fs, "/home/u", map[string]bool{"/home/u/srv": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 3 {
		t.Fatalf("%d rows: %+v", len(open), open)
	}
	if open[0].name != "srv" || !open[0].directory || open[0].depth != 0 {
		t.Errorf("first row %+v", open[0])
	}
	if open[1].name != "app.log" || open[1].depth != 1 {
		t.Errorf("open folder row %+v", open[1])
	}
	if open[2].name != "notes.txt" || open[2].depth != 0 {
		t.Errorf("last row %+v", open[2])
	}
	folded, _ := readRemoteFileTree(fs, "/home/u", nil)
	if len(folded) != 2 {
		t.Errorf("%d rows with the folder folded: %+v", len(folded), folded)
	}
	empty, _ := readRemoteFileTree(&fakeRemote{dirs: map[string][]os.FileInfo{"/e": {}}}, "/e", nil)
	if len(empty) != 1 || empty[0].name != "Empty folder" {
		t.Errorf("empty folder rows %+v", empty)
	}
	failed, failedErr := readRemoteFileTree(&fakeRemote{}, "/missing", nil)
	if len(failed) != 1 || failed[0].name != "Cannot read folder" || failedErr == nil {
		t.Errorf("unreadable folder rows %+v, %v", failed, failedErr)
	}
}

func TestSFTPConnLost(t *testing.T) {
	for _, err := range []error{io.EOF, os.ErrClosed, errors.New("read: broken pipe"), errors.New("connection reset by peer")} {
		if !sftpConnLost(err) {
			t.Errorf("%v is not seen as a lost connection", err)
		}
	}
	for _, err := range []error{nil, errors.New("permission denied"), errors.New("no such file")} {
		if sftpConnLost(err) {
			t.Errorf("%v is seen as a lost connection", err)
		}
	}
}

func TestRemoteTreePanel(t *testing.T) {
	a, _ := newTestApp(t)
	p := a.tab().Focus
	p.profile = sshConnection{ID: "one", Name: "omni", User: "u", Host: "box.local"}.profile()
	p.term = nil // no shell folder to follow
	a.sftpSessions = map[int]*sftpSession{}
	session := a.sftpFor(p)
	session.fs = &fakeRemote{home: "/home/u"}
	session.home = "/home/u"
	a.remoteTree = &remoteTreeState{
		pane: p, session: session, root: "/home/u", loaded: true,
		expanded: map[string]bool{}, next: time.Now().Add(time.Hour),
		rows: []fileTreeRow{
			{path: "/home/u/srv", name: "srv", depth: 0, directory: true},
			{path: "/home/u/notes.txt", name: "notes.txt", depth: 0},
		},
	}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
			a.remoteTreePanel(c, colorsOf(c), p, 300)
		})
	}, 340, 600)
	tt.Frame()
	for _, text := range []string{"Files", "srv", "notes.txt"} {
		if _, ok := tt.Find(text); !ok {
			t.Errorf("%q is not in the remote tree; texts %q", text, tt.Texts())
		}
	}
	if err := tt.Click("Toggle folder srv"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.remoteTree.expanded["/home/u/srv"] {
		t.Errorf("clicking a folder did not open it: %+v", a.remoteTree.expanded)
	}
}

func TestSSHPaneSidebarIsRemote(t *testing.T) {
	a, tt := newTestApp(t)
	previous := prefs
	t.Cleanup(func() { prefs = previous })
	prefs.FileTree = true
	p := a.tab().Focus
	p.profile = sshConnection{ID: "one", Name: "omni", User: "u", Host: "box.local"}.profile()
	p.term = nil
	a.sftpSessions = map[int]*sftpSession{}
	session := a.sftpFor(p)
	fake := &fakeRemote{home: "/home/u"}
	session.fs = fake
	session.closeFS = func() { fake.Close() }
	session.home = "/home/u"
	a.remoteTree = &remoteTreeState{
		pane: p, session: session, root: "/home/u", loaded: true,
		expanded: map[string]bool{}, next: time.Now().Add(time.Hour),
		rows: []fileTreeRow{{path: "/home/u/notes.txt", name: "notes.txt", depth: 0}},
	}
	tt.Frame()
	if _, ok := tt.Find("Refresh Remote Tree"); !ok {
		t.Fatalf("the sidebar is not the remote tree; texts %q", tt.Texts())
	}
	if _, ok := tt.Find("notes.txt"); !ok {
		t.Errorf("the remote entry is missing; texts %q", tt.Texts())
	}
	a.closeSFTPForPane(p)
	if !fake.closed {
		t.Error("closing the pane left the SFTP connection open")
	}
}

func TestRemotePreview(t *testing.T) {
	a, tt := newTestApp(t)
	p := a.tab().Focus
	p.profile = sshConnection{ID: "one", Name: "omni", User: "u", Host: "box.local"}.profile()
	a.sftpSessions = map[int]*sftpSession{}
	session := a.sftpFor(p)
	session.fs = &fakeRemote{files: map[string][]byte{"/home/u/notes.txt": []byte("hello remote")}}
	session.home = "/home/u"
	s := &remoteTreeState{pane: p, session: session, root: "/home/u"}
	a.openRemotePreview(p, s, "/home/u/notes.txt")
	waitFor(t, tt, "the remote preview", func() bool {
		tab := a.tab()
		return tab.preview != nil && !tab.preview.loading && tab.preview.source != ""
	})
	preview := a.tab().preview
	if preview.source != "/home/u/notes.txt" {
		t.Errorf("preview source %q", preview.source)
	}
	if filepath.Base(preview.path) != "notes.txt" {
		t.Errorf("preview cache %q", preview.path)
	}
	tt.Frame()
	if !tt.HasText("Remote · Read only") {
		t.Errorf("the preview is not marked remote; texts %q", tt.Texts())
	}
}

func TestTreeRefreshWaitsForFocus(t *testing.T) {
	a, _ := newTestApp(t)
	p := a.tab().Focus
	p.profile = sshConnection{ID: "one", Name: "omni", User: "u", Host: "box.local"}.profile()
	p.term = nil
	a.sftpSessions = map[int]*sftpSession{}
	session := a.sftpFor(p)
	fake := &fakeRemote{dirs: map[string][]os.FileInfo{"/home/u": {}}, home: "/home/u"}
	session.fs = fake
	session.home = "/home/u"
	a.remoteTree = nil
	a.focusedWin = true
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
			a.remoteTreePanel(c, colorsOf(c), p, 300)
		})
	}, 340, 600)
	tt.Frame()
	waitFor(t, tt, "the first read", func() bool {
		a.runPosted()
		return fake.reads.Load() >= 1 && a.remoteTree != nil && !a.remoteTree.loading
	})
	reads := fake.reads.Load()

	// A background window asks the host nothing, even when a read is due.
	a.focusedWin = false
	a.remoteTree.next = time.Time{}
	for range 3 {
		tt.Frame()
		a.runPosted()
	}
	if fake.reads.Load() != reads {
		t.Errorf("the tree read while the window was in the background")
	}

	// Coming back reads at once.
	a.focusedWin = true
	a.refreshTrees()
	waitFor(t, tt, "a read after focus", func() bool {
		a.runPosted()
		return fake.reads.Load() > reads
	})
}

func TestRemoteTreeRefreshDoesNotBlink(t *testing.T) {
	a, _ := newTestApp(t)
	p := a.tab().Focus
	p.profile = sshConnection{ID: "one", Name: "omni", User: "u", Host: "box.local"}.profile()
	p.term = nil
	a.sftpSessions = map[int]*sftpSession{}
	session := a.sftpFor(p)
	session.fs = &fakeRemote{home: "/home/u"}
	session.home = "/home/u"
	a.focusedWin = true
	state := &remoteTreeState{
		pane: p, session: session, root: "/home/u",
		loaded: true, loading: true, next: time.Now().Add(time.Hour),
		expanded: map[string]bool{},
		rows:     []fileTreeRow{{path: "/home/u/notes.txt", name: "notes.txt", depth: 0}},
	}
	a.remoteTree = state
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
			a.remoteTreePanel(c, colorsOf(c), p, 300)
		})
	}, 340, 600)
	tt.Frame()
	if _, ok := tt.Find("Loading…"); ok {
		t.Fatal("a refresh in flight shows the loading row")
	}
	if _, ok := tt.Find("notes.txt"); !ok {
		t.Fatalf("the rows shown during a refresh are gone; texts %q", tt.Texts())
	}
	// A reload keeps the rows while the next read is in flight.
	state.reload()
	if !state.loaded || len(state.rows) != 1 || !state.loading {
		t.Errorf("reload left loaded=%v loading=%v rows=%d", state.loaded, state.loading, len(state.rows))
	}
	// A toggle invalidates the result, but waits for the read to finish.
	state.loading = true
	state.toggle("/home/u/srv")
	if !state.loading {
		t.Error("a toggle allowed another read in flight")
	}
	if !state.loaded || len(state.rows) != 1 {
		t.Error("a toggle dropped the rows before the new ones arrived")
	}
}

func TestNewTabMenuListsSSH(t *testing.T) {
	a, tt := newTestApp(t)
	a.sshConns = []sshConnection{
		{ID: "one", Name: "omni", Host: "omnivis.my.id", User: "omni"},
	}
	tt.Frame()
	if err := tt.Click("New Tab Menu"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, ok := tt.Find("Open omni"); !ok {
		t.Fatalf("the SSH connection is not in the new tab menu; texts %q", tt.Texts())
	}
	if _, ok := tt.Find("New SSH connection…"); !ok {
		t.Errorf("the menu does not offer a new connection; texts %q", tt.Texts())
	}
}

func TestSSHSettings(t *testing.T) {
	a, tt := newTestApp(t)
	a.sshConns = []sshConnection{
		{ID: "one", Name: "omni", Host: "omnivis.my.id", User: "omni"},
	}
	a.openSSHSettings(false)
	tt.Frame()
	if _, ok := tt.Find("SSH connections"); !ok {
		t.Fatalf("the SSH settings are missing; texts %q", tt.Texts())
	}
	if _, ok := tt.Find("Import from ~/.ssh/config"); !ok {
		t.Errorf("the import button is missing; texts %q", tt.Texts())
	}
	a.sshDraft = &sshDraft{Host: "example.com", Port: "22", Name: "Example"}
	tt.Frame()
	a.saveSSHDraft(false)
	if a.sshDraft != nil {
		t.Fatal("the form stayed open after saving")
	}
	found := false
	for _, c := range a.sshConns {
		if c.Host == "example.com" && c.Name == "Example" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the connection was not saved: %+v", a.sshConns)
	}
	if _, err := os.Stat(sshPath()); err != nil {
		t.Errorf("ssh.json was not written: %v", err)
	}
}
