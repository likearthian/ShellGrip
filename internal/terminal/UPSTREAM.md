# MyGo terminal plugin

Source: `github.com/egoist/mygo/plugins/terminal`
Version: `v0.10.0`
License: MIT, reproduced in LICENSE.

Copy-on-select and copy notifications now use upstream's `CopyOnSelect`,
`SetCopyOnSelect` and `OnCopy` APIs. ShellGrip maps its saved boolean setting to
`CopyOnSelectClipboard` or `CopyOnSelectNone`, and shows copy feedback for
`CopyUser` and `CopySelect`, leaving program clipboard writes quiet.

Local keybinding changes in `keybind.go` keep Ctrl+C for programs even with
a selection, make Ctrl+V paste on every platform, show Ctrl+V in the Paste
context menu on Windows and Linux, and move non-macOS reset zoom to
Ctrl+Shift+0 so Ctrl+0 remains available for tab 10. ShellGrip handles `OnZoom`
to update the saved font size of every pane.

`scroll.go` and the wheel hooks in `view.go` ease discrete mouse-wheel
scrollback over 150 ms, in whole terminal rows. Trackpad momentum and
wheel reports to programs stay direct. Keys, selection, scrollbar drags,
and changes to the viewport stop the glide; reduced motion disables it.

Other changes are import paths, English labels in `view.go` in place of
upstream's inaccessible `internal/l10n`, and omission of the upstream
generation tool and its directive. `internal/download` is copied from
MyGo's same-version internal package because Go's internal import rules
prevent using it directly. Its downloads retain upstream's SHA-256 checks,
retries and cross-process locking. The native-library manifest and
generated Ghostty themes are unchanged.

When updating MyGo, synchronize the plugin's production Go files and
manifest, omit `internal/libbuild`, and synchronize `internal/download`.
Reapply the import paths, English labels, generation-directive omission
and keybindings and wheel scrolling above. Keep LICENSE and ShellGrip's local regression tests;
upstream tests and fixtures are not vendored.
