//go:build !windows

package main

import (
	"os/exec"

	"github.com/egoist/mygo"
)

func configureEditorCommand(cmd *exec.Cmd) {}

func openHTMLInDefaultBrowser(fileURL string) error {
	return mygo.Shell.OpenExternal(fileURL)
}
