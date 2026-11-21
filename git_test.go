package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// runGit runs git in dir for a test, or skips the test when git is not
// installed.
func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
}

func TestLoadGitRepo(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "gorex@example.invalid")
	runGit(t, dir, "config", "user.name", "GoRex Test")
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "notes.txt")
	runGit(t, dir, "commit", "-m", "first")
	runGit(t, dir, "branch", "feature")
	runGit(t, dir, "tag", "v1.0")
	runGit(t, dir, "tag", "agent/v1")
	runGit(t, dir, "remote", "add", "origin", "https://example.invalid/repo.git")
	runGit(t, dir, "update-ref", "refs/remotes/origin/main", "HEAD")
	runGit(t, dir, "update-ref", "refs/remotes/origin/dev", "HEAD")

	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	repo := loadGitRepo(&Pane{}, sub)
	if repo == nil {
		t.Fatal("no repository from a subdirectory")
	}
	if !samePath(repo.Dir, dir) {
		t.Fatalf("root is %q, want %q", repo.Dir, dir)
	}
	if repo.Branch != "main" || len(repo.Head) != 7 {
		t.Fatalf("branch %q, head %q", repo.Branch, repo.Head)
	}
	if !hasGitRef(repo.Locals, "main") || !hasGitRef(repo.Locals, "feature") {
		t.Fatalf("locals: %+v", repo.Locals)
	}
	if !hasGitRef(repo.Remotes, "origin/main") || !hasGitRef(repo.Remotes, "origin/dev") {
		t.Fatalf("remotes: %+v", repo.Remotes)
	}
	if !hasGitRef(repo.Tags, "v1.0") || !hasGitRef(repo.Tags, "agent/v1") {
		t.Fatalf("tags: %+v", repo.Tags)
	}
	if repo.Dirty != 0 {
		t.Fatalf("%d dirty files in a clean tree", repo.Dirty)
	}

	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if repo = loadGitRepo(&Pane{}, dir); repo == nil || repo.Dirty != 1 {
		t.Fatalf("dirty count %v", repo)
	}
}

func hasGitRef(refs []gitRef, name string) bool {
	for _, ref := range refs {
		if ref.Name == name {
			return true
		}
	}
	return false
}

func TestGitChipLoadsRepository(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "gorex@example.invalid")
	runGit(t, dir, "config", "user.name", "GoRex Test")
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "notes.txt")
	runGit(t, dir, "commit", "-m", "first")

	_, tt := newTestAppInDir(t, dir)
	waitFor(t, tt, "the branch chip of a real repository", func() bool {
		_, ok := tt.Find("Switch branch: main")
		return ok
	})
	if err := tt.Click("Switch branch: main"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, ok := tt.Find("Local"); !ok {
		t.Fatalf("the branch menu did not open; texts %q", tt.Texts())
	}
	if _, ok := tt.Find("main"); !ok {
		t.Fatalf("the menu lacks the current branch; texts %q", tt.Texts())
	}
}

func TestGitMenuEntries(t *testing.T) {
	a := &App{}
	a.gitMenu = gitMenuState{open: true, expanded: map[string]bool{}}
	p := &Pane{}
	repo := &gitRepo{
		Dir:    "C:/project",
		Branch: "main",
		Locals: []gitRef{{Name: "main"}, {Name: "feature"}},
		Remotes: []gitRef{
			{Name: "origin/main", Remote: "origin", Branch: "main"},
			{Name: "origin/dev", Remote: "origin", Branch: "dev"},
		},
		Tags: []gitRef{{Name: "v1.0"}, {Name: "agent/v1"}, {Name: "agent/v2"}},
	}

	entries := a.gitMenuEntries(p, repo)
	labels := make([]string, len(entries))
	for i, entry := range entries {
		labels[i] = entry.label
	}
	joined := strings.Join(labels, "|")
	for _, want := range []string{"Update Project…", "Commit…", "Push…", "New Branch…", "Checkout Tag or Revision…", "Local", "Remote", "Tags", "main", "feature", "v1.0", "agent", "origin/main", "origin/dev"} {
		if !strings.Contains(joined, want) {
			t.Errorf("menu lacks %q: %s", want, joined)
		}
	}
	if strings.Contains(joined, "agent/v1") {
		t.Errorf("tag folder shows its tags before unfolding: %s", joined)
	}

	a.gitMenu.expanded["remote:origin"] = false
	entries = a.gitMenuEntries(p, repo)
	joined = ""
	for _, entry := range entries {
		joined += entry.label + "|"
	}
	if strings.Contains(joined, "origin/main") || strings.Contains(joined, "origin/dev") {
		t.Errorf("a folded remote shows its branches: %s", joined)
	}

	// Each of the three sections folds on its own, keeping its heading.
	a.gitMenu.expanded = map[string]bool{"section:Local": false, "section:Remote": false, "section:Tags": false}
	entries = a.gitMenuEntries(p, repo)
	joined = ""
	for _, entry := range entries {
		joined += entry.label + "|"
	}
	if !strings.Contains(joined, "Local|") || !strings.Contains(joined, "Remote|") || !strings.Contains(joined, "Tags|") {
		t.Errorf("folded sections lost their headings: %s", joined)
	}
	for _, hidden := range []string{"feature", "origin/main", "origin/dev", "agent", "v1.0"} {
		if strings.Contains(joined, hidden) {
			t.Errorf("a folded section shows %q: %s", hidden, joined)
		}
	}
	a.gitMenu.expanded = map[string]bool{"remote:origin": true, "tag:agent": true}
	entries = a.gitMenuEntries(p, repo)
	joined = ""
	for _, entry := range entries {
		joined += entry.label + "|"
	}
	if !strings.Contains(joined, "origin/main") || !strings.Contains(joined, "origin/dev") {
		t.Errorf("unfolded remote branches are missing: %s", joined)
	}
	if !strings.Contains(joined, "v1|") || !strings.Contains(joined, "v2|") {
		t.Errorf("unfolded tags are missing: %s", joined)
	}

	a.gitMenu.query = "feature"
	entries = a.gitMenuEntries(p, repo)
	if len(entries) != 1 || entries[0].label != "feature" {
		t.Fatalf("search found %+v", entries)
	}
	a.gitMenu.query = "push"
	entries = a.gitMenuEntries(p, repo)
	if len(entries) != 1 || entries[0].label != "Push…" {
		t.Fatalf("action search found %+v", entries)
	}
}

