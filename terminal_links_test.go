package main

import (
	"path/filepath"
	"testing"
)

func TestTerminalFileParsing(t *testing.T) {
	for _, test := range []struct {
		link, name   string
		line, column int
		ok           bool
	}{
		{"src/main.go:42:8", "src/main.go", 42, 8, true},
		{`C:\project files\main.go:12`, `C:\project files\main.go`, 12, 0, true},
		{"file:///tmp/project%20files/a.go:7:2", "/tmp/project files/a.go", 7, 2, true},
		{"https://example.com/a.go:12", "", 0, 0, false},
		{"mailto:user@example.com", "", 0, 0, false},
		{"bad\x00.go", "", 0, 0, false},
	} {
		p, ok := parseTerminalFile(test.link)
		if ok != test.ok || ok && (p.name != test.name || p.line != test.line || p.column != test.column) {
			t.Errorf("parse %q = %+v, %v", test.link, p, ok)
		}
	}
}

func TestTerminalFileResolution(t *testing.T) {
	for _, test := range []struct{ name, dir, home, want string }{
		{"../src/main.go", "/work/project", "/home/me", "/work/src/main.go"},
		{"~/notes.txt", "/work", "/home/me", "/home/me/notes.txt"},
		{"/etc/hosts", "/work", "/home/me", "/etc/hosts"},
	} {
		got, err := terminalFilePath(test.name, test.dir, test.home, true)
		if err != nil || got != test.want {
			t.Errorf("resolve %+v = %q, %v", test, got, err)
		}
	}
	if _, err := terminalFilePath("main.go", "", "", true); err == nil {
		t.Fatal("resolved against an unknown remote directory")
	}
}

func TestTerminalLinkOpensOriginatingTab(t *testing.T) {
	dir := t.TempDir()
	a, tt := newTestApp(t)
	p := a.tab().Focus
	name := previewFixture(t, dir, "main.go", []byte("package main\n"))
	a.newTab(dir)
	if !a.openTerminalLink(p, name+":1:2") {
		t.Fatal("file link was not handled")
	}
	waitFor(t, tt, "linked file preview", func() bool { return p.Tab.preview != nil && !p.Tab.preview.loading })
	if got := p.Tab.preview.path; got != filepath.Clean(name) {
		t.Fatalf("preview = %q", got)
	}
	if a.tab().preview != nil {
		t.Fatal("link opened in the wrong tab")
	}
	if a.openTerminalLink(p, "https://example.com") {
		t.Fatal("web URL bypassed the normal terminal opener")
	}
}

func TestTerminalLinkSSH(t *testing.T) {
	a, tt := newTestApp(t)
	p := a.tab().Focus
	p.profile = sshConnection{ID: "linked", User: "u", Host: "box.local"}.profile()
	s := a.sftpFor(p)
	s.home = "/home/u"
	s.fs = &fakeRemote{files: map[string][]byte{"/home/u/notes.txt": []byte("remote text")}}
	if !a.openTerminalLink(p, "~/notes.txt:1") {
		t.Fatal("remote path was not handled")
	}
	waitFor(t, tt, "linked remote preview", func() bool { return p.Tab.preview != nil && !p.Tab.preview.loading })
	if p.Tab.preview.source != "/home/u/notes.txt" {
		t.Fatalf("remote source = %q", p.Tab.preview.source)
	}
}
