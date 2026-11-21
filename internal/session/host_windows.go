package session

import (
	"os"
	"os/user"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	kernel32                               = windows.NewLazySystemDLL("kernel32.dll")
	procGetPhysicallyInstalledSystemMemory = kernel32.NewProc("GetPhysicallyInstalledSystemMemory")
	procGetSystemPowerStatus               = kernel32.NewProc("GetSystemPowerStatus")
)

// HostInfo describes this machine.
func HostInfo() Host {
	h := Host{}
	if u, err := user.Current(); err == nil {
		h.User = u.Username[strings.LastIndexByte(u.Username, '\\')+1:] // without the domain
		h.Home = u.HomeDir
	}
	h.Name, _ = os.Hostname()
	value := func(path, name string) string {
		k, err := registry.OpenKey(registry.LOCAL_MACHINE, path, registry.QUERY_VALUE)
		if err != nil {
			return ""
		}
		defer k.Close()
		v, _, _ := k.GetStringValue(name)
		return strings.TrimSpace(v)
	}
	const version = `SOFTWARE\Microsoft\Windows NT\CurrentVersion`
	h.OS = value(version, "ProductName")
	if build, _ := strconv.Atoi(value(version, "CurrentBuildNumber")); build >= 22000 {
		// Windows 11 still names itself Windows 10 there.
		h.OS = strings.Replace(h.OS, "Windows 10", "Windows 11", 1)
	}
	if v := value(version, "DisplayVersion"); v != "" {
		h.OS += " " + v
	}
	h.Chip = value(`HARDWARE\DESCRIPTION\System\CentralProcessor\0`, "ProcessorNameString")
	h.Model = value(`HARDWARE\DESCRIPTION\System\BIOS`, "SystemProductName")
	if placeholder(h.Model) {
		h.Model = "Windows PC"
	}
	var kb uint64
	if r, _, _ := procGetPhysicallyInstalledSystemMemory.Call(uintptr(unsafe.Pointer(&kb))); r != 0 {
		h.Memory = kb << 10
	}
	// SYSTEM_POWER_STATUS: a BatteryFlag of 128 is no battery, 255 unknown.
	var power struct {
		ACLineStatus, BatteryFlag, BatteryLifePercent, SystemStatusFlag uint8
		BatteryLifeTime, BatteryFullLifeTime                            uint32
	}
	if r, _, _ := procGetSystemPowerStatus.Call(uintptr(unsafe.Pointer(&power))); r != 0 {
		h.Portable = power.BatteryFlag != 128 && power.BatteryFlag != 255
	}
	return h
}

// placeholder tells the names that boards without a model of their own
// give in its place.
func placeholder(model string) bool {
	switch strings.ToLower(model) {
	case "", "system product name", "to be filled by o.e.m.", "default string", "not applicable", "none", "o.e.m.":
		return true
	}
	return false
}
