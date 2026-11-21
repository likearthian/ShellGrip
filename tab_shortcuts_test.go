package main

import (
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func numberedTabs(count int) *App {
	a := &App{}
	for i := 0; i < count; i++ {
		tab := &Tab{ID: a.id(), Name: fmt.Sprintf("Tab %d", i+1)}
		pane := &Pane{ID: a.id(), Tab: tab}
		tab.Root = &Node{ID: a.id(), Pane: pane}
		pane.Node, tab.Focus = tab.Root, pane
		a.tabs = append(a.tabs, tab)
	}
	return a
}

func TestNumberedTabShortcuts(t *testing.T) {
	t.Setenv("GOREX_DIR", t.TempDir())
	a := numberedTabs(11)
	tt := ui.NewTester(a.view, 1400, 620)
	for _, test := range []struct {
		key   ui.Key
		index int
	}{{ui.Key9, 8}, {ui.Key0, 9}, {ui.Key1, 0}, {ui.Key5, 4}} {
		fontSize := termFont.Size
		tt.Key(ui.Ctrl, test.key)
		tt.Frame()
		if a.active != test.index {
			t.Fatalf("Ctrl+%v selected tab %d, want %d", test.key, a.active, test.index)
		}
		if termFont.Size != fontSize {
			t.Fatal("tab shortcut changed the font size")
		}
	}
	tt.Key(ui.Ctrl, ui.KeyTab)
	tt.Frame()
	if a.active != 5 {
		t.Fatal("Ctrl+Tab stopped cycling tabs")
	}
	a.tabs = a.tabs[:3]
	a.selectTab(1)
	tt.Frame()
	tt.Key(ui.Ctrl, ui.Key0)
	tt.Frame()
	if a.active != 1 {
		t.Fatal("shortcut for a missing tab changed selection")
	}
}

func TestTabHintDelay(t *testing.T) {
	a := &App{}
	start := time.Unix(100, 0)
	a.tabHintState(start, true)
	if a.tabHints {
		t.Fatal("hints appeared immediately")
	}
	a.tabHintState(start.Add(tabHintDelay-time.Millisecond), true)
	if a.tabHints {
		t.Fatal("hints appeared before the delay")
	}
	a.tabHintState(start.Add(tabHintDelay), true)
	if !a.tabHints {
		t.Fatal("hints did not appear after the delay")
	}
	a.tabHintState(start.Add(time.Second), false)
	if a.tabHints || !a.ctrlHeldAt.IsZero() {
		t.Fatal("hints did not clear when Ctrl was released")
	}
	a.tabHintState(start.Add(2*time.Second), true)
	if a.tabHints {
		t.Fatal("a new Ctrl press reused the previous hold")
	}
}

func TestTabHintDisplay(t *testing.T) {
	a := numberedTabs(11)
	tt := ui.NewTester(a.view, 1400, 620)
	before, _ := tt.Find("Tabs")
	tt.HoldModifiers(ui.Ctrl)
	if a.tabHints {
		t.Fatal("short Ctrl press showed hints")
	}
	tt.Advance(tabHintDelay)
	for number := 0; number <= 9; number++ {
		if rect, ok := tt.Find(fmt.Sprintf("Tab shortcut Ctrl+%d", number)); !ok || rect.H == 0 {
			t.Fatalf("missing visible hint for Ctrl+%d", number)
		}
	}
	after, _ := tt.Find("Tabs")
	if before != after {
		t.Fatal("showing hints moved the tab strip")
	}
	tt.HoldModifiers(0)
	if a.tabHints {
		t.Fatal("hints remained visible after Ctrl release")
	}
	a.fontFamilies = []string{defaultFontFamily}
	a.openSettings()
	tt.HoldModifiers(ui.Ctrl)
	tt.Advance(tabHintDelay)
	if !a.tabHints {
		t.Fatal("tab hints unavailable in Settings")
	}
}
