# Repository Guidelines

## Project Overview

**ShellGrip** (`module shellgrip`, identifier `dev.shellgrip.app`) is a native terminal workspace forked from GoRex, written in Go with [MyGo](https://mygo.egoist.dev)'s native UI and its terminal plugin (Ghostty's libghostty-vt). No webview, **no cgo**: a native app. It is a multi-tab, split-pane terminal emulator whose shells run in pseudo-terminals of the app itself (closing the window ends them, after asking about the programs that still run), with a file tree sidebar, a full Source Control sidebar (staging, side-by-side diffs, commit graph, branch menu with askpass), file previews (chroma/goldmark/images), VS Code Remote–WSL integration, and themes. Targets macOS (primary), Windows 10 1809+ (ConPTY/WSL), Linux. The single source of user documentation is `README.md` — keep it current with code changes.

## Architecture & Data Flow

One process:

```
GUI (root package main, MyGo ui) → Pane{term, sess} → internal/session (PTY + shell) → programs
                                   Layout ⇄ layout.json
```

- `main()` dispatch order (`main.go`): `SHELLGRIP_ASKPASS_SECRET` set → git askpass mode (`runAskpass`, `git_auth.go`); else set `SHELLGRIP_DIR` from the app data dir, `registerFonts()`, `loadSettings()`, then `App.open()` creates the window with `Content: ui.View(a.view)`, restores the saved layout with new shells, and closes the app with it (`beforeQuit`/`beforeClose` in `quit.go`).
- **Quit semantics:** closing the window quits. When a pane runs a program (`!info.Idle`) or an SSH connection is open, `workSummary()` (`quit.go`) stops the quit and a native `Dialog.Message` asks; tests answer through `App.askQuit`.
- **Immediate-mode UI, NOT bubbletea.** `func (a *App) view(c *ui.Context)` (`view.go`) rebuilds the UI each frame via `ui.Column/Row/Box/...` builders. There is no Model/Update/Init.
- **Async pattern:** mutations from goroutines go through `a.post(fn)` / `a.later(c, fn)` and drain in `runPosted()` at the top of every frame (`state.go`); terminal callbacks and the per-pane `watch()` goroutines marshal via `a.onUI(fn)` (window `Update`, or `post` in a view without one); `watch()` ticks 500 ms calling `sess.Info()`; view-level timers use `c.After(...)` (e.g. 2 s file-tree/git refresh; `gitRefreshInterval=4s`, `gitWSLRefreshInterval=12s` in `git.go`).
- State: one giant `App` struct (`state.go`) holding `Tab`s; each `Tab` roots a binary split tree `Node{Vertical,Ratio,A,B}` of `Pane{ID,SID,term,sess,info}`. Layout persists as JSON through `a.snapshot()` → `a.writeLayout()` → `layout.json` (atomic `.tmp`+rename, debounced save in `apply()`, `saveNow()` on quit); `restore()` recreates the tree and starts a new shell in each saved directory.


## Key Directories

| Path | Purpose |
|---|---|
| root `package main` | whole GUI: `state.go` (App/tabs/panes), `view.go`/`tabs.go`/`host.go`/`commands.go` (UI), `programs.go` (program names/glyphs/tile colors), `style.go`/`themes.go`/`settings.go` (colors/fonts/themes/prefs), `git.go`/`gitmenu.go`/`git_auth.go`/`scm.go`/`scm_graph.go` (git+SCM), `preview*.go`/`filetree*.go` (previews, tree), `profiles*.go`/`directories*.go` (shells, cwd), `vscode*.go`,
`ssh.go`/`ssh_settings.go`/`ssh_bootstrap.go` (saved SSH tabs, import of `~/.ssh/config`, folder hook),
`sftp*.go` (the SFTP connection and the remote file tree of SSH tabs),
`quit.go`/`clipboard.go`/`debug.go` |
| `internal/session/` | the panes' sessions: `New`/`Close`/`Info`, PTY + process inspection per OS (`pty_*.go`, `proc_*.go`), the host's details |
| `internal/appdir/` | `Dir()`: the app's data directory (`SHELLGRIP_DIR`) |
| `internal/terminal/` | **vendored copy** of MyGo's terminal plugin (see `UPSTREAM.md`); sub-`internal/pty`, `internal/vt` (purego FFI to libghostty-vt), `library.go` (lib load/download) |
| `assets/` | embedded SVG icons (Lucide), brand SVGs (Simple Icons), JetBrains Mono + Inter fonts |
| `resources/` | `icon.png` (embedded by `programs.go` for the About dialog), logo iterations + AI prompt provenance `.md` files |
| `tools/mkicon/` | copies `resources/gorex-terminal-logo-v3.png` → `resources/icon.png` |
| `tools/wslwatch/`, `tools/mkwslwatch/` | Linux inotify relay and its static amd64/arm64 builder; generated binaries live in ignored `resources/wslwatch/` and ship with app bundles for WSL and Linux SSH hosts |
| `build/`, `.mygo/` | gitignored build/dev output (exes, `ghostty-vt.dll`, caches) |
| `docs/` | only `screenshot.png` |

## Development Commands

No CI, no Makefile/scripts — these are the workflow:

```sh
go tool mygo dev        # "ShellGrip Dev": hot rebuild+restart; own data dir "ShellGrip Dev"
go test ./...           # sessions + view without a window (the standard gate)
go tool mygo build      # build/<os-arch>/ShellGrip.app + .dmg (mac) / ShellGrip.exe + Setup .exe (win)
go run ./tools/mkicon   # refresh the app icon
go run ./tools/mkwslwatch # prepare Linux relays for WSL dev/tests; mygo build runs this automatically
go get -tool github.com/egoist/mygo/cmd/mygo@latest   # update MyGo + CLI together
```

Single test: `go test -run TestLoadGitRepo .`; subpackages: `go test ./internal/session/ ./internal/terminal/`; benchmarks: `go test -bench . -run '^$' .`.

## Code Conventions & Common Patterns

- **Platform splits:** `*_windows.go` (implicit via filename) paired with `*_unix.go` carrying `//go:build !windows` (`directories_*`, `profiles_*`, `filetree_actions_*`). `internal/session` uses `//go:build darwin || linux` for `host_unix.go`/`pty_unix.go` and filename-only `*_windows.go`. The const `windowsProfiles` (`profiles_windows.go`/`profiles_unix.go`) is the runtime switch used throughout root code; `runtime.GOOS == "windows"` sets `ownControls` (frameless window, custom min/max/close).
- **NEVER introduce cgo.** FFI is purego only, gated to amd64/arm64 (`internal/terminal/internal/vt/ffi.go` build tag; `ffi_other.go` stub panics).
- **Naming:** root symbols are unexported lowercase (`loadGitRepo`, `gitChip`, `sourceControlPanel`); nearly everything is a `(a *App)` method; commands are package-level singletons `cmdXxx` with `Title`/`Accel`/`Keys`/`Other`/`Run`/`Hidden`. Exported APIs live in `internal/{session,terminal,appdir}` only.
- **Shortcuts convention (invariant):** off macOS, app shortcuts are `Ctrl+Shift+<letter>` (the terminal owns bare Ctrl+letter); paste is `Ctrl+V`, copy `Ctrl+Shift+C`; **no shortcut ever combines Ctrl+Alt** (AltGr typing). Rebinds keyed `GOOS+"/"+Title` in `settings.json.shortcuts`; empty string disables.
- **Errors:** return `error`, sentinel vars (`errUnsupported`, `errWSLExtensionMissing`, `pty.ErrClosed`); surface via `a.err` banner + `log.Printf` + `mygo.Dialog.Error`. `panic` only at init for embedded asset/font registration.
- **State:** no DI framework; package globals (`prefs`, `termFont`, `terminalLight/Dark`, git caches, `svgs`, `fades`). Cross-goroutine fields use `atomic.Int64/Bool`; caches guarded by `sync.Mutex`.
- **Comments:** dense, lowercase, declarative sentence-style doc comments with inline rationale. Match this voice.
- **Embeds:** `//go:embed assets/icons/*.svg assets/brands/*.svg` and `resources/icon.png` (`programs.go`), fonts (`style.go`), `mygo-plugin.json` (`internal/terminal/`).
- **Data dir files** live in `appdir.Dir()` (`SHELLGRIP_DIR` override; else `~/Library/Application Support/ShellGrip[ Dev]` / `%AppData%\ShellGrip`): `layout.json`, `settings.json`, `ssh.json` — dirs `0700`, files `0600`. Never let dev and production builds share a data dir.
- **Vendored terminal plugin:** `internal/terminal` mirrors `github.com/egoist/mygo/plugins/terminal` at the pinned version. On MyGo updates, re-sync production files and `internal/download`, and re-apply local deltas per `internal/terminal/UPSTREAM.md` (English labels, import paths, keybindings). Copy-on-select uses upstream's enum and `OnCopy`; ShellGrip filters program copies and handles `OnZoom` to keep all panes' saved font size in step.
- **Git execution:** backend-side via `gitOutput` (4 s timeout, 12 s for WSL; WSL panes run `wsl.exe -d <distro> git …`), never by typing into the pane shell. Ref inputs validated against `gitRefNamePattern`/`gitRevisionPattern`. Credentials handled by re-execing the binary as askpass with `SHELLGRIP_ASKPASS_SECRET` (0600 secret file).

## Important Files

- `main.go` — entry point, app/window/askpass dispatch, `SHELLGRIP_DIR`/`SHELLGRIP_DEBUG` wiring.
- `go.mod` / `mygo.json` — module `shellgrip`, `go 1.27.3`, MyGo 0.10.0 pinned via `tool` directive; app identity/version/out-dir manifest. `main.go` embeds `mygo.json`; `appVersion()` prefers the packaged `mygo.App.Version()` and falls back to the manifest (source/dev runs). No ldflags stamping.
- `state.go`, `view.go`, `commands.go`, `shortcuts.go` — App state, immediate-mode view, command/palette/keybinding system.
- `quit.go` — closing quits: `beforeQuit`/`beforeClose`, the work that stops a quit, and the dialog that asks about it.
- `git.go`, `scm.go`, `gitmenu.go`, `git_auth.go` — git plumbing and SCM UI.
- `internal/session/session.go` — `New`/`Read`/`Write`/`Resize`/`Close`/`Info` of a pane's session; `Info` reports the foreground program and its directory.
- `internal/session/host.go`, `host_unix.go`, `host_windows.go` — the host chip's details, read once at startup.
- `internal/terminal/library.go`, `internal/terminal/mygo-plugin.json` — libghostty-vt resolution: `$MYGO_GHOSTTY_VT` → app resources → beside exe → user-cache download (SHA-256 verified); pinned to an exact ghostty commit.
- `README.md` — features, shortcuts table, layout table, dev workflow.

## Runtime/Tooling Preferences

- Go **1.27.3+** required (`go.mod` says 1.27.3; uses `encoding/json/v2`). Build/dev is via `go tool mygo ...`, not plain `go build`, for app bundles.
- No cgo, no webview, no bubbletea, no charmbracelet. UI = MyGo (`github.com/egoist/mygo/ui`); terminal = libghostty-vt over purego (amd64/arm64 only).
- First `go test`/`go run` of `internal/terminal` downloads libghostty-vt into `<cache>/mygo/natives/<sha256>/` — network required unless packaged or `MYGO_GHOSTTY_VT` is set.
- Direct deps: `alecthomas/chroma/v2`, `ebitengine/purego`, `egoist/mygo`, `yuin/goldmark`, `golang.org/x/{image,sys,text}`.

## Testing & QA

- Stdlib `testing` only; colocated `*_test.go`; table-driven anonymous-slice loops; helpers `t.Helper()`-style (`newTestApp`, `waitFor`, `runGit`, `requireGit`, `requireWSL`, `previewFixture`). No testdata/golden/snapshot files; fixtures built in `t.TempDir()`.
- UI tests run the real view windowless via `ui.NewTester(a.view, W, H)` (`Click`, `Type`, `Key`, `Find`, `Texts`, `Frame`). The tester's clock only moves with `Advance`; real sleeps alone do not trigger tooltips or UI timers. `app_test.go:newTestApp` is the canonical setup: `t.Setenv("SHELLGRIP_DIR", ...)`, `t.Setenv("SHELL", testShell.path)`, `registerFonts()`, an `App` with one tab, `closeTestSessions` cleanup. `newTestAppInDir` starts shells in a fixture directory rather than racing the watcher with manual directory overrides. Use `nopConn` terminals for view-only tests; `refresh(a)` applies each pane's `sess.Info()` as the window's watcher does.
- Git tests exec **real git** (`runGit`, `init -b main` + commits); `requireGit`/`requireWSL` `t.Skip` when absent. Platform tests gated by `//go:build windows` (`git_windows_test.go`) or runtime skip (`if !windowsProfiles { t.Skip(...) }`).
- Test-affecting env vars: `SHELLGRIP_DIR`, `SHELL`, `SHELLGRIP_DEBUG` (script dir: lines like `shot`, `type <text>`, `size w h`, `tab N`, `focus N`, or a command title — drives the UI from a file), `SHELLGRIP_TEST_CTRLC` (pty re-exec role idiom), `SHELLGRIP_PREVIEW_TEST_FILE` (opt-in), `SHELLGRIP_ASKPASS_SECRET`.
- Gate for any change: `go test ./...` green (Windows-gated tests only run on Windows; no `-race`/coverage convention in-repo).
