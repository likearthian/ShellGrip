package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
	"shellgrip/internal/terminal"

	"shellgrip/internal/appdir"
	"shellgrip/internal/session"
)

// Tab is a tab of the window: panes in a tree of splits.
type Tab struct {
	preview  *filePreview
	ID       int
	Settings bool
	Name     string // the name the user gave it, "" to follow its panes
	Root     *Node
	// Focus is the pane with the focus, and Zoom the pane that fills the
	// tab, when one does.
	Focus *Pane
	Zoom  *Pane
	// recent lists the panes by when they had the focus last, the latest
	// last: closing a pane gives the focus back to the one before.
	recent []*Pane
}

// Node is a pane, or a split of two nodes.
type Node struct {
	ID     int
	Pane   *Pane
	Parent *Node
	// Vertical stacks A above B; otherwise A is left of B. Ratio is A's
	// share of the room.
	Vertical bool
	Ratio    float32
	A, B     *Node
}

// Pane is a terminal running a session: a shell in a pseudo-terminal.
type Pane struct {
	ID   int
	SID  string
	Tab  *Tab
	Node *Node
	term *terminal.Terminal
	sess *session.Session

	// info is what the session last said of itself.
	info session.Info
	// title is the last title the program set.
	title string
	// lastData is when the session last printed, in Unix nanoseconds.
	lastData atomic.Int64
	// attention marks a pane that rang the bell or whose program finished
	// while it was out of sight; seen clears it. attentionAt is when it
	// began to ask, which the cues flash after.
	attention   bool
	attentionAt time.Time
	// bounds is where the pane's card was in the last frame.
	bounds    ui.Rect
	closed    bool
	stopWatch chan struct{}
	wakeWatch chan struct{}
	stopOnce  sync.Once
	// restored tells that the session was made anew for a pane of a
	// saved layout whose session was gone.
	restored bool
	startDir string
	profile  terminalProfile
}

// App is the state of ShellGrip's window.
type App struct {
	sshRelays         map[string]*sshRelayInstall
	sshWatch          *sshRemoteWatch
	fileWatches       map[string]*localFileWatch
	vsCodePath        string
	vsCodeCheckedAt   time.Time
	vsCodeWSLMessage  string
	vsCodeWSLHelpOpen bool
	fileTree          *fileTreeState
	fileTreeDragging  bool
	win               *mygo.Window
	host              session.Host
	err               string

	tabs   []*Tab
	active int
	nextID int

	// focusReq asks the view to give the keyboard focus to a pane's
	// terminal.
	focusReq *Pane

	// The command palette, the host's popover and the tab being renamed.
	paletteOpen  bool
	paletteQuery string
	paletteSel   int
	hostOpen     bool
	aboutOpen    bool
	// ramApp is the memory the app works in, read while the host's
	// popover is open.
	ramApp uint64
	ramAt  time.Time

	profilesOpen       bool
	settingsCategory   string
	shortcutQuery      string
	shortcutEditing    *command
	shortcutDraft      string
	shortcutError      string
	settingsProfile    string
	settingsFont       string
	settingsFontSize   string
	fontFamilies       []string
	fontHighlight      string
	quickThemeOpen     bool
	quickThemeQuery    string
	themePreview       string
	themePreviewActive bool
	themePickerOpen    bool
	themeQuery         string
	themeNames         []string
	profiles           []terminalProfile
	// sshConns are the saved SSH targets, and the draft is the form that
	// edits one; sshNotice is what the last import did.
	sshConns  []sshConnection
	sshDraft  *sshDraft
	sshNotice string
	// sftpSessions are the SFTP connections of SSH panes, which the remote
	// tree reads through.
	sftpSessions map[int]*sftpSession
	remoteTree   *remoteTreeState
	renaming     *Tab
	renameText   string
	renameFocus  bool
	saveDue      bool
	lastSave     time.Time
	quitting     bool
	// exiting tells that the quit is decided: the window closes, and the
	// sessions end with it.
	exiting atomic.Bool
	// confirming tells that the quit dialog is up, and askQuit lets tests
	// answer it instead of the native dialog.
	confirming bool
	askQuit    func(summary string) bool
	focusedWin bool
	background atomic.Bool
	// cueOn tells that the window itself asks for the user: its taskbar
	// button flashes, or its Dock icon bounces, while a pane waits.
	cueOn               bool
	lastSnapshot        string
	layoutMu            sync.Mutex
	title               string
	ctrlHeldAt          time.Time
	tabHints            bool
	copiedCharacters    int
	pendingTerminalFile *pendingTerminalFile

	// posted are changes to make in the next frame, from terminals of a
	// view without a window, as in tests.
	postMu sync.Mutex
	posted []func()
	// services keeps redraw requests available to menu callbacks.
	services ui.Services

	// gitByDir caches what each directory's repository is, and the branch
	// menu and its prompts belong to the panes' headers.
	gitMu     sync.Mutex
	gitByDir  map[string]*gitCacheEntry
	gitMenu   gitMenuState
	gitPrompt gitPromptState
	gitAuth   gitAuthState

	// The Source Control pane's draft message, folded groups and history.
	scmDir           string
	scmMessage       string
	scmMessages      map[string]string
	scmExpanded      map[string]bool
	scmGraphHistory  *gitHistory
	scmGraphRows     []scmGraphRow
	scmGraphSelected string
	historyMu        sync.Mutex
	historyByDir     map[string]*gitHistoryEntry
}

