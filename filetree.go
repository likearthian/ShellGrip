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
}

func (a *App) toggleFileTree() {
	prefs.FileTree = !prefs.FileTree
	a.fileTree = nil
	saveSettings()
	if t := a.tab(); t != nil {
		a.focusReq = t.Focus
	}
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
			}
			s.root, s.rows, s.loaded = root, rows, true
			s.next = time.Now().Add(2 * time.Second)
		})
		services.Invalidate()
	}()
	return s
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
	s := a.refreshFileTree(c, p)
	c.After(2 * time.Second)
	ui.Column(c.Key("file-tree")).Width(width).Shrink(0).MinHeight(0).
		Radius(cardR).Background(k.panel).Border(1, k.panelBorder).Clip().Children(func() {
		ui.Row(c).Height(headerH).Padding(0, 10).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Files").FontSize(12.5).FontWeight(600).TextColor(k.text).Grow(1)
			if iconButton(c, k, "rotate-ccw", "Refresh File Tree", 24, 14).Tooltip("Refresh").Clicked() {
				s.next = time.Time{}
				s.revision++
				c.Invalidate()
			}
			if iconButton(c, k, "x", "Hide File Tree", 24, 14).Tooltip("Hide File Tree").Clicked() {
				a.toggleFileTree()
			}
		})
		root := s.root
		if root == "" {
			root = s.hint
		}
		rootLabel := ui.Text(c, root).Label("File Tree Directory").FontSize(11).TextColor(k.textMuted).SingleLine().Ellipsis("…").Tooltip(root).Margin(0, 10, 8, 10)
		if s.root != "" {
			a.fileTreeContextMenu(c, rootLabel, s.root, fileTreeRow{path: s.root, directory: true})
		}
		ui.Scroll(c.Key(root)).Grow(1).MinHeight(0).Padding(4, 6).Children(func() {
			if !s.loaded {
				ui.Text(c, "Loading…").FontSize(12).TextColor(k.textFaint).Padding(6)
			} else if s.root == "" {
				ui.Text(c, "Directory unavailable").FontSize(12).TextColor(k.textFaint).Padding(6)
			}
			for _, entry := range s.rows {
				row := ui.Row(c.Key(entry.path+entry.name)).Height(26).Padding(0, 6, 0, float32(6+entry.depth*14)).Gap(6).
					AlignItems(ui.Center).Radius(5).Tooltip(entry.path)
				a.fileTreeContextMenu(c, row, s.root, entry)
				if entry.directory {
					row.Cursor(ui.CursorPointer).Role(ui.RoleButton).Label("Toggle folder " + entry.name)
					if row.Hovered() {
						row.Background(k.hover)
					}
					if row.Clicked() {
						s.expanded[entry.path] = !s.expanded[entry.path]
						s.next = time.Time{}
						s.revision++
						c.Invalidate()
					}
				} else if !entry.message {
					row.Cursor(ui.CursorPointer).Role(ui.RoleButton).Label("Preview file " + entry.name)
					if t := a.tab(); t != nil && t.preview != nil && t.preview.path == entry.path {
						row.Background(k.panelSel)
					} else if row.Hovered() {
						row.Background(k.hover)
					}
					if row.Clicked() {
						a.openFilePreview(c, p, entry.path)
					}
				}
				row.Children(func() {
					if !entry.message {
						glyph := "file-code"
						if entry.directory {
							mark := "+"
							if s.expanded[entry.path] {
								mark = "−"
							}
							ui.Text(c, mark).FontSize(11).TextColor(k.textFaint).Width(8)
							glyph = "folder-open"
						} else {
							ui.Spacer(c).Width(8).Grow(0)
						}
						ui.Icon(c, icon(glyph)).Size(13, 13).TextColor(k.textMuted).Shrink(0)
					}
					ui.Text(c, entry.name).FontSize(12).TextColor(k.textMuted).SingleLine().Ellipsis("…").MinWidth(0)
				})
			}
		})
	})
}
