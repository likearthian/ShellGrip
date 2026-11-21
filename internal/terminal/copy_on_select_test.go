package terminal

import (
	"io"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

type selectionTestConn struct {
	closed  chan struct{}
	once    sync.Once
	mu      sync.Mutex
	written []byte
}

func (c *selectionTestConn) Read([]byte) (int, error) { <-c.closed; return 0, io.EOF }
func (c *selectionTestConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	c.written = append(c.written, p...)
	c.mu.Unlock()
	return len(p), nil
}
func (c *selectionTestConn) Close() error { c.once.Do(func() { close(c.closed) }); return nil }

func selectionTerminal(t *testing.T, enabled bool) (*Terminal, *ui.Tester) {
	t.Helper()
	copyOnSelect := CopyOnSelectNone
	if enabled {
		copyOnSelect = CopyOnSelectClipboard
	}
	term, err := New(Options{Conn: &selectionTestConn{closed: make(chan struct{})}, CopyOnSelect: copyOnSelect})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { term.Close() })
	term.Feed([]byte("hello world\r\nsecond line"))
	tt := ui.NewTester(func(c *ui.Context) { View(c, term).Fill().AutoFocus() }, 400, 200)
	return term, tt
}

func selectionCell(term *Terminal, col, row int) (float32, float32) {
	v := term.v
	return (float32(v.ox) + (float32(col)+0.5)*float32(v.cellW)) / v.scale,
		(float32(v.oy) + (float32(row)+0.5)*float32(v.cellH)) / v.scale
}

func dragHello(term *Terminal, tt *ui.Tester) {
	x0, y0 := selectionCell(term, 0, 0)
	x1, y1 := selectionCell(term, 4, 0)
	tt.Press(x0-2, y0)
	tt.Move(x1, y1)
	tt.Move(x1+2, y1)
	tt.Release(x1+2, y1)
}

func TestCopyOnSelect(t *testing.T) {
	term, tt := selectionTerminal(t, true)
	tt.SetClipboard("previous clipboard")
	x0, y0 := selectionCell(term, 0, 0)
	x1, y1 := selectionCell(term, 4, 0)
	tt.Press(x0-2, y0)
	tt.Move(x1+2, y1)
	if tt.Clipboard() != "previous clipboard" {
		t.Fatal("copied before the selection finished")
	}
	tt.Release(x1+2, y1)
	if got := tt.Clipboard(); got != "hello" {
		t.Fatalf("drag copied %q", got)
	}
	x, y := selectionCell(term, 8, 0)
	for range 2 {
		tt.Press(x, y)
		tt.Release(x, y)
	}
	if got := tt.Clipboard(); got != "world" {
		t.Fatalf("word selection copied %q", got)
	}
	x, y = selectionCell(term, 2, 1)
	for range 3 {
		tt.Press(x, y)
		tt.Release(x, y)
	}
	if got := tt.Clipboard(); got != "second line" {
		t.Fatalf("line selection copied %q", got)
	}
	tt.SetClipboard("keep me")
	x, y = selectionCell(term, 0, 4)
	tt.Press(x, y)
	tt.Release(x, y)
	if got := tt.Clipboard(); got != "keep me" {
		t.Fatalf("empty click replaced clipboard with %q", got)
	}
}

func TestCopyOnSelectToggle(t *testing.T) {
	term, tt := selectionTerminal(t, false)
	tt.SetClipboard("keep me")
	dragHello(term, tt)
	if tt.Clipboard() != "keep me" {
		t.Fatal("disabled copy on select changed the clipboard")
	}
	mods := ui.Ctrl | ui.Shift
	if runtime.GOOS == "darwin" {
		mods = ui.Super
	}
	tt.Key(mods, ui.KeyC)
	if tt.Clipboard() != "hello" {
		t.Fatal("manual copy stopped working")
	}
	term.SetCopyOnSelect(CopyOnSelectClipboard)
	tt.SetClipboard("keep me")
	dragHello(term, tt)
	if tt.Clipboard() != "hello" {
		t.Fatal("enabling copy on select did not update the existing terminal")
	}
}

func TestCopySelectionNotification(t *testing.T) {
	term, tt := selectionTerminal(t, true)
	var notifications []string
	term.opts.OnCopy = func(text string, source CopySource) {
		if tt.Clipboard() != text {
			t.Fatal("notification ran before the clipboard was updated")
		}
		notifications = append(notifications, text)
	}
	dragHello(term, tt)
	if len(notifications) != 1 || notifications[0] != "hello" {
		t.Fatalf("selection-copy notifications = %q", notifications)
	}
	tt.Command("copy")
	if len(notifications) != 2 || notifications[1] != "hello" {
		t.Fatalf("manual-copy notification missing: %q", notifications)
	}
	x, y := selectionCell(term, 0, 4)
	tt.Press(x, y)
	tt.Release(x, y)
	if len(notifications) != 2 {
		t.Fatal("empty selection notified a copy")
	}
}

