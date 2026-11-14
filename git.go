package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

// gitRefreshInterval is how long a repository's branch and refs are kept
// before the menu asks git for them again. WSL pays for a round trip
// through wsl.exe, so it asks less often.
const (
	gitRefreshInterval    = 4 * time.Second
	gitWSLRefreshInterval = 12 * time.Second
)

// gitRef is a local branch, a remote branch, or a tag.
type gitRef struct {
	// Name is the short name of a local branch or tag, and of a remote
	// branch its remote and branch, as "origin/main".
	Name     string
	OID      string
	Upstream string
	// Remote and Branch split a remote branch's Name.
	Remote string
	Branch string
}

// gitFile is a path that differs from HEAD, with its index (X) and work
// tree (Y) status, as porcelain v2 writes them.
type gitFile struct {
	Path string
	X, Y byte
}

// gitRepo is what a pane's title bar shows of the repository around its
// directory, and the refs and changes of its menus and Source Control pane.
type gitRepo struct {
	// Dir is the top level of the work tree.
	Dir           string
	Branch        string
	Head          string
	Upstream      string
	Ahead, Behind int
	// Dirty counts the files that differ from HEAD.
	Dirty    int
	Staged   []gitFile
	Unstaged []gitFile
	Locals   []gitRef
	Remotes  []gitRef
	Tags     []gitRef
}

// gitCacheEntry is a directory's repository, and when it was read last.
// A nil repo means the directory is not in a repository.
type gitCacheEntry struct {
	repo    *gitRepo
	checked time.Time
	loading bool
}

// gitCommit includes parent identities for drawing connected graph lanes.
type gitCommit struct {
	ID           string
	Parents      []string
	Graph        string
	Hash         string
	Author       string
	Subject      string
	Decorations  string
	Continuation bool
}

// gitHistory is the recent commits of a repository.
type gitHistory struct {
	Commits []gitCommit
}

// gitHistoryEntry is a repository's history, and when it was read last.
type gitHistoryEntry struct {
	history *gitHistory
	checked time.Time
	loading bool
}

// gitHistoryFor returns the recent commits of a repository, starting a
// read of them when the last one is old.
func (a *App) gitHistoryFor(c *ui.Context, p *Pane, repo *gitRepo) *gitHistory {
	if repo == nil {
		return nil
	}
	key := gitKey(repo.Dir)
	now := time.Now()
	a.historyMu.Lock()
	if a.historyByDir == nil {
		a.historyByDir = map[string]*gitHistoryEntry{}
	}
	entry := a.historyByDir[key]
	if entry == nil {
		entry = &gitHistoryEntry{}
		a.historyByDir[key] = entry
	}
	fresh := !entry.checked.IsZero() && now.Sub(entry.checked) < 10*time.Second
	start := !fresh && !entry.loading
	if start {
		entry.loading = true
	}
	history := entry.history
	services := c.Services()
	a.historyMu.Unlock()
	if start {
		go func() {
			loaded := loadGitHistory(p, repo.Dir)
			a.historyMu.Lock()
			entry.history, entry.checked, entry.loading = loaded, time.Now(), false
			a.historyMu.Unlock()
			services.Invalidate()
		}()
	}
	return history
}

// invalidateGitHistory asks for the next frame to read the history again,
// as after a commit.
func (a *App) invalidateGitHistory(dir string) {
	if dir == "" {
		return
	}
	key := gitKey(dir)
	a.historyMu.Lock()
	if entry := a.historyByDir[key]; entry != nil {
		entry.checked = time.Time{}
	}
	a.historyMu.Unlock()
}

func loadGitHistory(p *Pane, dir string) *gitHistory {
	out, err := gitOutput(p, dir, "log", "--topo-order", "--all", "--format=%H%x00%h%x00%P%x00%an%x00%s%x00%D%x00", "-n", "60")
	if err != nil {
		return &gitHistory{}
	}
	history := &gitHistory{}
	fields := strings.Split(string(out), "\x00")
	for i := 0; i+5 < len(fields); i += 6 {
		history.Commits = append(history.Commits, gitCommit{
			ID: strings.TrimSpace(fields[i]), Hash: fields[i+1], Parents: strings.Fields(fields[i+2]),
			Author: fields[i+3], Subject: fields[i+4], Decorations: strings.TrimSpace(fields[i+5]),
		})
	}
	return history
}

