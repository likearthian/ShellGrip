package main

import (
	"testing"
	"time"

	"shellgrip/internal/session"
)

func TestSessionPollInterval(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name                string
		idle, background    bool
		outputAge, inputAge time.Duration
		want                time.Duration
	}{
		{"foreground program", false, false, time.Minute, time.Minute, 500 * time.Millisecond},
		{"foreground shell", true, false, time.Minute, time.Minute, 1500 * time.Millisecond},
		{"background shell", true, true, time.Minute, time.Minute, 10 * time.Second},
		{"quiet background WSL", false, true, time.Minute, time.Minute, 10 * time.Second},
		{"recent output", false, true, time.Second, time.Minute, 500 * time.Millisecond},
		{"recent input", true, true, time.Minute, time.Second, 500 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := session.Info{Idle: tc.idle, LastInput: now.Add(-tc.inputAge)}
			if got := sessionPollInterval(in, now.Add(-tc.outputAge), tc.background, now); got != tc.want {
				t.Fatalf("poll interval = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestBackgroundWakesEveryPane(t *testing.T) {
	p := &Pane{wakeWatch: make(chan struct{}, 1)}
	q := &Pane{wakeWatch: make(chan struct{}, 1)}
	a := &App{tabs: []*Tab{
		{Root: &Node{Pane: p}},
		{Root: &Node{A: &Node{Pane: q}, B: &Node{Pane: &Pane{}}}},
	}}
	for _, background := range []bool{true, false} {
		a.setBackground(background)
		// Repeated events must coalesce without blocking the UI thread.
		a.setBackground(background)
		if a.background.Load() != background {
			t.Fatal("background state did not follow window focus")
		}
		for _, pane := range []*Pane{p, q} {
			select {
			case <-pane.wakeWatch:
			default:
				t.Fatal("pane did not receive a focus refresh")
			}
		}
	}
}
