package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestParseGitStatusFiles(t *testing.T) {
	out := strings.Join([]string{
		"# branch.oid abcdef1234567890",
		"# branch.head main",
		"# branch.upstream origin/main",
		"# branch.ab +1 -2",
		"1 A. N... 000000 100644 100644 0000 1111 assets/new.svg",
		"1 .M N... 100644 100644 100644 0000 0000 main.go",
		"1 MM N... 100644 100644 100644 0000 0000 both.txt",
		"2 R. N... 100644 100644 100644 0000 0000 R100 renamed.txt",
		"old.txt",
		"? freelance.txt",
		"u UU N... 100644 100644 100644 100644 0000 0000 0000 conflict.txt",
	}, "\x00") + "\x00"
	repo := &gitRepo{}
	parseGitStatus(repo, []byte(out))

	if repo.Branch != "main" || repo.Upstream != "origin/main" || repo.Head != "abcdef1" {
		t.Fatalf("branch %q, upstream %q, head %q", repo.Branch, repo.Upstream, repo.Head)
	}
	if repo.Ahead != 1 || repo.Behind != 2 {
		t.Fatalf("ahead %d, behind %d", repo.Ahead, repo.Behind)
	}
	for _, path := range []string{"assets/new.svg", "both.txt", "renamed.txt", "conflict.txt"} {
		if !hasGitFile(repo.Staged, path) {
			t.Errorf("staged lacks %q: %+v", path, repo.Staged)
		}
	}
	for _, path := range []string{"main.go", "both.txt", "freelance.txt", "conflict.txt"} {
		if !hasGitFile(repo.Unstaged, path) {
			t.Errorf("unstaged lacks %q: %+v", path, repo.Unstaged)
		}
	}
	if hasGitFile(repo.Staged, "old.txt") || hasGitFile(repo.Unstaged, "old.txt") {
		t.Error("the rename's old path is shown")
	}
	if repo.Dirty != 6 {
		t.Fatalf("%d changed files, want 6", repo.Dirty)
	}
}

func hasGitFile(files []gitFile, path string) bool {
	for _, file := range files {
		if file.Path == path {
			return true
		}
	}
	return false
}

