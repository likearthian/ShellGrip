package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type localFileWatch struct {
	path    string
	file    bool
	cancel  context.CancelFunc
	retry   time.Time
	wsl     bool
	ready   bool
	gitDir  string
	pending atomic.Bool
	batches atomic.Int64
	updates atomic.Int64
}

const watchedSafetyRefresh = 30 * time.Second

func (a *App) watchPollInterval(key string) time.Duration {
	if w := a.fileWatches[key]; w != nil && w.ready && w.retry.IsZero() {
		return watchedSafetyRefresh
	}
	return fileTreeRefresh
}

func (a *App) treePollInterval(p *Pane) time.Duration {
	// Without OSC 7, keep looking for cwd changes; directory reads can still be skipped.
	if p.profile.WSL && p.reportedDirectory() == "" {
		return fileTreeRefresh
	}
	return a.watchPollInterval("dir:" + a.fileWatchDirectory(p))
}

func (a *App) fileWatchReady(key string, spec *localFileWatch) {
	a.post(func() {
		if a.fileWatches[key] == spec {
			spec.ready = true
			a.localFilesChanged(spec)
		}
	})
	a.invalidate()
}

type previewFileStamp struct {
	modified time.Time
	size     int64
	mode     os.FileMode
	exists   bool
}

func statPreviewFile(name string) previewFileStamp {
	info, err := os.Stat(name)
	if err != nil {
		return previewFileStamp{}
	}
	return previewFileStamp{info.ModTime(), info.Size(), info.Mode(), true}
}

func localWatchPane(p *Pane) bool {
	return p != nil && !p.closed && p.profile.SSH == nil
}

