# MyGo terminal plugin

Source: `github.com/egoist/mygo/plugins/terminal`
Version: `v0.3.5-0.20261008155018-bad73d119dc8`
License: MIT, reproduced in LICENSE.

This local copy adds `Options.CopyOnSelect`, `Terminal.SetCopyOnSelect`, and
the UI-thread `Options.OnCopySelection` callback for selection-copy feedback.
On completion of a mouse selection, it copies nonempty selected text through
the view's clipboard service. Mouse events sent to terminal programs do not
copy text. The option is disabled by default in the plugin; GoRex enables it
through its saved settings.

Ctrl+V also pastes (in addition to the upstream platform shortcuts), and
the Paste context-menu shortcut is Ctrl+V on Windows and Linux.

Other changes are import paths and omission of the upstream generation tool.
The native-library manifest and generated Ghostty themes are unchanged.
When updating MyGo, synchronize this directory with its terminal plugin and
reapply the options, setter, and selection-copy hooks.
