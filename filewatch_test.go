package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWatchedPreviewReload(t *testing.T) {
	a, tt := newTestApp(t)
	p := a.tab().Focus
	dir := t.TempDir()
	name := previewFixture(t, dir, "notes.txt", []byte(strings.Repeat("old line\n", 100)))
	a.showPreview(p, name, "")
	waitFor(t, tt, "initial preview", func() bool { return a.tab().preview != nil && !a.tab().preview.loading })
	preview := a.tab().preview
	preview.scroll.Y = 210
	// An editor's replacement save must reload even with the same byte count.
	replacement := filepath.Join(dir, "save.tmp")
	if err := os.WriteFile(replacement, []byte(strings.Repeat("new line\n", 100)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, name); err != nil {
		t.Fatal(err)
	}
	a.localFilesChanged(&localFileWatch{path: name, file: true})
	waitFor(t, tt, "replacement save", func() bool { return strings.Contains(string(preview.content.source), "new line") })
	if a.tab().preview != preview || preview.scroll.Y == 0 {
		t.Fatal("reload lost the preview or its scroll position")
	}
	if err := os.Remove(name); err != nil {
		t.Fatal(err)
	}
	a.localFilesChanged(&localFileWatch{path: name, file: true})
	waitFor(t, tt, "deleted file message", func() bool { return preview.content.message != "" })
	if err := os.WriteFile(name, []byte("recreated"), 0600); err != nil {
		t.Fatal(err)
	}
	preview.nextFileCheck = time.Time{}
	waitFor(t, tt, "recreated file via polling", func() bool { return string(preview.content.source) == "recreated" })
}

func TestLocalWatchInvalidationAndCleanup(t *testing.T) {
	a := &App{}
	dir := t.TempDir()
	a.fileTree = &fileTreeState{hint: dir, revision: 4, next: time.Now().Add(time.Hour)}
	a.gitByDir = map[string]*gitCacheEntry{gitKey(dir): {checked: time.Now()}, "unrelated": {checked: time.Now()}}
	a.localFilesChanged(&localFileWatch{path: dir})
	if a.fileTree.revision != 5 || !a.fileTree.next.IsZero() || !a.gitByDir[gitKey(dir)].checked.IsZero() {
		t.Fatal("watch event did not invalidate tree and git")
	}
	if a.gitByDir["unrelated"].checked.IsZero() {
		t.Fatal("unrelated Git cache was invalidated")
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.fileWatches = map[string]*localFileWatch{"dir": {cancel: cancel}}
	a.closeFileWatches()
	if ctx.Err() == nil || len(a.fileWatches) != 0 {
		t.Fatal("watch lifecycle leaked")
	}
	for _, p := range []*Pane{nil, {closed: true}, {profile: sshConnection{Host: "remote"}.profile()}} {
		if localWatchPane(p) {
			t.Fatal("nonlocal or closed pane accepted for watching")
		}
	}
}

func TestWatchBurstCoalesces(t *testing.T) {
	a := &App{}
	w := &localFileWatch{path: "missing", file: true}
	a.fileWatches = map[string]*localFileWatch{"watch": w}
	for range 10000 {
		a.queueFileChange("watch", w)
	}
	if len(a.posted) != 1 {
		t.Fatalf("burst queued %d callbacks", len(a.posted))
	}
	a.runPosted()
	if w.batches.Load() != 10000 || w.updates.Load() != 1 {
		t.Fatal("burst was not coalesced")
	}
	a.queueFileChange("watch", w)
	delete(a.fileWatches, "watch")
	a.runPosted()
	if w.updates.Load() != 1 {
		t.Fatal("retired watcher updated the UI")
	}
}

func TestWSLWatchPaths(t *testing.T) {
	if !windowsProfiles {
		t.Skip("WSL is a Windows profile")
	}
	p := &Pane{profile: terminalProfile{ID: "wsl:Ubuntu", WSL: true}}
	for _, name := range []string{"/home/u/project", `\\wsl.localhost\Ubuntu\home\u\project`} {
		if got := fileWatchPath(p, name); got != `\\wsl.localhost\Ubuntu\home\u\project` {
			t.Fatalf("watch path %q = %q", name, got)
		}
	}
	for _, name := range []string{"relative", `\\server\share`, `\\wsl.localhost\Other\home\u`} {
		if got := fileWatchPath(p, name); got != "" {
			t.Fatalf("accepted foreign path %q", got)
		}
	}
	a := &App{fileTree: &fileTreeState{pane: p, hint: "/home/u/project", root: `\\wsl.localhost\Ubuntu\home\u\project`, next: time.Now().Add(time.Hour)}}
	if got := a.fileWatchDirectory(p); got != a.fileTree.root {
		t.Fatalf("resolved tree root was not reused: %q", got)
	}
	a.localFilesChanged(&localFileWatch{path: `\\wsl.localhost\Ubuntu\home\u\project`, wsl: true})
	if !a.fileTree.next.IsZero() {
		t.Fatal("WSL event did not refresh its Linux-rooted tree")
	}
}

func BenchmarkWatchBurstCoalescing(b *testing.B) {
	a := &App{}
	w := &localFileWatch{path: "missing", file: true}
	a.fileWatches = map[string]*localFileWatch{"watch": w}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.queueFileChange("watch", w)
		if i%1000 == 999 {
			a.runPosted()
		}
	}
}

func TestHealthyWatchPolling(t *testing.T) {
	a := &App{}
	key := "file:preview"
	w := &localFileWatch{path: "preview", file: true}
	a.fileWatches = map[string]*localFileWatch{key: w}
	if got := a.watchPollInterval(key); got != 2*time.Second {
		t.Fatalf("starting watch polls every %s", got)
	}
	a.fileWatchReady(key, w)
	a.runPosted()
	if got := a.watchPollInterval(key); got != 30*time.Second {
		t.Fatalf("healthy watch polls every %s", got)
	}
	w.ready = false
	w.retry = time.Now().Add(30 * time.Second)
	if got := a.watchPollInterval(key); got != 2*time.Second {
		t.Fatalf("failed watch polls every %s", got)
	}
	delete(a.fileWatches, key)
	a.fileWatchReady(key, w)
	a.runPosted()
	if w.ready {
		t.Fatal("retired watch became healthy")
	}
}

func TestHealthyTreeSkipsSafetyReadsUntilEventOrFailure(t *testing.T) {
	dir := t.TempDir()
	previewFixture(t, dir, "first.go", []byte("package first"))
	oldPrefs := prefs
	t.Cleanup(func() { prefs = oldPrefs })
	prefs.FileTree = true
	prefs.FileTreeView = ""
	a, tt := newTestAppInDir(t, dir)
	waitFor(t, tt, "initial tree", func() bool { return a.fileTree != nil && a.fileTree.loaded })
	w := &localFileWatch{path: dir, gitDir: dir, ready: true, cancel: func() {}}
	a.fileWatches = map[string]*localFileWatch{"dir:" + dir: w}
	s := a.fileTree
	has := func(name string) bool {
		for _, row := range s.rows {
			if row.name == name {
				return true
			}
		}
		return false
	}
	previewFixture(t, dir, "second.go", []byte("package second"))
	s.next = time.Time{}
	tt.Frame()
	waitFor(t, tt, "lightweight directory check", func() bool { return !s.loading })
	if has("second.go") {
		t.Fatal("healthy watch performed a redundant directory read")
	}
	a.localFilesChanged(w)
	tt.Frame()
	waitFor(t, tt, "watch notification refresh", func() bool { return has("second.go") })
	w.ready = false
	previewFixture(t, dir, "third.go", []byte("package third"))
	s.next = time.Time{}
	tt.Frame()
	waitFor(t, tt, "failed watch polling fallback", func() bool { return has("third.go") })
}