// gitFor returns the repository around a pane's directory, starting a
// read of it when the last one is old. It returns nil when the pane is
// not in a repository.
func (a *App) gitFor(c *ui.Context, p *Pane) *gitRepo {
	if p == nil {
		return nil
	}
	dir := p.directory()
	if dir == "" {
		return nil
	}
	key := gitKey(dir)
	ttl := gitRefreshInterval
	if p.profile.WSL {
		ttl = gitWSLRefreshInterval
	}
	now := time.Now()

	a.gitMu.Lock()
	if a.gitByDir == nil {
		a.gitByDir = map[string]*gitCacheEntry{}
	}
	entry := a.gitByDir[key]
	if entry == nil {
		entry = &gitCacheEntry{}
		a.gitByDir[key] = entry
	}
	// A directory outside a repository costs only a walk up its parents,
	// and that answer is cached too, as repositories appear and go.
	fresh := !entry.checked.IsZero() && now.Sub(entry.checked) < ttl
	start := !fresh && !entry.loading
	if start {
		entry.loading = true
	}
	repo := entry.repo
	a.gitMu.Unlock()

	if start {
		services := c.Services()
		go func() {
			loaded := loadGitRepo(p, dir)
			a.gitMu.Lock()
			entry.repo, entry.checked, entry.loading = loaded, time.Now(), false
			a.gitMu.Unlock()
			services.Invalidate()
		}()
	}
	return repo
}

// invalidateGit asks for the next frame to read a directory's repository
// again, as after a checkout or a commit.
func (a *App) invalidateGit(dir string) {
	if dir == "" {
		return
	}
	key := gitKey(dir)
	a.gitMu.Lock()
	if entry := a.gitByDir[key]; entry != nil {
		entry.checked = time.Time{}
	}
	a.gitMu.Unlock()
}

func gitKey(dir string) string {
	dir = filepath.Clean(dir)
	if runtime.GOOS == "windows" {
		return strings.ToLower(dir)
	}
	return dir
}

// gitLoading reports whether a directory's repository is being read.
func (a *App) gitLoading(dir string) bool {
	if dir == "" {
		return false
	}
	key := gitKey(dir)
	a.gitMu.Lock()
	defer a.gitMu.Unlock()
	entry := a.gitByDir[key]
	return entry != nil && entry.loading
}

// loadGitRepo reads a directory's repository, or returns nil when it is
// not in one.
func loadGitRepo(p *Pane, dir string) *gitRepo {
	root := dir
	if !p.profile.WSL {
		root = findGitRoot(dir)
		if root == "" {
			return nil
		}
	}
	out, err := gitOutput(p, root, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil
	}
	if top := strings.TrimSpace(string(out)); top != "" {
		if p.profile.WSL {
			root = top
		} else {
			root = filepath.Clean(nativeDirectory(top))
		}
	}

	repo := &gitRepo{Dir: root}
	if out, err = gitOutput(p, root, "status", "--porcelain=v2", "-z", "--branch", "--untracked-files=normal"); err == nil {
		parseGitStatus(repo, out)
	}
	if out, err = gitOutput(p, root, "for-each-ref", "--format=%(refname)%09%(objectname:short)%09%(upstream:short)", "refs/heads", "refs/remotes", "refs/tags"); err == nil {
		parseGitRefs(repo, out)
	}
	return repo
}

// findGitRoot walks up from dir to the directory that holds .git.
func findGitRoot(dir string) string {
	dir = filepath.Clean(dir)
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir || parent == "" {
			return ""
		}
		dir = parent
	}
}

// gitOutput runs git in dir and returns its standard output.
func gitOutput(p *Pane, dir string, args ...string) ([]byte, error) {
	exe, full := gitInvocation(p, dir, args...)
	timeout := 4 * time.Second
	if p.profile.WSL {
		timeout = 8 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, full...)
	configureEditorCommand(cmd)
	return cmd.Output()
}

// gitInvocation builds the program and arguments that run git in dir,
// inside a WSL distribution for a WSL pane.
func gitInvocation(p *Pane, dir string, args ...string) (string, []string) {
	if p != nil && p.profile.WSL {
		exe := "wsl.exe"
		if len(p.profile.Command) > 0 {
			exe = p.profile.Command[0]
		}
		full := []string{}
		if distro := p.profile.distribution(); distro != "" {
			full = append(full, "--distribution", distro)
		}
		full = append(full, "--cd", dir, "--exec", "git")
		// --cd already chooses the directory, and it expands ~ as WSL's
		// shells do.
		if len(args) >= 2 && args[0] == "-C" {
			args = args[2:]
		}
		return exe, append(full, args...)
	}
	return "git", append([]string{"-C", dir}, args...)
}

