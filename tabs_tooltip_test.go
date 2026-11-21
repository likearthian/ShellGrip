package main

import (
	"os"
	"strings"
	"testing"
)

// TestTabTooltip checks that resting the pointer on a tab shows its whole
// label, which the tab fades out when it is too narrow for it.
func TestTabTooltip(t *testing.T) {
	a, tt := newTestApp(t)
	a.newTab(os.TempDir())
	tab := a.tabs[0]
	tab.Name = "console.sql"
	tt.Frame()
	name, detail := tab.label()
	full := strings.TrimSpace(name + " " + detail)
	count := func() int {
		n := 0
		for _, s := range tt.Texts() {
			if s == full {
				n++
			}
		}
		return n
	}
	if n := count(); n != 1 {
		t.Fatalf("%q appears %d times before the pointer rests on it; texts %q", full, n, tt.Texts())
	}
	track, ok := tt.Find("Tabs")
	if !ok {
		t.Fatal("no tab bar")
	}
	tt.Move(track.X+20, track.Y+track.H/2)
	tt.Frame()
	// The tip shows once the pointer has rested on the tab for a while.
	waitFor(t, tt, "the tab's tooltip", func() bool { return count() > 1 })
}
