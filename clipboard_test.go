package main

import (
	"encoding/json"
	"os"
	"testing"

	"shellgrip/internal/terminal"

	"github.com/egoist/mygo/ui"
)

func TestCopyToast(t *testing.T) {
	a := numberedTabs(1)
	tt := ui.NewTester(a.view, 1000, 620)
	a.selectionCopied("é漢🙂")
	tt.Frame()
	if rect, ok := tt.Find("Copied 3 characters to clipboard"); !ok {
		t.Fatalf("missing Unicode copy count: %q", tt.Texts())
	} else if rect.X < 600 || rect.Y < titleH || rect.Y > 100 {
		t.Fatalf("toast is not in the top-right corner: %v", rect)
	}
	if a.copiedCharacters != 0 {
		t.Fatal("copy notification was not consumed")
	}
	a.selectionCopied("x")
	tt.Frame()
	if _, ok := tt.Find("Copied 1 character to clipboard"); !ok {
		t.Fatalf("missing singular copy count: %q", tt.Texts())
	}
	if _, ok := tt.Find("Copied 3 characters to clipboard"); ok {
		t.Fatal("repeated copies stacked instead of updating the toast")
	}
	a.selectionCopied("")
	if a.copiedCharacters != 0 {
		t.Fatal("empty selection scheduled a toast")
	}
}

func TestTerminalCopySources(t *testing.T) {
	a := &App{}
	a.terminalCopied("program clipboard", terminal.CopyProgram)
	if a.copiedCharacters != 0 {
		t.Fatal("a program's clipboard write scheduled selection feedback")
	}
	for _, source := range []terminal.CopySource{terminal.CopyUser, terminal.CopySelect} {
		a.terminalCopied("é漢🙂", source)
		if a.copiedCharacters != 3 {
			t.Fatalf("copy source %v lost Unicode copy feedback", source)
		}
		a.copiedCharacters = 0
	}
}

func TestTerminalZoomSavesSharedFontSize(t *testing.T) {
	t.Setenv("SHELLGRIP_DIR", t.TempDir())
	oldPrefs, oldFont := prefs, termFont
	t.Cleanup(func() { prefs, termFont = oldPrefs, oldFont })
	a := &App{}
	termFont.Size = defaultFontSize
	if !a.terminalZoom(terminal.Zoom{Steps: 2}) || prefs.FontSize != defaultFontSize+2 {
		t.Fatal("terminal zoom did not update the saved font size")
	}
	data, err := os.ReadFile(settingsPath())
	if err != nil {
		t.Fatal(err)
	}
	var saved settings
	if err := json.Unmarshal(data, &saved); err != nil || saved.FontSize != defaultFontSize+2 {
		t.Fatalf("terminal zoom was not persisted: %s, %v", data, err)
	}
	a.terminalZoom(terminal.Zoom{Reset: true})
	if termFont.Size != defaultFontSize || prefs.FontSize != defaultFontSize {
		t.Fatal("terminal zoom did not reset to ShellGrip's default")
	}
}