func parseGitStatus(repo *gitRepo, out []byte) {
	records := strings.Split(string(out), "\x00")
	for i := 0; i < len(records); i++ {
		line := records[i]
		switch {
		case strings.HasPrefix(line, "# branch.oid "):
			if oid := strings.TrimSpace(strings.TrimPrefix(line, "# branch.oid ")); oid != "(initial)" {
				repo.Head = shortOID(oid)
			}
		case strings.HasPrefix(line, "# branch.head "):
			if head := strings.TrimSpace(strings.TrimPrefix(line, "# branch.head ")); head != "(detached)" && head != "(unknown)" {
				repo.Branch = head
			}
		case strings.HasPrefix(line, "# branch.upstream "):
			repo.Upstream = strings.TrimSpace(strings.TrimPrefix(line, "# branch.upstream "))
		case strings.HasPrefix(line, "# branch.ab "):
			fmt.Sscanf(strings.TrimPrefix(line, "# branch.ab "), "+%d -%d", &repo.Ahead, &repo.Behind)
		case strings.HasPrefix(line, "# "):
			// Another branch header, as branch.ab before an upstream exists.
		case strings.HasPrefix(line, "1 "):
			fields := strings.SplitN(line, " ", 9)
			if len(fields) < 9 {
				continue
			}
			addGitStatusFile(repo, fields[1], fields[8])
		case strings.HasPrefix(line, "2 "):
			fields := strings.SplitN(line, " ", 10)
			if len(fields) < 10 {
				continue
			}
			// With -z, the path Git renamed from follows as another
			// record; the menu only needs the new one.
			if i+1 < len(records) {
				i++
			}
			addGitStatusFile(repo, fields[1], fields[9])
		case strings.HasPrefix(line, "u "):
			fields := strings.SplitN(line, " ", 11)
			if len(fields) < 11 {
				continue
			}
			addGitStatusFile(repo, fields[1], fields[10])
		case strings.HasPrefix(line, "? "):
			repo.Unstaged = append(repo.Unstaged, gitFile{Path: strings.TrimPrefix(line, "? "), X: '.', Y: '?'})
		}
	}
	dirty := map[string]bool{}
	for _, file := range repo.Staged {
		dirty[file.Path] = true
	}
	for _, file := range repo.Unstaged {
		dirty[file.Path] = true
	}
	repo.Dirty = len(dirty)
}

// addGitStatusFile adds a changed path to the side or sides that differ:
// X is the index, Y the work tree.
func addGitStatusFile(repo *gitRepo, xy, path string) {
	if len(xy) < 2 || path == "" {
		return
	}
	file := gitFile{Path: path, X: xy[0], Y: xy[1]}
	if file.X != '.' {
		repo.Staged = append(repo.Staged, file)
	}
	if file.Y != '.' {
		repo.Unstaged = append(repo.Unstaged, file)
	}
}

func parseGitRefs(repo *gitRepo, out []byte) {
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		parts := strings.Split(line, "\t")
		if len(parts) < 2 {
			continue
		}
		full, oid := parts[0], parts[1]
		upstream := ""
		if len(parts) > 2 {
			upstream = parts[2]
		}
		switch {
		case strings.HasPrefix(full, "refs/heads/"):
			name := strings.TrimPrefix(full, "refs/heads/")
			repo.Locals = append(repo.Locals, gitRef{Name: name, OID: oid, Upstream: upstream})
		case strings.HasPrefix(full, "refs/remotes/"):
			name := strings.TrimPrefix(full, "refs/remotes/")
			if strings.HasSuffix(name, "/HEAD") {
				continue
			}
			remote, branch, ok := strings.Cut(name, "/")
			if !ok {
				continue
			}
			repo.Remotes = append(repo.Remotes, gitRef{Name: name, OID: oid, Remote: remote, Branch: branch})
		case strings.HasPrefix(full, "refs/tags/"):
			name := strings.TrimPrefix(full, "refs/tags/")
			repo.Tags = append(repo.Tags, gitRef{Name: name, OID: oid})
		}
	}
}

func shortOID(oid string) string {
	if len(oid) > 7 {
		return oid[:7]
	}
	return oid
}

// gitActionTimeout is how long a git action may take: local work ends at
// once, while push and pull wait on the network.
const gitActionTimeout = 60 * time.Second

// gitRun runs git for an action in the backend, outside the pane's shell,
// and returns its combined output.
func gitRun(p *Pane, dir string, args ...string) ([]byte, error) {
	return gitRunWithAuth(p, dir, nil, args...)
}

