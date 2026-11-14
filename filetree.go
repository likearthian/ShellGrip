package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

type fileTreeRow struct {
	path, name string
	depth      int
	directory  bool
	message    bool
}

const (
	// The local tree reads its folders every two seconds; the remote one
	// asks the host, whose reads are round trips, less often.
	fileTreeRefresh   = 2 * time.Second
	remoteTreeRefresh = 5 * time.Second
)

type fileTreeState struct {
	pane     *Pane
	hint     string
	root     string
	rows     []fileTreeRow
	expanded map[string]bool
	loading  bool
	loaded   bool
	next     time.Time
	revision int
	selected string
	list     ui.ListState
}

func (a *App) toggleFileTree() {
	prefs.FileTree = !prefs.FileTree
	a.fileTree = nil
	saveSettings()
	if t := a.tab(); t != nil {
		a.focusReq = t.Focus
	}
}

// showSidebar shows the sidebar in its files or Source Control view.
func (a *App) showSidebar(view string) {
	prefs.FileTree, prefs.FileTreeView = true, view
	saveSettings()
	if t := a.tab(); t != nil {
		a.focusReq = t.Focus
	}
}

// refreshTrees reads the trees again when the window comes back or another
// tab is shown, rather than after their next poll.
func (a *App) refreshTrees() {
	if a.fileTree != nil {
		a.fileTree.next = time.Time{}
	}
	if a.remoteTree != nil {
		a.remoteTree.next = time.Time{}
	}
	a.invalidate()
}

// Only expanded folders are read. Symlinks remain leaves, so recursive
// links cannot turn an expanded tree into an infinite traversal.
func readFileTree(root string, expanded map[string]bool) []fileTreeRow {
	var rows []fileTreeRow
	var read func(string, int)
	read = func(dir string, depth int) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			rows = append(rows, fileTreeRow{path: dir, name: "Cannot read folder", depth: depth, message: true})
			return
		}
		sort.SliceStable(entries, func(i, j int) bool {
			if entries[i].IsDir() != entries[j].IsDir() {
				return entries[i].IsDir()
			}
			return strings.ToLower(entries[i].Name()) < strings.ToLower(entries[j].Name())
		})
		if len(entries) == 0 {
			rows = append(rows, fileTreeRow{path: dir, name: "Empty folder", depth: depth, message: true})
		}
		for _, entry := range entries {
			path := filepath.Join(dir, entry.Name())
			rows = append(rows, fileTreeRow{path: path, name: entry.Name(), depth: depth, directory: entry.IsDir()})
			if entry.IsDir() && expanded[path] {
				read(path, depth+1)
			}
		}
	}
	read(root, 0)
	return rows
}

