package main

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
)

// gitMenuState is the branch menu of a pane's title bar.
type gitMenuState struct {
	open     bool
	pane     *Pane
	query    string
	selected int
	// expanded remembers which remote and tag folders the menu unfolds,
	// by "remote:origin" or "tag:agent".
	expanded map[string]bool
}

// gitPromptState asks for what an action needs before it runs in the
// shell: a commit message, a branch name, or a revision.
type gitPromptState struct {
	open   bool
	kind   string
	pane   *Pane
	dir    string
	title  string
	detail string
	value  string
	error  string
}

// gitEntryKind tells how a row of the branch menu shows and behaves.
type gitEntryKind int

const (
	gitEntryAction gitEntryKind = iota
	gitEntrySection
	gitEntryGroup
	gitEntryBranch
	gitEntryTag
	gitEntryDivider
)

// gitMenuEntry is a row of the branch menu.
type gitMenuEntry struct {
	kind     gitEntryKind
	label    string
	glyph    string
	depth    int
	current  bool
	expanded bool
	run      func()
}

// gitChip shows the current branch in a pane's header; clicking it opens
// the branch menu.
func (a *App) gitChip(c *ui.Context, k *colors, p *Pane, repo *gitRepo) {
	name := repo.Branch
	if name == "" {
		name = repo.Head
	}
	if name == "" {
		return
	}
	open := a.gitMenu.open && a.gitMenu.pane == p
	chip := ui.Row(c).Gap(5).Padding(2, 7, 2, 5).Radius(6).AlignItems(ui.Center).
		Cursor(ui.CursorPointer).Role(ui.RoleButton).Label("Switch branch: " + name).MaxWidth(170).Shrink(0)
	if chip.Hovered() || open {
		chip.Background(k.hover)
	}
	if chip.Clicked() {
		if open {
			a.gitMenu.open = false
			a.focusReq = p
		} else {
			a.gitMenu = gitMenuState{open: true, pane: p, expanded: map[string]bool{}}
		}
	}
	chip.Children(func() {
		ui.Icon(c, icon("git-branch")).Size(12.5, 12.5).TextColor(k.textMuted).Shrink(0)
		ui.Text(c, name).FontSize(11.5).FontWeight(600).TextColor(k.text).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
		ui.Icon(c, icon("chevron-down")).Size(11, 11).TextColor(k.textFaint).Shrink(0)
	})
	if !a.gitMenu.open || a.gitMenu.pane != p {
		return
	}
	ui.PopoverBase(c, chip, &a.gitMenu.open, func(panel ui.Element) {
		panel.Role(ui.RoleMenu).Label("Git branch menu").Width(340).Padding(5).Radius(10).
			Background(k.panel).Border(1, k.panelBorder).Shadow(0, 14, 36, -4, k.shadowFocused)
		a.gitMenuPanel(c, k, p, repo, panel)
	})
	if !a.gitMenu.open && a.focusReq == nil && !a.gitPrompt.open {
		a.focusReq = p
	}
}

// gitMenuPanel fills the popover of the branch menu.
func (a *App) gitMenuPanel(c *ui.Context, k *colors, p *Pane, repo *gitRepo, panel ui.Element) {
	entries := a.gitMenuEntries(p, repo)
	selectable := gitSelectable(entries)
	a.gitMenu.selected = min(max(a.gitMenu.selected, 0), max(len(selectable)-1, 0))

	ui.Row(c).Padding(7, 8, 7, 8).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, icon("search")).Size(16, 16).TextColor(k.textFaint).Shrink(0)
		in := ui.TextInputBase(c, &a.gitMenu.query).Grow(1).FontSize(13).Placeholder("Search for branches and actions").Label("Search branches and actions").AutoFocus()
		if in.Changed() {
			a.gitMenu.selected = 0
		}
		if in.Shortcut(0, ui.KeyDown) {
			a.gitMenuMove(selectable, 1)
		}
		if in.Shortcut(0, ui.KeyUp) {
			a.gitMenuMove(selectable, -1)
		}
		if in.Submitted() && len(selectable) > 0 {
			a.runGitMenuEntry(entries[selectable[a.gitMenu.selected]])
		}
	})
	ui.Divider(c)
	height := min(float32(500), max(float32(170), c.Root().Bounds().H-150))
	ui.Scroll(c).Height(height).Children(func() {
		if len(selectable) == 0 {
			ui.Text(c, "No matches").FontSize(12.5).TextColor(k.textFaint).Padding(12)
		}
		ui.Column(c).Gap(1).Children(func() {
			selected := map[int]int{}
			for i, index := range selectable {
				selected[index] = i
			}
			for i, entry := range entries {
				a.gitMenuRow(c, k, entry, selected[i], &a.gitMenu)
			}
		})
	})
}