func TestCtrlVPaste(t *testing.T) {
	modifiers := []ui.Modifiers{ui.Ctrl, ui.Ctrl | ui.Shift}
	if runtime.GOOS == "darwin" {
		modifiers = []ui.Modifiers{ui.Ctrl, ui.Super}
	}
	for _, mods := range modifiers {
		for _, bracketed := range []bool{false, true} {
			term, tt := selectionTerminal(t, false)
			if bracketed {
				term.Feed([]byte("\x1b[?2004h"))
				tt.Frame()
			}
			tt.SetClipboard("pasted é漢🙂")
			tt.Key(mods, ui.KeyV)
			want := "pasted é漢🙂"
			if bracketed {
				want = "\x1b[200~" + want + "\x1b[201~"
			}
			conn := term.opts.Conn.(*selectionTestConn)
			deadline := time.Now().Add(time.Second)
			for {
				conn.mu.Lock()
				got := string(conn.written)
				conn.mu.Unlock()
				if got == want {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("paste with modifiers %v (bracketed=%v) wrote %q, want %q", mods, bracketed, got, want)
				}
				time.Sleep(time.Millisecond)
			}
		}
	}
}

func TestMouseTrackingDoesNotCopy(t *testing.T) {
	term, tt := selectionTerminal(t, true)
	term.Feed([]byte("\x1b[?1000h"))
	tt.Frame()
	tt.SetClipboard("keep me")
	dragHello(term, tt)
	if tt.Clipboard() != "keep me" {
		t.Fatal("application mouse events copied terminal text")
	}
	// Tester.Press sends pointer events with no modifiers. Send the real
	// Shift-modified events directly to exercise the selection override.
	x0, y0 := selectionCell(term, 0, 0)
	x1, y1 := selectionCell(term, 4, 0)
	for _, event := range []ui.InputEvent{
		{Kind: ui.InputPointerDown, X: x0 - 2, Y: y0, Mods: ui.Shift},
		{Kind: ui.InputPointerMove, X: x1 + 2, Y: y1, Mods: ui.Shift},
		{Kind: ui.InputPointerUp, X: x1 + 2, Y: y1, Mods: ui.Shift},
	} {
		term.v.input(event)
	}
	if tt.Clipboard() != "hello" {
		t.Fatalf("Shift selection copied %q while mouse tracking was enabled", tt.Clipboard())
	}
}

func TestCtrlCReachesProgramWithSelection(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("Control+C is only overridden by upstream on Windows")
	}
	term, tt := selectionTerminal(t, false)
	dragHello(term, tt)
	tt.SetClipboard("keep me")
	tt.Key(ui.Ctrl, ui.KeyC)
	conn := term.opts.Conn.(*selectionTestConn)
	deadline := time.Now().Add(time.Second)
	for {
		conn.mu.Lock()
		got := string(conn.written)
		conn.mu.Unlock()
		if got == "\x03" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Ctrl+C with a selection wrote %q, want interrupt", got)
		}
		time.Sleep(time.Millisecond)
	}
	if tt.Clipboard() != "keep me" {
		t.Fatal("Ctrl+C copied instead of interrupting the program")
	}
}

func TestGoRexTerminalKeybinds(t *testing.T) {
	for _, platform := range []string{"darwin", "windows", "linux"} {
		t.Run(platform, func(t *testing.T) {
			term := &Terminal{binds: defaultKeybinds(platform)}
			for _, b := range term.binds {
				if b.Mods&(ui.Ctrl|ui.Alt) == ui.Ctrl|ui.Alt {
					t.Fatalf("binding consumes AltGr: %+v", b)
				}
				if platform != "darwin" && b.Mods == ui.Ctrl && (b.Key == ui.KeyC || b.Key == ui.Key0) {
					t.Fatalf("binding consumes interrupt or tab 10: %+v", b)
				}
			}
			b, ok := term.binding(ui.Ctrl, ui.KeyV)
			if !ok || b.Action != ActionPaste || b.Performable {
				t.Fatalf("Ctrl+V must always paste: %+v, found %v", b, ok)
			}
			if platform != "darwin" {
				b, ok := term.shortcut(ActionPaste)
				if !ok || b.Mods != ui.Ctrl || b.Key != ui.KeyV {
					t.Fatalf("paste menu shortcut = %+v, found %v", b, ok)
				}
			}
		})
	}
}
