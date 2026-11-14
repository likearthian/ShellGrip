package main

import (
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

// remoteTreeState is the sidebar's view of an SSH pane's folder: what the
// folder holds, with the folders the user opened, as the local tree does.
type remoteTreeState struct {
	pane     *Pane
	session  *sftpSession
	root     string
	rows     []fileTreeRow
	expanded map[string]bool
	selected string
	loading  bool
	loaded   bool
	next     time.Time
	// revision numbers the reads, so that one that lands late, after a
	// folder was opened or the refresh button pressed, is dropped.
	revision int
	list     ui.ListState
}

// remoteShellDir is the folder the remote shell reports over OSC 7, when
// it reports an absolute path.
func remoteShellDir(p *Pane) string {
	if p == nil || p.term == nil {
		return ""
	}
	dir := strings.TrimSpace(p.term.Dir())
	if !strings.HasPrefix(dir, "/") || strings.HasPrefix(dir, "//") {
		return ""
	}
	return path.Clean(dir)
}

// remoteRoot is what the tree is rooted at: the shell's folder, then the
// connection's start folder, then the host's home.
func remoteRoot(p *Pane, session *sftpSession) string {
	if dir := remoteShellDir(p); dir != "" {
		return dir
	}
	if conn := p.profile.SSH; conn != nil && conn.StartDir != "" {
		return conn.StartDir
	}
	if session != nil {
		return session.home
	}
	return ""
}

// refreshRemoteTree keeps the sidebar's remote state for the focused
// pane, and reads the folder again when it is due.
func (a *App) refreshRemoteTree(c *ui.Context, p *Pane) *remoteTreeState {
	session := a.sftpFor(p)
	session.ensure()
	hint := remoteRoot(p, session)
	s := a.remoteTree
	if s == nil || s.pane != p || s.root != hint {
		// A new shell folder starts a new tree, as the local one does.
		s = &remoteTreeState{pane: p, root: hint, expanded: map[string]bool{}}
		a.remoteTree = s
	}
	s.session = session
	// The tree follows the window: a background window asks the host
	// nothing, and reads again as soon as it comes back.
	if !a.focusedWin {
		return s
	}
	if s.root == "" || session.fs == nil || s.loading || c.Now().Before(s.next) {
		return s
	}
	s.load(c.Services())
	return s
}

// load reads the folder and the folders opened under it, off the frame,
// and again five seconds later as the local tree does. The rows shown stay
// until the new ones arrive, so a refresh never blinks.
func (s *remoteTreeState) load(services ui.Services) {
	if s.session == nil || s.session.fs == nil || s.root == "" {
		return
	}
	s.loading = true
	s.revision++
	revision := s.revision
	root := s.root
	expanded := make(map[string]bool, len(s.expanded))
	for dir, open := range s.expanded {
		expanded[dir] = open
	}
	fs := s.session.fs
	app := s.app()
	go func() {
		rows, rootErr := readRemoteFileTree(fs, root, expanded)
		app.post(func() {
			if app.remoteTree != s || s.revision != revision {
				return
			}
			if sftpConnLost(rootErr) {
				// Keep the rows shown and say why above them; the
				// Reconnect button starts a new connection.
				s.loading, s.loaded = false, true
				s.next = time.Time{}
				if s.session != nil {
					s.session.drop("The connection was lost: " + rootErr.Error())
				}
				return
			}
			s.loading, s.loaded = false, true
			s.next = time.Now().Add(remoteTreeRefresh)
			s.rows, s.root = rows, root
		})
		services.Invalidate()
	}()
}

// readRemoteFileTree lists a folder and, deeper, the folders opened under
// it, as readFileTree does for the local tree. The error of the root
// folder is returned, so a lost connection can be told from a folder that
// cannot be read.
func readRemoteFileTree(fs remoteFS, root string, expanded map[string]bool) ([]fileTreeRow, error) {
	var rows []fileTreeRow
	var rootErr error
	var read func(string, int)
	read = func(dir string, depth int) {
		infos, err := fs.ReadDir(dir)
		if err != nil {
			if depth == 0 {
				rootErr = err
			}
			rows = append(rows, fileTreeRow{path: dir, name: "Cannot read folder", depth: depth, message: true})
			return
		}
		entries := sftpRows(infos)
		if len(entries) == 0 {
			rows = append(rows, fileTreeRow{path: dir, name: "Empty folder", depth: depth, message: true})
		}
		for _, entry := range entries {
			path := remoteJoin(dir, entry.name)
			rows = append(rows, fileTreeRow{path: path, name: entry.name, depth: depth, directory: entry.dir})
			if entry.dir && expanded[path] {
				read(path, depth+1)
			}
		}
	}
	read(root, 0)
	return rows, rootErr
}

// app returns the App a tree state belongs to, through its session.
func (s *remoteTreeState) app() *App { return s.session.app }

// toggle opens or folds a folder, reading the tree again. The read in
// flight is dropped, and the rows shown stay until the new ones arrive.
func (s *remoteTreeState) toggle(dir string) {
	s.expanded[dir] = !s.expanded[dir]
	s.next = time.Time{}
	s.loading = false
	s.revision++
}

// reload reads the tree again, as the refresh button asks.
func (s *remoteTreeState) reload() {
	s.next = time.Time{}
	s.loading = false
	s.revision++
}

// remoteTreePanel draws the sidebar of an SSH pane: the remote folder,
// through the tab's connection, laid out as the local tree is.
func (a *App) remoteTreePanel(c *ui.Context, k *colors, p *Pane, width float32) {
	k = sidebarColors(c, k)
	s := a.refreshRemoteTree(c, p)
	c.After(remoteTreeRefresh)
	ui.Column(c.Key("remote-tree")).Width(width).Shrink(0).MinHeight(0).
		Radius(cardR).Background(k.panel).Clip().Children(func() {
		ui.Row(c).Height(36).Padding(0, 10).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icon("globe")).Size(14, 14).Shrink(0).TextColor(k.textMuted)
			ui.Text(c, "Files").FontSize(13).FontWeight(500).TextColor(k.text).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
			if iconButton(c, k, "rotate-ccw", "Refresh Remote Tree", 24, 14).Tooltip("Refresh").Clicked() {
				s.reload()
				c.Invalidate()
			}
			if iconButton(c, k, "x", "Hide File Tree", 24, 14).Tooltip("Hide File Tree").Clicked() {
				a.toggleFileTree()
			}
		})
		if s.session == nil || s.session.fs == nil {
			a.remoteTreeStatus(c, k, s)
		}
		root := s.root
		selected := s.selected
		if selected == "" {
			selected = root
		}
		message := ""
		switch {
		case root == "":
			message = "Waiting for the connection…"
		case s.loading && !s.loaded:
			message = "Loading…"
		}
		// A List builds only the rows in view, as the local tree's does: a
		// remote folder of thousands reads as fast as its first page.
		if root != "" {
			rows := 1 + len(s.rows)
			if message != "" {
				rows = 2
			}
			s.list.Key = func(row int) any {
				switch {
				case row == 0:
					return "remote-tree-root"
				case message != "":
					return "remote-tree-message"
				}
				return s.rows[row-1].path
			}
			ui.List(c, &s.list, rows, func(i int) {
				switch {
				case i == 0:
					a.remoteTreeRootRow(c, k, s, root, selected)
				case message != "":
					ui.Text(c, message).FontSize(12).TextColor(k.textFaint).Padding(6)
				default:
					a.remoteTreeEntry(c, k, s, p, s.rows[i-1], selected)
				}
			}).Grow(1).MinHeight(0).Padding(4, 6).Label("Remote File Tree")
			return
		}
		ui.Scroll(c).Grow(1).MinHeight(0).Padding(4, 6).Children(func() {
			ui.Text(c, message).FontSize(12).TextColor(k.textFaint).Padding(6)
		})
	})
}

