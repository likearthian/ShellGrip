package main

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"
)

func benchmarkDiffPreview(rows int) (*ui.Tester, *filePreview) {
	registerFonts()
	a := &App{}
	p := &filePreview{subtitle: "Changes"}
	for i := range rows {
		left := diffCell{number: i + 1, text: fmt.Sprintf("    configuration.Value%d = original.Value%d", i, i), present: true, kind: '-', changeStart: 28, changeEnd: 36}
		right := left
		right.kind = '+'
		p.content.diffRows = append(p.content.diffRows, diffRow{left: left, right: right})
	}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Children(func() { a.sideBySideDiff(c, colorsOf(c), p, c.Theme().Dark) })
	}, 1000, 620)
	tt.Frame()
	return tt, p
}

func BenchmarkDiffPreviewScroll(b *testing.B) {
	for _, rows := range []int{100, 1000, 2000, 10000} {
		b.Run(fmt.Sprint(rows), func(b *testing.B) {
			tt, p := benchmarkDiffPreview(rows)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				delta := float32(63)
				if i/100%2 == 1 {
					delta = -delta
				}
				p.diffScroll[0].Y += delta
				tt.Frame()
			}
		})
	}
}