func TestLoadGitRepoChanges(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "gorex@example.invalid")
	runGit(t, dir, "config", "user.name", "GoRex Test")
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "tracked.txt")
	runGit(t, dir, "commit", "-m", "first")

	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "staged.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "staged.txt")
	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "both.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "both.txt")
	if err := os.WriteFile(filepath.Join(dir, "both.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	repo := loadGitRepo(&Pane{}, dir)
	if repo == nil {
		t.Fatal("no repository")
	}
	for _, path := range []string{"staged.txt", "both.txt"} {
		if !hasGitFile(repo.Staged, path) {
			t.Errorf("staged lacks %q: %+v", path, repo.Staged)
		}
	}
	for _, path := range []string{"tracked.txt", "untracked.txt", "both.txt"} {
		if !hasGitFile(repo.Unstaged, path) {
			t.Errorf("unstaged lacks %q: %+v", path, repo.Unstaged)
		}
	}
}

func TestSCMTreeAndDecorations(t *testing.T) {
	tree := scmTree([]gitFile{
		{Path: "assets/icons/a.svg"}, {Path: "assets/b.svg"}, {Path: "main.go"},
	})
	if len(tree.files) != 1 || tree.files[0].Path != "main.go" {
		t.Fatalf("root files: %+v", tree.files)
	}
	if len(tree.children) != 1 || tree.children[0].dir != "assets" {
		t.Fatalf("root folders: %+v", tree.children)
	}
	assets := tree.children[0]
	if len(assets.files) != 1 || assets.files[0].Path != "assets/b.svg" {
		t.Fatalf("assets files: %+v", assets.files)
	}
	if len(assets.children) != 1 || assets.children[0].dir != "assets/icons" {
		t.Fatalf("assets folders: %+v", assets.children)
	}
	// A folder chain with no files of its own reads as one row.
	deep := scmTree([]gitFile{{Path: "pathA/pathB/file.go"}, {Path: "pathA/pathB/other.go"}})
	if len(deep.children) != 1 || deep.children[0].name != "pathA/pathB" || deep.children[0].dir != "pathA/pathB" {
		t.Fatalf("collapsed chain: %+v", deep.children)
	}
	if len(deep.children[0].files) != 2 {
		t.Fatalf("collapsed files: %+v", deep.children[0].files)
	}
	// A folder with its own files keeps its shape.
	split := scmTree([]gitFile{{Path: "pathA/pathB/x.go"}, {Path: "pathA/top.go"}})
	if len(split.children) != 1 || split.children[0].name != "pathA" ||
		len(split.children[0].children) != 1 || split.children[0].children[0].name != "pathB" {
		t.Fatalf("kept tree: %+v", split.children)
	}
	decorations := scmDecorations(" (HEAD -> main, origin/main, tag: v1)")
	if len(decorations) != 3 || decorations[0].label != "main" || decorations[1].label != "origin/main" || decorations[2].label != "v1" {
		t.Fatalf("decorations: %+v", decorations)
	}
}

func TestLoadDiff(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "gorex@example.invalid")
	runGit(t, dir, "config", "user.name", "GoRex Test")
	notes := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(notes, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "notes.txt")
	runGit(t, dir, "commit", "-m", "first")
	if err := os.WriteFile(notes, []byte("one\nchanged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo := &gitRepo{Dir: dir}
	check := func(name string, content previewContent, want ...string) {
		t.Helper()
		if content.kind != "diff" || content.message != "" {
			t.Fatalf("%s: %+v", name, content)
		}
		var text strings.Builder
		for _, line := range content.diffLines {
			text.WriteString(line.text)
			text.WriteByte('\n')
		}
		for _, part := range want {
			if !strings.Contains(text.String(), part) {
				t.Fatalf("%s lacks %q: %s", name, part, text.String())
			}
		}
		if len(content.diffRows) == 0 {
			t.Fatalf("%s has no side-by-side rows", name)
		}
	}
	check("unstaged", loadDiff(&Pane{}, repo, gitFile{Path: "notes.txt", X: '.', Y: 'M'}, false), "-two", "+changed")
	checkRow(t, "unstaged", loadDiff(&Pane{}, repo, gitFile{Path: "notes.txt", X: '.', Y: 'M'}, false), "two", "changed", 2, 2)
	runGit(t, dir, "add", "notes.txt")
	check("staged", loadDiff(&Pane{}, repo, gitFile{Path: "notes.txt", X: 'M', Y: '.'}, true), "-two", "+changed")
	checkRow(t, "staged", loadDiff(&Pane{}, repo, gitFile{Path: "notes.txt", X: 'M', Y: '.'}, true), "two", "changed", 2, 2)

	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	check("untracked", loadDiff(&Pane{}, repo, gitFile{Path: "new.txt", X: '.', Y: '?'}, false), "new file", "+new")
	untracked := loadDiff(&Pane{}, repo, gitFile{Path: "new.txt", X: '.', Y: '?'}, false)
	if len(untracked.diffRows) == 0 || untracked.diffRows[0].left.present || untracked.diffRows[0].right.text != "new" {
		t.Fatalf("untracked rows: %+v", untracked.diffRows)
	}
}

func TestParseDiffRows(t *testing.T) {
	diff := `diff --git a/x.txt b/x.txt
index 1111111..2222222 100644
--- a/x.txt
+++ b/x.txt
@@ -1,3 +1,3 @@
 one
-two
+changed
 three
@@ -10,1 +10,2 @@
 ten
+eleven
`
	rows := parseDiffRows(diff)
	var separators int
	for _, row := range rows {
		if row.separator {
			separators++
		}
	}
	if separators != 1 {
		t.Fatalf("%d separators in %+v, want 1", separators, rows)
	}
	checkRow(t, "replacement", previewContent{diffRows: rows}, "two", "changed", 2, 2)
	for _, row := range rows {
		if row.left.present && row.right.present && row.left.text == "one" {
			if row.left.number != 1 || row.right.number != 1 {
				t.Fatalf("context line numbers %d/%d", row.left.number, row.right.number)
			}
		}
		if row.right.present && row.right.text == "eleven" {
			if row.left.present || row.right.number != 11 || row.marker != "+" {
				t.Fatalf("addition row %+v", row)
			}
		}
	}
}

func checkRow(t *testing.T, name string, content previewContent, left, right string, leftNo, rightNo int) {
	t.Helper()
	for _, row := range content.diffRows {
		if row.left.present && row.right.present && row.left.text == left && row.right.text == right {
			if row.left.number != leftNo || row.right.number != rightNo {
				t.Fatalf("%s: line numbers %d/%d, want %d/%d", name, row.left.number, row.right.number, leftNo, rightNo)
			}
			if row.marker != "→" {
				t.Fatalf("%s: marker %q, want →", name, row.marker)
			}
			return
		}
	}
	t.Fatalf("%s: no row %q → %q in %+v", name, left, right, content.diffRows)
}

func TestSourceControlDiffPreview(t *testing.T) {
	requireGit(t)
	previous := prefs
	t.Cleanup(func() { prefs = previous })
	prefs.FileTree, prefs.FileTreeView, prefs.FileTreeWidth = true, "scm", 320

	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "gorex@example.invalid")
	runGit(t, dir, "config", "user.name", "GoRex Test")
	notes := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(notes, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "notes.txt")
	runGit(t, dir, "commit", "-m", "first")
	if err := os.WriteFile(notes, []byte("one\nchanged\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	a, tt := newTestApp(t)
	p := a.tab().Focus
	p.info.Dir, p.startDir = dir, dir
	waitFor(t, tt, "the changed file", func() bool {
		_, ok := tt.Find("Show diff for notes.txt")
		return ok
	})
	if err := tt.Click("Show diff for notes.txt"); err != nil {
		t.Fatal(err)
	}
	tab := a.tab()
	waitFor(t, tt, "the diff preview", func() bool {
		return tab.preview != nil && !tab.preview.loading
	})
	if !tab.preview.diff || tab.preview.content.kind != "diff" {
		t.Fatalf("preview is %+v, want a diff", tab.preview)
	}
	var text strings.Builder
	for _, line := range tab.preview.content.diffLines {
		text.WriteString(line.text)
		text.WriteByte('\n')
	}
	for _, part := range []string{"-two", "+changed"} {
		if !strings.Contains(text.String(), part) {
			t.Fatalf("the diff lacks %q: %s", part, text.String())
		}
	}
	left, ok := tt.Find("two")
	if !ok {
		t.Fatalf("the side-by-side view lacks the original line; texts %q", tt.Texts())
	}
	right, ok := tt.Find("changed")
	if !ok {
		t.Fatalf("the side-by-side view lacks the modified line; texts %q", tt.Texts())
	}
	if left.X >= right.X {
		t.Fatalf("the diff sides are not side by side: original %v, modified %v", left, right)
	}
}

func TestSCMCommands(t *testing.T) {
	repo := &gitRepo{Dir: "/work/project", Head: "abcdef1"}
	check := func(name string, got, want []string) {
		t.Helper()
		if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
			t.Fatalf("%s: %q, want %q", name, got, want)
		}
	}
	check("stage", scmStageArgs(repo, "a file.txt", false), []string{"add", "--", "a file.txt"})
	check("unstage", scmStageArgs(repo, "a file.txt", true), []string{"restore", "--staged", "--", "a file.txt"})
	check("stage all", scmStageArgs(repo, "", false), []string{"add", "-A"})
	check("unstage all", scmStageArgs(repo, "", true), []string{"restore", "--staged", "--", "."})
	bare := &gitRepo{Dir: "/work/project"}
	check("unstage before the first commit", scmStageArgs(bare, "a file.txt", true), []string{"rm", "--cached", "--", "a file.txt"})
	check("stage folder", scmStageDirArgs(repo, "assets/icons", false), []string{"add", "-A", "--", "assets/icons"})
	check("unstage folder", scmStageDirArgs(repo, "assets/icons", true), []string{"restore", "--staged", "--", "assets/icons"})
	check("unstage folder before the first commit", scmStageDirArgs(bare, "assets/icons", true), []string{"rm", "--cached", "-r", "--", "assets/icons"})
}

func TestSourceControlPanel(t *testing.T) {
	previous := prefs
	t.Cleanup(func() { prefs = previous })
	prefs.FileTree, prefs.FileTreeView, prefs.FileTreeWidth = true, "scm", 320

	a, tt := newTestApp(t)
	p := a.tab().Focus
	dir := p.directory()
	repo := &gitRepo{
		Dir:    dir,
		Branch: "main",
		Staged: []gitFile{
			{Path: "assets/icons/new.svg", X: 'A', Y: '.'},
			{Path: "main.go", X: 'M', Y: '.'},
		},
		Unstaged: []gitFile{
			{Path: "main.go", X: '.', Y: 'M'},
			{Path: "freelance.txt", X: '.', Y: '?'},
		},
	}
	a.gitMu.Lock()
	a.gitByDir = map[string]*gitCacheEntry{gitKey(dir): {repo: repo, checked: time.Now().Add(time.Hour)}}
	a.gitMu.Unlock()
	history := &gitHistory{Commits: []gitCommit{
		{Graph: "* ", Hash: "abc1234", Author: "Ziska", Subject: "feat: first commit", Decorations: " (HEAD -> main, origin/main)"},
	}}
	a.historyMu.Lock()
	a.historyByDir = map[string]*gitHistoryEntry{gitKey(dir): {history: history, checked: time.Now().Add(time.Hour)}}
	a.historyMu.Unlock()

	tt.Frame()
	for _, text := range []string{"Source Control", "Staged Changes", "Changes", "Graph", "Commit", "Toggle folder assets/icons", "new.svg", "freelance.txt", "feat: first commit", "origin/main"} {
		if _, ok := tt.Find(text); !ok {
			t.Fatalf("Source Control lacks %q; texts %q", text, tt.Texts())
		}
	}

	if err := tt.Click("Toggle folder assets/icons"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, ok := tt.Find("new.svg"); ok {
		t.Fatal("the folded folder shows its files")
	}
	if err := tt.Click("Toggle folder assets/icons"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, ok := tt.Find("new.svg"); !ok {
		t.Fatal("the unfolded folder lost its files")
	}

	// The folder row stages and unstages like a file, under its pointer.
	folder, ok := tt.Find("Toggle folder assets/icons")
	if !ok {
		t.Fatal("the collapsed folder lost its row")
	}
	tt.Move(folder.X+40, 2)
	tt.Frame()
	if _, ok := tt.Find("Unstage assets/icons"); ok {
		t.Fatal("the folder shows its button before the pointer arrives")
	}
	tt.Move(folder.X+40, folder.Y+folder.H/2)
	tt.Frame()
	if err := tt.Click("Unstage assets/icons"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, ok := tt.Find("new.svg"); !ok {
		t.Fatal("Unstage on the folder folded its tree")
	}

	if err := tt.Click("Stage All Changes"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.scmGroupExpanded("section:Changes", true) {
		t.Fatal("Stage All Changes folded its section")
	}

	if err := tt.Click("Show Files"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if prefs.FileTreeView != "" {
		t.Fatalf("the sidebar did not switch to files: %q", prefs.FileTreeView)
	}
	if err := tt.Click("Show Source Control"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if prefs.FileTreeView != "scm" {
		t.Fatalf("the sidebar did not switch to Source Control: %q", prefs.FileTreeView)
	}

	if err := tt.Click("Commit message"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Type("fix things")
	tt.Frame()
	if a.scmMessage != "fix things" {
		t.Fatalf("commit message %q", a.scmMessage)
	}
	tt.Key(ui.Ctrl, ui.KeyEnter)
	tt.Frame()
	if a.scmMessage != "" {
		t.Fatalf("Ctrl+Enter did not commit: %q", a.scmMessage)
	}
}

func TestSourceControlPanelWithoutRepository(t *testing.T) {
	previous := prefs
	t.Cleanup(func() { prefs = previous })
	prefs.FileTree, prefs.FileTreeView = true, "scm"

	a, tt := newTestApp(t)
	p := a.tab().Focus
	dir := p.directory()
	a.gitMu.Lock()
	a.gitByDir = map[string]*gitCacheEntry{gitKey(dir): {checked: time.Now().Add(time.Hour)}}
	a.gitMu.Unlock()
	tt.Frame()
	if _, ok := tt.Find("No Git repository in this folder."); !ok {
		t.Fatalf("the pane does not say there is no repository; texts %q", tt.Texts())
	}
}
