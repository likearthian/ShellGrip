package terminal

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"
)

// the viewport moves in terminal rows. Animate the wheel's distance across
// frames, retaining the grid's alignment and the program's mouse protocol.
var wheelAnimation = ui.Animation{Duration: 150 * time.Millisecond, Ease: ui.EaseOut}

// queueWheel adds a notch; reversing abandons travel still ahead. t.mu held.
func (v *view) queueWheel(rows int) {
	s := v.t.term.Scrollbar()
	at := int(s.Offset)
	if !v.wheelActive || at != v.wheelExpected || (rows > 0) != (v.wheelTarget > float32(at)) {
		v.wheelTarget = float32(at)
		v.wheelReset = true
	}
	end := max(0, int(s.Total)-int(s.Len))
	v.wheelTarget = max(0, min(float32(end), v.wheelTarget+float32(rows)))
	v.wheelExpected = at
	v.wheelActive = v.wheelTarget != float32(at)
	v.services.Invalidate()
}

// advanceWheel runs on the UI clock. Output, a scrollbar drag, a key or
// a program taking the wheel stops an old glide rather than fighting it.
// t.mu held.
func (v *view) advanceWheel(c *ui.Context, e ui.Element) {
	v.wheelSmooth = !c.Preferences().ReduceMotion && !c.Instant()
	at := int(v.t.term.Scrollbar().Offset)
	if !v.wheelActive || at != v.wheelExpected || v.wheelTracked(c.Modifiers()) || v.wheelArrows() {
		v.wheelActive, v.wheelReset = false, false
		v.wheelTarget = float32(at)
		e.Jump("terminal-wheel", float32(at))
		return
	}
	if v.wheelReset {
		e.Jump("terminal-wheel", float32(at))
		v.wheelReset = false
	}
	position := e.AnimateTo("terminal-wheel", v.wheelTarget, wheelAnimation)
	v.t.term.ScrollBy(int(math.Round(float64(position))) - at)
	v.wheelExpected = int(v.t.term.Scrollbar().Offset)
	if v.wheelExpected == int(v.wheelTarget) {
		v.wheelActive = false
		e.Jump("terminal-wheel", v.wheelTarget)
	}
}
