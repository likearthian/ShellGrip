package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestCopyToast(t *testing.T) {
	a := numberedTabs(1)
	tt := ui.NewTester(a.view, 1000, 620)
	a.selectionCopied("é漢🙂")
	tt.Frame()
	if rect, ok := tt.Find("Copied 3 characters to clipboard"); !ok {
		t.Fatalf("missing Unicode copy count: %q", tt.Texts())
	} else if rect.X < 600 || rect.Y < titleH || rect.Y > 100 {
		t.Fatalf("toast is not in the top-right corner: %v", rect)
	}
	if a.copiedCharacters != 0 {
		t.Fatal("copy notification was not consumed")
	}
	a.selectionCopied("x")
	tt.Frame()
	if _, ok := tt.Find("Copied 1 character to clipboard"); !ok {
		t.Fatalf("missing singular copy count: %q", tt.Texts())
	}
	if _, ok := tt.Find("Copied 3 characters to clipboard"); ok {
		t.Fatal("repeated copies stacked instead of updating the toast")
	}
	a.selectionCopied("")
	if a.copiedCharacters != 0 {
		t.Fatal("empty selection scheduled a toast")
	}
}
