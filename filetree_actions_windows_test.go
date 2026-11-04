package main

import (
	"os"
	"testing"
)

func TestDefaultBrowserExecutable(t *testing.T) {
	exe, err := defaultBrowserExecutable()
	if err != nil {
		t.Skipf("no browser configured in this Windows environment: %v", err)
	}
	if info, err := os.Stat(exe); err != nil || info.IsDir() {
		t.Fatalf("default browser executable %q is unavailable: %v", exe, err)
	}
}