// gitMenuMove moves the selection through the rows that run something.
func (a *App) gitMenuMove(selectable []int, direction int) {
	if len(selectable) == 0 {
		a.gitMenu.selected = 0
		return
	}
	n := len(selectable)
	a.gitMenu.selected = ((a.gitMenu.selected+direction)%n + n) % n
}

// gitSelectable returns the rows a selection visits: everything but
// dividers.
func gitSelectable(entries []gitMenuEntry) []int {
	var out []int
	for i, entry := range entries {
		if entry.kind != gitEntryDivider {
			out = append(out, i)
		}
	}
	return out
}

// gitMenuRow draws a row of the branch menu.
func (a *App) gitMenuRow(c *ui.Context, k *colors, entry gitMenuEntry, selectedIndex int, state *gitMenuState) {
	switch entry.kind {
	case gitEntryDivider:
		ui.Box(c).Height(1).Margin(4, 5).Background(k.panelBorder)
		return
	case gitEntrySection:
		row := ui.Row(c).Height(28).Padding(0, 9).Gap(8).Radius(5).AlignItems(ui.Center).
			Cursor(ui.CursorPointer).Role(ui.RoleMenuItem).Label(entry.label)
		if row.Hovered() {
			state.selected = selectedIndex
		}
		if selectedIndex == state.selected {
			row.Background(k.panelSel)
		}
		row.Children(func() {
			chevron := "chevron-right"
			if entry.expanded {
				chevron = "chevron-down"
			}
			ui.Icon(c, icon(chevron)).Size(11, 11).TextColor(k.textFaint).Shrink(0)
			ui.Text(c, entry.label).FontSize(12).FontWeight(600).TextColor(k.textFaint).Grow(1)
		})
		if row.Clicked() {
			a.runGitMenuEntry(entry)
		}
		return
	}
	row := ui.Row(c).Height(30).Padding(0, 9).Gap(8).Radius(5).AlignItems(ui.Center).
		Cursor(ui.CursorPointer).Role(ui.RoleMenuItem).Label(entry.label)
	if entry.depth > 0 {
		row.Padding(0, 9, 0, 9+float32(entry.depth)*14)
	}
	if row.Hovered() {
		state.selected = selectedIndex
	}
	selected := selectedIndex == state.selected
	if selected {
		row.Background(k.panelSel)
	}
	row.Children(func() {
		color := k.textMuted
		if entry.current {
			color = k.busy
		}
		if entry.glyph != "" {
			ui.Icon(c, icon(entry.glyph)).Size(13, 13).TextColor(color).Shrink(0)
		} else {
			ui.Box(c).Size(13, 13).Shrink(0)
		}
		ui.Text(c, entry.label).FontSize(12.5).FontWeight(500).TextColor(k.text).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
		if entry.kind == gitEntryGroup {
			chevron := "chevron-right"
			if entry.expanded {
				chevron = "chevron-down"
			}
			ui.Icon(c, icon(chevron)).Size(11, 11).TextColor(k.textFaint).Shrink(0)
		}
	})
	if row.Clicked() {
		a.runGitMenuEntry(entry)
	}
}

// runGitMenuEntry runs a row: actions close the menu, while a section or
// a folder folds or unfolds in place.
func (a *App) runGitMenuEntry(entry gitMenuEntry) {
	if entry.kind != gitEntryGroup && entry.kind != gitEntrySection {
		a.gitMenu.open = false
	}
	if entry.run != nil {
		entry.run()
	}
}

