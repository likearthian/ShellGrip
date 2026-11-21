package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gorex/internal/terminal"

	"github.com/egoist/mygo/transfer"
)

type fakeUploadFS struct {
	mu    sync.Mutex
	dirs  map[string]bool
	files map[string][]byte
}

func newFakeUploadFS() *fakeUploadFS {
	return &fakeUploadFS{
		dirs:  map[string]bool{},
		files: map[string][]byte{},
	}
}

func (f *fakeUploadFS) ReadDir(string) ([]os.FileInfo, error) { return nil, nil }
func (f *fakeUploadFS) Open(string) (io.ReadCloser, error)    { return nil, os.ErrNotExist }
func (f *fakeUploadFS) Getwd() (string, error)                { return "/home/user", nil }
func (f *fakeUploadFS) Close() error                          { return nil }

func (f *fakeUploadFS) MkdirAll(dir string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dirs[dir] = true
	return nil
}

func (f *fakeUploadFS) CreateUpload(name string) (io.WriteCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.files[name]; exists {
		return nil, os.ErrExist
	}
	f.files[name] = nil
	return &fakeUploadWriter{fs: f, name: name}, nil
}

type fakeUploadWriter struct {
	fs   *fakeUploadFS
	name string
	buf  bytes.Buffer
}

func (w *fakeUploadWriter) Write(p []byte) (int, error) { return w.buf.Write(p) }
func (w *fakeUploadWriter) Close() error {
	w.fs.mu.Lock()
	defer w.fs.mu.Unlock()
	w.fs.files[w.name] = append([]byte(nil), w.buf.Bytes()...)
	return nil
}

type recordConn struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	closed chan struct{}
	once   sync.Once
}

func newRecordConn() *recordConn {
	return &recordConn{closed: make(chan struct{})}
}

func (c *recordConn) Read([]byte) (int, error) {
	<-c.closed
	return 0, io.EOF
}
func (c *recordConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.Write(p)
}
func (c *recordConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return nil
}
func (c *recordConn) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

func newTestSSHPane(t *testing.T, fs remoteFS) (*App, *Pane, *recordConn) {
	t.Helper()
	conn := newRecordConn()
	term, err := terminal.New(terminal.Options{Conn: conn})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { term.Close() })

	a := &App{}
	p := &Pane{
		ID:      1,
		term:    term,
		profile: terminalProfile{SSH: &sshConnection{Host: "example.com", User: "dev"}},
	}
	if fs != nil {
		s := a.sftpFor(p)
		s.fs = fs
	}
	return a, p, conn
}

func waitForPosted(t *testing.T, a *App, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		a.runPosted()
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for SSH upload")
}

func TestShellQuotePOSIX(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"/tmp/gorex-uploads/a.png", "/tmp/gorex-uploads/a.png"},
		{"/tmp/gorex-uploads/my file.txt", "'/tmp/gorex-uploads/my file.txt'"},
		{"/tmp/it's.txt", `'/tmp/it'\''s.txt'`},
		{"", "''"},
	}
	for _, tc := range cases {
		if got := shellQuotePOSIX(tc.in); got != tc.want {
			t.Errorf("shellQuotePOSIX(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestTerminalDropAndPasteLocalIgnored(t *testing.T) {
	conn := newRecordConn()
	term, err := terminal.New(terminal.Options{Conn: conn})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { term.Close() })

	a := &App{}
	p := &Pane{ID: 1, term: term}
	if a.terminalDrop(p, []string{"/tmp/a.txt"}) {
		t.Fatal("expected local pane drop to return false")
	}
	if a.terminalPaste(p, transfer.TextData("hello")) {
		t.Fatal("expected local pane paste to return false")
	}
}

func TestSSHDropUploadsFilesAndDirectory(t *testing.T) {
	dir := t.TempDir()
	f1 := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(f1, []byte("hello world"), 0o600); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "my folder")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	f2 := filepath.Join(sub, "nested.txt")
	if err := os.WriteFile(f2, []byte("nested"), 0o600); err != nil {
		t.Fatal(err)
	}

	fs := newFakeUploadFS()
	a, p, conn := newTestSSHPane(t, fs)

	if !a.terminalDrop(p, []string{f1, sub}) {
		t.Fatal("expected SSH pane drop to be handled")
	}
	waitForPosted(t, a, func() bool {
		return strings.Contains(conn.String(), "/tmp/gorex-uploads/hello.txt")
	})

	if got := string(fs.files["/tmp/gorex-uploads/hello.txt"]); got != "hello world" {
		t.Fatalf("uploaded file = %q, want %q", got, "hello world")
	}
	if got := string(fs.files["/tmp/gorex-uploads/my folder/nested.txt"]); got != "nested" {
		t.Fatalf("uploaded nested file = %q, want %q", got, "nested")
	}
	wantPaste := "/tmp/gorex-uploads/hello.txt '/tmp/gorex-uploads/my folder'"
	if got := conn.String(); got != wantPaste {
		t.Fatalf("pasted = %q, want %q", got, wantPaste)
	}
}

