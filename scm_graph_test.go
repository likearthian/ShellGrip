package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestSCMGraphLayout(t *testing.T) {
	history := &gitHistory{Commits: []gitCommit{
		{ID: "merge", Parents: []string{"main", "feature"}},
		{ID: "main", Parents: []string{"base"}},
		{ID: "feature", Parents: []string{"base"}},
		{ID: "base"},
	}}
	rows := scmGraphLayout(history)
	if len(rows) != 4 || rows[0].lanes != 2 || rows[1].lane != 0 || rows[2].lane != 1 || rows[3].lane != 0 {
		t.Fatalf("bad branch lanes: %+v", rows)
	}
	if rows[0].color != rows[1].color || rows[1].color != rows[3].color || rows[2].color == rows[1].color {
		t.Fatalf("branch colors changed: %+v", rows)
	}
	joined := false
	for _, edge := range rows[2].edges {
		if edge.from == 1 && edge.to == 0 && edge.top == scmGraphRowHeight/2 && edge.bottom == scmGraphRowHeight {
			joined = true
		}
	}
	if !joined {
		t.Fatal("feature lane does not join its shared parent")
	}
	for i := 0; i < len(rows)-1; i++ {
		bottom, top := map[int]int{}, map[int]int{}
		for _, edge := range rows[i].edges {
			if edge.bottom == scmGraphRowHeight {
				bottom[edge.to] = edge.color
			}
		}
		for _, edge := range rows[i+1].edges {
			if edge.top == 0 {
				top[edge.from] = edge.color
			}
		}
		for lane, color := range bottom {
			if next, ok := top[lane]; !ok || next != color {
				t.Fatalf("disconnected rail at row %d lane %d", i, lane)
			}
		}
	}
}

func graphTestPanel(t *testing.T) (*App, *ui.Tester) {
	t.Helper()
	registerFonts()
	t.Setenv("SHELLGRIP_DIR", t.TempDir())
	previous := prefs
	t.Cleanup(func() { prefs = previous })
	prefs.SCMGraphRatio = 0
	dir := t.TempDir()
	p := &Pane{startDir: dir}
	repo := &gitRepo{Dir: dir, Branch: "main", Head: "abcdef1"}
	history := &gitHistory{}
	for i := range 40 {
		repo.Unstaged = append(repo.Unstaged, gitFile{Path: fmt.Sprintf("file-%02d.go", i), X: '.', Y: 'M'})
		commit := gitCommit{ID: fmt.Sprint(i), Hash: fmt.Sprintf("abc%04d", i), Subject: fmt.Sprintf("Commit message %02d", i), Author: "Ziska"}
		if i < 39 {
			commit.Parents = []string{fmt.Sprint(i + 1)}
		}
		if i == 0 {
			commit.Decorations = "HEAD -> main, origin/main"
		}
		history.Commits = append(history.Commits, commit)
	}
	a := &App{
		gitByDir:     map[string]*gitCacheEntry{gitKey(dir): {repo: repo, checked: time.Now().Add(time.Hour)}},
		historyByDir: map[string]*gitHistoryEntry{gitKey(dir): {history: history, checked: time.Now().Add(time.Hour)}},
	}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() { a.sourceControlPanel(c, colorsOf(c), p, 380) })
	}, 380, 700)
	tt.Frame()
	return a, tt
}

