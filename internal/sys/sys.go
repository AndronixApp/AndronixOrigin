// Package sys covers the host side: CPU detection, Termux paths, Android
// quirks (DNS, time zone) and free space.
package sys

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// Arch is the Andronix CPU name: aarch64, arm, i686 or x86_64.
type Arch string

// OCI returns the registry platform (architecture, variant).
func (a Arch) OCI() (string, string) {
	switch a {
	case "aarch64":
		return "arm64", ""
	case "arm":
		return "arm", "v7"
	case "i686":
		return "386", ""
	case "x86_64":
		return "amd64", ""
	}
	return "", ""
}

// Label is a friendly CPU name for screens.
func (a Arch) Label() string {
	switch a {
	case "aarch64":
		return "ARM 64-bit"
	case "arm":
		return "ARM 32-bit"
	case "i686":
		return "x86 32-bit"
	case "x86_64":
		return "x86 64-bit"
	}
	return string(a)
}

// DetectArch uses the architecture this binary was built for: we ship one
// binary per CPU, so that is also the userland Termux runs (a 64-bit phone
// can run 32-bit Termux). ANDRONIX_ARCH overrides it.
func DetectArch() Arch {
	if a := os.Getenv("ANDRONIX_ARCH"); a != "" {
		return normalise(a)
	}
	switch runtime.GOARCH {
	case "arm64":
		return "aarch64"
	case "arm":
		return "arm"
	case "386":
		return "i686"
	case "amd64":
		return "x86_64"
	}
	return Arch(runtime.GOARCH)
}

func normalise(a string) Arch {
	switch strings.ToLower(a) {
	case "aarch64", "arm64":
		return "aarch64"
	case "arm", "armhf", "armv7l", "armv8l":
		return "arm"
	case "i686", "i386", "386", "x86":
		return "i686"
	case "x86_64", "amd64":
		return "x86_64"
	}
	return Arch(a)
}

// Prefix is Termux's $PREFIX (or "" off Termux).
func Prefix() string {
	if p := os.Getenv("PREFIX"); strings.Contains(p, "com.termux") {
		return p
	}
	if _, err := os.Stat("/data/data/com.termux/files/usr"); err == nil {
		return "/data/data/com.termux/files/usr"
	}
	return ""
}

// IsTermux reports whether we run inside Termux (not inside a distro).
func IsTermux() bool { return Prefix() != "" && os.Getenv("ANDRONIX_DISTRO") == "" }

// Home is $HOME, falling back to Termux's home.
func Home() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return "/data/data/com.termux/files/home"
}

// FixDNS makes Go's resolver work on Android. A static GOOS=linux build
// reads /etc/resolv.conf, which Android doesn't have, and would then ask
// 127.0.0.1:53. Use Termux's resolv.conf, else public resolvers.
func FixDNS() {
	if _, err := os.Stat("/etc/resolv.conf"); err == nil {
		return
	}
	servers := []string{"8.8.8.8:53", "1.1.1.1:53"}
	if b, err := os.ReadFile(filepath.Join(Prefix(), "etc/resolv.conf")); err == nil {
		var found []string
		for _, line := range strings.Split(string(b), "\n") {
			f := strings.Fields(line)
			if len(f) >= 2 && f[0] == "nameserver" {
				found = append(found, net.JoinHostPort(f[1], "53"))
			}
		}
		if len(found) > 0 {
			servers = append(found, servers...)
		}
	}
	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: 5 * time.Second}
			var err error
			for _, s := range servers {
				var c net.Conn
				if c, err = d.DialContext(ctx, network, s); err == nil {
					return c, nil
				}
			}
			return nil, err
		},
	}
	// Termux ships a CA bundle too; Go already reads Android's
	// /system/etc/security/cacerts, this is a fallback.
	if os.Getenv("SSL_CERT_FILE") == "" {
		if p := filepath.Join(Prefix(), "etc/tls/cert.pem"); fileExists(p) {
			os.Setenv("SSL_CERT_FILE", p)
		}
	}
}

// Timezone from Android, for the distro's /etc/localtime.
func Timezone() string {
	tz := ""
	if out, err := Command("getprop", "persist.sys.timezone").Output(); err == nil {
		tz = strings.TrimSpace(string(out))
	}
	if tz == "" {
		tz = os.Getenv("TZ")
	}
	for _, r := range tz {
		if !(r == '/' || r == '_' || r == '+' || r == '-' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
			tz = ""
			break
		}
	}
	if tz == "" {
		tz = "Etc/UTC"
	}
	return tz
}

// FreeMB is the free space on the filesystem holding dir.
func FreeMB(dir string) int64 {
	for dir != "/" && !fileExists(dir) {
		dir = filepath.Dir(dir)
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return -1
	}
	return int64(st.Bavail) * int64(st.Bsize) / (1 << 20)
}

// TotalRAMMB is MemTotal from /proc/meminfo (0 if unknown).
func TotalRAMMB() int64 {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	for _, l := range strings.Split(string(b), "\n") {
		f := strings.Fields(l)
		if len(f) >= 2 && f[0] == "MemTotal:" {
			var kb int64
			fmt.Sscan(f[1], &kb)
			return kb / 1024
		}
	}
	return 0
}

// KernelRelease is uname -r.
func KernelRelease() string {
	var u unix.Utsname
	if err := unix.Uname(&u); err != nil {
		return ""
	}
	return unix.ByteSliceToString(u.Release[:])
}

// Hostname as proot will show it inside the distro.
func Hostname() string {
	h, _ := os.Hostname()
	return h
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }
