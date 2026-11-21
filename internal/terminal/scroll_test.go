package terminal

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func scrollTerminal(t *testing.T) (*Terminal, *ui.Tester, float32) {
	t.Helper()
	term, tt := selectionTerminal(t, false)
	term.Feed([]byte(strings.Repeat("scrollback line\r\n", 200)))
	tt.Frame()
	dy := float32(40)
	if runtime.GOOS == "windows" {
		dy = 99
	} else if runtime.GOOS == "linux" {
		dy = 50
	}
	return term, tt, dy * 4 // twelve rows, on every backend.
}

func scrollOffset(term *Terminal) int {
	term.mu.Lock()
	defer term.mu.Unlock()
	return int(term.term.Scrollbar().Offset)
}

func TestWheelScrollGlidesAndAccumulates(t *testing.T) {
	term, tt, dy := scrollTerminal(t)
	start := scrollOffset(term)
	tt.Wheel(100, 80, 0, -dy)
	if scrollOffset(term) != start || !term.v.wheelActive {
		t.Fatal("wheel jumped instead of starting a glide")
	}
	tt.Advance(40 * time.Millisecond)
	mid := scrollOffset(term)
	if mid >= start || mid <= start-12 {
		t.Fatalf("expected an intermediate scroll offset, got %d from %d", mid, start)
	}
	tt.Wheel(100, 80, 0, -dy)
	tt.Advance(250 * time.Millisecond)
	if got := scrollOffset(term); got != start-24 || term.v.wheelActive {
		t.Fatalf("accumulated wheel ended at %d, want %d", got, start-24)
	}
}

func TestWheelReversalAndDirectInput(t *testing.T) {
	for _, cancel := range []string{"reverse", "trackpad", "key", "mouse program", "reduced motion"} {
		t.Run(cancel, func(t *testing.T) {
			term, tt, dy := scrollTerminal(t)
			if cancel == "reduced motion" {
				tt.SetPreferences(ui.Preferences{ReduceMotion: true})
			}
			start := scrollOffset(term)
			tt.Wheel(100, 80, 0, -dy)
			if cancel == "reduced motion" {
				if term.v.wheelActive || scrollOffset(term) != start-12 {
					t.Fatal("reduced motion did not scroll directly")
				}
				return
			}
			tt.Advance(40 * time.Millisecond)
			at := scrollOffset(term)
			switch cancel {
			case "reverse":
				tt.Wheel(100, 80, 0, dy/4)
				tt.Advance(250 * time.Millisecond)
				if got := scrollOffset(term); got != at+3 {
					t.Fatalf("reverse ended at %d, want %d", got, at+3)
				}
				return
			case "trackpad":
				tt.Scroll(100, 80, 0, -float32(term.v.cellH)/term.v.scale*2)
			case "key":
				tt.Key(0, ui.KeyLeft)
			case "mouse program":
				term.Feed([]byte("\x1b[?1000h"))
				tt.Frame()
			}
			at = scrollOffset(term)
			tt.Advance(250 * time.Millisecond)
			if scrollOffset(term) != at || term.v.wheelActive {
				t.Fatal("old wheel motion continued after direct input")
			}
		})
	}
}

func TestWheelOwnedByProgramStaysDirect(t *testing.T) {
	for _, mode := range []string{"mouse", "arrows", "observer"} {
		t.Run(mode, func(t *testing.T) {
			term, tt, dy := scrollTerminal(t)
			seen := 0
			switch mode {
			case "mouse":
				term.Feed([]byte("\x1b[?1000h\x1b[?1006h"))
			case "arrows":
				term.Feed([]byte("\x1b[?1049h\x1b[?1007h"))
			case "observer":
				term.opts.OnScroll = func(rows int) bool {
					seen += rows
					return true
				}
			}
			tt.Frame()
			at := scrollOffset(term)
			tt.Wheel(100, 80, 0, -dy)
			if term.v.wheelActive || scrollOffset(term) != at {
				t.Fatal("terminal glided while another handler owned the wheel")
			}
			if mode == "mouse" && term.v.mouse == nil || mode == "arrows" && term.v.keys == nil || mode == "observer" && seen != -12 {
				t.Fatal("wheel was not delivered directly to its handler")
			}
		})
	}
}