// gitMenuEntries builds the rows of the branch menu, narrowed by its
// search query.
func (a *App) gitMenuEntries(p *Pane, repo *gitRepo) []gitMenuEntry {
	state := &a.gitMenu
	if state.expanded == nil {
		state.expanded = map[string]bool{}
	}
	query := strings.ToLower(strings.TrimSpace(state.query))
	match := func(parts ...string) bool {
		if query == "" {
			return true
		}
		for _, s := range parts {
			if fuzzy(strings.ToLower(s), query) {
				return true
			}
		}
		return false
	}

	var entries []gitMenuEntry
	add := func(to *[]gitMenuEntry, label, glyph string, run func()) {
		if match(label) {
			*to = append(*to, gitMenuEntry{kind: gitEntryAction, label: label, glyph: glyph, run: run})
		}
	}
	var update []gitMenuEntry
	add(&update, "Update Project…", "check", func() {
		a.runGitAction(p, repo.Dir, "pull")
	})
	add(&update, "Commit…", "circle-dot", func() {
		a.openGitPrompt(p, repo, "commit")
	})
	add(&update, "Push…", "arrow-up", func() {
		if repo.Branch != "" && repo.Upstream == "" {
			a.runGitAction(p, repo.Dir, "push", "-u", "origin", repo.Branch)
			return
		}
		a.runGitAction(p, repo.Dir, "push")
	})
	var manage []gitMenuEntry
	add(&manage, "New Branch…", "plus", func() {
		a.openGitPrompt(p, repo, "branch")
	})
	add(&manage, "Checkout Tag or Revision…", "tag", func() {
		a.openGitPrompt(p, repo, "checkout")
	})
	entries = append(entries, update...)
	if len(update) > 0 && len(manage) > 0 {
		entries = append(entries, gitMenuEntry{kind: gitEntryDivider})
	}
	entries = append(entries, manage...)

	if query != "" {
		for _, ref := range repo.Locals {
			if match(ref.Name) {
				entries = append(entries, a.gitBranchEntry(p, repo, ref))
			}
		}
		for _, ref := range repo.Remotes {
			if match(ref.Name) {
				entries = append(entries, a.gitRemoteEntry(p, repo, ref))
			}
		}
		for _, ref := range repo.Tags {
			if match(ref.Name) {
				entries = append(entries, a.gitTagEntry(p, repo, ref, ""))
			}
		}
		return entries
	}

	section := func(label string) bool {
		key := "section:" + label
		expanded := a.gitGroupExpanded(key, true)
		entries = append(entries, gitMenuEntry{kind: gitEntrySection, label: label, expanded: expanded, run: func() {
			state.expanded[key] = !expanded
		}})
		return expanded
	}

	if len(repo.Locals) > 0 && section("Local") {
		for _, ref := range repo.Locals {
			entries = append(entries, a.gitBranchEntry(p, repo, ref))
		}
	}
	if len(repo.Remotes) > 0 && section("Remote") {
		for _, remote := range gitRemotes(repo.Remotes) {
			key := "remote:" + remote
			expanded := a.gitGroupExpanded(key, true)
			entries = append(entries, gitMenuEntry{kind: gitEntryGroup, label: remote, glyph: "folder", depth: 1, expanded: expanded, run: func() {
				state.expanded[key] = !expanded
			}})
			if !expanded {
				continue
			}
			for _, ref := range repo.Remotes {
				if ref.Remote == remote {
					entry := a.gitRemoteEntry(p, repo, ref)
					entry.depth = 2
					entries = append(entries, entry)
				}
			}
		}
	}
	if len(repo.Tags) > 0 && section("Tags") {
		groups, plain := gitTagGroups(repo.Tags)
		for _, tag := range plain {
			entries = append(entries, a.gitTagEntry(p, repo, tag, ""))
		}
		for _, group := range groups {
			key := "tag:" + group
			expanded := a.gitGroupExpanded(key, false)
			entries = append(entries, gitMenuEntry{kind: gitEntryGroup, label: group, glyph: "folder", depth: 1, expanded: expanded, run: func() {
				state.expanded[key] = !expanded
			}})
			if !expanded {
				continue
			}
			for _, ref := range repo.Tags {
				if prefix, rest, ok := strings.Cut(ref.Name, "/"); ok && prefix == group {
					entry := a.gitTagEntry(p, repo, ref, rest)
					entry.depth = 2
					entries = append(entries, entry)
				}
			}
		}
	}
	return entries
}