// post appends a change to make in the next frame, from a terminal or a
// goroutine of a view without a window, as in tests.
func (a *App) post(fn func()) {
	a.postMu.Lock()
	a.posted = append(a.posted, fn)
	a.postMu.Unlock()
}

// invalidate asks the window to draw again, from any goroutine.
func (a *App) invalidate() {
	if a.win != nil {
		a.win.Invalidate()
	}
}

// later changes the tree of tabs and panes once the frame building now
// is built, as closing what it is building would pull it from under it.
func (a *App) later(c *ui.Context, fn func()) {
	a.post(fn)
	c.Invalidate()
}

// runPosted makes the changes posted since the last frame.
func (a *App) runPosted() {
	a.postMu.Lock()
	fns := a.posted
	a.posted = nil
	a.postMu.Unlock()
	for _, fn := range fns {
		fn()
	}
}

func (a *App) id() int {
	a.nextID++
	return a.nextID
}

func (a *App) tab() *Tab {
	if a.active >= 0 && a.active < len(a.tabs) {
		return a.tabs[a.active]
	}
	return nil
}

// panes returns the panes of a node in order, left to right and top to
// bottom.
func (n *Node) panes() []*Pane {
	if n == nil {
		return nil
	}
	if n.Pane != nil {
		return []*Pane{n.Pane}
	}
	return append(n.A.panes(), n.B.panes()...)
}

func (t *Tab) panes() []*Pane { return t.Root.panes() }

// setFocus gives a pane the focus of its tab.
func (t *Tab) setFocus(p *Pane) {
	if p == nil {
		return
	}
	t.Focus = p
	p.attention = false
	t.recent = slices.DeleteFunc(t.recent, func(q *Pane) bool { return q == p })
	t.recent = append(t.recent, p)
}

// newPane makes a pane with a new session in dir.
func (a *App) newPane(dir string, cols, rows int) *Pane {
	return a.newPaneWithProfile(dir, cols, rows, a.defaultProfile())
}

func (a *App) newPaneWithProfile(dir string, cols, rows int, profile terminalProfile) *Pane {
	dir = profile.startingDir(dir)
	if cols <= 0 {
		cols, rows = 80, 24
	}
	command, launchDir := profile.launch(dir)
	sess, err := session.New(session.Options{Command: command, Dir: launchDir, Env: profile.environment(), Cols: cols, Rows: rows, CustomPrompt: prefs.CustomPrompt})
	p := &Pane{ID: a.id(), startDir: dir, profile: profile}
	if err != nil {
		log.Printf("starting a session: %v", err)
		a.err = err.Error()
		p.info = session.Info{Exited: true}
	} else {
		p.sess, p.SID, p.info = sess, sess.ID(), sess.Info()
	}
	if p.info.Dir == "" {
		p.info.Dir = dir
	}
	a.attach(p, cols, rows)
	return p
}