// WSL already exposes its files through the distribution's UNC share.
// Keep other network shares out of the native watch set.
func fileWatchPath(p *Pane, name string) string {
	if !localWatchPane(p) {
		return ""
	}
	if p.profile.WSL {
		if !windowsProfiles || p.profile.distribution() == "" {
			return ""
		}
		if strings.HasPrefix(name, "/") {
			name = fileTreeDirectory(p, name)
		}
		prefix := `\\wsl.localhost\` + p.profile.distribution() + `\`
		if !strings.HasPrefix(strings.ToLower(name), strings.ToLower(prefix)) {
			return ""
		}
	} else if strings.HasPrefix(name, `\\`) {
		return ""
	}
	if !filepath.IsAbs(name) {
		return ""
	}
	return filepath.Clean(name)
}

func (a *App) fileWatchDirectory(p *Pane) string {
	if dir := fileWatchPath(p, p.directory()); dir != "" {
		return dir
	}
	// Reuse the tree's asynchronous WSL lookup when the shell does not emit OSC 7.
	if p.profile.WSL && a.fileTree != nil && a.fileTree.pane == p {
		return fileWatchPath(p, a.fileTree.root)
	}
	return ""
}

// keep watches only for the active folder and file previews. WSL watches and
// metadata polling pause for inactive tabs/windows to avoid idle UNC traffic.
// Native watches never follow SSH caches.
func (a *App) syncFileWatches(c *ui.Context) {
	if a.quitting || a.exiting.Load() {
		a.closeFileWatches()
		return
	}
	wanted := map[string]*localFileWatch{}
	if t := a.tab(); t != nil && !t.Settings && localWatchPane(t.Focus) && (!t.Focus.profile.WSL || a.focusedWin) {
		if dir := a.fileWatchDirectory(t.Focus); dir != "" {
			wanted["dir:"+dir] = &localFileWatch{path: dir, wsl: t.Focus.profile.WSL, gitDir: t.Focus.directory()}
		}
	}
	for _, t := range a.tabs {
		p := t.preview
		if p == nil || p.diff || p.source != "" || !localWatchPane(p.pane) {
			continue
		}
		if p.pane.profile.WSL && (t != a.tab() || !a.focusedWin) {
			continue
		}
		if name := fileWatchPath(p.pane, p.path); name != "" {
			wanted["file:"+name] = &localFileWatch{path: name, file: true, wsl: p.pane.profile.WSL}
		}
		// Polling also detects deletion/recreation after a watcher fails.
		interval := a.watchPollInterval("file:" + fileWatchPath(p.pane, p.path))
		if p.nextFileCheck.After(c.Now().Add(interval)) {
			p.nextFileCheck = c.Now().Add(interval)
		}
		if !p.loading && !p.checkingFile && !c.Now().Before(p.nextFileCheck) {
			p.nextFileCheck = c.Now().Add(interval)
			p.checkingFile = true
			revision := p.reloadRevision
			go func() {
				stamp := statPreviewFile(p.path)
				a.post(func() {
					p.checkingFile = false
					if t.preview == p && p.reloadRevision == revision && stamp != p.fileStamp {
						a.loadFilePreview(t, p)
					}
				})
				a.invalidate()
			}()
		}
		c.After(interval)
	}
	for key, w := range a.fileWatches {
		if wanted[key] == nil {
			w.cancel()
			delete(a.fileWatches, key)
		}
	}
	// Native watches need a running app; windowless views use the poll fallback.
	if a.win == nil {
		return
	}
	if a.fileWatches == nil {
		a.fileWatches = map[string]*localFileWatch{}
	}
	for key, spec := range wanted {
		if old := a.fileWatches[key]; old != nil {
			if old.retry.IsZero() || time.Now().Before(old.retry) {
				if !old.retry.IsZero() {
					c.After(time.Until(old.retry))
				}
				continue
			}
			old.cancel()
		}
		ctx, cancel := context.WithCancel(context.Background())
		spec.cancel = cancel
		a.fileWatches[key] = spec
		go a.watchLocalFiles(ctx, key, spec)
	}
}

func (a *App) watchLocalFiles(ctx context.Context, key string, spec *localFileWatch) {
	started := time.Now()
	if spec.wsl {
		defer func() {
			log.Printf("WSL file watch %s: lifetime=%s batches=%d UI-updates=%d", spec.path, time.Since(started).Round(time.Millisecond), spec.batches.Load(), spec.updates.Load())
		}()
	}
	var err error
	if spec.wsl {
		err = runWSLFileWatch(ctx, spec, func(pid int) {
			a.fileWatchReady(key, spec)
			log.Printf("WSL inotify %s: ready in %s (pid %d)", spec.path, time.Since(started).Round(time.Millisecond), pid)
		}, func() { a.queueFileChange(key, spec) })
	} else {
		w, watchErr := mygo.WatchFiles(ctx, spec.path, mygo.FileWatchOptions{
			Recursive: !spec.file, HintsOnly: !spec.file, Debounce: 150 * time.Millisecond, MaxDelay: 500 * time.Millisecond,
			Ignore: func(name string, isDir bool) bool { return isDir && filepath.Base(name) == "node_modules" },
		})
		err = watchErr
		if err == nil {
			a.fileWatchReady(key, spec)
			defer w.Close()
			for {
				_, err = w.Next(ctx)
				if err != nil {
					break
				}
				a.queueFileChange(key, spec)
			}
		}
	}
	if ctx.Err() != nil {
		return
	}
	log.Printf("file watch %s: %v; using polling", spec.path, err)
	a.post(func() {
		if a.fileWatches[key] == spec {
			spec.ready = false
			spec.retry = time.Now().Add(30 * time.Second)
			a.localFilesChanged(spec)
		}
	})
	a.invalidate()
}

// A stalled or busy UI gets one callback per watcher, regardless of burst size.
func (a *App) queueFileChange(key string, spec *localFileWatch) {
	spec.batches.Add(1)
	if !spec.pending.CompareAndSwap(false, true) {
		return
	}
	a.post(func() {
		spec.pending.Store(false)
		if a.fileWatches[key] == spec {
			spec.updates.Add(1)
			a.localFilesChanged(spec)
		}
	})
	a.invalidate()
}

func (a *App) localFilesChanged(w *localFileWatch) {
	if w.file {
		for _, t := range a.tabs {
			if p := t.preview; p != nil && !p.diff && p.source == "" && p.path == w.path {
				a.loadFilePreview(t, p)
			}
		}
		return
	}
	if s := a.fileTree; s != nil && (filepath.Clean(s.root) == filepath.Clean(w.path) || fileWatchPath(s.pane, s.hint) == w.path || filepath.Clean(s.hint) == w.path) {
		s.revision++
		s.next = time.Time{}
	}
	a.gitMu.Lock()
	dir := w.gitDir
	if dir == "" {
		dir = w.path
	}
	for key, entry := range a.gitByDir {
		if key == gitKey(dir) || entry.repo != nil && watchContains(entry.repo.Dir, dir) {
			entry.checked = time.Time{}
		}
	}
	a.gitMu.Unlock()
}

func watchContains(root, name string) bool {
	root, name = gitKey(root), gitKey(name)
	return root == name || strings.HasPrefix(name, root+string(filepath.Separator))
}

func (a *App) closeFileWatches() {
	for key, w := range a.fileWatches {
		w.cancel()
		delete(a.fileWatches, key)
	}
}
