package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const windowsProfiles = true

func discoverProfiles() []terminalProfile {
	profiles := []terminalProfile{{ID: "auto", Name: "Default shell"}}
	for _, shell := range []struct{ exe, name string }{
		{"pwsh.exe", "PowerShell"}, {"powershell.exe", "Windows PowerShell"}, {"cmd.exe", "Command Prompt"},
	} {
		if path, err := exec.LookPath(shell.exe); err == nil {
			profiles = append(profiles, terminalProfile{ID: shell.exe, Name: shell.name, Command: []string{path}})
		}
	}
	for _, base := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs")} {
		path := filepath.Join(base, "Git", "bin", "bash.exe")
		if st, err := os.Stat(path); err == nil && !st.IsDir() {
			profiles = append(profiles, terminalProfile{ID: "git-bash", Name: "Git Bash", Command: []string{path, "--login", "-i"}})
			break
		}
	}
	wsl, err := exec.LookPath("wsl.exe")
	if err != nil {
		return profiles
	}
	// Registered distributions can be enumerated without starting WSL or
	// opening a console window. Their names are Unicode registry strings.
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Lxss`, registry.READ)
	if err != nil {
		return profiles
	}
	defer key.Close()
	names, _ := key.ReadSubKeyNames(-1)
	for _, name := range names {
		distro, err := registry.OpenKey(key, name, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		label, _, err := distro.GetStringValue("DistributionName")
		distro.Close()
		if err == nil && label != "" && !strings.HasPrefix(label, "docker-desktop") {
			profiles = append(profiles, terminalProfile{ID: "wsl:" + label, Name: label + " (WSL)", Command: []string{wsl, "--distribution", label}, WSL: true})
		}
	}
	return profiles
}