// attach makes the pane's terminal, connected to its session.
func (a *App) attach(p *Pane, cols, rows int) {
	if p.sess == nil {
		term, err := newConfiguredTerminal(terminal.Options{Conn: nopConn{}, Transparent: true, CopyOnSelect: terminalCopyOnSelect(prefs.CopyOnSelect), OnCopy: a.terminalCopied, OnZoom: a.terminalZoom, Font: termFont, Theme: terminalLight, DarkTheme: terminalDark})
		if err == nil {
			term.Feed([]byte("\x1b[31mCould not start a session: " + a.err + "\x1b[0m\r\n"))
			p.term = term
		}
		return
	}
	// The terminal's callbacks change the state on the main thread, in
	// the window that made the pane, as long as it is open.
	term, err := newConfiguredTerminal(terminal.Options{
		Conn:         paneConn{Session: p.sess, p: p},
		Font:         termFont,
		Theme:        terminalLight,
		DarkTheme:    terminalDark,
		Transparent:  true,
		CopyOnSelect: terminalCopyOnSelect(prefs.CopyOnSelect),
		OnCopy:       a.terminalCopied,
		OnZoom:       a.terminalZoom,
		OpenLink:     func(link string) bool { return a.openTerminalLink(p, link) },
		OnDrop:       func(paths []string) bool { return a.terminalDrop(p, paths) },
		OnPaste:      func(d transfer.Data) bool { return a.terminalPaste(p, d) },
		OnTitle: func(title string) {
			a.onUI(func() { p.title = title })
		},
		OnBell: func() {
			a.onUI(func() { a.ring(p) })
		},
		OnNotify: func(title, body string) {
			if title == "" {
				title = "ShellGrip"
			}
			mygo.NewNotification(mygo.NotificationOptions{Title: title, Body: body}).Show()
			// A program that asks for a notification asks for the user
			// too, as an agent waiting for an answer does.
			a.onUI(func() { a.ring(p) })
		},
		OnExit: func(int) {
			a.onUI(func() {
				if !p.closed && !a.quitting {
					a.closePane(p)
				}
			})
		},
	})
	if err != nil {
		log.Printf("terminal: %v", err)
		a.err = err.Error()
		p.sess.Close()
		return
	}
	// Until a view shows it, the terminal has the session's size.
	term.Resize(cols, rows)
	p.term = term
	p.stopWatch = make(chan struct{})
	p.wakeWatch = make(chan struct{}, 1)
	go a.watch(p, p.stopWatch)
}

func (p *Pane) markClosed() {
	p.closed = true
	p.stopOnce.Do(func() {
		if p.stopWatch != nil {
			close(p.stopWatch)
		}
	})
}

// onUI changes the state on the main thread, in the window that made the
// pane, or in the next frame of a view without a window, as in tests.
func (a *App) onUI(fn func()) {
	if win := a.win; win != nil {
		win.Update(fn)
	} else {
		a.post(fn)
	}
}

// paneConn is the terminal's connection to its pane's session: it notes
// when the session printed, for the activity dot.
type paneConn struct {
	*session.Session
	p *Pane
}

func (c paneConn) Read(b []byte) (int, error) {
	n, err := c.Session.Read(b)
	if n > 0 {
		c.p.lastData.Store(time.Now().UnixNano())
	}
	return n, err
}

