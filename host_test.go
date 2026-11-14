package main

import (
	"os"
	"testing"
)

// The memory row names sizes in MB, and in GB once they pass a thousand.
func TestMemorySize(t *testing.T) {
	const mb = 1 << 20
	for _, tc := range []struct {
		bytes uint64
		want  string
	}{
		{0, "0 MB"},
		{186 * mb, "186 MB"},
		{1536 * mb, "1.5 GB"},
	} {
		if got := memorySize(tc.bytes); got != tc.want {
			t.Errorf("memorySize(%d) = %q, want %q", tc.bytes, got, tc.want)
		}
	}
}

// This machine reports the memory the app works in, as the popover reads it.
func TestProcessMemory(t *testing.T) {
	if used := processMemory(os.Getpid()); used == 0 {
		t.Skip("the machine does not report a process's memory")
	}
}

// The host popover carries the version under the sessions, and its About
// button opens the dialog over the app's identity.
func TestHostPopoverVersionAndAbout(t *testing.T) {
	a, tt := newTestApp(t)
	version := appVersion()
	if version == "" {
		t.Fatal("the app has no version to show")
	}
	a.hostOpen = true
	tt.Frame()
	if _, ok := tt.Find("Version"); !ok {
		t.Fatalf("the popover has no Version row; texts %q", tt.Texts())
	}
	if _, ok := tt.Find(version); !ok {
		t.Fatalf("the popover lacks the version %q; texts %q", version, tt.Texts())
	}
	if _, ok := tt.Find("Memory"); !ok {
		t.Fatalf("the popover has no Memory row; texts %q", tt.Texts())
	}

	if err := tt.Click("About"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.aboutOpen || a.hostOpen {
		t.Fatalf("About did not swap the dialog for the popover: about %v host %v", a.aboutOpen, a.hostOpen)
	}
	line := "Version " + version
	if _, ok := tt.Find(line); !ok {
		t.Fatalf("the dialog lacks %q; texts %q", line, tt.Texts())
	}
	if _, ok := tt.Find(manifest.Copyright); !ok {
		t.Fatalf("the dialog lacks the copyright; texts %q", tt.Texts())
	}

	if err := tt.Click("Dismiss"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.aboutOpen {
		t.Fatal("Dismiss left the dialog open")
	}
	if _, ok := tt.Find(line); ok {
		t.Fatal("the dialog still draws after Dismiss")
	}
}
