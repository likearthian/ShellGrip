package main

import (
	"fmt"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var queryAssociation = windows.NewLazySystemDLL("shlwapi.dll").NewProc("AssocQueryStringW")

func defaultBrowserExecutable() (string, error) {
	// Query the user's HTTPS handler, independently of .html file associations.
	const (
		assocIsProtocol = 0x1000
		assocExecutable = 2
	)
	protocol, _ := windows.UTF16PtrFromString("https")
	verb, _ := windows.UTF16PtrFromString("open")
	var size uint32
	queryAssociation.Call(assocIsProtocol, assocExecutable, uintptr(unsafe.Pointer(protocol)), uintptr(unsafe.Pointer(verb)), 0, uintptr(unsafe.Pointer(&size)))
	if size == 0 || size > 32768 {
		return "", fmt.Errorf("no default web browser is configured")
	}
	buffer := make([]uint16, size)
	result, _, _ := queryAssociation.Call(assocIsProtocol, assocExecutable, uintptr(unsafe.Pointer(protocol)), uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&size)))
	if uint32(result) != 0 {
		return "", fmt.Errorf("resolve default browser: Windows error 0x%08x", uint32(result))
	}
	exe := windows.UTF16ToString(buffer)
	if exe == "" {
		return "", fmt.Errorf("no default web browser is configured")
	}
	return exe, nil
}

func openHTMLInDefaultBrowser(fileURL string) error {
	exe, err := defaultBrowserExecutable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, fileURL)
	configureEditorCommand(cmd)
	return cmd.Run()
}

func configureEditorCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
}