// remoteTreeRootRow shows the root of the remote tree, which a click
// chooses.
func (a *App) remoteTreeRootRow(c *ui.Context, k *colors, s *remoteTreeState, root, selected string) {
	rootRow := ui.Row(c).Height(28).Padding(0, 6).Gap(5).
		AlignItems(ui.Center).Radius(4).Tooltip(root).Label("Remote Tree Directory")
	if selected == root {
		rootRow.Background(k.panelSel)
	} else if rootRow.Hovered() {
		rootRow.Background(k.hover)
	}
	if rootRow.Clicked() {
		s.selected = root
	}
	a.remoteTreeContextMenu(c, rootRow, s, fileTreeRow{path: root, directory: true})
	rootRow.Children(func() {
		ui.Icon(c, icon("chevron-down")).Size(14, 14).Shrink(0).TextColor(k.textMuted)
		ui.Icon(c, icon("tree-folder")).Size(17, 17).Shrink(0).TextColor(k.textMuted)
		name := path.Base(root)
		if name == "." || name == "/" {
			name = root
		}
		ui.Text(c, name).FontSize(13).FontWeight(600).TextColor(k.text).SingleLine().Ellipsis("…").MinWidth(0)
	})
}

// remoteTreeEntry draws an entry, opening its folders and previewing its
// files as the local tree does.
func (a *App) remoteTreeEntry(c *ui.Context, k *colors, s *remoteTreeState, p *Pane, entry fileTreeRow, selected string) {
	row := ui.Row(c.Key(entry.path+entry.name)).Height(28).Padding(0, 6, 0, float32(6+(entry.depth+1)*18)).Gap(5).
		AlignItems(ui.Center).Radius(4).Tooltip(entry.path)
	if selected == entry.path {
		row.Background(k.panelSel)
	} else if !entry.message && row.Hovered() {
		row.Background(k.hover)
	}
	a.remoteTreeContextMenu(c, row, s, entry)
	if entry.directory {
		row.Cursor(ui.CursorPointer).Role(ui.RoleButton).Label("Toggle folder " + entry.name)
		if row.Clicked() {
			s.selected = entry.path
			s.toggle(entry.path)
			c.Invalidate()
		}
	} else if !entry.message {
		row.Cursor(ui.CursorPointer).Role(ui.RoleButton).Label("Preview file " + entry.name)
		if row.Clicked() {
			s.selected = entry.path
			a.openRemotePreview(p, s, entry.path)
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

// remoteTreeContextMenu offers the actions the local tree's menu has,
// for a path on the host.
func (a *App) remoteTreeContextMenu(c *ui.Context, row ui.Element, s *remoteTreeState, entry fileTreeRow) {
	if entry.message {
		return
	}
	state := ui.Local(row, "remote-tree-menu", func() fileTreeMenuState { return fileTreeMenuState{} })
	if state.open && row.Clicked() {
		state.open = false
	}
	if row.RightClicked() {
		state.open, state.selected = true, 0
	}
	if !state.open {
		return
	}
	actions := []fileTreeAction{
		{label: "Copy Remote Path", run: func() {
			c.WriteClipboard(entry.path)
			a.selectionCopied(entry.path)
		}},
	}
	if entry.directory {
		actions = append(actions, fileTreeAction{label: "Refresh Folder", run: func() {
			s.reload()
			c.Invalidate()
		}})
	} else {
		actions = append(actions, fileTreeAction{label: "Preview", run: func() {
			a.openRemotePreview(s.pane, s, entry.path)
		}})
	}
	a.fileTreePopup(c, row, state, actions)
}

// remoteTreeStatus carries the connection's state, and the password it
// may ask for.
func (a *App) remoteTreeStatus(c *ui.Context, k *colors, s *remoteTreeState) {
	sess := s.session
	switch {
	case sess == nil:
		return
	case sess.connecting:
		ui.Text(c, "Connecting…").FontSize(11.5).TextColor(k.textFaint).Padding(0, 10, 4, 10)
	case sess.needPassword:
		ui.Column(c).Padding(0, 10, 6, 10).Gap(4).Children(func() {
			ui.Text(c, "Password for "+s.pane.profile.SSH.dest()).FontSize(11.5).TextColor(k.textMuted)
			ui.Row(c).Gap(6).Children(func() {
				in := ui.TextInput(c, &sess.password).Password().Grow(1).FontSize(11.5)
				if in.Submitted() {
					sess.connect()
				}
				if ui.Button(c, "Connect").Clicked() {
					sess.connect()
				}
			})
			if sess.err != "" {
				ui.Text(c, sess.err).FontSize(11).TextColor(k.attention).MaxWidth(260)
			}
		})
	case sess.err != "":
		ui.Column(c).Padding(0, 10, 6, 10).Gap(4).Children(func() {
			ui.Text(c, sess.err).FontSize(11).TextColor(k.attention).MaxWidth(280)
			if ui.Button(c, "Reconnect").Clicked() {
				sess.connect()
			}
		})
	}
}

// remotePreviewLimit is the largest remote file the preview downloads.
const remotePreviewLimit = 32 << 20

// remotePreviewPath is where a remote file is kept for the preview: its
// name in a folder of its own, so names with the same base do not
// collide.
func remotePreviewPath(remotePath string) string {
	sum := fnv.New32a()
	sum.Write([]byte(remotePath))
	dir := filepath.Join(os.TempDir(), "gorex-remote", fmt.Sprintf("%x", sum.Sum32()))
	os.MkdirAll(dir, 0o700)
	name := path.Base(remotePath)
	if name == "." || name == "/" || name == "" {
		name = "remote-file"
	}
	return filepath.Join(dir, name)
}

// openRemotePreview downloads a remote file to the cache and shows it in
// the tab's preview, off the frame.
func (a *App) openRemotePreview(p *Pane, s *remoteTreeState, remotePath string) {
	if p == nil || s == nil || s.session == nil || s.session.fs == nil {
		return
	}
	fs := s.session.fs
	cache := remotePreviewPath(remotePath)
	fail := func(message string) {
		a.post(func() { a.err = message })
		a.invalidate()
	}
	go func() {
		file, err := fs.Open(remotePath)
		if err != nil {
			fail("Could not open " + remotePath + ": " + err.Error())
			return
		}
		data, err := io.ReadAll(io.LimitReader(file, remotePreviewLimit+1))
		file.Close()
		if err != nil {
			fail("Could not read " + remotePath + ": " + err.Error())
			return
		}
		if len(data) > remotePreviewLimit {
			fail("File is too large to preview: " + remotePath)
			return
		}
		if err := os.WriteFile(cache, data, 0o600); err != nil {
			fail("Could not cache " + remotePath + ": " + err.Error())
			return
		}
		a.post(func() {
			if !p.closed {
				a.showPreview(p, cache, remotePath)
			}
		})
		a.invalidate()
	}()
}
