# GoRex

A replica of [Superlogical's Rex](https://www.superlogical.com/updates/public-testing-beginning)
terminal, written in Go with [MyGo](https://mygo.egoist.dev)'s native UI and its
terminal plugin (Ghostty's libghostty-vt). No webview, no cgo: one ~15 MB app.

![GoRex](docs/screenshot.png)

## What it does

- **File tree sidebar.** Toggle the left sidebar with the title-bar button,
  View ▸ Toggle File Tree, or Ctrl+Shift+B (⇧⌘B on macOS). It follows the
  focused shell's current directory, including WSL, with expandable folders
  and automatic refresh every two seconds. Drag the sidebar's right edge to
  resize it; double-click the divider to reset its width. Visibility and width
  are saved.
  Right-click a file or folder to copy its full path or its path relative to
  the tree's current directory. HTML files include **Open in Default Browser**;
  folders include **Open Folder in VS Code** when VS Code is installed. These
  folder actions are also available on the directory label above the tree.
- **File previews.** Click a file in the tree to view it in the main pane area.
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
  Source files above 64 KB, or with lines longer than 4,096 bytes, open in a
  virtualized viewer, with syntax colors added in the background for recognized
  languages. Long lines wrap in bounded chunks to keep scrolling responsive;
  copying preserves the original text.
- **Tabs and split panes.** Every pane is a card over the window's gradient,
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
  as do panes recreated after the session server restarts.
- **Persistent sessions.** Shells run in a session server, a background
  process of the same binary (`GoRex -server`). Quit GoRex and everything
  keeps running; open it again and every tab, split and pane comes back as
  it was, programs still running. The server keeps each session's screen in
  a headless libghostty-vt emulator and sends a snapshot on attach, so
  full-screen programs (lazygit, vim, htop…) come back exactly. Closing the
  window hides GoRex to the system tray (the menu bar on macOS), without a
  dialog. Click the tray icon to show the window; right-click for its menu
  (click on macOS). **Quit GoRex (Keep Sessions)** leaves sessions running;
  **Quit and End All Sessions** closes their programs and the server first.
  If a tray is unavailable, closing the window uses the platform's normal
  behavior and keeps sessions running.
- **Program activity.** The server watches each terminal's foreground
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
  have a context menu.
- **Host chip** with the machine's name and model; its popover shows the
  chip, memory, OS and the session server.
- **Command palette** (⇧⌘P, Ctrl+Shift+P, or the ⌘ button): every command, and every pane
  to jump to, fuzzy-matched.
- Light and dark appearances (View ▸ Appearance), text size (⌘+ ⌘− ⌘0),
  JetBrains Mono embedded.
- **Settings GUI.** Open the title-bar gear, View ▸ Settings, or Settings
  in the command palette to choose appearance, terminal font family and size,
  and the default terminal. Preview installed monospace fonts, then click
  **Apply font** to update every open pane and save your choice. Other changes
  apply and save automatically. **Reset font** restores bundled JetBrains Mono,
  which also serves as the fallback if a saved font is unavailable.
  Font size can be typed directly (8–32 pt, including decimals), or adjusted
  with the −/+ buttons.
  Under **Split panes**, **Highlight active pane** toggles the stronger active
  border, Active label, and blended inactive backgrounds. It is enabled by
  default, applies immediately, and saves automatically.
- **Terminal themes.** Settings includes a searchable list of bundled Ghostty
  themes, with a prompt preview and ANSI color swatches. Choosing a theme
  updates every open pane and saves it for new sessions. Terminal colors are
  independent of app appearance; choose **GoRex (follow appearance)** to restore
  the original light/dark terminal colors.
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
| ⌥⌘Q | | Quit and end all sessions |

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

GoRex needs [Go](https://go.dev/dl/) 1.27+ and MyGo 0.3.0, whose CLI
`go.mod` pins as a tool:

```sh
go tool mygo dev     # GoRex Dev, rebuilt and restarted as the code changes
go test ./...        # the server, and the view without a window
go tool mygo build   # build/darwin-arm64/GoRex.app and a .dmg
go run ./tools/mkicon  # copy the approved terminal logo to resources/icon.png
```

`go get -tool github.com/egoist/mygo/cmd/mygo@latest` updates MyGo and its
CLI together.

The app keeps its state in its data directory: the server's socket and
log, `layout.json` and `settings.json`, in `~/Library/Application
Support/GoRex` for the built app and `GoRex Dev` for `mygo dev`'s, so
that developing never touches the sessions of the app you use
(`GOREX_DIR` names another directory). The sessions outlive the app, but
not a rebuild: a development build replaces a server that an older build
started, ending its sessions, which then start again in the same
directories.

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
Native Windows process inspection cannot see Linux foreground programs.
See Microsoft's
[WSL command reference](https://learn.microsoft.com/en-us/windows/wsl/basic-commands).

## Layout

| | |
|---|---|
| `main.go` | the app, its window, and `-server` |
| `state.go` | tabs, the tree of splits, panes, saving and restoring the layout |
| `view.go`, `tabs.go`, `host.go`, `commands.go` | the interface: title bar, tabs, panes, host popover, menus and palette |
| `programs.go` | how programs show: names, glyphs, tile colors |
| `style.go`, `settings.go` | colors, fonts, terminal themes; appearance and text size |
| `internal/rex` | the session server and its client: PTYs, foreground processes, headless screens, the socket protocol |

Not replicated: Rex's connections to servers on other machines; GoRex's
server listens on a local socket only.

Icons: [Lucide](https://lucide.dev) (ISC) and [Simple Icons](https://simpleicons.org)
(CC0). Font: [JetBrains Mono](https://www.jetbrains.com/lp/mono/) (OFL).