// gitRunWithAuth runs git with the user's credentials when an action needs
// them. A WSL pane's git runs in its distribution through the same helper
// that reads the repository.
func gitRunWithAuth(p *Pane, dir string, creds *gitCredentials, args ...string) ([]byte, error) {
	exe, full := gitInvocation(p, dir, args...)
	env := gitEnvironment()
	cleanup := func() {}
	if creds != nil {
		if p != nil && p.profile.WSL {
			values, done, err := wslAskpass(p, *creds)
			if err != nil {
				return nil, err
			}
			full = wslEnvironment(full, append([]string{"GIT_TERMINAL_PROMPT=0"}, values...)...)
			cleanup = done
		} else {
			values, done, err := nativeAskpass(*creds)
			if err != nil {
				return nil, err
			}
			env = withEnvironment(env, values...)
			cleanup = done
		}
	} else if p != nil && p.profile.WSL {
		full = wslEnvironment(full, "GIT_TERMINAL_PROMPT=0")
	}
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), gitActionTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, full...)
	configureEditorCommand(cmd)
	cmd.Env = env
	return cmd.CombinedOutput()
}

// wslEnvironment runs the command under env in the distribution, which is
// how a variable reaches a WSL pane's git.
func wslEnvironment(args []string, values ...string) []string {
	for i, arg := range args {
		if arg != "--exec" {
			continue
		}
		full := make([]string, 0, len(args)+len(values)+1)
		full = append(full, args[:i+1]...)
		full = append(full, "env")
		full = append(full, values...)
		full = append(full, args[i+1:]...)
		return full
	}
	return args
}

// gitEnvironment is the app's environment with git's terminal prompt off:
// the backend has no terminal to answer it.
func gitEnvironment() []string {
	env := make([]string, 0, len(os.Environ())+1)
	for _, value := range os.Environ() {
		name, _, _ := strings.Cut(value, "=")
		if !strings.EqualFold(name, "GIT_TERMINAL_PROMPT") {
			env = append(env, value)
		}
	}
	return append(env, "GIT_TERMINAL_PROMPT=0")
}

// withEnvironment replaces variables in env, so that the app's askpass
// wins over one the user set.
func withEnvironment(env []string, values ...string) []string {
	for _, value := range values {
		name, _, _ := strings.Cut(value, "=")
		filtered := env[:0]
		for _, existing := range env {
			existingName, _, _ := strings.Cut(existing, "=")
			if existingName == name || runtime.GOOS == "windows" && strings.EqualFold(existingName, name) {
				continue
			}
			filtered = append(filtered, existing)
		}
		env = append(filtered, value)
	}
	return env
}

// runGitAction runs one git command in the backend and reads the
// repository again when it ends.
func (a *App) runGitAction(p *Pane, dir string, args ...string) {
	a.runGitActions(p, dir, args)
}

// runGitActions runs git commands in order in the backend, outside the
// pane's shell, and reads the repository and history again when they end.
func (a *App) runGitActions(p *Pane, dir string, commands ...[]string) {
	a.runGitCommands(p, dir, nil, commands...)
}

// runGitCommands runs git commands in order, with credentials when they
// are known. When git stops for credentials, it opens the dialog that asks
// for them and runs the commands again.
func (a *App) runGitCommands(p *Pane, dir string, creds *gitCredentials, commands ...[]string) {
	if p == nil || dir == "" || len(commands) == 0 {
		return
	}
	services := a.services
	pdir := p.directory()
	// Show the optimistic change, as a cleared commit box, while git runs.
	services.Invalidate()
	go func() {
		var failed []string
		var out []byte
		var err error
		for _, args := range commands {
			out, err = gitRunWithAuth(p, dir, creds, args...)
			if err != nil {
				failed = args
				break
			}
		}
		a.invalidateGit(dir)
		a.invalidateGitHistory(dir)
		if pdir != "" && pdir != dir {
			a.invalidateGit(pdir)
		}
		if err != nil {
			message := strings.TrimSpace(string(out))
			if message == "" {
				message = err.Error()
			}
			switch {
			case gitNeedsCredentials(out) && creds == nil:
				username := gitAuthUsername(out)
				a.post(func() { a.openGitAuth(p, dir, commands, username, message) })
			case gitNeedsCredentials(out):
				username := creds.Username
				a.post(func() { a.openGitAuthRetry(p, dir, commands, username) })
			default:
				failure := "git " + strings.Join(failed, " ") + ": " + message
				a.post(func() { a.err = failure })
			}
		}
		services.Invalidate()
	}()
}

// openGitAuth asks for the credentials a failed action needs.
func (a *App) openGitAuth(p *Pane, dir string, commands [][]string, username, message string) {
	if p == nil || p.closed {
		return
	}
	a.gitAuth = gitAuthState{
		open: true, pane: p, dir: dir, commands: commands,
		title:    "Authentication required",
		detail:   "Git needs credentials to continue. Enter a username and password or token, or a passphrase.",
		username: username,
		error:    firstLine(message),
	}
	a.focusReq = nil
}

