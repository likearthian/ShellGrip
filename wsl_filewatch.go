package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

var wslRelays = struct {
	sync.Mutex
	paths map[string]string
}{paths: map[string]string{}}

func wslRelayPath(ctx context.Context, distro string) (string, error) {
	wslRelays.Lock()
	defer wslRelays.Unlock()
	if name := wslRelays.paths[distro]; name != "" {
		return name, nil
	}
	setup := exec.CommandContext(ctx, "wsl.exe", "-d", distro, "--exec", "sh", "-s")
	configureEditorCommand(setup)
	setup.Stdin = strings.NewReader("set -eu\numask 077\nuname -m\nd=\"${XDG_CACHE_HOME:-$HOME/.cache}/gorex/wslwatch\"\nmkdir -p -- \"$d\"\nprintf '%s\\n' \"$d\"\n")
	out, err := setup.Output()
	if err != nil {
		return "", fmt.Errorf("WSL relay setup: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[1], "/") {
		return "", fmt.Errorf("invalid WSL relay setup response")
	}
	arch := ""
	switch strings.TrimSpace(lines[0]) {
	case "aarch64", "arm64":
		arch = "arm64"
	case "x86_64":
		arch = "amd64"
	default:
		return "", fmt.Errorf("unsupported WSL architecture %q", lines[0])
	}
	exe, _ := os.Executable()
	var data []byte
	for _, dir := range []string{filepath.Join(filepath.Dir(exe), "wslwatch"), filepath.Join("resources", "wslwatch")} {
		data, err = os.ReadFile(filepath.Join(dir, "wslwatch-"+arch))
		if err == nil {
			break
		}
	}
	if err != nil {
		return "", fmt.Errorf("missing Linux relay; build with go run ./tools/mkwslwatch: %w", err)
	}
	digest := sha256.Sum256(data)
	linux := lines[1] + fmt.Sprintf("/wslwatch-%x", digest)
	p := &Pane{profile: terminalProfile{WSL: true, ID: "wsl:" + distro}}
	unc := fileTreeDirectory(p, linux)
	old, readErr := os.ReadFile(unc)
	if readErr != nil || sha256.Sum256(old) != digest {
		f, err := os.CreateTemp(filepath.Dir(unc), ".relay-")
		if err != nil {
			return "", err
		}
		tmp := f.Name()
		defer os.Remove(tmp)
		_, writeErr := f.Write(data)
		closeErr := f.Close()
		if writeErr != nil {
			return "", writeErr
		}
		if closeErr != nil {
			return "", closeErr
		}
		if err := os.Rename(tmp, unc); err != nil {
			return "", err
		}
	}
	chmod := exec.CommandContext(ctx, "wsl.exe", "-d", distro, "--exec", "chmod", "700", linux)
	configureEditorCommand(chmod)
	if err := chmod.Run(); err != nil {
		return "", err
	}
	wslRelays.paths[distro] = linux
	return linux, nil
}

func wslWatchTarget(name string) (distro, directory string, err error) {
	parts := strings.SplitN(strings.ReplaceAll(name, `\`, "/"), "/", 5)
	if len(parts) != 5 || parts[0] != "" || parts[1] != "" || !strings.EqualFold(parts[2], "wsl.localhost") || parts[3] == "" || strings.ContainsAny(parts[3], "\x00\r\n") {
		return "", "", fmt.Errorf("invalid WSL watch path %q", name)
	}
	return parts[3], "/" + parts[4], nil
}

// One long-lived process per watch; closing stdin ends the Linux helper too.
// Arguments are passed directly, never interpolated into a shell command.
func runWSLFileWatch(ctx context.Context, spec *localFileWatch, ready func(int), change func()) error {
	distro, directory, err := wslWatchTarget(spec.path)
	if err != nil {
		return err
	}
	mode := "tree"
	if spec.file {
		mode = "file"
	}
	installCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	helper, err := wslRelayPath(installCtx, distro)
	cancel()
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "wsl.exe", "-d", distro, "--exec", helper, directory, mode)
	configureEditorCommand(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	defer stdin.Close()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Cancel = func() error { return stdin.Close() }
	cmd.WaitDelay = 2 * time.Second
	if err := cmd.Start(); err != nil {
		return err
	}
	scanner := bufio.NewScanner(stdout)
	started := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "READY ") && !started {
			pid, err := strconv.Atoi(strings.TrimPrefix(line, "READY "))
			if err != nil || pid <= 0 {
				break
			}
			started = true
			if ready != nil {
				ready(pid)
			}
		} else if line == "CHANGE" && started {
			change()
		} else {
			break
		}
	}
	stdin.Close()
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("WSL inotify: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return fmt.Errorf("WSL inotify relay ended: %s", strings.TrimSpace(stderr.String()))
}