func (a *App) gitBranchEntry(p *Pane, repo *gitRepo, ref gitRef) gitMenuEntry {
	current := ref.Name == repo.Branch
	entry := gitMenuEntry{kind: gitEntryBranch, label: ref.Name, glyph: "git-branch", current: current}
	if current {
		entry.glyph = "check"
	}
	if !current {
		entry.run = func() {
			a.runGitAction(p, repo.Dir, "switch", ref.Name)
		}
	}
	return entry
}

func (a *App) gitRemoteEntry(p *Pane, repo *gitRepo, ref gitRef) gitMenuEntry {
	current := ref.Name == repo.Upstream
	entry := gitMenuEntry{kind: gitEntryBranch, label: ref.Name, glyph: "git-branch", current: current}
	if current {
		entry.glyph = "star"
	}
	entry.run = func() {
		a.runGitAction(p, repo.Dir, gitRemoteCheckoutArgs(repo, ref)...)
	}
	return entry
}

// gitRemoteCheckoutArgs checks out the local branch that tracks a remote
// one, creating it when the local branch does not exist yet.
func gitRemoteCheckoutArgs(repo *gitRepo, ref gitRef) []string {
	local := ref.Branch
	for _, branch := range repo.Locals {
		if branch.Name == local {
			return []string{"switch", local}
		}
	}
	return []string{"switch", "--track", "-c", local, ref.Name}
}

func (a *App) gitTagEntry(p *Pane, repo *gitRepo, ref gitRef, label string) gitMenuEntry {
	if label == "" {
		label = ref.Name
	}
	return gitMenuEntry{
		kind:  gitEntryTag,
		label: label,
		glyph: "tag",
		run: func() {
			a.runGitAction(p, repo.Dir, "checkout", ref.Name)
		},
	}
}

// gitGroupExpanded tells whether a remote or tag folder is unfolded.
func (a *App) gitGroupExpanded(key string, byDefault bool) bool {
	if expanded, ok := a.gitMenu.expanded[key]; ok {
		return expanded
	}
	return byDefault
}

// gitRemotes lists the remotes of remote branches, first seen first.
func gitRemotes(refs []gitRef) []string {
	var out []string
	seen := map[string]bool{}
	for _, ref := range refs {
		if !seen[ref.Remote] {
			seen[ref.Remote] = true
			out = append(out, ref.Remote)
		}
	}
	return out
}

// gitTagGroups splits tags into those with a folder in their name, as
// agent/v1, and those without.
func gitTagGroups(tags []gitRef) (groups []string, plain []gitRef) {
	seen := map[string]bool{}
	for _, tag := range tags {
		group, _, ok := strings.Cut(tag.Name, "/")
		if !ok {
			plain = append(plain, tag)
			continue
		}
		if !seen[group] {
			seen[group] = true
			groups = append(groups, group)
		}
	}
	return groups, plain
}

func (a *App) openGitPrompt(p *Pane, repo *gitRepo, kind string) {
	prompt := gitPromptState{open: true, kind: kind, pane: p, dir: repo.Dir}
	switch kind {
	case "commit":
		prompt.title = "Commit changes"
		prompt.detail = "Write a message. GoRex stages all changes, then commits them in this pane's shell."
	case "branch":
		prompt.title = "New branch"
		prompt.detail = "The new branch starts at the current commit and is checked out."
	case "checkout":
		prompt.title = "Checkout tag or revision"
		prompt.detail = "A branch name, a tag, or a revision such as v1.2 or main."
	}
	a.gitPrompt = prompt
	a.focusReq = nil
}

