package main

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func diffFixture(text string) previewContent {
	patches, err := editor.ParsePatch(text)
	if err != nil || len(patches) != 1 {
		panic(fmt.Sprintf("invalid fixture: %v", err))
	}
	return previewContent{kind: "diff", source: []byte(text), diffPatch: &patches[0]}
}

func diffTester(p *filePreview) *ui.Tester {
	registerFonts()
	a := &App{}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Children(func() { a.sideBySideDiff(c, colorsOf(c), p, c.Theme().Dark) })
	}, 1000, 620)
	tt.Frame()
	return tt
}

func TestDiffNavigationAndFocus(t *testing.T) {
	var patch strings.Builder
	patch.WriteString("diff --git a/x.go b/x.go\n--- a/x.go\n+++ b/x.go\n@@ -1,80 +1,80 @@\n")
	for i := range 80 {
		if i == 5 || i == 79 {
			fmt.Fprintf(&patch, "-old%d\n+new%d\n", i, i)
		} else {
			fmt.Fprintf(&patch, " line%d\n", i)
		}
	}
	p := &filePreview{content: diffFixture(patch.String())}
	tt := diffTester(p)
	if err := tt.Click("Next Change"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if p.diffChange != 1 {
		t.Fatalf("first change: %d", p.diffChange)
	}
	tt.Key(0, ui.KeyF7)
	tt.Frame()
	if p.diffChange != 2 || p.diffDisplay.CursorPos().Line != 79 || p.diffDisplay.ScrollY() <= 0 {
		t.Fatalf("last change: %d, cursor %+v, scroll %v", p.diffChange, p.diffDisplay.CursorPos(), p.diffDisplay.ScrollY())
	}
	tt.Key(0, ui.KeyUp)
	tt.Frame()
	if p.diffDisplay.CursorPos().Line != 78 {
		t.Fatal("navigation did not restore editor focus")
	}
	tt.Key(ui.Shift, ui.KeyF7)
	tt.Frame()
	if p.diffChange != 1 {
		t.Fatal("previous change failed")
	}
	tt.Key(ui.Shift, ui.KeyF7)
	tt.Frame()
	if p.diffChange != 2 {
		t.Fatal("previous did not wrap")
	}
	tt.Key(0, ui.KeyF7)
	tt.Frame()
	if p.diffChange != 1 {
		t.Fatal("next did not wrap")
	}
}

func TestLargeDiffScrollCopyAndReadOnly(t *testing.T) {
	tt, p := benchmarkDiffPreview(2000)
	d := p.diffDisplay
	d.SetScrollY(420)
	tt.Frame()
	if d.ScrollY() <= 0 {
		t.Fatal("large diff did not scroll")
	}
	if err := tt.Click("Next Change"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	before := d.New.String()
	mods := ui.Ctrl | ui.Shift
	if runtime.GOOS == "darwin" {
		mods = ui.Super
	}
	tt.Key(mods, ui.KeyA)
	tt.Key(mods, ui.KeyC)
	if tt.Clipboard() != before {
		t.Fatalf("copy contains gutters or alignment rows: got %d bytes, want %d", len(tt.Clipboard()), len(before))
	}
	tt.Type("must not edit")
	tt.Frame()
	if d.New.String() != before {
		t.Fatal("diff is editable")
	}
	tt.SetSize(650, 350)
	tt.Frame()
	r, ok := tt.Find("Diff preview")
	if !ok || r.W > 650 || r.H < 200 {
		t.Fatalf("resized viewport: %+v", r)
	}
	// Copying the old side must also omit padding across unequal line counts.
	tt.ClickAt(r.X+120, r.Y+35)
	tt.Frame()
	tt.Key(mods, ui.KeyA)
	tt.Key(mods, ui.KeyC)
	if tt.Clipboard() != d.Old.String() {
		t.Fatal("old-side copy includes display text")
	}
	divider, ok := tt.Find("Divider")
	if !ok {
		t.Fatal("missing diff divider")
	}
	tt.ClickAt(divider.X+divider.W/2, divider.Y+30)
	tt.Key(0, ui.KeyRight)
	tt.Frame()
	if d.SplitPercent <= 50 {
		t.Fatalf("divider did not resize: %v", d.SplitPercent)
	}
	tt.SetDark(true)
	tt.Frame()
}

func TestDiffPatchPreservesGapsAndEOF(t *testing.T) {
	p := &filePreview{content: diffFixture("diff --git a/x.txt b/x.txt\n--- a/x.txt\n+++ b/x.txt\n@@ -2 +2,2 @@\n-old \u4e16\u754c\n+new \u4e16\u754c\n+extra\n@@ -100 +101 @@\n-last\n+final\n\\ No newline at end of file\n")}
	tt := diffTester(p)
	d := p.diffDisplay
	if d.Old.String() != "old \u4e16\u754c\nlast" || d.New.String() != "new \u4e16\u754c\nextra\nfinal" {
		t.Fatalf("patch copy text: %q / %q", d.Old.String(), d.New.String())
	}
	if len(d.Hunks()) != 2 {
		t.Fatalf("lost Git change boundaries: %+v", d.Hunks())
	}
	tt.Key(0, ui.KeyF7)
	tt.Frame()
	tt.Key(0, ui.KeyF7)
	tt.Frame()
	if p.diffChange != 2 {
		t.Fatal("cannot navigate disjoint patch hunks")
	}
}

func TestDiffBindings(t *testing.T) {
	d := &filePreview{content: diffFixture("diff --git a/x b/x\n--- a/x\n+++ b/x\n@@ -1 +1 @@\n-old\n+new\n")}
	diffTester(d)
	overrides := d.diffDisplay.Bindings
	for _, b := range editor.DefaultBindings() {
		forbidden := b.Mods&(ui.Ctrl|ui.Alt) == ui.Ctrl|ui.Alt || b.ThenKey != 0 || b.Key == ui.KeyEscape
		if runtime.GOOS != "darwin" && b.Mods&ui.Ctrl != 0 && b.Key >= ui.KeyA && b.Key <= ui.KeyZ {
			forbidden = true
		}
		if runtime.GOOS != "darwin" && b.Mods == ui.Ctrl|ui.Shift && (b.Key == ui.KeyA || b.Key == ui.KeyC || b.Key == ui.KeyF || b.Key == ui.KeyG) {
			continue
		}
		if !forbidden {
			continue
		}
		found := false
		for _, o := range overrides {
			if o.Mods == b.Mods && o.Key == b.Key && o.ThenMods == b.ThenMods && o.ThenKey == b.ThenKey {
				found = o.Command == editor.NoCommand
				break
			}
		}
		if !found {
			t.Fatalf("default binding remains active: %+v", b)
		}
	}
}

func TestDiffSearchEscapeAndHeaders(t *testing.T) {
	registerFonts()
	for _, subtitle := range []string{"Changes", "Staged changes", "Untracked file"} {
		t.Run(subtitle, func(t *testing.T) {
			p := &filePreview{path: "x.txt", diff: true, subtitle: subtitle, content: diffFixture("diff --git a/x.txt b/x.txt\n--- a/x.txt\n+++ b/x.txt\n@@ -1 +1 @@\n-old\n+new\n")}
			tab := &Tab{preview: p}
			a := &App{tabs: []*Tab{tab}}
			tt := ui.NewTester(func(c *ui.Context) {
				a.runPosted()
				if tab.preview != nil {
					ui.Column(c).Fill().Children(func() { a.filePreviewPanel(c, colorsOf(c), tab) })
				}
			}, 900, 500)
			tt.Frame()
			left, right := "Original · Index", "Modified · Working Tree"
			if subtitle == "Staged changes" {
				left, right = "Original · HEAD", "Modified · Index"
			}
			if subtitle == "Untracked file" {
				left = "Original · Empty"
			}
			for _, text := range []string{left, right} {
				if _, ok := tt.Find(text); !ok {
					t.Fatalf("missing header %q", text)
				}
			}
			if err := tt.Click("Next Change"); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			mods := ui.Ctrl | ui.Shift
			if runtime.GOOS == "darwin" {
				mods = ui.Super
			}
			tt.Key(mods, ui.KeyF)
			tt.Frame()
			if !p.diffBar {
				t.Fatal("find shortcut did not open bar")
			}
			tt.Type("new")
			tt.Frame()
			tt.Key(0, ui.KeyEscape)
			tt.Frame()
			tt.Frame()
			if tab.preview == nil || p.diffBar {
				t.Fatal("Escape should close find before the preview")
			}
			mods = ui.Ctrl | ui.Shift
			if runtime.GOOS == "darwin" {
				mods = ui.Ctrl
			}
			tt.Key(mods, ui.KeyG)
			tt.Frame()
			if !p.diffBar {
				t.Fatal("go-to-line shortcut did not open bar")
			}
			tt.Key(0, ui.KeyEscape)
			tt.Frame()
			tt.Frame()
			if tab.preview == nil || p.diffBar {
				t.Fatal("Escape should close go-to-line before the preview")
			}
			tt.Key(0, ui.KeyEscape)
			tt.Frame()
			tt.Frame()
			if tab.preview != nil {
				t.Fatal("Escape did not close preview")
			}
		})
	}
}

// the editor takes a font-size multiplier, unlike ui's fixed line height.
func TestDiffLineSpacing(t *testing.T) {
	tt, p := benchmarkDiffPreview(100)
	for range 3 {
		tt.Frame()
	}
	for _, side := range []editor.DiffSide{editor.DiffOld, editor.DiffNew} {
		height := p.diffDisplay.LineTop(side, 1) - p.diffDisplay.LineTop(side, 0)
		if height < 20 || height > 22 {
			t.Fatalf("diff row height = %v DIPs, want 21", height)
		}
	}
}
