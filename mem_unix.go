//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// processMemory reports the bytes of physical memory a process holds: its
// resident set, which /proc keeps on Linux and ps prints on a Mac.
func processMemory(pid int) uint64 {
	if runtime.GOOS == "darwin" {
		out, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(pid)).Output()
		if err != nil {
			return 0
		}
		kb, _ := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64)
		return kb << 10
	}
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/statm", pid))
	if err != nil {
		return 0
	}
	// The fields are pages: size, resident, shared…
	fields := strings.Fields(string(b))
	if len(fields) < 2 {
		return 0
	}
	resident, _ := strconv.ParseUint(fields[1], 10, 64)
	return resident * uint64(os.Getpagesize())
}
