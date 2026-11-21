package main

import (
	"os"
	"testing"
	"time"

	"gorex/internal/session"
)

// TestAttention checks that a pane whose program finished out of sight
// asks for the user, at a known time the cues flash after, and stops
// asking once the user selects it.
func TestAttention(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame() // the window counts as focused once a frame runs
	p := a.tab().Focus
	a.newTab(os.TempDir())
	if a.tab().Focus == p {
		t.Fatal("the first tab is not out of sight")
	}
	running := session.Info{ID: p.SID, Program: "sleep", Idle: false, LastInput: time.Now()}
	p.info = running
	a.apply(p, session.Info{ID: p.SID, Program: "cmd", Idle: true})
	if !p.attention || p.attentionAt.IsZero() {
		t.Fatalf("attention %v at %v, after its program finished out of sight", p.attention, p.attentionAt)
	}
	if !a.attention() {
		t.Error("no tab asks for the user")
	}
	a.selectTab(0)
	if p.attention {
		t.Error("the pane still asks for the user after it was selected")
	}
	if a.attention() {
		t.Error("a tab asks for the user with nothing waiting")
	}
}
