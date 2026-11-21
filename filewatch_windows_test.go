//go:build windows

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// opt in because this starts a distribution and measures real filesystem I/O.
func TestWSLFileWatchPerformance(t *testing.T) {
	distro := os.Getenv("GOREX_TEST_WSL_WATCH")
	if distro == "" {
		t.Skip("set GOREX_TEST_WSL_WATCH to the distribution to measure")
	}
	run := func(script string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "wsl.exe", "-d", distro, "--", "sh", "-s")
		cmd.Stdin = strings.NewReader(script)
		out, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	root, err := run("mktemp -d /tmp/gorex-watch-XXXXXXXX")
	if err != nil {
		t.Skipf("WSL unavailable: %v: %s", err, root)
	}
	if !strings.HasPrefix(root, "/tmp/gorex-watch-") || strings.ContainsAny(root, "'\n\r") {
		t.Fatalf("unexpected fixture path %q", root)
	}
	t.Cleanup(func() { run("rm -f -- '" + root + "/probe.txt' '" + root + "/save.tmp'; rmdir -- '" + root + "'") })
	p := &Pane{profile: terminalProfile{ID: "wsl:" + distro, WSL: true}}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	events := make(chan time.Time, 1024)
	ready := make(chan error, 1)
	done := make(chan error, 1)
	helperPID := 0
	go func() {
		err := runWSLFileWatch(ctx, &localFileWatch{path: fileWatchPath(p, root), wsl: true}, func(pid int) { helperPID = pid; ready <- nil }, func() {
			select {
			case events <- time.Now():
			case <-ctx.Done():
				return
			}
		})
		select {
		case ready <- err:
		default:
		}
		done <- err
	}()
	start := time.Now()
	if err := <-ready; err != nil {
		t.Fatalf("Linux WSL watcher: %v", err)
	}
	t.Logf("startup=%s", time.Since(start))
	linuxTicks := func() int64 {
		out, err := run(fmt.Sprintf("cat /proc/%d/stat", helperPID))
		if err != nil {
			t.Fatalf("helper CPU: %v: %s", err, out)
		}
		fields := strings.Fields(out)
		if len(fields) < 15 {
			t.Fatalf("invalid helper stat: %q", out)
		}
		u, err := strconv.ParseInt(fields[13], 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		s, err := strconv.ParseInt(fields[14], 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		return u + s
	}
	ticksBefore := linuxTicks()
	cpu := func() int64 {
		var created, exited, kernel, user windows.Filetime
		if err := windows.GetProcessTimes(windows.CurrentProcess(), &created, &exited, &kernel, &user); err != nil {
			t.Fatal(err)
		}
		return kernel.Nanoseconds() + user.Nanoseconds()
	}
	before := cpu()
	idle := time.Now()
	timer := time.NewTimer(3 * time.Second)
	select {
	case <-events:
		t.Fatal("idle watch generated an event")
	case <-timer.C:
	}
	t.Logf("idle process CPU=%s over %s", time.Duration(cpu()-before), time.Since(idle))
	t.Logf("idle Linux helper CPU=%d scheduler ticks", linuxTicks()-ticksBefore)
	for i := 0; i < 5; i++ {
		start := time.Now()
		if out, err := run(fmt.Sprintf("printf 'sample %d' > '%s/save.tmp'; mv '%s/save.tmp' '%s/probe.txt'", i, root, root, root)); err != nil {
			t.Fatalf("write: %v: %s", err, out)
		}
		select {
		case at := <-events:
			t.Logf("replacement sample %d write-command-to-notification=%s", i, at.Sub(start))
		case <-time.After(3 * time.Second):
			t.Fatal("Linux save did not notify the Windows watcher")
		}
	}
	start = time.Now()
	before = cpu()
	if out, err := run("i=0; while [ $i -lt 200 ]; do printf '%s' \"$i\" > '" + root + "/probe.txt'; i=$((i+1)); done"); err != nil {
		t.Fatalf("burst: %v: %s", err, out)
	}
	count := 0
	quiet := time.NewTimer(time.Second)
	defer quiet.Stop()
	for {
		select {
		case <-events:
			count++
		case <-quiet.C:
			if count == 0 {
				t.Fatal("burst did not notify")
			}
			t.Logf("200-write burst: batches=%d elapsed=%s process-CPU=%s", count, time.Since(start), time.Duration(cpu()-before))
			cancel()
			select {
			case <-done:
			case <-time.After(4 * time.Second):
				t.Fatal("watcher did not stop")
			}
			if out, err := run(fmt.Sprintf("test ! -d /proc/%d", helperPID)); err != nil {
				t.Fatalf("Linux helper survived cancellation: %v %s", err, out)
			}
			t.Log("Linux helper exited after cancellation")
			return
		}
	}
}

func TestWSLFileWatchRecursiveAndFile(t *testing.T) {
	distro := os.Getenv("GOREX_TEST_WSL_WATCH")
	if distro == "" {
		t.Skip("set GOREX_TEST_WSL_WATCH to run Linux integration checks")
	}
	run := func(script string) string {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "wsl.exe", "-d", distro, "--exec", "sh", "-s")
		cmd.Stdin = strings.NewReader(script)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("fixture: %v: %s", err, out)
		}
		return strings.TrimSpace(string(out))
	}
	base := run("mktemp -d /tmp/gorex-watch-XXXXXXXX")
	if !strings.HasPrefix(base, "/tmp/gorex-watch-") || strings.ContainsAny(base, "'\r\n") {
		t.Fatal("invalid fixture path")
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	root := base + "/project ' $ ; & files"
	run("mkdir -- " + quote(root))
	t.Cleanup(func() {
		run("rm -f -- " + quote(root+"/nested/a.txt") + " " + quote(root+"/nested/b.txt") + " " + quote(root+"/nested/save.tmp") + " " + quote(root+"/node_modules/ignored.txt") + "; rmdir -- " + quote(root+"/node_modules") + " " + quote(root+"/nested") + " " + quote(root) + " " + quote(base))
	})
	p := &Pane{profile: terminalProfile{ID: "wsl:" + distro, WSL: true}}
	start := func(single bool) (chan struct{}, func()) {
		ctx, cancel := context.WithCancel(context.Background())
		ready := make(chan struct{}, 1)
		events := make(chan struct{}, 32)
		done := make(chan error, 1)
		name := root
		if single {
			name += "/nested/a.txt"
		}
		go func() {
			done <- runWSLFileWatch(ctx, &localFileWatch{path: fileWatchPath(p, name), file: single, wsl: true}, func(int) { ready <- struct{}{} }, func() {
				select {
				case events <- struct{}{}:
				case <-ctx.Done():
				}
			})
		}()
		select {
		case <-ready:
		case err := <-done:
			cancel()
			t.Fatalf("start: %v", err)
		case <-time.After(5 * time.Second):
			cancel()
			t.Fatal("start timeout")
		}
		stop := func() {
			cancel()
			select {
			case <-done:
			case <-time.After(4 * time.Second):
				t.Error("relay did not stop")
			}
		}
		return events, stop
	}
	waitChange := func(events chan struct{}) {
		select {
		case <-events:
		case <-time.After(3 * time.Second):
			t.Fatal("no change event")
		}
	}
	noChange := func(events chan struct{}) {
		select {
		case <-events:
			t.Fatal("unrelated file generated an event")
		case <-time.After(350 * time.Millisecond):
		}
	}
	func() {
		events, stop := start(false)
		defer stop()
		run("mkdir -- " + quote(root+"/nested") + "; printf first > " + quote(root+"/nested/a.txt"))
		waitChange(events)
		run("printf second > " + quote(root+"/nested/a.txt"))
		waitChange(events)
		run("mkdir -- " + quote(root+"/node_modules") + "; printf ignored > " + quote(root+"/node_modules/ignored.txt"))
		noChange(events)
	}()
	func() {
		events, stop := start(true)
		defer stop()
		run("printf unrelated > " + quote(root+"/nested/b.txt"))
		noChange(events)
		run("printf replacement > " + quote(root+"/nested/save.tmp") + "; mv -- " + quote(root+"/nested/save.tmp") + " " + quote(root+"/nested/a.txt"))
		waitChange(events)
	}()
}
