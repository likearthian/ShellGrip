package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func benchmarkDiffPreview(rows int) (*ui.Tester, *filePreview) {
	registerFonts()
	a := &App{}
	p := &filePreview{subtitle: "Changes"}
	var patch strings.Builder
	fmt.Fprintf(&patch, "diff --git a/config.go b/config.go\n--- a/config.go\n+++ b/config.go\n@@ -1,%d +1,%d @@\n", rows, rows)
	for i := range rows {
		fmt.Fprintf(&patch, "-    configuration.Value%d = original.Value%d\n", i, i)
	}
	for i := range rows {
		fmt.Fprintf(&patch, "+    configuration.Value%d = modified.Value%d\n", i, i)
	}
	p.content = diffFixture(patch.String())
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
				delta := float64(63)
				if i/100%2 == 1 {
					delta = -delta
				}
				p.diffDisplay.SetScrollY(p.diffDisplay.ScrollY() + delta)
				tt.Frame()
			}
		})
	}
}