// gitPrompt asks for what an action needs before it runs.
func (a *App) gitPromptDialog(c *ui.Context, k *colors) {
	if !a.gitPrompt.open {
		return
	}
	prompt := &a.gitPrompt
	ui.DialogBase(c, &prompt.open, func(backdrop, panel ui.Element) {
		backdrop.Background(k.backdrop)
		name := "Git action"
		confirm := "Continue"
		switch prompt.kind {
		case "commit":
			name = "Write commit message"
			confirm = "Commit"
		case "branch":
			name = "Name the new branch"
			confirm = "Create Branch"
		case "checkout":
			name = "Tag or revision"
			confirm = "Checkout"
		}
		panel.Width(470).MaxWidthPercent(90).Padding(20).Gap(12).Radius(14).
			Background(k.panel).Border(1, k.panelBorder).Shadow(0, 18, 44, -6, k.shadowFocused)
		ui.Text(c, prompt.title).FontSize(17).FontWeight(700).TextColor(k.text)
		ui.Text(c, prompt.detail).FontSize(12.5).TextColor(k.textMuted).LineHeight(1.35)
		if prompt.error != "" {
			ui.Text(c, prompt.error).FontSize(12).TextColor(k.attention)
		}
		in := ui.TextInputBase(c, &prompt.value).FontSize(14).Placeholder(name).AutoFocus()
		submit := in.Submitted()
		if in.Changed() {
			prompt.error = ""
		}
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "Cancel").Clicked() {
				a.closeGitPrompt()
			}
			if ui.Button(c, confirm).Clicked() {
				submit = true
			}
		})
		if submit && prompt.open {
			a.submitGitPrompt()
		}
	})
	if !prompt.open {
		a.focusReq = prompt.pane
	}
}

// gitAuthDialog asks for the credentials a git action needs, with the
// password hidden.
func (a *App) gitAuthDialog(c *ui.Context, k *colors) {
	if !a.gitAuth.open {
		return
	}
	auth := &a.gitAuth
	ui.DialogBase(c, &auth.open, func(backdrop, panel ui.Element) {
		backdrop.Background(k.backdrop)
		panel.Width(440).MaxWidthPercent(90).Padding(20).Gap(12).Radius(14).
			Background(k.panel).Border(1, k.panelBorder).Shadow(0, 18, 44, -6, k.shadowFocused)
		ui.Text(c, auth.title).FontSize(17).FontWeight(700).TextColor(k.text)
		ui.Text(c, auth.detail).FontSize(12.5).TextColor(k.textMuted).LineHeight(1.35)
		if auth.error != "" {
			ui.Text(c, auth.error).FontSize(12).TextColor(k.attention).MaxWidth(400)
		}
		user := ui.TextInputBase(c, &auth.username).FontSize(14).Placeholder("Username").Label("Username")
		password := ui.TextInputBase(c, &auth.password).FontSize(14).Placeholder("Password or token").Label("Password").Password()
		if strings.TrimSpace(auth.username) == "" {
			user.AutoFocus()
		} else {
			password.AutoFocus()
		}
		if user.Changed() || password.Changed() {
			auth.error = ""
		}
		submit := user.Submitted() || password.Submitted()
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "Cancel").Clicked() {
				a.closeGitAuth()
			}
			if ui.Button(c, "Continue").Clicked() {
				submit = true
			}
		})
		if submit && auth.open {
			a.submitGitAuth()
		}
	})
	if !auth.open {
		auth.password = ""
		a.focusReq = auth.pane
	}
}

func (a *App) closeGitPrompt() {
	a.gitPrompt.open = false
	a.focusReq = a.gitPrompt.pane
}

func (a *App) submitGitPrompt() {
	prompt := &a.gitPrompt
	value := strings.TrimSpace(prompt.value)
	switch prompt.kind {
	case "commit":
		if value == "" {
			prompt.error = "Write a message first."
			return
		}
		path, err := writeCommitMessage(prompt.pane, value)
		if err != nil {
			prompt.error = fmt.Sprintf("Could not save the message: %v", err)
			return
		}
		a.runGitActions(prompt.pane, prompt.dir, []string{"add", "-A"}, []string{"commit", "-F", path})
	case "branch":
		if !validGitRefName(value) {
			prompt.error = "That is not a valid branch name."
			return
		}
		a.runGitAction(prompt.pane, prompt.dir, "switch", "-c", value)
	case "checkout":
		if !validGitRevision(value) {
			prompt.error = "That is not a valid tag or revision."
			return
		}
		a.runGitAction(prompt.pane, prompt.dir, "checkout", value)
	}
	a.closeGitPrompt()
}
