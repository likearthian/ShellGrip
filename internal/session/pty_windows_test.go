//go:build windows

package session

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// detached starts the test's child process without a console, as a terminal
// app's own process group is, so that the Ctrl-C typed in a pseudo console
// is what interrupts the program in it.
func detached() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP}
}

func TestExplicitPowerShellProfile(t *testing.T) {
	for _, shell := range []string{"pwsh.exe", "powershell.exe"} {
		t.Run(shell, func(t *testing.T) {
			path, err := exec.LookPath(shell)
			if err != nil {
				t.Skip("shell is not installed")
			}
			_, argv, _, err := shellCommand([]string{path})
			if err != nil || len(argv) != 4 || argv[3] != powershellPrompt {
				t.Fatalf("profile did not install directory tracking: %q, %v", argv, err)
			}
			_, argv, _, err = shellCommand([]string{path, "-NoProfile"})
			if err != nil || len(argv) != 2 || argv[1] != "-NoProfile" {
				t.Fatalf("explicit arguments were changed: %q, %v", argv, err)
			}
		})
	}
}

// TestCtrlC types Ctrl-C to a program in a pseudo console started by a
// process that a console of its own cannot interrupt, as a terminal app
// runs without one: the program must still take the interrupt.
func TestCtrlC(t *testing.T) {
	switch os.Getenv("SHELLGRIP_TEST_CTRLC") {
	case "console":
		ctrlCConsole(t)
		return
	case "program":
		interrupted := make(chan os.Signal, 1)
		signal.Notify(interrupted, os.Interrupt)
		fmt.Println("ready")
		select {
		case <-interrupted:
			fmt.Println("interrupted")
		case <-time.After(20 * time.Second):
		}
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestCtrlC$", "-test.v")
	cmd.Env = append(os.Environ(), "SHELLGRIP_TEST_CTRLC=console")
	cmd.SysProcAttr = detached()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

func ctrlCConsole(t *testing.T) {
	os.Setenv("SHELLGRIP_TEST_CTRLC", "program")
	p, err := startPTY(os.Args[0], []string{os.Args[0], "-test.run=^TestCtrlC$"}, os.TempDir(), os.Environ(), 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer p.close()
	chunks := make(chan string)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := p.read(buf)
			if n > 0 {
				chunks <- string(buf[:n])
			}
			if err != nil {
				close(chunks)
				return
			}
		}
	}()
	var out strings.Builder
	waitFor := func(s string) bool {
		timeout := time.After(10 * time.Second)
		for !strings.Contains(out.String(), s) {
			select {
			case c, ok := <-chunks:
				if !ok {
					return false
				}
				out.WriteString(c)
			case <-timeout:
				return false
			}
		}
		return true
	}
	if !waitFor("ready") {
		t.Fatalf("the program did not start: %q", out.String())
	}
	p.write([]byte{3}) // Ctrl-C
	if !waitFor("interrupted") {
		t.Fatalf("Ctrl-C did not interrupt the program: %q", out.String())
	}
}
