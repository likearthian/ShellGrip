package terminal

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"
)

// exercise the newly bound Ghostty APIs against the packaged library too.
func TestUpgradedTerminalSearch(t *testing.T) {
	term, tt := selectionTerminal(t, false)
	term.Search("hello")
	tt.Frame()
	if s := term.SearchStatus(); s.Total != 1 || s.Selected != 1 || s.Searching {
		t.Fatalf("search status = %+v", s)
	}
	term.Feed([]byte("\r\nHELLO"))
	tt.Frame()
	if s := term.SearchStatus(); s.Total != 2 {
		t.Fatalf("search did not follow new output: %+v", s)
	}
	selected := term.SearchStatus().Selected
	term.SearchNext()
	if s := term.SearchStatus(); s.Selected == selected || s.Selected < 1 || s.Selected > 2 {
		t.Fatalf("search did not navigate: %+v", s)
	}
	term.EndSearch()
	if s := term.SearchStatus(); s != (SearchStatus{}) {
		t.Fatalf("search did not end: %+v", s)
	}
}

func TestUpgradedKittyGraphics(t *testing.T) {
	term, tt := selectionTerminal(t, false)
	w, h := term.v.cellW, term.v.cellH
	data := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{255, 0, 0, 255}, w*h))
	term.Feed(fmt.Appendf(nil, "\x1b[3;3H\x1b_Ga=T,f=32,s=%d,v=%d,i=7,q=2,C=1;%s\x1b\\", w, h, data))
	tt.Frame()
	x, y := selectionCell(term, 2, 2)
	at := func() ui.Color {
		c := tt.Image().RGBAAt(int(x*term.v.scale), int(y*term.v.scale))
		return ui.RGB(c.R, c.G, c.B)
	}
	red := ui.RGB(255, 0, 0)
	if got := at(); got != red {
		t.Fatalf("Kitty image pixel = %v, want %v", got, red)
	}
	term.Feed([]byte("\x1b_Ga=d,d=I,i=7,q=2\x1b\\"))
	tt.Frame()
	if at() == red {
		t.Fatal("deleted Kitty image still shows")
	}
}
