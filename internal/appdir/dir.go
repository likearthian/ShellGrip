// Package appdir names the directory where ShellGrip keeps its state: the
// settings, the saved layout, the SSH targets, and git's askpass secret.
package appdir

import (
	"os"
	"path/filepath"
)

// Dir returns the directory of the app's state, from SHELLGRIP_DIR when it is
// set: the dev build takes a directory of its own.
func Dir() string {
	if d := os.Getenv("SHELLGRIP_DIR"); d != "" {
		return d
	}
	base, err := os.UserConfigDir()
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "ShellGrip")
}
