package main

import (
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

// scmNode is a folder of the Source Control tree: the changed files
// directly in it and the folders below.
type scmNode struct {
	dir      string
	name     string
	files    []gitFile
	children []*scmNode
}

// scmTree builds the folder hierarchy of changed files, so that the pane
// follows the repository's tree rather than flattening each parent folder.
func scmTree(files []gitFile) *scmNode {
	root := &scmNode{}
	nodes := map[string]*scmNode{"": root}
	for _, file := range files {
		dir := path.Dir(file.Path)
		if dir == "." {
			dir = ""
		}
		node := nodes[dir]
		if node == nil {
			parent := root
			prefix := ""
			for _, part := range strings.Split(dir, "/") {
				prefix = path.Join(prefix, part)
				child := nodes[prefix]
				if child == nil {
					child = &scmNode{dir: prefix, name: part}
					nodes[prefix] = child
					parent.children = append(parent.children, child)
				}
				parent = child
			}
			node = parent
		}
		node.files = append(node.files, file)
	}
	sortSCMNode(root)
	for _, child := range root.children {
		collapseSCMNode(child)
	}
	return root
}

// collapseSCMNode folds a folder that holds nothing but one folder into a
// single row named "parent/child", the way the tree reads deep change paths
// instead of nesting one row per step. A folder with files, or with more
// than one folder below, keeps its shape.
func collapseSCMNode(node *scmNode) {
	for _, child := range node.children {
		collapseSCMNode(child)
	}
	for len(node.children) == 1 && len(node.files) == 0 {
		only := node.children[0]
		node.name += "/" + only.name
		node.dir = only.dir
		node.files = only.files
		node.children = only.children
	}
}

func sortSCMNode(node *scmNode) {
	sort.SliceStable(node.children, func(i, j int) bool {
		return strings.ToLower(node.children[i].name) < strings.ToLower(node.children[j].name)
	})
	sort.SliceStable(node.files, func(i, j int) bool {
		return strings.ToLower(path.Base(node.files[i].Path)) < strings.ToLower(path.Base(node.files[j].Path))
	})
	for _, child := range node.children {
		sortSCMNode(child)
	}
}

// sourceControlPanel draws the sidebar's Source Control view: a commit
// message, the staged and unstaged changes, and the recent graph.
func (a *App) sourceControlPanel(c *ui.Context, k *colors, p *Pane, width float32) {
	k = sidebarColors(c, k)
	repo := a.gitFor(c, p)
	if a.focusedWin {
		c.After(2 * time.Second)
	}
	ui.Column(c.Key("source-control")).Width(width).Shrink(0).MinHeight(0).
		Radius(cardR).Background(k.panel).Clip().Children(func() {
		ui.Row(c).Height(36).Padding(0, 10).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Source Control").FontSize(13).FontWeight(500).TextColor(k.text).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
			if iconButton(c, k, "tree-file", "Show Files", 24, 14).Tooltip("Files").Clicked() {
				prefs.FileTreeView = ""
				saveSettings()
				c.Invalidate()
			}
			if iconButton(c, k, "rotate-ccw", "Refresh Source Control", 24, 14).Tooltip("Refresh").Clicked() {
				a.invalidateGit(p.directory())
				if repo != nil {
					a.invalidateGitHistory(repo.Dir)
				}
				c.Invalidate()
			}
			if iconButton(c, k, "x", "Hide Sidebar", 24, 14).Tooltip("Hide Sidebar").Clicked() {
				a.toggleFileTree()
			}
		})
		if repo == nil {
			message := "No Git repository in this folder."
			if a.gitLoading(p.directory()) {
				message = "Loading repository…"
			}
			ui.Text(c, message).FontSize(12).TextColor(k.textFaint).Padding(6, 10)
			return
		}
		if a.scmDir != repo.Dir {
			if a.scmDir != "" {
				if a.scmMessages == nil {
					a.scmMessages = map[string]string{}
				}
				if a.scmMessage != "" {
					a.scmMessages[a.scmDir] = a.scmMessage
				} else {
					delete(a.scmMessages, a.scmDir)
				}
			}
			a.scmDir, a.scmMessage, a.scmExpanded = repo.Dir, a.scmMessages[repo.Dir], nil
			a.scmGraphSelected = ""
		}
		ui.Row(c).Height(24).Padding(0, 10).Gap(5).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icon("git-branch")).Size(12, 12).TextColor(k.textMuted)
			branch := repo.Branch
			if branch == "" {
				branch = repo.Head
			}
			ui.Text(c, branch).FontSize(11.5).TextColor(k.textMuted).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
			var counts []string
			if repo.Ahead > 0 {
				counts = append(counts, fmt.Sprintf("↑%d", repo.Ahead))
			}
			if repo.Behind > 0 {
				counts = append(counts, fmt.Sprintf("↓%d", repo.Behind))
			}
			if len(counts) > 0 {
				ui.Text(c, strings.Join(counts, " ")).FontSize(10.5).TextColor(k.textFaint).Shrink(0)
			}
		})
		split := ui.Column(c.Key(repo.Dir)).Grow(1).MinHeight(0)
		split.Children(func() {
			available := split.Bounds().H
			if available <= 0 {
				available = max(0, c.Root().Bounds().H-titleH-gap-60)
				c.Invalidate()
			}
			expanded := a.scmGroupExpanded("graph", true)
			height := float32(scmGraphHeaderHeight)
			if expanded {
				height = scmGraphHeight(prefs.SCMGraphRatio, available)
			}
			ui.Scroll(c.Key("source-control-changes")).Label("Source Control Tree").Grow(1).MinHeight(0).Padding(4, 6).Children(func() {
				ui.Column(c).Gap(10).Children(func() {
					a.scmCommitBox(c, k, p, repo)
					if len(repo.Staged) > 0 {
						a.scmChanges(c, k, p, repo, "Staged Changes", repo.Staged, true)
					}
					if len(repo.Unstaged) > 0 {
						a.scmChanges(c, k, p, repo, "Changes", repo.Unstaged, false)
					}
					if len(repo.Staged) == 0 && len(repo.Unstaged) == 0 {
						ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
							ui.Icon(c, icon("check")).Size(14, 14).TextColor(k.busy)
							ui.Text(c, "No changes").FontSize(12).TextColor(k.textFaint)
						})
					}
				})
			})
			if expanded {
				a.scmGraphDivider(c, k, height, available)
			}
			ui.Column(c.Key("source-control-graph")).Height(height).Shrink(0).MinHeight(0).Clip().Label("Source Control Graph Pane").Children(func() {
				a.scmGraph(c, k, p, repo)
			})
		})
	})
}

