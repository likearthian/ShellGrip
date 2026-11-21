// Package session runs the shells of GoRex's panes in pseudo-terminals and
// says what they run.
//
// A session lives as long as its pane: closing the pane hangs the shell up,
// and quitting the app ends them all.
package session

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"
)

// Info describes a session and what runs in it now.
type Info struct {
	ID string
	// PID is the shell's process, and Shell the program it started as.
	PID   int
	Shell string
	// Program is the name of the program in the foreground, and Args its
	// arguments; Idle tells that it is the shell itself, at its prompt.
	Program string
	Args    []string
	Idle    bool
	// Dir is the working directory of the program in the foreground, or
	// the one the session started in, until a program says otherwise.
	Dir string
	// LastInput is when the pane last typed into the session.
	LastInput time.Time
	Exited    bool
	ExitCode  int
}

// Options start a session.
type Options struct {
	// Command runs instead of the user's login shell.
	Command []string
	Dir     string
	Env     []string
	Cols    int
	Rows    int
}

// Session is a pseudo-terminal of a pane and its shell.
type Session struct {
	id    string
	shell string
	dir   string
	p     *pty

	mu        sync.Mutex
	lastInput time.Time
	exited    bool
	code      int
	closeOnce sync.Once
	done      chan struct{}
}

// New starts a session and the program in it.
func New(o Options) (*Session, error) {
	path, argv, name, err := shellCommand(o.Command)
	if err != nil {
		return nil, err
	}
	cols, rows := o.Cols, o.Rows
	if cols <= 0 || rows <= 0 {
		cols, rows = 80, 24
	}
	dir := o.Dir
	if dir == "" {
		dir, _ = os.UserHomeDir()
	}
	id := newID()
	p, err := startPTY(path, argv, dir, sessionEnv(id, o.Env), cols, rows)
	if err != nil {
		return nil, err
	}
	s := &Session{id: id, shell: name, dir: dir, p: p, done: make(chan struct{})}
	go func() {
		code := p.wait()
		// What it printed last is still to read; then the terminal closes,
		// so the reader ends and the pane knows its program exited.
		time.Sleep(150 * time.Millisecond)
		s.mu.Lock()
		s.exited, s.code = true, code
		s.mu.Unlock()
		p.close()
		close(s.done)
	}()
	return s, nil
}

// ID names the session, as GOREX_SESSION does in its environment.
func (s *Session) ID() string { return s.id }

// Read reads what the program printed.
func (s *Session) Read(b []byte) (int, error) { return s.p.read(b) }

// Write types into the program.
func (s *Session) Write(b []byte) (int, error) {
	s.mu.Lock()
	s.lastInput = time.Now()
	s.mu.Unlock()
	return s.p.write(b)
}

// Resize sets the size of the terminal, which the program sees.
func (s *Session) Resize(cols, rows int) error { return s.p.resize(cols, rows) }

// Close hangs the session up, ending its programs. It returns at once;
// a program that outlives the hangup is killed a moment later.
func (s *Session) Close() error {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		exited := s.exited
		s.mu.Unlock()
		if exited {
			return
		}
		s.p.hangup()
		go func() {
			select {
			case <-s.done:
			case <-time.After(time.Second):
				s.p.kill()
			}
		}()
	})
	return nil
}

// Kill ends the session now, as quitting the app does: a program that
// ignores the hangup is killed at once, not a moment later.
func (s *Session) Kill() {
	s.closeOnce.Do(func() {
		s.p.hangup()
		s.p.kill()
	})
}

// Info describes what runs in the session now.
func (s *Session) Info() Info {
	s.mu.Lock()
	in := Info{ID: s.id, Shell: s.shell, Dir: s.dir, LastInput: s.lastInput, Exited: s.exited, ExitCode: s.code}
	s.mu.Unlock()
	in.PID = s.p.pid()
	if in.Exited {
		return in
	}
	fg := s.p.foreground()
	if fg <= 0 {
		fg = in.PID
	}
	in.Idle = fg == in.PID
	prog := inspect(fg)
	in.Program, in.Args, in.Dir = prog.programName(), prog.args, prog.dir
	if in.Program == "" {
		in.Program = s.shell
	}
	if in.Dir == "" && !in.Idle {
		in.Dir = inspect(in.PID).dir
	}
	return in
}

// sessionEnv returns the environment of a session's shell: the app's,
// without what other terminals set, and what terminal apps set.
func sessionEnv(id string, extra []string) []string {
	var env []string
	hasLang := false
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		switch {
		case k == "TERM", k == "COLORTERM", k == "TERM_PROGRAM", k == "TERM_PROGRAM_VERSION",
			k == "TERM_SESSION_ID", k == "TMUX", k == "TMUX_PANE", k == "STY", k == "WINDOWID",
			k == "COLUMNS", k == "LINES", k == "SHLVL", k == "OLDPWD", k == "PWD", k == "_",
			strings.HasPrefix(k, "ITERM_"), strings.HasPrefix(k, "GHOSTTY_"), strings.HasPrefix(k, "KITTY_"),
			strings.HasPrefix(k, "VSCODE_"), strings.HasPrefix(k, "WEZTERM_"), strings.HasPrefix(k, "ALACRITTY_"),
			strings.HasPrefix(k, "GOREX_"), strings.HasPrefix(k, "MYGO_"):
			continue
		case k == "LANG" || k == "LC_ALL" || k == "LC_CTYPE":
			hasLang = true
		}
		env = append(env, kv)
	}
	env = append(env,
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"TERM_PROGRAM=GoRex",
		"TERM_PROGRAM_VERSION=0.1.0",
		"GOREX_SESSION="+id,
	)
	if !hasLang {
		env = append(env, "LANG=en_US.UTF-8")
	}
	for _, value := range extra {
		key, _, _ := strings.Cut(value, "=")
		env = slices.DeleteFunc(env, func(existing string) bool {
			name, _, _ := strings.Cut(existing, "=")
			return name == key || runtime.GOOS == "windows" && strings.EqualFold(name, key)
		})
		env = append(env, value)
	}
	return env
}

func newID() string {
	var b [6]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
