package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestDiffReplacementHighlight(t *testing.T) {
	rows := parseDiffRows("@@ -1 +1 @@\n-hello 世界 old\n+hello 世界 new\n")
	if len(rows) != 1 {
		t.Fatalf("rows: %+v", rows)
	}
	for _, cell := range []diffCell{rows[0].left, rows[0].right} {
		if cell.changeStart != 9 || cell.changeEnd != 12 {
			t.Fatalf("highlight range %d:%d, want 9:12", cell.changeStart, cell.changeEnd)
		}
		for _, dark := range []bool{false, true} {
			spans := diffCellSpans(cell, &colors{}, dark)
			var text strings.Builder
			for _, span := range spans {
				text.WriteString(span.Text)
			}
			if text.String() != cell.text || len(spans) != 2 || spans[1].Background == (ui.Color{}) {
				t.Fatalf("highlight loses text or background: %+v", spans)
			}
		}
	}
}

func TestDiffPaneScrollingAndNavigation(t *testing.T) {
	registerFonts()
	a := &App{}
	p := &filePreview{subtitle: "Changes"}
	for i := range 100 {
		kind := byte(' ')
		if i == 5 || i == 60 {
			kind = '-'
		}
		left := diffCell{number: i + 1, text: fmt.Sprintf("line %d %s", i+1, strings.Repeat("wide ", 30)), present: true, kind: kind}
		right := left
		if kind == '-' {
			right.kind = '+'
		}
		p.content.diffRows = append(p.content.diffRows, diffRow{left: left, right: right})
	}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Children(func() { a.sideBySideDiff(c, colorsOf(c), p, c.Theme().Dark) })
	}, 900, 400)
	tt.Frame()
	left, ok := tt.Find("Original · Index diff pane")
	if !ok {
		t.Fatal("missing original pane")
	}
	right, ok := tt.Find("Modified · Working Tree diff pane")
	if !ok || left.W != right.W || left.X >= right.X || right.X+right.W > 900 {
		t.Fatalf("panes do not split the viewport equally: %v / %v", left, right)
	}
	tt.ClickAt(left.X+80, left.Y+10)
	tt.Key(ui.Ctrl, ui.KeyA)
	tt.Key(ui.Ctrl, ui.KeyC)
	var original []string
	for _, row := range p.content.diffRows {
		original = append(original, row.left.text)
	}
	if tt.Clipboard() != strings.Join(original, "\n") {
		t.Fatalf("copy must contain original code across lines without gutters, got %q", tt.Clipboard())
	}
	for _, pane := range []ui.Rect{left, right} {
		tt.Scroll(pane.X+50, pane.Y+50, 80, 160)
		tt.Frame()
		if p.diffScroll[0].Y <= 0 || p.diffScroll[0].Y != p.diffScroll[1].Y || p.diffScroll[0].X <= 0 || p.diffScroll[0].X != p.diffScroll[1].X {
			t.Fatalf("scrolling either pane must move both: %+v / %+v", p.diffScroll[0], p.diffScroll[1])
		}
	}
	if err := tt.Click("Next Change"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if p.diffChange != 1 || p.diffScroll[0].X != 0 || p.diffScroll[1].X != 0 {
		t.Fatalf("next must reveal the first change from its start: %d, %+v", p.diffChange, p.diffScroll)
	}
	tt.Key(0, ui.KeyF7)
	tt.Frame()
	if p.diffChange != 2 || p.diffScroll[0].Y != 57*diffLineHeight || p.diffScroll[0].Y != p.diffScroll[1].Y {
		t.Fatalf("F7 did not reveal second change: %d, %+v", p.diffChange, p.diffScroll)
	}
	tt.Key(ui.Shift, ui.KeyF7)
	tt.Frame()
	if p.diffChange != 1 {
		t.Fatalf("Shift+F7 selected change %d, want 1", p.diffChange)
	}
	tt.Key(ui.Shift, ui.KeyF7)
	tt.Frame()
	if p.diffChange != 2 {
		t.Fatalf("previous did not wrap: %d", p.diffChange)
	}
	tt.SetSize(500, 400)
	tt.Frame()
	left, _ = tt.Find("Original · Index diff pane")
	right, _ = tt.Find("Modified · Working Tree diff pane")
	if left.W != right.W || right.X+right.W > 500 {
		t.Fatalf("panes overflow after resize: %v / %v", left, right)
	}
}

