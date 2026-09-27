// Package compat measures what a phone's kernel, Android and Termux do,
// and picks fixes for it from a table of rules (DESIGN.md section 13).
package compat

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/AndronixApp/andronix-distros/internal/sys"
)

// ProbeVersion changes whenever the probe's logic or fields change
// (3: LineageOS detection), so a cached probe from an older andronix is
// measured again.
const ProbeVersion = 3

// Probe is what was measured on this phone.
type Probe struct {
	Version  int               `json:"probe_version"`
	Time     string            `json:"time"`
	Kernel   string            `json:"kernel"`
	SDK      int               `json:"sdk"`
	ROM      string            `json:"rom"`     // family only: lineage, miui, oneui, stock, other
	Phantom  string            `json:"phantom"` // on, off, or unknown
	RAMMB    int64             `json:"ram_mb"`
	FreeMB   int64             `json:"free_mb"`
	Termux   string            `json:"termux"` // f_droid, github, google_play_store, or ""
	TermuxV  string            `json:"termux_version"`
	Syscalls map[string]string `json:"syscalls,omitempty"` // name -> ok, ENOSYS, EPERM, EUNATCH, SIGSYS, ...
}

// Key is the cache key: a new kernel, Android, Termux or probe measures again.
func (p *Probe) Key() string {
	return strings.Join([]string{p.Kernel, strconv.Itoa(p.SDK), p.Termux, p.TermuxV, strconv.Itoa(p.Version)}, "|")
}

// Host measures what can be read without proot.
func Host() *Probe {
	p := &Probe{Version: ProbeVersion, Time: time.Now().UTC().Format(time.RFC3339), Kernel: sys.KernelRelease(),
		RAMMB: sys.TotalRAMMB(), FreeMB: sys.FreeMB(sys.Home())}
	p.SDK, _ = strconv.Atoi(getprop("ro.build.version.sdk"))
	p.ROM = romFamily()
	p.Phantom = phantom(p.SDK)
	p.Termux = strings.ToLower(os.Getenv("TERMUX_APK_RELEASE"))
	if p.Termux == "" && strings.HasPrefix(os.Getenv("TERMUX_VERSION"), "googleplay") {
		p.Termux = "google_play_store"
	}
	p.TermuxV = os.Getenv("TERMUX_VERSION")
	return p
}

func getprop(k string) string {
	out, err := sys.Command("getprop", k).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// romFamily is a coarse ROM name from getprop, never the fingerprint.
func romFamily() string {
	switch {
	case getprop("ro.lineage.version") != "" || getprop("ro.lineage.build.version") != "" || getprop("ro.modversion") != "" ||
		getprop("ro.cm.version") != "" || isLineageBuild(getprop("ro.build.flavor"), getprop("ro.build.display.id")):
		// Apps can't always read ro.lineage.* (a Redmi Note 7 Pro on
		// LineageOS showed "stock"); the build flavor and display id say it too.
		return "lineage"
	case getprop("ro.miui.ui.version.name") != "" || getprop("ro.mi.os.version.name") != "":
		return "miui"
	case getprop("ro.build.version.oneui") != "":
		return "oneui"
	case getprop("ro.build.version.sdk") == "":
		return "" // not Android
	case strings.Contains(strings.ToLower(getprop("ro.build.flavor")), "sdk_gphone"):
		return "emulator"
	}
	return "stock"
}

// isLineageBuild: "lineage_violet-userdebug", "lineage_violet-bp1a.250505.005-...".
func isLineageBuild(vals ...string) bool {
	for _, v := range vals {
		if strings.HasPrefix(strings.ToLower(v), "lineage") {
			return true
		}
	}
	return false
}

// phantom is Android 12+'s phantom-process killer: the setting when Termux
// may read it, else what the Android version implies.
func phantom(sdk int) string {
	if out, err := sys.Command("/system/bin/settings", "get", "global", "settings_enable_monitor_phantom_procs").Output(); err == nil {
		switch strings.TrimSpace(string(out)) {
		case "false", "0":
			return "off"
		case "true", "1":
			return "on"
		}
	}
	switch {
	case sdk >= 31:
		return "on"
	case sdk > 0:
		return "off"
	}
	return "unknown"
}

// Cached reads ~/.andronix/probe.json if it matches this phone now.
func Cached(home string, now *Probe) *Probe {
	b, err := os.ReadFile(filepath.Join(home, "probe.json"))
	if err != nil {
		return nil
	}
	var p Probe
	if json.Unmarshal(b, &p) != nil || p.Key() != now.Key() || p.Syscalls == nil {
		return nil
	}
	return &p
}

// Save writes the probe to ~/.andronix/probe.json.
func (p *Probe) Save(home string) error {
	b, _ := json.MarshalIndent(p, "", "  ")
	os.MkdirAll(home, 0o755)
	return os.WriteFile(filepath.Join(home, "probe.json"), b, 0o644)
}

// Summary is the probe as a compact string for telemetry and logs, e.g.
// "openat2:ENOSYS,clone3:SIGSYS" (only what isn't ok; "all-ok" otherwise).
func (p *Probe) Summary() string {
	var bad []string
	for _, n := range Syscalls {
		if r, ok := p.Syscalls[n]; ok && r != "ok" {
			bad = append(bad, n+":"+r)
		}
	}
	if len(bad) == 0 {
		if len(p.Syscalls) == 0 {
			return "not-probed"
		}
		return "all-ok"
	}
	return strings.Join(bad, ",")
}

// KernelMajorMinor is "4.14" from "4.14.356-perf+".
func (p *Probe) KernelMajorMinor() string {
	a, b := kernelVersion(p.Kernel)
	return strconv.Itoa(a) + "." + strconv.Itoa(b)
}

func kernelVersion(s string) (int, int) {
	f := strings.FieldsFunc(s, func(r rune) bool { return r < '0' || r > '9' })
	var a, b int
	if len(f) > 0 {
		a, _ = strconv.Atoi(f[0])
	}
	if len(f) > 1 {
		b, _ = strconv.Atoi(f[1])
	}
	return a, b
}

// RAMBucket is the RAM rounded for telemetry: "<3G", "3-4G", "4-6G", "6G+".
func (p *Probe) RAMBucket() string {
	switch g := p.RAMMB; {
	case g <= 0:
		return "unknown"
	case g < 3000:
		return "<3G"
	case g < 4000:
		return "3-4G"
	case g < 6000:
		return "4-6G"
	}
	return "6G+"
}