func fileTreeDirectory(p *Pane, dir string) string {
	if windowsProfiles && p.profile.WSL {
		if distro := p.profile.distribution(); distro != "" && strings.HasPrefix(dir, "/") {
			return `\\wsl.localhost\` + distro + filepath.FromSlash(dir)
		}
		return ""
	}
	return dir
}

func (a *App) refreshFileTree(c *ui.Context, p *Pane) *fileTreeState {
	hint := p.directory()
	s := a.fileTree
	if s == nil || s.pane != p || s.hint != hint {
		s = &fileTreeState{pane: p, hint: hint, expanded: map[string]bool{}}
		a.fileTree = s
	}
	// The tree follows the window: a background window reads nothing, and
	// reads again as soon as it comes back.
	if !a.focusedWin {
		return s
	}
	if s.loading || c.Now().Before(s.next) {
		return s
	}
	s.loading = true
	revision := s.revision
	expanded := make(map[string]bool, len(s.expanded))
	for path, open := range s.expanded {
		expanded[path] = open
	}
	// Copy only the fields the background WSL lookup needs.
	lookup := &Pane{SID: p.SID, profile: p.profile}
	lookupWSL := p.profile.WSL && p.reportedDirectory() == ""
	services := c.Services()
	go func() {
		dir := hint
		if lookupWSL {
			if current := wslDirectory(lookup); current != "" {
				dir = current
			}
		}
		root := fileTreeDirectory(lookup, dir)
		var rows []fileTreeRow
		if root != "" {
			rows = readFileTree(root, expanded)
		}
		a.post(func() {
			if a.fileTree != s {
				return
			}
			s.loading = false
			if s.revision != revision {
				return
			}
			if s.root != root {
				s.expanded = map[string]bool{}
				s.selected = ""
			}
			s.root, s.rows, s.loaded = root, rows, true
			s.next = time.Now().Add(fileTreeRefresh)
		})
		services.Invalidate()
	}()
	return s
}

// sidebarColors gives the sidebar its own surfaces, a little flatter than
// the panes', in both appearances.
func sidebarColors(c *ui.Context, k *colors) *colors {
	sidebar := *k
	if c.Theme().Dark {
		sidebar.panel = ui.Hex("#191a1c")
		sidebar.text = ui.Hex("#d3d6db")
		sidebar.textMuted = ui.Hex("#a9adb5")
		sidebar.panelSel = ui.Hex("#2d4672")
		sidebar.hover = ui.Hex("#25272b")
	} else {
		sidebar.panel = ui.Hex("#f7f8fa")
		sidebar.panelSel = ui.Hex("#d4e2ff")
	}
	return &sidebar
}

// sidebarPanel draws the left sidebar: the file tree, or Source Control.
func (a *App) sidebarPanel(c *ui.Context, k *colors, p *Pane, width float32) {
	// An SSH pane shows the host's folders, through the same SFTP
	// connection as the remote tree.
	if p.profile.SSH != nil {
		a.remoteTreePanel(c, k, p, width)
		return
	}
	if prefs.FileTreeView == "scm" {
		a.sourceControlPanel(c, k, p, width)
		return
	}
	a.fileTreePanel(c, k, p, width)
}

const defaultFileTreeWidth float32 = 240

func fileTreeWidth(width, available float32) float32 {
	if width <= 0 {
		width = defaultFileTreeWidth
	}
	limit := float32(600)
	if available > 0 {
		// Leave room for the terminal at the window's minimum size too.
		limit = min(limit, max(160, available-gap-280))
	}
	return min(max(width, 160), limit)
}

func (a *App) fileTreeDivider(c *ui.Context, k *colors, width, available float32) {
	div := ui.Box(c.Key("file-tree-divider")).Width(gap).Shrink(0).
		Role(ui.RoleSplitter).Label("Resize File Tree").Cursor(ui.CursorResizeColumn).
		Tooltip("Drag to resize; double-click to reset")
	if dx, _, ok := div.Dragged(); ok && dx != 0 {
		prefs.FileTreeWidth = fileTreeWidth(width+dx, available)
		saveSettings()
		c.Invalidate()
	}
	if div.DoubleClicked() {
		prefs.FileTreeWidth = defaultFileTreeWidth
		saveSettings()
		c.Invalidate()
	}
	if div.Hovered() || div.Dragging() || div.Pressed() {
		div.Draw(func(p *ui.Painter, r ui.Rect) {
			p.Fill(ui.Rect{X: r.X + r.W/2 - 1.5, Y: r.Y + r.H/2 - 18, W: 3, H: 36}, k.iconMuted.Alpha(0.6), 1.5)
		})
	}
}

func (a *App) fileTreePanel(c *ui.Context, k *colors, p *Pane, width float32) {
	k = sidebarColors(c, k)
	s := a.refreshFileTree(c, p)
	c.After(fileTreeRefresh)
	selectedPath := s.selected
	if selectedPath == "" {
		selectedPath = s.root
		if t := a.tab(); t != nil && t.preview != nil {
			selectedPath = t.preview.path
		}
	}
	message := ""
	switch {
	case !s.loaded:
		message = "Loading…"
	case s.root == "":
		message = "Directory unavailable"
	}
	ui.Column(c.Key("file-tree")).Width(width).Shrink(0).MinHeight(0).
		Radius(cardR).Background(k.panel).Clip().Children(func() {
		ui.Row(c).Height(36).Padding(0, 10).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Files").FontSize(13).FontWeight(500).TextColor(k.text).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
			if iconButton(c, k, "git-branch", "Show Source Control", 24, 14).Tooltip("Source Control").Clicked() {
				prefs.FileTreeView = "scm"
				saveSettings()
				c.Invalidate()
			}
			if iconButton(c, k, "rotate-ccw", "Refresh File Tree", 24, 14).Tooltip("Refresh").Clicked() {
				s.next = time.Time{}
				s.revision++
				c.Invalidate()
			}
			if iconButton(c, k, "x", "Hide File Tree", 24, 14).Tooltip("Hide File Tree").Clicked() {
				a.toggleFileTree()
			}
		})
		// A List builds only the rows in view: a folder of thousands
		// costs a page of it, not all of it, in the frames a page of a
		// preview scrolls beside it. Its root comes first, then either
		// what the root holds or the one message saying why it shows
		// none.
		rows := 1 + len(s.rows)
		if message != "" {
			rows = 2
		}
		s.list.Key = func(row int) any {
			switch {
			case row == 0:
				return "file-tree-root"
			case message != "":
				return "file-tree-message"
			}
			return s.rows[row-1].path
		}
		ui.List(c, &s.list, rows, func(i int) {
			switch {
			case i == 0:
				a.fileTreeRootRow(c, k, s, selectedPath)
			case message != "":
				ui.Text(c, message).FontSize(12).TextColor(k.textFaint).Padding(6)
			default:
				a.fileTreeEntryRow(c, k, s, selectedPath, s.rows[i-1])
			}
		}).Grow(1).MinHeight(0).Padding(4, 6).Label("File Tree")
	})
}

// fileTreeRootRow shows the root of the tree, which a click chooses.
func (a *App) fileTreeRootRow(c *ui.Context, k *colors, s *fileTreeState, selectedPath string) {
	root := s.root
	if root == "" {
		root = s.hint
	}
	rootRow := ui.Row(c).Height(28).Padding(0, 6).Gap(5).AlignItems(ui.Center).
		Radius(4).Tooltip(root).Label("File Tree Directory")
	if selectedPath == s.root {
		rootRow.Background(k.panelSel)
	} else if rootRow.Hovered() {
		rootRow.Background(k.hover)
	}
	if rootRow.Clicked() {
		s.selected = s.root
	}
	rootRow.Children(func() {
		ui.Icon(c, icon("chevron-down")).Size(14, 14).Shrink(0).TextColor(k.textMuted)
		ui.Icon(c, icon("tree-folder")).Size(17, 17).Shrink(0).TextColor(k.textMuted)
		name := filepath.Base(root)
		if name == "." || name == string(filepath.Separator) {
			name = root
		}
		ui.Text(c, name).FontSize(13).FontWeight(600).TextColor(k.text).SingleLine().Ellipsis("…").MinWidth(0)
	})
	if s.root != "" {
		a.fileTreeContextMenu(c, rootRow, s.root, fileTreeRow{path: s.root, directory: true})
	}
}

// fileTreeEntryRow shows one entry of the tree, indented by its depth, and
// opens folders and previews files as it is clicked.
func (a *App) fileTreeEntryRow(c *ui.Context, k *colors, s *fileTreeState, selectedPath string, entry fileTreeRow) {
	row := ui.Row(c).Height(28).Padding(0, 6, 0, float32(6+(entry.depth+1)*18)).Gap(5).
		AlignItems(ui.Center).Radius(4).Tooltip(entry.path)
	if selectedPath == entry.path {
		row.Background(k.panelSel)
	} else if !entry.message && row.Hovered() {
		row.Background(k.hover)
	}
	a.fileTreeContextMenu(c, row, s.root, entry)
	if entry.directory {
		row.Cursor(ui.CursorPointer).Role(ui.RoleButton).Label("Toggle folder " + entry.name)
		if row.Clicked() {
			s.selected = entry.path
			s.expanded[entry.path] = !s.expanded[entry.path]
			s.next = time.Time{}
			s.revision++
			c.Invalidate()
		}
	} else if !entry.message {
		row.Cursor(ui.CursorPointer).Role(ui.RoleButton).Label("Preview file " + entry.name)
		if row.Clicked() {
			s.selected = entry.path
			a.openFilePreview(c, s.pane, entry.path)
		}
	}
	row.Children(func() {
		if !entry.message {
			if entry.directory {
				mark := "chevron-right"
				if s.expanded[entry.path] {
					mark = "chevron-down"
				}
				ui.Icon(c, icon(mark)).Size(14, 14).TextColor(k.textMuted).Shrink(0)
			} else {
				ui.Spacer(c).Width(14).Grow(0).Shrink(0)
			}
			glyph, color := fileTreeGlyph(entry, k)
			ui.Icon(c, icon(glyph)).Size(17, 17).TextColor(color).Shrink(0)
		}
		color := k.text
		if entry.message || strings.HasPrefix(entry.name, ".") {
			color = k.textMuted
		}
		ui.Text(c, entry.name).FontSize(13).TextColor(color).SingleLine().Ellipsis("…").MinWidth(0)
	})
}

func fileTreeGlyph(entry fileTreeRow, k *colors) (string, ui.Color) {
	if entry.directory {
		if strings.HasPrefix(entry.name, ".") {
			return "tree-folder", k.textMuted
		}
		return "tree-folder", ui.Hex("#5893f8")
	}
	switch strings.ToLower(filepath.Ext(entry.name)) {
	case ".kt", ".kts":
		return "tree-kotlin", ui.Hex("#b484ed")
	case ".json", ".jsonc", ".json5":
		return "tree-json", ui.Hex("#b484ed")
	case ".go":
		return "brand:go", ui.Hex("#4bbecf")
	case ".py":
		return "brand:python", ui.Hex("#e1bd6a")
	case ".js", ".jsx", ".ts", ".tsx", ".vue":
		return "file-code", ui.Hex("#e1bd6a")
	case ".html", ".css", ".scss", ".xml", ".svg":
		return "file-code", ui.Hex("#e29b67")
	case ".md", ".txt":
		return "tree-file", k.textMuted
	case ".yaml", ".yml", ".toml", ".ini":
		return "tree-file", ui.Hex("#92b97a")
	default:
		return "tree-file", k.textMuted
	}
}