func TestLargeDiffScrollAndCopy(t *testing.T) {
	tt, p := benchmarkDiffPreview(2000)
	left, _ := tt.Find("Original · Index diff pane")
	right, _ := tt.Find("Modified · Working Tree diff pane")
	if left.H < 400 || right.H != left.H {
		t.Fatalf("large diff has no viewport: %v / %v", left, right)
	}
	for _, rect := range []ui.Rect{left, right} {
		tt.Scroll(rect.X+100, rect.Y+100, 0, 420)
		tt.Frame()
		if p.diffScroll[0].Y <= 0 || p.diffScroll[0].Y != p.diffScroll[1].Y {
			t.Fatalf("large diff scroll is not synchronized: %+v", p.diffScroll)
		}
	}
	// Both native buffers retain all source lines, including those never laid out.
	tt.ClickAt(left.X+120, left.Y+30)
	tt.Key(ui.Ctrl, ui.KeyA)
	tt.Key(ui.Ctrl, ui.KeyC)
	var lines []string
	for _, row := range p.content.diffRows {
		lines = append(lines, row.left.text)
	}
	if tt.Clipboard() != strings.Join(lines, "\n") {
		t.Fatalf("copy must include all 2000 original lines without gutters, got %d bytes", len(tt.Clipboard()))
	}
	tt.SetSize(650, 350)
	tt.Frame()
	left, _ = tt.Find("Original · Index diff pane")
	right, _ = tt.Find("Modified · Working Tree diff pane")
	if left.W != right.W || right.X+right.W > 650 || left.H < 200 {
		t.Fatalf("large diff resize: %v / %v", left, right)
	}
	// A native horizontal scroll must not wrap lines or change their alignment.
	tt.Scroll(left.X+120, left.Y+60, 80, 0)
	tt.Frame()
	if p.diffHorizontal[0].X <= 0 || p.diffHorizontal[0].X != p.diffHorizontal[1].X {
		t.Fatalf("large horizontal scroll is not synchronized: %+v", p.diffHorizontal)
	}
	if p.diffScroll[0].MaxY != p.diffScroll[1].MaxY {
		t.Fatalf("large panes have different line heights: %+v", p.diffScroll)
	}
	if err := tt.Click("Next Change"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if p.diffScroll[0].Y != 0 || p.diffScroll[1].Y != 0 || p.diffHorizontal[0].X != 0 || p.diffHorizontal[1].X != 0 {
		t.Fatalf("navigation did not reset scrolling: %+v / %+v", p.diffScroll, p.diffHorizontal)
	}
}

func TestLargeDiffCopySkipsAlignmentRows(t *testing.T) {
	registerFonts()
	a := &App{}
	p := &filePreview{}
	var want [2][]string
	for i := range 2000 {
		row := diffRow{}
		if i%10 == 0 {
			row.separator = true
		} else {
			for side := range 2 {
				if i%3 == side {
					continue
				}
				cell := diffCell{number: i, text: fmt.Sprintf("value 世界 %d", i), present: true, kind: ' '}
				for theme := range cell.spans {
					cell.spans[theme] = []ui.Span{{Text: cell.text, Color: ui.Hex("#7aa2f7")}}
				}
				if side == 0 {
					row.left = cell
				} else {
					row.right = cell
				}
				want[side] = append(want[side], cell.text)
			}
		}
		p.content.diffRows = append(p.content.diffRows, row)
	}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Children(func() { a.sideBySideDiff(c, colorsOf(c), p, c.Theme().Dark) })
	}, 1000, 620)
	tt.Frame()
	for side, label := range []string{"Original · Index diff pane", "Modified · Working Tree diff pane"} {
		pane := &p.diffDisplay.panes[side].preview.content
		if pane.selectedSource(0, len([]rune(pane.buffer.String()))) != strings.Join(want[side], "\n") {
			t.Fatalf("side %d copy mapping includes alignment or separators", side)
		}
		r, _ := tt.Find(label)
		tt.ClickAt(r.X+150, r.Y+30)
		tt.Key(ui.Ctrl, ui.KeyA)
		tt.Key(ui.Ctrl, ui.KeyC)
		if tt.Clipboard() != strings.Join(want[side], "\n") {
			t.Fatalf("side %d native copy includes display insertions: got %d bytes, want %d", side, len(tt.Clipboard()), len(strings.Join(want[side], "\n")))
		}
	}
	tt.SetDark(true)
	tt.Frame()
}