func TestSCMGraphSplitResizeAndScroll(t *testing.T) {
	_, tt := graphTestPanel(t)
	tree, ok := tt.Find("Source Control Tree")
	graph, _ := tt.Find("Source Control Graph Pane")
	sash, _ := tt.Find("Resize Source Control Graph")
	if !ok || tree.H < 140 || graph.H < 90 || tree.Y+tree.H > graph.Y || sash.H != scmGraphSashHeight {
		t.Fatalf("not a vertical split: %v / %v / %v", tree, sash, graph)
	}
	x, y := sash.X+sash.W/2, sash.Y+sash.H/2
	tt.Press(x, y)
	tt.Frame()
	tt.Move(x, y+60)
	tt.Frame()
	tt.Release(x, y+60)
	tt.Frame()
	resized, _ := tt.Find("Source Control Graph Pane")
	if math.Abs(float64(resized.H-(graph.H-60))) > 1 {
		t.Fatalf("resize height %v, want %v", resized.H, graph.H-60)
	}
	data, err := os.ReadFile(settingsPath())
	if err != nil {
		t.Fatal(err)
	}
	var saved settings
	if err := json.Unmarshal(data, &saved); err != nil || saved.SCMGraphRatio != prefs.SCMGraphRatio || saved.SCMGraphRatio == 0 {
		t.Fatalf("split ratio not saved: %s", data)
	}
	header, _ := tt.Find("Toggle Source Control Graph")
	commitBefore, _ := tt.Find("Commit abc0000")
	tt.Scroll(tree.X+100, tree.Y+100, 0, 200)
	tt.Frame()
	headerAfter, _ := tt.Find("Toggle Source Control Graph")
	commitAfter, _ := tt.Find("Commit abc0000")
	if header.Y != headerAfter.Y || commitBefore.Y != commitAfter.Y {
		t.Fatal("scrolling the source tree moved graph content")
	}
	fileBefore, _ := tt.Find("Show diff for file-12.go")
	tt.Scroll(resized.X+150, resized.Y+60, 0, 140)
	tt.Frame()
	fileAfter, _ := tt.Find("Show diff for file-12.go")
	commitAfter, _ = tt.Find("Commit abc0000")
	if fileBefore.Y != fileAfter.Y || commitAfter.W != 0 {
		t.Fatalf("graph scrolling did not stay independent: file %v -> %v, commit %v -> %v, pane %v", fileBefore, fileAfter, commitBefore, commitAfter, resized)
	}
	if err := tt.Click("Toggle Source Control Graph"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	collapsed, _ := tt.Find("Source Control Graph Pane")
	if collapsed.H != scmGraphHeaderHeight {
		t.Fatalf("collapsed height %v", collapsed.H)
	}
	if err := tt.Click("Toggle Source Control Graph"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	reopened, _ := tt.Find("Source Control Graph Pane")
	if reopened.H != resized.H {
		t.Fatal("expanding the graph lost its split size")
	}
	sash, _ = tt.Find("Resize Source Control Graph")
	x, y = sash.X+sash.W/2, sash.Y+sash.H/2
	tt.ClickAt(x, y)
	tt.ClickAt(x, y)
	tt.Frame()
	if prefs.SCMGraphRatio != defaultSCMGraphRatio {
		t.Fatal("double-click did not reset graph size")
	}
	tt.SetSize(380, 260)
	tt.Frame()
	tt.Frame()
	graph, _ = tt.Find("Source Control Graph Pane")
	tree, _ = tt.Find("Source Control Tree")
	if graph.Y+graph.H > 260 || tree.H <= 0 {
		t.Fatalf("small window lost a pane: %v / %v", tree, graph)
	}
}

func TestLoadGitGraphParents(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.name", "Graph Test")
	runGit(t, dir, "config", "user.email", "graph@example.invalid")
	runGit(t, dir, "commit", "--allow-empty", "-m", "base")
	base := strings.TrimSpace(runGit(t, dir, "rev-parse", "HEAD"))
	runGit(t, dir, "checkout", "-b", "feature")
	runGit(t, dir, "commit", "--allow-empty", "-m", "feature")
	runGit(t, dir, "checkout", "main")
	runGit(t, dir, "commit", "--allow-empty", "-m", "main")
	runGit(t, dir, "merge", "--no-ff", "feature", "-m", "merge")
	history := loadGitHistory(&Pane{}, dir)
	if len(history.Commits) != 4 || len(history.Commits[0].Parents) != 2 {
		t.Fatalf("history omitted merge parents: %+v", history.Commits)
	}
	if history.Commits[3].ID != base || history.Commits[0].Author != "Graph Test" || history.Commits[0].Subject != "merge" {
		t.Fatalf("history parsing: %+v", history.Commits)
	}
	if len(scmGraphLayout(history)) != 4 {
		t.Fatal("graph must have one row per commit")
	}
}