// openGitAuthRetry asks again after git rejected the credentials.
func (a *App) openGitAuthRetry(p *Pane, dir string, commands [][]string, username string) {
	if p == nil || p.closed {
		return
	}
	a.gitAuth = gitAuthState{
		open: true, pane: p, dir: dir, commands: commands,
		title:    "Authentication required",
		detail:   "Git still needs credentials. Check them and try again.",
		username: username,
		error:    "Authentication failed.",
	}
	a.focusReq = nil
}

func (a *App) closeGitAuth() {
	a.gitAuth.open = false
	a.gitAuth.password = ""
	a.focusReq = a.gitAuth.pane
}

func (a *App) submitGitAuth() {
	auth := &a.gitAuth
	password := auth.password
	if password == "" {
		auth.error = "Enter a password or token."
		return
	}
	pane, dir, commands := auth.pane, auth.dir, auth.commands
	creds := &gitCredentials{Username: strings.TrimSpace(auth.username), Password: password}
	auth.open = false
	auth.password = ""
	a.focusReq = pane
	a.runGitCommands(pane, dir, creds, commands...)
}

// writeCommitMessage keeps a commit message where the pane's git can read
// it. A WSL pane's git cannot see Windows's temporary directory, so the
// message goes to the distribution's /tmp.
func writeCommitMessage(p *Pane, message string) (string, error) {
	if p != nil && p.profile.WSL && len(p.profile.Command) > 0 {
		return writeWSLCommitMessage(p, message)
	}
	return writeGitCommitMessage(message)
}

// writeGitCommitMessage keeps a commit message in a file, which git reads
// with commit -F, so that quotes and spaces reach it unchanged.
func writeGitCommitMessage(message string) (string, error) {
	file, err := os.CreateTemp("", "gorex-commit-*.txt")
	if err != nil {
		return "", err
	}
	path := file.Name()
	if _, err := file.WriteString(message); err != nil {
		file.Close()
		os.Remove(path)
		return "", err
	}
	if err := file.Close(); err != nil {
		os.Remove(path)
		return "", err
	}
	time.AfterFunc(2*time.Minute, func() { os.Remove(path) })
	return path, nil
}

// writeWSLCommitMessage writes a commit message inside the distribution,
// where its git can read it, and returns the Linux path.
func writeWSLCommitMessage(p *Pane, message string) (string, error) {
	name := fmt.Sprintf("/tmp/gorex-commit-%d-%d.txt", os.Getpid(), time.Now().UnixNano())
	args := append([]string(nil), p.profile.Command[1:]...)
	args = append(args, "--exec", "sh", "-c", `cat > "$1"`, "gorex", name)
	cmd := exec.Command(p.profile.Command[0], args...)
	configureEditorCommand(cmd)
	cmd.Env = environmentWithoutSession()
	cmd.Stdin = strings.NewReader(message)
	if err := cmd.Run(); err != nil {
		return "", err
	}
	time.AfterFunc(2*time.Minute, func() {
		args := append([]string(nil), p.profile.Command[1:]...)
		args = append(args, "--exec", "rm", "-f", name)
		cleanup := exec.Command(p.profile.Command[0], args...)
		configureEditorCommand(cleanup)
		cleanup.Env = environmentWithoutSession()
		cleanup.Run()
	})
	return name, nil
}

func environmentWithoutSession() []string {
	env := make([]string, 0, len(os.Environ()))
	for _, value := range os.Environ() {
		name, _, _ := strings.Cut(value, "=")
		if !strings.EqualFold(name, "GOREX_SESSION") {
			env = append(env, value)
		}
	}
	return env
}

// validGitRefName reports whether name is safe to give git as a branch or
// tag argument.
func validGitRefName(name string) bool {
	if name == "" || len(name) > 200 || strings.HasPrefix(name, "-") || strings.HasSuffix(name, "/") || strings.HasSuffix(name, ".") {
		return false
	}
	if strings.Contains(name, "..") || strings.Contains(name, "@{") || strings.Contains(name, "\\") {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return gitRefNamePattern.MatchString(name)
}

var gitRefNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/+~-]*$`)

// validGitRevision reports whether ref is safe to pass to git checkout.
func validGitRevision(ref string) bool {
	if ref == "" || len(ref) > 200 || strings.HasPrefix(ref, "-") {
		return false
	}
	return gitRevisionPattern.MatchString(ref)
}

var gitRevisionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/+~-]*$`)
