package main

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

func wslDirectory(p *Pane) string {
	if p.SID == "" || len(p.profile.Command) == 0 {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	args := append([]string(nil), p.profile.Command[1:]...)
	args = append(args, "--exec", "sh", "-c", wslDirectoryScript, "shellgrip", p.SID)
	cmd := exec.CommandContext(ctx, p.profile.Command[0], args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	// A ShellGrip launched from another pane must not tag the lookup itself.
	for _, value := range os.Environ() {
		name, _, _ := strings.Cut(value, "=")
		if !strings.EqualFold(name, "SHELLGRIP_SESSION") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	dir := strings.TrimSuffix(string(out), "\n")
	if !strings.HasPrefix(dir, "/") || strings.ContainsAny(dir, "\x00\r\n") {
		return ""
	}
	return dir
}

// Every descendant inherits the session tag. The oldest matching process
// is the interactive shell, whose cwd follows cd even while a child runs.
const wslDirectoryScript = `oldest=2147483647
directory=
for environment in $(grep -z -F -x -l -- "SHELLGRIP_SESSION=$1" /proc/[0-9]*/environ 2>/dev/null); do
    process=${environment%/environ}
    pid=${process##*/}
    if [ "$pid" -lt "$oldest" ]; then
        current=$(readlink "$process/cwd" 2>/dev/null) || continue
        oldest=$pid
        directory=$current
    fi
done
[ -n "$directory" ] && printf '%s\n' "$directory"`
