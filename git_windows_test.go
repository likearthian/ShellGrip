//go:build windows

package main

import (
	"os/exec"
	"strings"
	"testing"
)

// requireWSL skips a test when no WSL distribution can run.
func requireWSL(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("wsl.exe"); err != nil {
		t.Skip("WSL is not installed")
	}
	if err := exec.Command("wsl.exe", "--exec", "true").Run(); err != nil {
		t.Skip("no WSL distribution is available")
	}
}

// TestLoadGitRepoWSL reads a repository through a WSL distribution, where
// git's ref format reaches it without the distribution's shell reading
// its parentheses.
func TestLoadGitRepoWSL(t *testing.T) {
	requireWSL(t)
	script := `set -e
d=$(mktemp -d)
git -C "$d" init -q -b main
git -C "$d" config user.email gorex@example.invalid
git -C "$d" config user.name "GoRex Test"
printf 'hello\n' > "$d/notes.txt"
git -C "$d" add notes.txt
git -C "$d" commit -qm first
git -C "$d" branch feature
git -C "$d" remote add origin https://example.invalid/repo.git
git -C "$d" update-ref refs/remotes/origin/main HEAD
git -C "$d" tag v1.0
mkdir "$d/sub"
printf '%s\n' "$d"`
	out, err := exec.Command("wsl.exe", "--exec", "sh", "-c", script).CombinedOutput()
	if err != nil {
		t.Skipf("could not make a repository in WSL: %v\n%s", err, out)
	}
	dir := strings.TrimSpace(string(out))
	t.Cleanup(func() { exec.Command("wsl.exe", "--exec", "rm", "-rf", dir).Run() })

	p := &Pane{profile: terminalProfile{ID: "wsl", WSL: true, Command: []string{"wsl.exe"}}}
	repo := loadGitRepo(p, dir+"/sub")
	if repo == nil {
		t.Fatal("no repository through WSL")
	}
	if repo.Branch != "main" {
		t.Fatalf("branch %q", repo.Branch)
	}
	if !hasGitRef(repo.Locals, "main") || !hasGitRef(repo.Locals, "feature") {
		t.Fatalf("locals: %+v", repo.Locals)
	}
	if !hasGitRef(repo.Remotes, "origin/main") {
		t.Fatalf("remotes: %+v", repo.Remotes)
	}
	if !hasGitRef(repo.Tags, "v1.0") {
		t.Fatalf("tags: %+v", repo.Tags)
	}
}

// TestWSLGitRun writes through a real WSL distribution, as the branch
// menu and Source Control do, without the pane's shell.
func TestWSLGitRun(t *testing.T) {
	requireWSL(t)
	script := `set -e
d=$(mktemp -d)
git -C "$d" init -q -b main
git -C "$d" config user.email gorex@example.invalid
git -C "$d" config user.name "GoRex Test"
printf 'hello\n' > "$d/notes.txt"
printf '%s\n' "$d"`
	out, err := exec.Command("wsl.exe", "--exec", "sh", "-c", script).CombinedOutput()
	if err != nil {
		t.Skipf("could not make a repository in WSL: %v\n%s", err, out)
	}
	dir := strings.TrimSpace(string(out))
	t.Cleanup(func() { exec.Command("wsl.exe", "--exec", "rm", "-rf", dir).Run() })

	p := &Pane{profile: terminalProfile{ID: "wsl", WSL: true, Command: []string{"wsl.exe"}}}
	if _, err := gitRun(p, dir, "add", "notes.txt"); err != nil {
		t.Fatal(err)
	}
	out, err = gitRun(p, dir, "status", "--porcelain")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(out)); got != "A  notes.txt" {
		t.Fatalf("status after add: %q", got)
	}
}

