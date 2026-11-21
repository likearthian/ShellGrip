package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

type stalledRemote struct {
	fakeRemote
	started chan struct{}
	release chan struct{}
}

func (f *stalledRemote) Getwd() (string, error) {
	close(f.started)
	<-f.release
	return "", os.ErrClosed
}

func (f *stalledRemote) ReadDir(string) ([]os.FileInfo, error) {
	f.reads.Add(1)
	f.started <- struct{}{}
	<-f.release
	return []os.FileInfo{fakeInfo{name: "new.txt"}}, nil
}

func TestSFTPHomeCancellation(t *testing.T) {
	for _, timeout := range []bool{false, true} {
		t.Run(fmt.Sprint(timeout), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			if timeout {
				ctx, cancel = context.WithTimeout(context.Background(), 50*time.Millisecond)
			}
			defer cancel()
			fs := &stalledRemote{started: make(chan struct{}), release: make(chan struct{})}
			done := make(chan error, 1)
			go func() {
				_, err := sftpHome(ctx, fs, func() { close(fs.release) })
				done <- err
			}()
			<-fs.started
			if !timeout {
				cancel()
			}
			select {
			case err := <-done:
				if !errors.Is(err, ctx.Err()) {
					t.Fatalf("setup returned %v, want %v", err, ctx.Err())
				}
			case <-time.After(2 * time.Second):
				t.Fatal("setup retained a blocked request after cancellation")
			}
		})
	}
}

func TestRemoteTreeSerializesReads(t *testing.T) {
	a := &App{}
	fs := &stalledRemote{started: make(chan struct{}, 10), release: make(chan struct{})}
	defer close(fs.release)
	session := &sftpSession{app: a, fs: fs}
	s := &remoteTreeState{session: session, root: "/", expanded: map[string]bool{}}
	a.remoteTree = s
	tt := ui.NewTester(func(c *ui.Context) { s.load(c.Services()) }, 100, 100)
	tt.Frame()
	select {
	case <-fs.started:
	case <-time.After(time.Second):
		t.Fatal("read did not start")
	}
	for range 20 {
		s.reload()
		s.toggle("/folder")
		tt.Frame()
	}
	// Replacing the tree after a cwd change must share the same read gate.
	s = &remoteTreeState{session: session, root: "/other", expanded: map[string]bool{}}
	a.remoteTree = s
	tt.Frame()
	if fs.reads.Load() != 1 {
		t.Fatalf("started %d overlapping reads", fs.reads.Load())
	}
	fs.release <- struct{}{}
	deadline := time.Now().Add(time.Second)
	for session.treeReading && time.Now().Before(deadline) {
		a.runPosted()
		time.Sleep(time.Millisecond)
	}
	if session.treeReading {
		t.Fatal("replaced tree retained the session read gate")
	}
	if s.loaded || len(s.rows) != 0 {
		t.Fatal("replaced tree accepted the old folder's result")
	}
	tt.Frame()
	select {
	case <-fs.started:
	case <-time.After(time.Second):
		t.Fatal("replacement tree did not resume reading")
	}
	fs.release <- struct{}{}
	deadline = time.Now().Add(time.Second)
	for session.treeReading && time.Now().Before(deadline) {
		a.runPosted()
		time.Sleep(time.Millisecond)
	}
	if !s.loaded || len(s.rows) != 1 || s.rows[0].path != "/other/new.txt" {
		t.Fatal("replacement tree did not show its own result")
	}
}

func TestGitCacheEvictsOldDirectories(t *testing.T) {
	repos := map[string]*gitCacheEntry{}
	history := map[string]*gitHistoryEntry{}
	for i := range gitCacheLimit * 3 {
		makeGitCacheRoom(repos)
		makeGitCacheRoom(history)
		key := fmt.Sprint(i)
		used := time.Unix(int64(i), 0)
		repos[key] = &gitCacheEntry{used: used, loading: true}
		history[key] = &gitHistoryEntry{used: used}
	}
	if len(repos) != gitCacheLimit || len(history) != gitCacheLimit || repos["0"] != nil || history["0"] != nil {
		t.Fatal("Git caches retained old directories beyond their limit")
	}
	key := fmt.Sprint(gitCacheLimit * 2)
	repos[key].used = time.Now()
	history[key].used = time.Now()
	makeGitCacheRoom(repos)
	makeGitCacheRoom(history)
	if repos[key] == nil || history[key] == nil {
		t.Fatal("evicted a recently viewed directory")
	}
}
