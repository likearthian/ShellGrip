# GoRex

A replica of [Superlogical's Rex](https://www.superlogical.com/updates/public-testing-beginning)
terminal, written in Go with [MyGo](https://mygo.egoist.dev)'s native UI and its
terminal plugin (Ghostty's libghostty-vt). No webview, no cgo: a native app.

![GoRex](docs/screenshot.png)

## What it does

- **File tree sidebar.** Toggle the left sidebar with the title-bar button,
  View ▸ Toggle File Tree, or Ctrl+Shift+B (⇧⌘B on macOS). It follows the
  focused shell's current directory, including WSL, with expandable folders
  and file-change notifications for local folders. Healthy watchers use a
  30-second safety check; starting or failed watchers use a two-second polling
  fallback while the window is focused. Changes refresh the affected Git cache.
  WSL folders use Linux inotify through a small static Go helper, packaged for
  Linux amd64 and arm64. It needs neither Python nor Go in the distribution.
  GoRex installs the matching binary in the distribution's user cache, checking
  its SHA-256 against the packaged copy. Polling remains available if setup fails.
  The helper watches recursively without following symlinks, skips `node_modules`,
  and falls back to polling if its 8,192-directory limit is exceeded.
  Linux SSH hosts can use the same relay. Deployment runs in the background
  over SFTP while the terminal and five-second polling remain available.
  The sidebar shows **Enabling live updates…** with upload progress, then
  **Live updates** with 30-second safety refreshes. Host/user connections share
  deployment state; a verified cached version is reused without uploading.
  Relay builds exclude Git commit metadata, so unrelated GoRex commits do not
  force another upload of an unchanged helper.
  Unsupported hosts or setup failures retain **Using periodic refresh** with
  a **Retry** action. No Go or Python installation is required on the server.
  Remote previews check metadata and, for ambiguous notifications, checksums
  before downloading changed content again, including equal-size rapid saves;
  previews outside the watched folder retain five-second checks. Drag
  the sidebar's right edge to
  resize it; double-click the divider to reset its width. Visibility and width
  are saved.
  Right-click a file or folder to copy its full path or its path relative to
  the tree's current directory. HTML files include **Open in Default Browser**;
  folders include **Open Folder in VS Code** when VS Code is installed. These
  folder actions are also available on the directory label above the tree.
- **Source Control sidebar.** Switch the left sidebar between Files and
  Source Control with the button in its header, or with **Show Files** and
  **Show Source Control** in the command palette. The pane shows the current
  branch and its distance from upstream, a commit message box (Ctrl+Enter
  commits), and the staged and unstaged changes as a folder tree, where a
  folder holding only one folder reads as one path row. Status letters and
  stage/unstage actions cover one file, one folder, or all of them.
  Clicking a changed file shows its diff side by side — staged, unstaged,
  or untracked — with both line numbers and the changed lines highlighted,
  instead of its content. The native MyGo diff viewer provides a resizable
  split, word-level change highlighting, search, and go-to-line.
  F7 / Shift+F7 navigate changes and focus the text; Ctrl+Shift+F searches
  and Ctrl+Shift+G goes to a line off macOS (Cmd+F / Ctrl+G on macOS).
  Copy uses Ctrl+Shift+C off macOS or Cmd+C on macOS. Escape closes an
  open search or go-to-line bar first, then returns to the shell.
  Diffs remain read-only; binary and metadata-only changes retain the
  unified patch view.
  Below, the recent commit graph lists
  its hashes, subjects and branch or tag decorations. The sidebar's choice
  is saved. WSL directories use the same helper as the branch menu.
- **SSH tabs.** Save connections in Settings ▸ SSH, or import your
  `~/.ssh/config` with one click, then open one from the menu beside + in the
  title bar. An SSH tab is a normal tab: it splits, comes back after a restart, and
  names its host in the header. Connections live in `ssh.json` in GoRex's
  data directory; the app never writes your ssh config. Password prompts use
  a dialog, and the connection keeps itself alive. The left **Files** tree of
  an SSH tab behaves like the local one: it follows the shell's folder,
  expands folders, and previews files, downloading them to a cache first. To
  follow `cd`, GoRex starts the shell with a small prompt hook for bash, zsh
  and fish; **Follow the shell's folder** in a connection's settings turns it
  off for hosts without a POSIX shell. The tree refreshes every five seconds
  while the window is focused, and at once when the window or its tab comes
  back. It speaks SFTP over the system `ssh`, using the same keys, agent,
  ProxyJump
  and config as the terminal. On macOS and Linux it joins the tab's
  authenticated connection.
- **Git branch menu.** When a shell's current directory is inside a Git
  repository, its pane header shows the current branch (or the commit, when
  HEAD is detached). Click it to search branches and actions: **Update
  Project** (pull), **Commit**, **Push**, **New Branch**, **Checkout Tag or
  Revision**, and the local branches, remote branches and tags of the
  repository. Local, Remote and Tags fold, remote branches show under their
  remote, and tags with a folder in their name, as `agent/v1`, group under
  it. Actions run in GoRex's backend, not in the pane's shell, so they work
  while another program holds the pane. A WSL pane's git runs inside its
  distribution through the same helper that reads the repository. Commits
  stage all changes. When git needs a username or password, GoRex asks for
  them in a dialog with the password hidden, then retries with a temporary
  askpass helper; the error of an action appears in the window.
- **File previews.** Click a file in the tree to view it in the main pane area.
  Ctrl-click a file path printed in a terminal (Cmd-click on macOS) to open
  it in that pane's tab. Relative paths use the shell's current directory;
  WSL paths use its distribution and SSH paths download through SFTP.
  Compiler-style `file:line:column` links scroll text previews near the line.
  URLs continue to open in the default application.
  Local previews reload after external saves, including replacement saves,
  while retaining their scroll position. Deleted files show an error until
  they reappear; periodic checks keep reloads working if file watching fails.
  WSL file previews reload too; their watchers and background file checks pause
  while their tab or window is inactive. Notifications are debounced, and each
  watcher queues at most one pending UI refresh during a burst of changes.
  Preview reloads are serialized so rapid saves do not start overlapping reads.
  Healthy preview watchers also use 30-second safety checks, returning to fast
  checks when the watcher fails. WSL shells without directory reports still
  check their current directory every two seconds, but skip unchanged tree
  reads until a notification, user action, or safety refresh requires one.
  Source files use syntax highlighting and line numbers; Markdown renders
  headings, lists, tables, emphasis, code blocks, and local images. Images
  (PNG, JPEG, GIF, WebP, BMP, TIFF, and SVG) fit inside the preview. Other
  text-based files fall back to plain text; binary files show **unsupported
  content**. Text previews accept UTF-8 and legacy Windows-1252 encoding.
  Drag to select preview text, then use Ctrl+C (⌘C on macOS) or
  right-click ▸ Copy. Select All includes the whole document; line numbers are
  excluded from copied code. Tabs display at four-column stops; copying keeps
  the original tabs and spaces. Previews are read-only; press Escape or
  close the preview to return to the shell with its layout and session intact.
  Text previews are limited to 2 MB / 20,000 lines; image files to 32 MB and
  raster images to 32 million pixels. Remote Markdown images remain placeholders.
  Source files above 40 KB, or with lines longer than 4,096 bytes, open in a
  virtualized viewer, with syntax colors added in the background for recognized
  languages. Long lines wrap in bounded chunks to keep scrolling responsive;
  copying preserves the original text. Markdown documents above 400 blocks
  scroll in a virtualized list that builds only the blocks in view, so Select
  All there copies the blocks it shows.
- **Tabs and split panes.** Every pane is a rounded panel inset into the window chrome,
  with its program's icon, name and directory in its header, and buttons to
  split right, split down, zoom and close. Drag the gaps between panes to
  resize them (double-click resets a split to half).
  When VS Code is installed, its logo in the pane header opens that shell's
  current directory in VS Code. WSL shells open through Remote–WSL in the
  shell's distribution, using the Microsoft **WSL** extension
  (`ms-vscode-remote.remote-wsl`). If the extension check or launch fails,
  a dialog shows the error, extension information, a Marketplace link, and
  an install command to copy. WSL folders in the file tree use Remote–WSL too.
- **Terminal profiles.** Click **+** to open the default terminal, or the
  adjacent dropdown to choose an installed shell. Use the button beside a
  profile to make it the saved default (marked with a check). Windows profiles
  include PowerShell, Windows PowerShell, Command Prompt, Git Bash, and
  registered WSL distributions. Splits and restarted panes keep their profile,
  as do the panes of a restored layout.
- **Closing quits.** Closing the window ends GoRex and every shell in it: it
  is a terminal you open to work and close when you are done. When a program
  still runs in a pane, or an SSH connection is open, GoRex asks first;
  **Cancel** keeps the app and everything running. One GoRex runs per user:
  launching it again while it is running (e.g. from the taskbar) shows that
  instance's window instead of starting a second one. The next launch brings
  back the tabs, their splits, ratios and names, and the directory of every
  pane, each pane with a new shell.
- **Program activity.** GoRex watches each terminal's foreground
  process (`tcgetpgrp`, `sysctl`, `proc_pidinfo`; on Windows, the shell's
  newest child, its command line and directory): headers and tabs name the
  program (`Node`, `Git Changes` for lazygit, `Codex`, `SSH host`…) and its
  working directory, a green dot shows a program printing, and an orange dot
  marks a pane whose program finished, rang the bell, or asked for a
  notification out of sight. Its tab flashes amber for a few seconds and the
  window's taskbar button (Windows, Linux) or Dock icon (macOS) asks for you
  until the window comes back to the front, unless the desktop asks for less
  motion. When the window is in the background, a long command finishing
  posts a notification.
- **Tab icons** stack the tiles of a tab's programs, the focused pane's in
  front, as Rex does. WSL panes use their distribution's icon and color,
  with a Linux icon for unrecognized distributions. Tabs reorder by dragging, rename by double-click, and
  have a context menu. The active tab has a blue-tinted fill and blue outline.
- **Host chip** with the machine's name and model; its popover shows the
  chip, OS and user, the sessions of the window, the memory the app works in,
  and the app version, with an **About** button for the app's identity and
  credits.
- **Command palette** (⇧⌘P, Ctrl+Shift+P, or the ⌘ button): every command, and every pane
  to jump to, fuzzy-matched.
- Light and dark appearances (View ▸ Appearance), text size (⌘+ ⌘− ⌘0),
  with flat charcoal surfaces, subtle borders and muted blue selection in dark
  mode, a matching neutral light palette, and JetBrains Mono embedded.
- **Settings GUI.** Open the title-bar gear, View ▸ Settings, or Settings
  in the command palette to choose appearance, terminal font family and size,
  and the default terminal. Preview installed monospace fonts, then click
  **Apply font** to update every open pane and save your choice. Other changes
  apply and save automatically. **Reset font** restores bundled JetBrains Mono,
  which also serves as the fallback if a saved font is unavailable.
  Font size can be typed directly (8–32 pt, including decimals), or adjusted
  with the −/+ buttons.
  Under **Window**, **Translucent window chrome** uses the system backdrop for
  the title bar and frame. On supported Windows 11 builds, Mica Alt tints
  the chrome using your desktop wallpaper. Terminal panes stay opaque.
  Under **Split panes**, **Highlight active pane** toggles the stronger active
  border, Active label, and dimmed inactive shells. It is enabled by
  default, applies immediately, and saves automatically.
- **Terminal behavior.** Settings ▸ Terminal includes minimum text contrast
  and scroll speed, applied to all open panes. Scrollback capacity (10–100 MiB
  per pane) and macOS Option-as-Alt apply to newly opened panes. Existing
  preferences keep the default colors, scrolling speed, and 10 MiB history.
- **Terminal themes.** Settings includes a searchable list of bundled Ghostty
  themes, with a prompt preview and ANSI color swatches. Choosing a theme
  updates every open pane and saves it for new sessions. Terminal colors are
  independent of app appearance; choose **GoRex (follow appearance)** to restore
  the matching light/dark terminal colors.
- **Copy on select.** Highlight terminal text and release the mouse to copy it
  to the clipboard. Double-clicking a word or triple-clicking a line copies it
  too. Enabled by default; turn it off under Settings ▸ Clipboard. While a
  terminal program captures the mouse, hold Shift to select and copy text.
  A brief toast in the top-right corner confirms how many characters were copied, including when
  using the copy shortcut or context menu. Unicode text counts characters,
  not encoded bytes; repeated copies update the same toast.

## Shortcuts

| macOS | Windows, Linux | |
|---|---|---|
| ⌘T | Ctrl+Shift+T | New tab |
| ⌘D / ⇧⌘D | Ctrl+Shift+D / Ctrl+Shift+E | Split right / down |
| ⌘W / ⇧⌘W | Ctrl+Shift+W | Close pane / tab |
| ⇧⌘↩ | Ctrl+Shift+Enter | Zoom the pane |
| ⌥⌘ arrows | Alt+arrows | Focus the pane in that direction |
| ⌃⌘ arrows | Alt+Shift+arrows | Move the nearest divider |
| ⌃⌘= | | Equalize panes |
| ⌘1…⌘9, ⇧⌘[ ⇧⌘], ⌃Tab | Ctrl+1…9, Ctrl+0 (tab 10), Ctrl+PageUp/PageDown, Ctrl+Tab | Switch tabs |
| ⇧⌘R | Ctrl+Shift+R | Rename tab |
| ⇧⌘P, ⌘P | Ctrl+Shift+P | Command palette |
| ⌘K | Ctrl+Shift+K | Clear |
| ⇧⌘B | Ctrl+Shift+B | Toggle file tree |
| ⌘+ ⌘− ⌘0 | Ctrl+= Ctrl+- Ctrl+Shift+0 | Text size |

Elsewhere than on macOS, Control and a letter are the terminal's (Ctrl+C,
Ctrl+D, Ctrl+W…), so GoRex's shortcuts take Shift too, as copying does
(Ctrl+Shift+C). Paste uses Ctrl+V; Ctrl+Shift+V also remains available.
No shortcuts take Control and Alt,
which type characters (AltGr) on many keyboards.

Hold Ctrl for half a second to reveal number hints on the first ten tabs;
release it to hide them. Ctrl+1–9 selects the corresponding tab, and Ctrl+0
selects tab 10. Ctrl+Tab still cycles tabs. The previous Ctrl+Shift+1–9
shortcuts remain available (Ctrl+Shift+9 selects the last tab).

## Develop and build

GoRex needs [Go](https://go.dev/dl/) 1.27.3+ and MyGo 0.10.0, whose CLI
`go.mod` pins as a tool:

```sh
go run ./tools/mkwslwatch # prepare Linux relays before WSL development/testing
go tool mygo dev     # GoRex Dev, rebuilt and restarted as the code changes
go test ./...        # the view without a window, and the sessions
go tool mygo build   # build/darwin-arm64/GoRex.app and a .dmg
go run ./tools/mkicon  # copy the approved terminal logo to resources/icon.png
```

`mygo build` runs the relay builder automatically through `buildCommand`.
The builder sets `CGO_ENABLED=0` for Linux amd64 and arm64 and writes ignored
files under `resources/wslwatch`; app bundles ship them for WSL and Linux SSH hosts.

`go get -tool github.com/egoist/mygo/cmd/mygo@latest` updates MyGo and its
CLI together.

To measure WSL watcher startup, idle process CPU, replacement-save latency and
a 200-write burst against a real distribution, run this opt-in check in PowerShell:

```powershell
$env:GOREX_TEST_WSL_WATCH = "Ubuntu"
go test -v -run '^TestWSLFileWatchPerformance$' .
Remove-Item Env:GOREX_TEST_WSL_WATCH
```

The check creates and removes its own temporary folder inside the distribution;
it skips if WSL cannot start. A watcher that cannot receive Linux changes fails
the check. It also measures Linux helper CPU ticks and verifies the helper exits
when canceled. WSL watcher logs report startup time and notification/UI-update
counts. One long-lived helper serves each watch; notifications launch no new
Linux processes, and closing the watch closes the helper's input to end it.

For an end-to-end SSH/SFTP watcher test without contacting saved hosts, set
`GOREX_TEST_SSH_WATCH=Ubuntu` and run `go test -v -run '^TestSSHRelayLoopback$' .`.
This Windows opt-in check requires OpenSSH server in that WSL distribution;
it starts a temporary loopback server with temporary keys, verifies upload,
cache reuse and notifications, then stops the relay/server and removes its files.

The terminal plugin is vendored from the same MyGo version; after updating
the module, synchronize it too, following `internal/terminal/UPSTREAM.md`.
Its newer renderer supports Kitty graphics (including Unicode image
placeholders), clipboard image pastes for programs that support them, and
a draggable scrollback bar. Ctrl/Cmd+wheel changes the saved text size in
every pane. Programs cannot read the clipboard unless GoRex explicitly
allows it; a paste supplies only the clipboard data offered by that paste.

The app keeps its state in its data directory: `layout.json`,
`settings.json` and the saved SSH connections, in `~/Library/Application
Support/GoRex` for the built app and `GoRex Dev` for `mygo dev`'s, so that
developing never touches the app you use (`GOREX_DIR` names another
directory). A rebuild starts the shells anew, in the directories the layout
saved.

### Windows

GoRex runs on Windows 10 1809 or later, whose ConPTY runs the shells; its
data directory is `%AppData%\GoRex`. Panes run `$SHELL` when it names a
Windows program (`nu`, `C:\Program Files\Git\bin\bash.exe`), else
PowerShell (`pwsh`, then `powershell`), else `cmd`. PowerShell's `cd`
leaves its process's directory where it was, so GoRex starts it with a
prompt that sets it and reports its location with OSC 7, wrapping the
profile's own prompt: the header then follows it, and new panes open there.
Windows file URIs are converted back to drive paths before launching a split.
The window is frameless and its
title bar draws minimize, maximize and close with the other buttons; it
resizes from its sides and bottom, not its top.

WSL profiles use `wsl.exe --distribution <name> --cd <directory>` inside
ConPTY. WSL must already be installed with a registered Linux distribution;
GoRex does not install one. Opening WSL from a Windows shell starts in the
Linux user's home directory (`~`). Splits use the source shell's current
directory: shell-reported paths (OSC 7) when available, refreshed process
inspection for native shells, and a session-tagged Linux process lookup for
WSL. WSL sessions opened before this feature need a new tab for directory
lookup; when lookup is unavailable, splits retain the last known directory.
Native Windows process inspection cannot see Linux foreground programs,
so the Git menu and Source Control run git through `wsl.exe` in the pane's
distribution; they do not need the pane's shell.
See Microsoft's
[WSL command reference](https://learn.microsoft.com/en-us/windows/wsl/basic-commands).

## Layout

| | |
|---|---|
| `main.go` | the app and its window |
| `state.go` | tabs, the tree of splits, panes, saving the layout and restoring it |
| `view.go`, `tabs.go`, `host.go`, `commands.go` | the interface: title bar, tabs, panes, host popover, menus and palette |
| `programs.go` | how programs show: names, glyphs, tile colors |
| `ssh.go`, `ssh_settings.go` | saved SSH connections, importing `~/.ssh/config`, and the new-tab menu entries |
| `sftp.go`, `sftp_tree.go`, `sftp_session.go` | the SFTP connection and the SSH tab's remote file tree |
| `style.go`, `settings.go` | colors, fonts, terminal themes; appearance and text size |
| `git.go`, `gitmenu.go`, `scm.go` | the branch chip of pane headers, its menu and the Source Control sidebar |
| `internal/session` | the panes' pseudo-terminals: shells, foreground processes, the host |
| `internal/appdir` | the data directory of the app's state |

Not replicated: Rex's connections to servers on other machines. Every shell
runs in a pseudo-terminal of this app, on this machine.

Icons: [Lucide](https://lucide.dev) (ISC) and [Simple Icons](https://simpleicons.org)
(CC0). Font: [JetBrains Mono](https://www.jetbrains.com/lp/mono/) (OFL).
