package main

import "testing"

func TestWSLWatchTarget(t *testing.T) {
	for _, test := range []struct{ name, distro, dir string }{
		{`\\wsl.localhost\Ubuntu\home\u\project files`, "Ubuntu", "/home/u/project files"},
		{`\\wsl.localhost\Ubuntu\tmp\a '$ ; & file`, "Ubuntu", "/tmp/a '$ ; & file"},
	} {
		distro, dir, err := wslWatchTarget(test.name)
		if err != nil || distro != test.distro || dir != test.dir {
			t.Fatalf("target %q = %q %q %v", test.name, distro, dir, err)
		}
	}
	for _, name := range []string{"/home/u", `\\server\share\file`, `\\wsl.localhost\Ubuntu`} {
		if _, _, err := wslWatchTarget(name); err == nil {
			t.Fatalf("accepted %q", name)
		}
	}
}