// scmCommitBox is the message and Commit button at the top of the pane.
func (a *App) scmCommitBox(c *ui.Context, k *colors, p *Pane, repo *gitRepo) {
	branch := repo.Branch
	if branch == "" {
		branch = "HEAD"
	}
	ready := len(repo.Staged) > 0 && strings.TrimSpace(a.scmMessage) != ""
	in := ui.TextAreaBase(c, &a.scmMessage).Label("Commit message").FontSize(12.5).Height(62).
		Padding(7).Radius(6).Background(k.hover).Border(1, k.panelBorder).
		Placeholder("Message (Ctrl+Enter to commit on \"" + branch + "\")")
	commit := in.Shortcut(ui.Ctrl, ui.KeyEnter) || in.Shortcut(ui.Super, ui.KeyEnter)
	ui.Row(c).Gap(6).Children(func() {
		button := ui.Button(c, "Commit").Grow(1).Height(30)
		button.Disabled(!ready)
		if ready {
			button.Background(ui.Hex("#3b6fd4")).TextColor(ui.Hex("#ffffff"))
		}
		if button.Clicked() {
			a.commitSourceControl(p, repo)
		}
	})
	if commit {
		a.commitSourceControl(p, repo)
	}
}

func (a *App) commitSourceControl(p *Pane, repo *gitRepo) {
	message := strings.TrimSpace(a.scmMessage)
	if message == "" || len(repo.Staged) == 0 {
		return
	}
	file, err := writeCommitMessage(p, message)
	if err != nil {
		a.err = "Could not save the commit message: " + err.Error()
		return
	}
	a.runGitAction(p, repo.Dir, "commit", "-F", file)
	a.scmMessage = ""
	delete(a.scmMessages, repo.Dir)
}

