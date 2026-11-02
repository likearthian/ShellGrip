//go:build !windows

package main

import "os/exec"

const windowsProfiles = false

func discoverProfiles() []terminalProfile {
	profiles := []terminalProfile{{ID: "auto", Name: "Default shell"}}
	for _, shell := range []string{"bash", "zsh", "fish", "sh"} {
		if path, err := exec.LookPath(shell); err == nil {
			profiles = append(profiles, terminalProfile{ID: shell, Name: shell, Command: []string{path, "-l"}})
		}
	}
	return profiles
}