func TestSSHDropUniqueNameOnCollision(t *testing.T) {
	dir := t.TempDir()
	f1 := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(f1, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}

	fs := newFakeUploadFS()
	fs.files["/tmp/gorex-uploads/note.txt"] = []byte("first")
	a, p, conn := newTestSSHPane(t, fs)

	if !a.terminalDrop(p, []string{f1}) {
		t.Fatal("expected SSH pane drop to be handled")
	}
	waitForPosted(t, a, func() bool {
		return strings.Contains(conn.String(), "/tmp/gorex-uploads/note-2.txt")
	})
	if got := string(fs.files["/tmp/gorex-uploads/note-2.txt"]); got != "second" {
		t.Fatalf("uploaded file = %q, want %q", got, "second")
	}
}

func TestSSHPasteImageAndText(t *testing.T) {
	fs := newFakeUploadFS()
	a, p, conn := newTestSSHPane(t, fs)

	// Plain text paste should be left to the terminal.
	if a.terminalPaste(p, transfer.TextData("echo hi")) {
		t.Fatal("expected plain text paste to return false")
	}

	// Clipboard image paste should upload and paste remote path.
	pngBytes := []byte("\x89PNG\r\n\x1a\nfake-png")
	imgData := transfer.New(transfer.NewItem(transfer.Bytes(transfer.Format("image/png"), pngBytes)))
	if !a.terminalPaste(p, imgData) {
		t.Fatal("expected image paste on SSH pane to be handled")
	}
	waitForPosted(t, a, func() bool {
		return strings.HasPrefix(conn.String(), "/tmp/gorex-uploads/paste-") &&
			strings.HasSuffix(conn.String(), ".png")
	})
	remotePath := conn.String()
	if got := fs.files[remotePath]; !bytes.Equal(got, pngBytes) {
		t.Fatalf("uploaded png = %q, want %q", got, pngBytes)
	}
}

func TestSSHPasteFiles(t *testing.T) {
	dir := t.TempDir()
	f1 := filepath.Join(dir, "clip.log")
	if err := os.WriteFile(f1, []byte("logdata"), 0o600); err != nil {
		t.Fatal(err)
	}
	fileData, err := transfer.FileData(f1)
	if err != nil {
		t.Fatal(err)
	}

	fs := newFakeUploadFS()
	a, p, conn := newTestSSHPane(t, fs)

	if !a.terminalPaste(p, fileData) {
		t.Fatal("expected file paste on SSH pane to be handled")
	}
	waitForPosted(t, a, func() bool {
		return conn.String() == "/tmp/gorex-uploads/clip.log"
	})
	if got := string(fs.files["/tmp/gorex-uploads/clip.log"]); got != "logdata" {
		t.Fatalf("uploaded file = %q, want %q", got, "logdata")
	}
}