// scmChanges draws a section of changed files, each folder folded apart.
// Its rows sit in a column of their own, so the pane's section gap does not
// stretch the tree.
func (a *App) scmChanges(c *ui.Context, k *colors, p *Pane, repo *gitRepo, title string, files []gitFile, staged bool) {
	key := "section:" + title
	expanded := a.scmGroupExpanded(key, true)
	ui.Column(c).Gap(0).Children(func() {
		section := ui.Row(c).Height(28).Padding(0, 4).Gap(6).AlignItems(ui.Center).
			Cursor(ui.CursorPointer).Role(ui.RoleButton).Label(title)
		acted := false
		if section.Hovered() {
			section.Background(k.hover)
		}
		section.Children(func() {
			ui.Icon(c, icon(scmChevron(expanded))).Size(12, 12).TextColor(k.textFaint).Shrink(0)
			ui.Text(c, title).FontSize(12).FontWeight(600).TextColor(k.textMuted).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
			ui.Text(c, fmt.Sprint(len(files))).FontSize(10).TextColor(k.textMuted).Padding(1, 5).Radius(8).Background(k.hover).Shrink(0)
			glyph, label := "plus", "Stage All Changes"
			if staged {
				glyph, label = "minus", "Unstage All Changes"
			}
			if iconButton(c, k, glyph, label, 20, 12).Tooltip(label).Clicked() {
				acted = true
				a.stageSourceControl(p, repo, "", staged)
			}
		})
		if section.Clicked() && !acted {
			a.scmExpanded[key] = !expanded
			c.Invalidate()
		}
		if !expanded {
			return
		}
		a.scmTreeNodes(c, k, p, repo, title, scmTree(files), staged, 0)
	})
}

func scmChevron(expanded bool) string {
	if expanded {
		return "chevron-down"
	}
	return "chevron-right"
}

// scmTreeNodes draws a folder's children and then its files, each folder
// folded on its own.
func (a *App) scmTreeNodes(c *ui.Context, k *colors, p *Pane, repo *gitRepo, section string, node *scmNode, staged bool, depth int) {
	for _, child := range node.children {
		a.scmFolder(c, k, p, repo, section, child, staged, depth)
	}
	for _, file := range node.files {
		a.scmFile(c, k, p, repo, file, staged, depth)
	}
}

func (a *App) scmFolder(c *ui.Context, k *colors, p *Pane, repo *gitRepo, section string, node *scmNode, staged bool, depth int) {
	key := "group:" + section + ":" + node.dir
	expanded := a.scmGroupExpanded(key, true)
	row := ui.Row(c).Height(28).Padding(0, 6, 0, 6+float32(depth)*18).Gap(5).AlignItems(ui.Center).Radius(4).
		Cursor(ui.CursorPointer).Role(ui.RoleButton).Label("Toggle folder " + node.name)
	hovered := row.Hovered()
	if hovered {
		row.Background(k.hover)
	}
	acted := false
	row.Children(func() {
		ui.Icon(c, icon(scmChevron(expanded))).Size(14, 14).TextColor(k.textFaint).Shrink(0)
		ui.Icon(c, icon("tree-folder")).Size(17, 17).TextColor(ui.Hex("#5893f8")).Shrink(0)
		ui.Text(c, node.name).FontSize(13).TextColor(k.textMuted).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
		if hovered {
			mark, label := "plus", "Stage "+node.dir
			if staged {
				mark, label = "minus", "Unstage "+node.dir
			}
			if iconButton(c, k, mark, label, 18, 11).Tooltip(label).Clicked() {
				acted = true
				a.stageSourceControlDir(p, repo, node.dir, staged)
			}
		} else {
			ui.Box(c).Size(6, 6).Radius(3).Background(k.busy).Shrink(0)
		}
	})
	if row.Clicked() && !acted {
		a.scmExpanded[key] = !expanded
		c.Invalidate()
	}
	if !expanded {
		return
	}
	a.scmTreeNodes(c, k, p, repo, section, node, staged, depth+1)
}

func (a *App) scmFile(c *ui.Context, k *colors, p *Pane, repo *gitRepo, file gitFile, staged bool, depth int) {
	name := path.Base(file.Path)
	glyph, color := fileTreeGlyph(fileTreeRow{path: file.Path, name: name}, k)
	side := "changes:"
	if staged {
		side = "staged:"
	}
	row := ui.Row(c.Key(side+file.Path)).Height(28).Padding(0, 6, 0, 30+float32(depth)*18).Gap(5).AlignItems(ui.Center).Radius(4).
		Tooltip(file.Path)
	row.Cursor(ui.CursorPointer).Role(ui.RoleButton).Label("Show diff for " + file.Path)
	hovered := row.Hovered()
	if hovered {
		row.Background(k.hover)
	}
	acted := false
	row.Children(func() {
		ui.Icon(c, icon(glyph)).Size(17, 17).TextColor(color).Shrink(0)
		ui.Text(c, name).FontSize(13).TextColor(k.text).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
		if hovered {
			mark, label := "plus", "Stage "+file.Path
			if staged {
				mark, label = "minus", "Unstage "+file.Path
			}
			if iconButton(c, k, mark, label, 18, 11).Tooltip(label).Clicked() {
				acted = true
				a.stageSourceControl(p, repo, file.Path, staged)
			}
		} else {
			letter, color := scmStatus(file, staged)
			ui.Text(c, letter).FontSize(11).FontWeight(700).TextColor(color).Shrink(0)
		}
	})
	if row.Clicked() && !acted {
		a.openDiffPreview(c, p, repo, file, staged)
	}
}