// TestLoadDiffWSL reads a diff through a real distribution, including an
// untracked file, which git compares with /dev/null.
func TestLoadDiffWSL(t *testing.T) {
	requireWSL(t)
	script := `set -e
d=$(mktemp -d)
git -C "$d" init -q -b main
git -C "$d" config user.email gorex@example.invalid
git -C "$d" config user.name "GoRex Test"
printf 'one\ntwo\n' > "$d/notes.txt"
git -C "$d" add notes.txt
git -C "$d" commit -qm first
printf 'one\nchanged\n' > "$d/notes.txt"
printf 'new\n' > "$d/new.txt"
printf '%s\n' "$d"`
	out, err := exec.Command("wsl.exe", "--exec", "sh", "-c", script).CombinedOutput()
	if err != nil {
		t.Skipf("could not make a repository in WSL: %v\n%s", err, out)
	}
	dir := strings.TrimSpace(string(out))
	t.Cleanup(func() { exec.Command("wsl.exe", "--exec", "rm", "-rf", dir).Run() })

	p := &Pane{profile: terminalProfile{ID: "wsl", WSL: true, Command: []string{"wsl.exe"}}}
	repo := &gitRepo{Dir: dir}
	for _, tc := range []struct {
		name string
		file gitFile
		want string
	}{
		{"modified", gitFile{Path: "notes.txt", X: '.', Y: 'M'}, "+changed"},
		{"untracked", gitFile{Path: "new.txt", X: '.', Y: '?'}, "+new"},
	} {
		content := loadDiff(p, repo, tc.file, false)
		if content.kind != "diff" || content.message != "" {
			t.Fatalf("%s: %+v", tc.name, content)
		}
		var text strings.Builder
		for _, line := range content.diffLines {
			text.WriteString(line.text)
			text.WriteByte('\n')
		}
		if !strings.Contains(text.String(), tc.want) {
			t.Fatalf("%s lacks %q: %s", tc.name, tc.want, text.String())
		}
	}
}

// TestWSLAskpass runs the askpass script inside a real distribution, as
// its git would.
func TestWSLAskpass(t *testing.T) {
	requireWSL(t)
	p := &Pane{profile: terminalProfile{ID: "wsl", WSL: true, Command: []string{"wsl.exe"}}}
	env, cleanup, err := wslAskpass(p, gitCredentials{Username: "ziska", Password: "s3cret"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	script := ""
	for _, value := range env {
		if strings.HasPrefix(value, "GIT_ASKPASS=") {
			script = strings.TrimPrefix(value, "GIT_ASKPASS=")
		}
	}
	if script == "" {
		t.Fatal("no askpass script")
	}
	for _, tc := range []struct{ prompt, want string }{
		{"Username for 'https://github.com':", "ziska"},
		{"Login for 'https://github.com':", "ziska"},
		{"Password for 'https://ziska@github.com':", "s3cret"},
		{"Enter passphrase for key '/home/ziska/.ssh/id_ed25519':", "s3cret"},
	} {
		out, err := exec.Command("wsl.exe", "--exec", script, tc.prompt).CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", tc.prompt, err, out)
		}
		if got := strings.TrimSpace(string(out)); got != tc.want {
			t.Errorf("%q: %q, want %q", tc.prompt, got, tc.want)
		}
	}
}

// TestWriteWSLCommitMessage writes a message inside the distribution, where
// its git can read it.
func TestWriteWSLCommitMessage(t *testing.T) {
	requireWSL(t)
	p := &Pane{profile: terminalProfile{ID: "wsl", WSL: true, Command: []string{"wsl.exe"}}}
	path, err := writeCommitMessage(p, "first line\nsecond line\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(path, "/tmp/gorex-commit-") {
		t.Fatalf("message path %q", path)
	}
	t.Cleanup(func() { exec.Command("wsl.exe", "--exec", "rm", "-f", path).Run() })
	out, err := exec.Command("wsl.exe", "--exec", "cat", path).CombinedOutput()
	if err != nil {
		t.Fatalf("cat %s: %v\n%s", path, err, out)
	}
	if string(out) != "first line\nsecond line\n" {
		t.Fatalf("message is %q", out)
	}
}