func TestGitInvocation(t *testing.T) {
	p := &Pane{profile: terminalProfile{ID: "wsl:Ubuntu", WSL: true, Command: []string{"wsl.exe", "--distribution", "Ubuntu"}}}
	exe, args := gitInvocation(p, "/home/user/project", "-C", "/home/user/project", "status")
	if exe != "wsl.exe" {
		t.Fatalf("WSL program %q", exe)
	}
	want := "--distribution|Ubuntu|--cd|/home/user/project|--exec|git|status"
	if got := strings.Join(args, "|"); got != want {
		t.Fatalf("WSL arguments %q, want %q", got, want)
	}

	p = &Pane{}
	exe, args = gitInvocation(p, `C:\work`, "status")
	if exe != "git" {
		t.Fatalf("native program %q", exe)
	}
	if got := strings.Join(args, "|"); got != `-C|C:\work|status` {
		t.Fatalf("native arguments %q", got)
	}
}

func TestGitRefNames(t *testing.T) {
	for _, name := range []string{"main", "feature/login", "release-1.0", "agent/v2"} {
		if !validGitRefName(name) {
			t.Errorf("%q is not accepted", name)
		}
	}
	for _, name := range []string{"", "-bad", "bad name", "a..b", "/bad", "bad/", ".bad", "bad.lock", "bad\\ref", "bad@{ref"} {
		if validGitRefName(name) {
			t.Errorf("%q is accepted", name)
		}
	}
}

func TestGitChipAndMenu(t *testing.T) {
	a, tt := newTestApp(t)
	p := a.tab().Focus
	dir := p.directory()
	if dir == "" {
		t.Fatal("the test pane has no directory")
	}
	a.gitMu.Lock()
	a.gitByDir = map[string]*gitCacheEntry{gitKey(dir): {
		repo: &gitRepo{
			Dir:    dir,
			Branch: "main",
			Locals: []gitRef{{Name: "main"}, {Name: "feature"}},
			Remotes: []gitRef{
				{Name: "origin/main", Remote: "origin", Branch: "main"},
				{Name: "origin/dev", Remote: "origin", Branch: "dev"},
			},
			Tags: []gitRef{{Name: "agent/v1"}},
		},
		checked: time.Now().Add(time.Hour),
	}}
	a.gitMu.Unlock()
	tt.Frame()

	if _, ok := tt.Find("Switch branch: main"); !ok {
		t.Fatalf("the header does not show the branch; texts %q", tt.Texts())
	}
	if err := tt.Click("Switch branch: main"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, label := range []string{"Update Project…", "Commit…", "Push…", "New Branch…", "Checkout Tag or Revision…", "Local", "Remote", "Tags"} {
		if _, ok := tt.Find(label); !ok {
			t.Fatalf("menu lacks %q; texts %q", label, tt.Texts())
		}
	}

	if err := tt.Click("Local"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, ok := tt.Find("feature"); ok {
		t.Fatalf("the folded Local section shows its branches; texts %q", tt.Texts())
	}
	if _, ok := tt.Find("Local"); !ok {
		t.Fatalf("the folded Local section lost its heading; texts %q", tt.Texts())
	}
	if err := tt.Click("Local"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, ok := tt.Find("feature"); !ok {
		t.Fatalf("the unfolded Local section lost its branches; texts %q", tt.Texts())
	}

	if err := tt.Click("Search branches and actions"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Type("feature")
	tt.Frame()
	if _, ok := tt.Find("feature"); !ok {
		t.Fatalf("search does not find the branch; texts %q", tt.Texts())
	}
	if _, ok := tt.Find("Update Project…"); ok {
		t.Fatal("search keeps an action that does not match")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()

	if err := tt.Click("Switch branch: main"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, ok := tt.Find("origin/dev"); !ok {
		t.Fatalf("remote branches do not show at once; texts %q", tt.Texts())
	}
	if err := tt.Click("origin"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, ok := tt.Find("origin/dev"); ok {
		t.Fatalf("the remote folder did not fold; texts %q", tt.Texts())
	}
	if err := tt.Click("origin"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, ok := tt.Find("origin/dev"); !ok {
		t.Fatalf("the remote folder did not unfold; texts %q", tt.Texts())
	}

	if err := tt.Click("New Branch…"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, ok := tt.Find("New branch"); !ok {
		t.Fatalf("the branch prompt is missing; texts %q", tt.Texts())
	}
	tt.Type("bad name")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if _, ok := tt.Find("That is not a valid branch name."); !ok {
		t.Fatalf("an invalid branch name is accepted; texts %q", tt.Texts())
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.gitPrompt.open {
		t.Fatal("Escape keeps the branch prompt open")
	}
}