// scmStatus is the letter and color of a change, as Source Control shows
// it: A for added, M for modified, D for deleted, U for untracked.
func scmStatus(file gitFile, staged bool) (string, ui.Color) {
	code := file.Y
	if staged {
		code = file.X
	}
	switch code {
	case 'A':
		return "A", ui.Hex("#73c991")
	case 'M':
		return "M", ui.Hex("#e2c08d")
	case 'D':
		return "D", ui.Hex("#f14c4c")
	case 'R', 'C':
		return string(code), ui.Hex("#3b9eff")
	case 'U':
		return "!", ui.Hex("#e2c08d")
	case '?':
		return "U", ui.Hex("#73c991")
	}
	return string(code), ui.Hex("#8e8e93")
}

// stageSourceControl stages or unstages one path, or all of them when
// name is empty.
func (a *App) stageSourceControl(p *Pane, repo *gitRepo, name string, unstage bool) {
	a.runGitAction(p, repo.Dir, scmStageArgs(repo, name, unstage)...)
}

// stageSourceControlDir stages or unstages every change under a folder.
func (a *App) stageSourceControlDir(p *Pane, repo *gitRepo, dir string, unstage bool) {
	a.runGitAction(p, repo.Dir, scmStageDirArgs(repo, dir, unstage)...)
}

// scmStageDirArgs is the git command for a whole folder. add takes -A so
// the deletions inside it travel with the edits, and the index clear
// before the first commit goes recursive.
func scmStageDirArgs(repo *gitRepo, dir string, unstage bool) []string {
	if unstage {
		if repo.Head == "" {
			return []string{"rm", "--cached", "-r", "--", dir}
		}
		return []string{"restore", "--staged", "--", dir}
	}
	return []string{"add", "-A", "--", dir}
}

// scmStageArgs is the git command that stages or unstages one path, or all
// of them when name is empty.
func scmStageArgs(repo *gitRepo, name string, unstage bool) []string {
	if name == "" {
		if unstage {
			if repo.Head == "" {
				return []string{"rm", "--cached", "-r", "--", "."}
			}
			return []string{"restore", "--staged", "--", "."}
		}
		return []string{"add", "-A"}
	}
	if unstage {
		if repo.Head == "" {
			return []string{"rm", "--cached", "--", name}
		}
		return []string{"restore", "--staged", "--", name}
	}
	return []string{"add", "--", name}
}

func (a *App) scmGroupExpanded(key string, fallback bool) bool {
	if a.scmExpanded == nil {
		a.scmExpanded = map[string]bool{}
	}
	if expanded, ok := a.scmExpanded[key]; ok {
		return expanded
	}
	return fallback
}

type scmDecoration struct {
	label string
	color ui.Color
}

// scmDecorations turns Git's ref decorations, as
// " (HEAD -> main, origin/main, tag: v1)", into labels.
func scmDecorations(raw string) []scmDecoration {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "(")
	raw = strings.TrimSuffix(raw, ")")
	if raw == "" {
		return nil
	}
	var out []scmDecoration
	for _, part := range strings.Split(raw, ", ") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		label, color := part, ui.Hex("#7aa2f7")
		switch {
		case strings.HasPrefix(part, "HEAD -> "):
			label, color = strings.TrimPrefix(part, "HEAD -> "), ui.Hex("#e2c08d")
		case strings.HasPrefix(part, "tag: "):
			label, color = strings.TrimPrefix(part, "tag: "), ui.Hex("#d7a86e")
		case strings.Contains(part, "/"):
			color = ui.Hex("#9aa5b1")
		}
		out = append(out, scmDecoration{label: label, color: color})
		if len(out) == 3 {
			break
		}
	}
	return out
}
