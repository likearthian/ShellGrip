package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/goregular"
)

func TestScanFontFamilies(t *testing.T) {
	dir := t.TempDir()
	for name, data := range map[string][]byte{
		"mono.ttf": gomono.TTF, "duplicate.otf": gomono.TTF,
		"proportional.ttf": goregular.TTF, "invalid.ttf": []byte("not a font"),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{defaultFontFamily, "Go Mono"}
	if got := scanFontFamilies([]string{dir, filepath.Join(dir, "missing")}); !reflect.DeepEqual(got, want) {
		t.Fatalf("font families = %q, want %q", got, want)
	}
}

func TestTerminalFontFallback(t *testing.T) {
	if got := terminalFontFamily(""); got != defaultFontFamily+", monospace" {
		t.Fatalf("default font = %q", got)
	}
	if got := terminalFontFamily("Missing Font"); got != "Missing Font, JetBrains Mono, monospace" {
		t.Fatalf("fallback stack = %q", got)
	}
}
