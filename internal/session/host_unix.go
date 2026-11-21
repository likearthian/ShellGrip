//go:build darwin || linux

package session

import (
	"encoding/json"
	"os"
	"os/exec"
	"os/user"
	"runtime"
	"strconv"
	"strings"
)

// HostInfo describes this machine; it may take a second.
func HostInfo() Host {
	h := Host{}
	if u, err := user.Current(); err == nil {
		h.User = u.Username
		h.Home = u.HomeDir
	}
	out := func(name string, args ...string) string {
		b, err := exec.Command(name, args...).Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(b))
	}
	if runtime.GOOS == "darwin" {
		h.Name = out("scutil", "--get", "ComputerName")
		h.OS = "macOS " + out("sw_vers", "-productVersion")
		h.Chip = sysctlString("machdep.cpu.brand_string")
		h.Memory = sysctlUint64("hw.memsize")
		var hw struct {
			Data []struct {
				MachineName string `json:"machine_name"`
				ChipType    string `json:"chip_type"`
			} `json:"SPHardwareDataType"`
		}
		if json.Unmarshal([]byte(out("system_profiler", "SPHardwareDataType", "-json", "-detailLevel", "mini")), &hw) == nil && len(hw.Data) > 0 {
			h.Model = hw.Data[0].MachineName
			if hw.Data[0].ChipType != "" {
				h.Chip = hw.Data[0].ChipType
			}
		}
		if h.Model == "" {
			h.Model = sysctlString("hw.model")
		}
	} else {
		h.Name, _ = os.Hostname()
		h.OS = runtime.GOOS
		if b, err := os.ReadFile("/etc/os-release"); err == nil {
			for _, l := range strings.Split(string(b), "\n") {
				if v, ok := strings.CutPrefix(l, "PRETTY_NAME="); ok {
					h.OS, _ = strconv.Unquote(v)
				}
			}
		}
		h.Model = "Linux"
	}
	if h.Name == "" {
		h.Name, _ = os.Hostname()
		h.Name = strings.TrimSuffix(h.Name, ".local")
	}
	return h
}