// watch asks the session what runs in it until the pane closes, backing off
// while the shell sits idle and waking the UI only when the info changes.
func (a *App) watch(p *Pane, stop <-chan struct{}) {
	sess := p.sess
	if sess == nil {
		return
	}
	var prev session.Info
	var havePrev bool
	timer := time.NewTimer(500 * time.Millisecond)
	defer timer.Stop()
	for {
		select {
		case <-stop:
			return
		case <-timer.C:
		case <-p.wakeWatch:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
		in := sess.Info()
		if !havePrev || !infoEqual(prev, in) {
			prev, havePrev = in, true
			a.onUI(func() {
				if !p.closed {
					a.apply(p, in)
				}
			})
		}
		timer.Reset(sessionPollInterval(in, time.Unix(0, p.lastData.Load()), a.background.Load(), time.Now()))
	}
}

// quiet background panes include wrappers such as wsl.exe, whose foreground
// process stays alive even while the shell inside it sits at a prompt.
func sessionPollInterval(in session.Info, lastData time.Time, background bool, now time.Time) time.Duration {
	if now.Sub(lastData) > 2*time.Second && now.Sub(in.LastInput) > 2*time.Second {
		if background {
			return 10 * time.Second
		}
		if in.Idle {
			return 1500 * time.Millisecond
		}
	}
	return 500 * time.Millisecond
}

// setBackground wakes the session watchers on focus changes so foreground
// metadata refreshes immediately and idle background shells can poll less often.
func (a *App) setBackground(background bool) {
	a.background.Store(background)
	for _, t := range a.tabs {
		for _, p := range t.panes() {
			select {
			case p.wakeWatch <- struct{}{}:
			default:
			}
		}
	}
}

func infoEqual(a, b session.Info) bool {
	return a.ID == b.ID &&
		a.PID == b.PID &&
		a.Shell == b.Shell &&
		a.Program == b.Program &&
		a.Idle == b.Idle &&
		a.Dir == b.Dir &&
		a.Exited == b.Exited &&
		a.ExitCode == b.ExitCode &&
		a.LastInput.Equal(b.LastInput) &&
		slices.Equal(a.Args, b.Args)
}

// nopConn is the Conn of a terminal that only shows what the app feeds it.
type nopConn struct{}

func (nopConn) Read([]byte) (int, error)    { select {} }
func (nopConn) Write(p []byte) (int, error) { return len(p), nil }
func (nopConn) Close() error                { return nil }

func (a *App) ring(p *Pane) {
	if t := a.tab(); t == nil || t.Focus != p || p.Tab != t || !a.focusedWin {
		a.wantAttention(p)
	}
}

// wantAttention marks a pane as asking for the user, and starts the cues
// that tell of it: its tab flashes, and so does the window.
func (a *App) wantAttention(p *Pane) {
	if p.attention {
		return
	}
	p.attention = true
	p.attentionAt = time.Now()
	a.flashWindow()
}

// flashWindow asks for the user with the window itself: its taskbar
// button flashes on Windows and Linux, and its Dock icon bounces on
// macOS, until the window comes to the front.
func (a *App) flashWindow() {
	if a.win == nil || a.cueOn || a.focusedWin {
		return
	}
	a.cueOn = true
	a.win.FlashFrame(true)
}

// settleWindow stops the window's flash, once the user is back or
// nothing waits for them.
func (a *App) settleWindow() {
	if !a.cueOn {
		return
	}
	a.cueOn = false
	if a.win != nil {
		a.win.FlashFrame(false)
	}
}

// attention tells whether a pane of any tab asks for the user.
func (a *App) attention() bool {
	for _, t := range a.tabs {
		for _, p := range t.panes() {
			if p.attention {
				return true
			}
		}
	}
	return false
}

// newTab opens a tab with a shell in dir, after the active tab.
func (a *App) newTab(dir string) *Tab {
	return a.newTabWithProfile(dir, a.defaultProfile())
}

func (a *App) newTabWithProfile(dir string, profile terminalProfile) *Tab {
	t := &Tab{ID: a.id()}
	p := a.newPaneWithProfile(dir, 0, 0, profile)
	t.Root = &Node{ID: a.id(), Pane: p}
	p.Tab, p.Node = t, t.Root
	t.setFocus(p)
	at := min(a.active+1, len(a.tabs))
	a.tabs = slices.Insert(a.tabs, at, t)
	a.active = at
	a.focusReq = p
	a.changed()
	return t
}

// currentDir returns the directory new panes open in: the focused pane's.
func (a *App) currentDir() string {
	if t := a.tab(); t != nil && t.Focus != nil {
		if d := t.Focus.directory(); d != "" {
			return d
		}
	}
	home, _ := os.UserHomeDir()
	return home
}

// split splits the focused pane, the new pane right of it or below it.
func (a *App) split(vertical bool) {
	t := a.tab()
	if t == nil || t.Focus == nil {
		return
	}
	old := t.Focus
	t.Zoom = nil
	n := old.Node
	cols, rows := 80, 24
	if old.term != nil {
		cols, rows = old.term.Size()
		if vertical {
			rows = max(rows/2, 2)
		} else {
			cols = max(cols/2, 10)
		}
	}
	p := a.newPaneWithProfile(old.splitDirectory(), cols, rows, old.profile)
	p.Tab = t
	left := &Node{ID: a.id(), Pane: old, Parent: n}
	right := &Node{ID: a.id(), Pane: p, Parent: n}
	old.Node, p.Node = left, right
	n.Pane, n.A, n.B, n.Vertical, n.Ratio = nil, left, right, vertical, 0.5
	t.setFocus(p)
	a.focusReq = p
	a.changed()
}

// closePane closes a pane and ends its session.
func (a *App) closePane(p *Pane) {
	if p.closed {
		return
	}
	p.markClosed()
	a.closeSFTPPane(p)
	if a.fileTree != nil && a.fileTree.pane == p {
		a.fileTree = nil
	}
	if a.remoteTree != nil && a.remoteTree.pane == p {
		a.remoteTree = nil
	}
	if a.gitMenu.pane == p {
		a.gitMenu = gitMenuState{}
	}
	if a.gitPrompt.pane == p {
		a.gitPrompt = gitPromptState{}
	}
	if a.gitAuth.pane == p {
		a.gitAuth = gitAuthState{}
	}
	if p.term != nil {
		p.term.Close()
	}
	if p.sess != nil {
		p.sess.Close()
	}
	t := p.Tab
	if t.Zoom == p {
		t.Zoom = nil
	}
	t.recent = slices.DeleteFunc(t.recent, func(q *Pane) bool { return q == p })
	n := p.Node
	if n.Parent == nil {
		a.removeTab(t)
		return
	}
	parent := n.Parent
	other := parent.A
	if other == n {
		other = parent.B
	}
	// The other side takes the parent's place.
	parent.Pane, parent.A, parent.B, parent.Vertical, parent.Ratio = other.Pane, other.A, other.B, other.Vertical, other.Ratio
	if parent.Pane != nil {
		parent.Pane.Node = parent
	} else {
		parent.A.Parent, parent.B.Parent = parent, parent
	}
	if t.Focus == p {
		next := (*Pane)(nil)
		if k := len(t.recent); k > 0 {
			next = t.recent[k-1]
		} else if ps := t.panes(); len(ps) > 0 {
			next = ps[0]
		}
		t.Focus = nil
		t.setFocus(next)
	}
	if a.tab() == t {
		a.focusReq = t.Focus
	}
	a.changed()
}

func (a *App) removeTab(t *Tab) {
	i := slices.Index(a.tabs, t)
	if i < 0 {
		return
	}
	a.tabs = slices.Delete(a.tabs, i, i+1)
	if a.active > i || a.active >= len(a.tabs) {
		a.active = max(a.active-1, 0)
	}
	if a.renaming == t {
		a.renaming = nil
	}
	if len(a.tabs) == 0 {
		a.changed()
		a.save()
		if a.win != nil {
			a.win.Close()
		}
		return
	}
	if nt := a.tab(); nt != nil {
		a.focusReq = nt.Focus
	}
	a.changed()
}

// closeTab closes a tab and ends the sessions of its panes.
func (a *App) closeTab(t *Tab) {
	for _, p := range t.panes() {
		p.markClosed()
		a.closeSFTPPane(p)
		if a.gitMenu.pane == p {
			a.gitMenu = gitMenuState{}
		}
		if a.gitPrompt.pane == p {
			a.gitPrompt = gitPromptState{}
		}
		if a.gitAuth.pane == p {
			a.gitAuth = gitAuthState{}
		}
		if p.term != nil {
			p.term.Close()
		}
		if p.sess != nil {
			p.sess.Close()
		}
	}
	a.removeTab(t)
}

func (a *App) selectTab(i int) {
	if i < 0 || i >= len(a.tabs) {
		return
	}
	a.active = i
	t := a.tabs[i]
	if t.Focus != nil {
		t.Focus.attention = false
	}
	a.focusReq = t.Focus
	a.refreshTrees()
	a.changed()
}

// moveFocus gives the focus to the pane next to the focused one, toward
// dx, dy.
func (a *App) moveFocus(dx, dy int) {
	t := a.tab()
	if t == nil || t.Focus == nil || t.Zoom != nil {
		return
	}
	from := t.Focus.bounds
	cx, cy := from.X+from.W/2, from.Y+from.H/2
	var best *Pane
	bestScore := float32(1e9)
	for _, p := range t.panes() {
		if p == t.Focus {
			continue
		}
		r := p.bounds
		var dist, overlap float32
		switch {
		case dx > 0:
			dist = r.X - (from.X + from.W)
			overlap = min(r.Y+r.H, from.Y+from.H) - max(r.Y, from.Y)
		case dx < 0:
			dist = from.X - (r.X + r.W)
			overlap = min(r.Y+r.H, from.Y+from.H) - max(r.Y, from.Y)
		case dy > 0:
			dist = r.Y - (from.Y + from.H)
			overlap = min(r.X+r.W, from.X+from.W) - max(r.X, from.X)
		default:
			dist = from.Y - (r.Y + r.H)
			overlap = min(r.X+r.W, from.X+from.W) - max(r.X, from.X)
		}
		if dist < -4 || overlap <= 0 {
			continue
		}
		// The nearest, then the one most in line with the focused pane's
		// center.
		px, py := r.X+r.W/2, r.Y+r.H/2
		off := px - cx
		if dx != 0 {
			off = py - cy
		}
		score := dist*4 + abs(off)
		if score < bestScore {
			best, bestScore = p, score
		}
	}
	if best != nil {
		t.setFocus(best)
		a.focusReq = best
	}
}

func abs(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// resizeFocused moves the nearest divider of the focused pane on the
// axis of dx, dy.
func (a *App) resizeFocused(dx, dy int) {
	t := a.tab()
	if t == nil || t.Focus == nil {
		return
	}
	vertical := dy != 0
	for n := t.Focus.Node; n.Parent != nil; n = n.Parent {
		p := n.Parent
		if p.Vertical != vertical {
			continue
		}
		step := float32(0.05)
		if dx < 0 || dy < 0 {
			step = -step
		}
		p.Ratio = min(max(p.Ratio+step, 0.1), 0.9)
		a.changed()
		return
	}
}

// equalize gives every split of the tab equal shares, by the panes on
// either side.
func (a *App) equalize() {
	t := a.tab()
	if t == nil || t.Settings {
		return
	}
	var walk func(n *Node) (cols, rows int)
	walk = func(n *Node) (int, int) {
		if n.Pane != nil {
			return 1, 1
		}
		ac, ar := walk(n.A)
		bc, br := walk(n.B)
		if n.Vertical {
			n.Ratio = float32(ar) / float32(ar+br)
			return max(ac, bc), ar + br
		}
		n.Ratio = float32(ac) / float32(ac+bc)
		return ac + bc, max(ar, br)
	}
	walk(t.Root)
	a.changed()
}

func (a *App) toggleZoom() {
	t := a.tab()
	if t == nil || t.Focus == nil {
		return
	}
	if t.Zoom != nil {
		t.Zoom = nil
	} else if len(t.panes()) > 1 {
		t.Zoom = t.Focus
	}
	a.focusReq = t.Focus
	a.changed()
}

// label returns how a pane names itself: its program, or the title the
// program set, and the directory it works in.
func (p *Pane) label() (name, detail string) {
	if conn := p.profile.SSH; conn != nil {
		name = conn.label()
		detail = "SSH"
		if d := conn.detail(); d != "" && d != name {
			detail = d
		}
		return
	}
	in := p.info
	if p.profile.WSL {
		return p.profile.Name, shortDir(p.directory())
	}
	prog := programOf(in.Program)
	dir := shortDir(in.Dir)
	if in.Program == "" {
		return in.Shell, shortDir(p.startDir)
	}
	if in.Idle || prog.Shell {
		name := in.Program
		if name == "" {
			name = in.Shell
		}
		return name, dir
	}
	if t := strings.TrimSpace(p.title); t != "" && !titleOfShell(t, in) {
		return t, ""
	}
	name = prog.Name
	if name == "ssh" || in.Program == "ssh" {
		if host := sshHost(in.Args); host != "" {
			return "SSH", host
		}
	}
	return name, dir
}

// titleOfShell tells a title a shell set, naming the program or the
// directory, from one a program set of its own.
func titleOfShell(title string, in session.Info) bool {
	l := strings.ToLower(title)
	for _, w := range []string{strings.ToLower(in.Program), strings.ToLower(in.Shell)} {
		// Windows's consoles title themselves with the shell's file:
		// "C:\WINDOWS\system32\cmd.exe - ping".
		if w != "" && (strings.HasPrefix(l, w) || strings.Contains(l, `\`+w+".exe")) {
			return true
		}
	}
	if home, _ := os.UserHomeDir(); strings.Contains(title, "~") || (home != "" && strings.Contains(title, home)) {
		return true
	}
	return strings.Contains(title, "@") && strings.Contains(title, ":")
}

func sshHost(args []string) string {
	for i := 1; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			if len(a) == 2 && strings.Contains("bcDEeFIiJLlmOoPpQRSWw", a[1:]) {
				i++
			}
			continue
		}
		return a
	}
	return ""
}

// shortDir shows a directory as shells do, from ~, and only its last
// parts when it is deep.
func shortDir(d string) string {
	if d == "" {
		return ""
	}
	sep := string(filepath.Separator)
	if home, _ := os.UserHomeDir(); home != "" {
		if samePath(d, home) {
			return "~"
		}
		if len(d) > len(home) && samePath(d[:len(home)], home) && d[len(home)] == filepath.Separator {
			d = "~" + d[len(home):]
		}
	}
	parts := strings.Split(d, sep)
	if len(parts) > 5 || len(d) > 48 && len(parts) > 3 {
		return "…" + sep + strings.Join(parts[len(parts)-2:], sep)
	}
	return d
}

// samePath reports whether two paths are the same, without case on
// Windows.
func samePath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// tabLabel names a tab: its name, or its focused pane's.
func (t *Tab) label() (name, detail string) {
	if t.Settings {
		return "Settings", ""
	}
	if t.Name != "" {
		return t.Name, ""
	}
	if t.Focus == nil {
		return "Shell", ""
	}
	return t.Focus.label()
}

// The saved layout: the tabs, their splits and where their panes work,
// which the app keeps and restores when it starts, with new shells.
type savedLayout struct {
	Active int        `json:"active"`
	Tabs   []savedTab `json:"tabs"`
}

type savedTab struct {
	Name  string     `json:"name,omitempty"`
	Root  *savedNode `json:"root"`
	Focus int        `json:"focus"`
	Zoom  bool       `json:"zoom,omitempty"`
}

type savedNode struct {
	Profile  terminalProfile `json:"profile,omitzero"`
	Dir      string          `json:"dir,omitempty"`
	Cols     int             `json:"cols,omitempty"`
	Rows     int             `json:"rows,omitempty"`
	Vertical bool            `json:"vertical,omitempty"`
	Ratio    float32         `json:"ratio,omitempty"`
	A        *savedNode      `json:"a,omitempty"`
	B        *savedNode      `json:"b,omitempty"`
}

func (a *App) snapshot() savedLayout {
	l := savedLayout{}
	var save func(n *Node) *savedNode
	save = func(n *Node) *savedNode {
		if p := n.Pane; p != nil {
			s := &savedNode{Dir: p.directory(), Profile: p.profile}
			if p.term != nil {
				s.Cols, s.Rows = p.term.Size()
			}
			return s
		}
		return &savedNode{Vertical: n.Vertical, Ratio: n.Ratio, A: save(n.A), B: save(n.B)}
	}
	for i, t := range a.tabs {
		if t.Settings {
			continue
		}
		if i <= a.active {
			l.Active = len(l.Tabs)
		}
		st := savedTab{Name: t.Name, Root: save(t.Root), Zoom: t.Zoom != nil}
		st.Focus = max(slices.Index(t.panes(), t.Focus), 0)
		l.Tabs = append(l.Tabs, st)
	}
	return l
}

// changed notes that the layout changed, to save it soon.
func (a *App) changed() { a.saveDue = true }

// layoutPath is where the layout is kept, in the app's data directory.
func (a *App) layoutPath() string { return filepath.Join(appdir.Dir(), "layout.json") }

// save keeps the layout on disk, once per second at most.
func (a *App) save() {
	a.saveDue = false
	b, err := json.Marshal(a.snapshot())
	if err != nil || string(b) == a.lastSnapshot {
		return
	}
	a.lastSave = time.Now()
	a.writeLayout(b)
}

// writeLayout writes the layout to its file, atomically: a reader never
// sees half of it.
func (a *App) writeLayout(b []byte) {
	a.layoutMu.Lock()
	defer a.layoutMu.Unlock()
	path := a.layoutPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return
	}
	a.lastSnapshot = string(b)
}

// restore opens the tabs of the saved layout, with a new shell in the
// directory of each pane.
func (a *App) restore() bool {
	raw, err := os.ReadFile(a.layoutPath())
	if err != nil || len(raw) == 0 {
		return false
	}
	var l savedLayout
	if json.Unmarshal(raw, &l) != nil || len(l.Tabs) == 0 {
		return false
	}
	var load func(s *savedNode, t *Tab, parent *Node) *Node
	load = func(s *savedNode, t *Tab, parent *Node) *Node {
		n := &Node{ID: a.id(), Parent: parent}
		if s.A == nil || s.B == nil {
			dir := s.Profile.startingDir(s.Dir)
			if st, err := os.Stat(dir); !s.Profile.WSL && (err != nil || !st.IsDir()) {
				dir, _ = os.UserHomeDir()
			}
			p := a.newPaneWithProfile(dir, s.Cols, s.Rows, s.Profile)
			p.restored = true
			p.Tab, p.Node = t, n
			n.Pane = p
			return n
		}
		n.Vertical, n.Ratio = s.Vertical, min(max(s.Ratio, 0.05), 0.95)
		n.A, n.B = load(s.A, t, n), load(s.B, t, n)
		return n
	}
	for _, st := range l.Tabs {
		if st.Root == nil {
			continue
		}
		t := &Tab{ID: a.id(), Name: st.Name}
		t.Root = load(st.Root, t, nil)
		ps := t.panes()
		for _, p := range ps {
			t.setFocus(p)
		}
		t.setFocus(ps[min(max(st.Focus, 0), len(ps)-1)])
		if st.Zoom && len(ps) > 1 {
			t.Zoom = t.Focus
		}
		a.tabs = append(a.tabs, t)
	}
	if len(a.tabs) == 0 {
		return false
	}
	a.active = min(max(l.Active, 0), len(a.tabs)-1)
	a.focusReq = a.tab().Focus
	return true
}

// apply takes what a session said of itself.
func (a *App) apply(p *Pane, in session.Info) {
	was := p.info
	p.info = in
	// A program that ran a while and finished out of sight asks for
	// attention.
	if !was.Idle && was.Program != "" && in.Idle && was.Program != in.Program {
		seen := a.tab() == p.Tab && p.Tab != nil && p.Tab.Focus == p && a.focusedWin
		if !seen {
			a.wantAttention(p)
		}
		if !a.focusedWin && time.Since(was.LastInput) > 8*time.Second {
			prog := programOf(was.Program)
			n := mygo.NewNotification(mygo.NotificationOptions{
				Title: prog.Name + " finished",
				Body:  "in " + shortDir(in.Dir),
			})
			n.Show()
		}
	}
	if was.Dir != in.Dir {
		a.changed()
	}
	if a.saveDue && time.Since(a.lastSave) > time.Second {
		a.save()
	}
}

// laterService queues a menu action without keeping a build context.
func (a *App) laterService(fn func()) { a.post(fn); a.services.Invalidate() }
