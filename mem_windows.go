package main

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var procGetProcessMemoryInfo = windows.NewLazySystemDLL("kernel32.dll").NewProc("K32GetProcessMemoryInfo")

// processMemory reports the bytes of physical memory a process works in,
// its working set, as Task Manager counts it.
func processMemory(pid int) uint64 {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_INFORMATION|windows.PROCESS_VM_READ, false, uint32(pid))
	if err != nil {
		return 0
	}
	defer windows.CloseHandle(h)
	// PROCESS_MEMORY_COUNTERS: two DWORDs, then SIZE_Ts.
	var c struct {
		Size, PageFaultCount                               uint32
		PeakWorkingSetSize, WorkingSetSize                 uintptr
		QuotaPeakPagedPoolUsage, QuotaPagedPoolUsage       uintptr
		QuotaPeakNonPagedPoolUsage, QuotaNonPagedPoolUsage uintptr
		PagefileUsage, PeakPagefileUsage                   uintptr
	}
	c.Size = uint32(unsafe.Sizeof(c))
	if r, _, _ := procGetProcessMemoryInfo.Call(uintptr(h), uintptr(unsafe.Pointer(&c)), uintptr(c.Size)); r == 0 {
		return 0
	}
	return uint64(c.WorkingSetSize)
}
