package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGitStatusDoesNotRefreshIndex(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	name := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(name, []byte("hello\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "notes.txt")
	index := filepath.Join(dir, ".git", "index")
	before, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	// Change only the stat data: ordinary status refreshes the cached index
	// even though the file's contents still match the staged version.
	stamp := time.Now().Add(-time.Hour)
	if err := os.Chtimes(name, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if _, err := gitOutput(&Pane{}, dir, "status", "--porcelain=v2"); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("read-only status rewrote the index and could trigger a watcher loop")
	}
}
