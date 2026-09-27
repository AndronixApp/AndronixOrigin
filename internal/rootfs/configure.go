package rootfs

import (
	"compress/gzip"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	andronix "github.com/AndronixApp/andronix-distros"
	"github.com/AndronixApp/andronix-distros/internal/sys"
)

// Info is written to /etc/andronix-release inside the distro.
type Info struct {
	ID, Label, Start, Version string
	// Family (apt, dnf, ...) and Edition (free or modded) are for layers
	// and scripts that run inside the distro.
	Family, Edition string
}

// Configure applies the host-side fixups every fresh rootfs needs:
// DNS, hosts, Android ids, time zone, the Andronix files, fake /proc
// data and the /dev/shm directory. dir is ~/.andronix/distros/<id>.
func Configure(dir, root string, info Info) error {
	for _, d := range []string{"etc/profile.d", "usr/local/bin", "tmp", "root", "etc/andronix"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			return err
		}
	}
	os.Chmod(filepath.Join(root, "tmp"), 0o1777)
	os.Chmod(filepath.Join(root, "root"), 0o700) // Fedora ships 0550

	w := func(rel, s string, mode os.FileMode) error {
		p := filepath.Join(root, rel)
		os.Remove(p) // images ship some of these as symlinks
		return os.WriteFile(p, []byte(s), mode)
	}
	if err := w("etc/resolv.conf", "nameserver 8.8.8.8\nnameserver 1.1.1.1\n", 0o644); err != nil {
		return err
	}
	hosts := "127.0.0.1   localhost.localdomain localhost\n" +
		"::1         localhost.localdomain localhost ip6-localhost ip6-loopback\n"
	// proot keeps the phone's hostname; make it resolvable (sudo checks).
	if h := sys.Hostname(); h != "" && h != "localhost" && !strings.ContainsAny(h, " \t/") {
		hosts += "127.0.1.1   " + h + "\n"
	}
	w("etc/hosts", hosts, 0o644)
	w("etc/hostname", "localhost\n", 0o644)

	tz := sys.Timezone()
	w("etc/timezone", tz+"\n", 0o644)
	os.Remove(filepath.Join(root, "etc/localtime"))
	os.Symlink("/usr/share/zoneinfo/"+tz, filepath.Join(root, "etc/localtime"))

	androidIDs(root)
	machineID(root)

	w("etc/andronix-release", fmt.Sprintf("ANDRONIX_DISTRO=%s\nANDRONIX_NAME=%q\nANDRONIX_START=%s\nANDRONIX_VERSION=%s\nANDRONIX_FAMILY=%s\nANDRONIX_EDITION=%s\n",
		info.ID, info.Label, info.Start, info.Version, info.Family, info.Edition), 0o644)
	w("etc/profile.d/andronix.sh", profileSh, 0o644)
	if err := InstallSelf(root); err != nil {
		return err
	}

	sysdata(filepath.Join(dir, "sysdata"))
	os.MkdirAll(filepath.Join(dir, "shm"), 0o1777)
	os.Chmod(filepath.Join(dir, "shm"), 0o1777)
	return nil
}

// machineID gives each install its own id (images ship it empty, so no
// two phones share one). dbus reads /etc/machine-id.
func machineID(root string) {
	p := filepath.Join(root, "etc/machine-id")
	if b, err := os.ReadFile(p); err == nil && len(strings.TrimSpace(string(b))) == 32 {
		return
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return
	}
	os.Remove(p)
	os.WriteFile(p, []byte(hex.EncodeToString(buf)+"\n"), 0o444)
}

// CleanScript strips identity, passwords, caches and upstream default
// users from an image (DESIGN.md section 9). CI runs the same steps in
// ci/build-rootfs.sh; the installer runs it on an upstream tarball it
// downloads itself (Arch Linux ARM ships user alarm/alarm and root/root).
const CleanScript = `
: >/etc/machine-id 2>/dev/null
[ -f /var/lib/dbus/machine-id ] && [ ! -L /var/lib/dbus/machine-id ] && rm -f /var/lib/dbus/machine-id
rm -f /etc/ssh/ssh_host_*
for u in $(awk -F: '$3 >= 1000 && $3 < 60000 { print $1 }' /etc/passwd); do
    userdel -r "$u" 2>/dev/null || deluser --remove-home "$u" 2>/dev/null || true
done
rm -rf /home/*
passwd -l root >/dev/null 2>&1 || sed -i 's/^root:[^:]*:/root:*:/' /etc/shadow 2>/dev/null
[ -f /etc/shadow ] || sed -i 's/^root:[^:]*:/root:*:/' /etc/passwd
rm -rf /var/cache/pacman/pkg/* /var/cache/apk/* /var/cache/xbps/* /tmp/* /var/tmp/*
find /var/log -type f | while read -r f; do : >"$f"; done
rm -f /root/.*_history
true
`

// SetRelease sets keys in /etc/andronix-release (e.g. ANDRONIX_DE once
// the desktop is in).
func SetRelease(root string, kv map[string]string) error {
	p := filepath.Join(root, "etc/andronix-release")
	b, _ := os.ReadFile(p)
	var out []string
	for _, l := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		k, _, _ := strings.Cut(l, "=")
		if _, replaced := kv[k]; !replaced && l != "" {
			out = append(out, l)
		}
	}
	for k, v := range kv {
		out = append(out, k+"="+v)
	}
	return writeFile(p, strings.Join(out, "\n")+"\n", 0o644)
}

// RefreshProfile rewrites /etc/profile.d/andronix.sh (andronix update).
func RefreshProfile(root string) error {
	return writeFile(filepath.Join(root, "etc/profile.d/andronix.sh"), profileSh, 0o644)
}

// Login profile: sound to Termux's PulseAudio, GUI apps to the VNC
// display, and the Andronix greeting (or first-boot setup) once.
const profileSh = `# /etc/profile.d/andronix.sh - written by the Andronix installer.
export PULSE_SERVER="${PULSE_SERVER:-127.0.0.1}"
[ -z "${DISPLAY:-}" ] && export DISPLAY=:1
# Firefox's sandboxes can't start under proot: no text (content) and no
# audio (the RDD and utility decoder processes) without these.
export MOZ_DISABLE_CONTENT_SANDBOX=1 MOZ_DISABLE_RDD_SANDBOX=1 MOZ_DISABLE_UTILITY_SANDBOX=1
# Shared memory without memfd seals: some phones' content processes all
# died otherwise ("Shared memory PlatformHandle is not safe to map").
export MOZ_SHM_NO_SEALS=1
case $- in
    *i*)
        if [ -z "${ANDRONIX_WELCOMED:-}" ] && [ -x /usr/local/bin/andronix ]; then
            /usr/local/bin/andronix welcome
            export ANDRONIX_WELCOMED=1
        fi
        ;;
esac
`

// InstallSelf copies this binary into the distro, where it runs the VNC
// helpers, first-boot setup and the greeting. Same CPU, so it just runs.
func InstallSelf(root string) error {
	bin, err := linuxSelf()
	if err != nil {
		return err
	}
	dst := filepath.Join(root, "usr/local/bin/andronix")
	os.Remove(dst + ".new")
	if err := os.WriteFile(dst+".new", bin, 0o755); err != nil {
		return err
	}
	if err := os.Rename(dst+".new", dst); err != nil {
		return err
	}
	// bwrap stand-in: sandboxes can't work under proot, and glycin's image
	// loaders go through bwrap (app.BwrapShim). /usr/local/bin is first in
	// PATH, so this wins over the distro's /usr/bin/bwrap.
	os.MkdirAll(filepath.Join(root, "usr/local/lib/andronix"), 0o755)
	if err := writeFile(filepath.Join(root, bwrapShim), bwrapScript, 0o755); err != nil {
		return err
	}
	os.Remove(filepath.Join(root, "usr/local/bin/bwrap"))
	if err := os.Symlink("/"+bwrapShim, filepath.Join(root, "usr/local/bin/bwrap")); err != nil {
		return err
	}
	EnsureBwrapShim(root)
	InstallShm(root)
	for _, n := range []string{"vncserver-start", "vncserver-stop"} {
		sub := strings.TrimPrefix(n, "vncserver-")
		script := "#!/bin/sh\n# Andronix: " + n + " [size] [display]. See: andronix vnc --help\nexec /usr/local/bin/andronix vnc " + sub + " \"$@\"\n"
		p := filepath.Join(root, "usr/local/bin", n)
		os.Remove(p)
		if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
			return err
		}
	}
	return nil
}

// EnsureBwrapShim points the distro's own /usr/bin/bwrap at the andronix
// stand-in (app.BwrapShim), keeping the original as bwrap.andronix-real.
// glycin starts bwrap with a cleared environment, so the lookup uses the
// default path (/usr/bin) and never sees /usr/local/bin. Package updates
// can put the real one back, so this runs at install, after package steps
// and on every start. Does nothing where the distro has no bwrap.
func EnsureBwrapShim(root string) {
	for _, dir := range []string{"usr/bin", "bin"} {
		p := filepath.Join(root, dir, "bwrap")
		st, err := os.Lstat(p)
		if err != nil {
			continue
		}
		if st.Mode()&os.ModeSymlink != 0 {
			t, _ := os.Readlink(p)
			if t == "/"+bwrapShim {
				return
			}
			if t == "/usr/local/bin/andronix" { // an earlier stand-in: re-point only
				os.Remove(p)
				os.Symlink("/"+bwrapShim, p)
				return
			}
			if r, err := filepath.EvalSymlinks(p); err == nil && strings.HasPrefix(r, root+"/usr/bin") && r != p {
				continue // bin -> usr/bin symlink farm: handled via usr/bin
			}
		}
		real := p + ".andronix-real"
		os.Remove(real)
		if err := os.Rename(p, real); err != nil {
			return
		}
		os.Symlink("/"+bwrapShim, p)
		return
	}
}

// The bwrap stand-in (see bwrap.sh). A shell script, not this Go binary:
// it must start under glycin's RLIMIT_AS cap, and the Go runtime reserves
// more address space than that at startup.
const bwrapShim = "usr/local/lib/andronix/bwrap"

//go:embed bwrap.sh
var bwrapScript string

func writeFile(p, s string, mode os.FileMode) error {
	os.Remove(p)
	if err := os.WriteFile(p, []byte(s), mode); err != nil {
		return err
	}
	return os.Chmod(p, mode)
}

// FetchLinux downloads the GOOS=linux build for this CPU; the app sets it
// (the mirror and version live there). Used only by builds without one.
var FetchLinux func() ([]byte, error)

var (
	linuxOnce sync.Once
	linuxBin  []byte
	linuxErr  error
)

// linuxSelf is the andronix that goes inside distros: the static GOOS=linux
// build. A linux build is its own; the android build (what Termux runs)
// carries its CPU's linux build (ci/build-go.sh), else downloads it.
func linuxSelf() ([]byte, error) {
	linuxOnce.Do(func() {
		if runtime.GOOS == "linux" {
			var self string
			if self, linuxErr = os.Executable(); linuxErr == nil {
				linuxBin, linuxErr = os.ReadFile(self)
			}
			return
		}
		if f, err := andronix.SelfBin.Open("selfbin/andronix-linux.gz"); err == nil {
			defer f.Close()
			var z *gzip.Reader
			if z, linuxErr = gzip.NewReader(f); linuxErr == nil {
				linuxBin, linuxErr = io.ReadAll(z)
			}
			return
		}
		if FetchLinux == nil {
			linuxErr = errors.New("this andronix build has no copy for inside distros")
			return
		}
		linuxBin, linuxErr = FetchLinux()
	})
	return linuxBin, linuxErr
}

// androidIDs adds Termux's uid and groups so `id` and apt stop warning
// about unknown ids.
func androidIDs(root string) {
	uid := os.Getuid()
	if uid == 0 {
		return
	}
	etc := filepath.Join(root, "etc")
	for _, f := range []string{"passwd", "shadow", "group", "gshadow"} {
		os.Chmod(filepath.Join(etc, f), 0o644)
	}
	user := "u" + strconv.Itoa(uid)
	if out, err := sys.Command("id", "-un").Output(); err == nil {
		user = strings.TrimSpace(string(out))
	}
	if !hasID(filepath.Join(etc, "passwd"), uid) {
		appendLine(filepath.Join(etc, "passwd"), fmt.Sprintf("aid_%s:x:%d:%d:Termux:/:/sbin/nologin", user, uid, os.Getgid()))
		appendLine(filepath.Join(etc, "shadow"), fmt.Sprintf("aid_%s:*:18446:0:99999:7:::", user))
	}
	gids, _ := os.Getgroups()
	names := map[int]string{}
	if out, err := sys.Command("id", "-G").Output(); err == nil {
		if nout, err := sys.Command("id", "-Gn").Output(); err == nil {
			ids, ns := strings.Fields(string(out)), strings.Fields(string(nout))
			for i := range ids {
				if i < len(ns) {
					n, _ := strconv.Atoi(ids[i])
					names[n] = ns[i]
				}
			}
		}
	}
	for _, g := range append([]int{os.Getgid()}, gids...) {
		if g == 0 || hasID(filepath.Join(etc, "group"), g) {
			continue
		}
		n := names[g]
		if n == "" {
			n = "g" + strconv.Itoa(g)
		}
		appendLine(filepath.Join(etc, "group"), fmt.Sprintf("aid_%s:x:%d:root,aid_%s", n, g, user))
		if _, err := os.Stat(filepath.Join(etc, "gshadow")); err == nil {
			appendLine(filepath.Join(etc, "gshadow"), fmt.Sprintf("aid_%s:*::root,aid_%s", n, user))
		}
	}
	for _, f := range []string{"shadow", "gshadow"} {
		os.Chmod(filepath.Join(etc, f), 0o640)
	}
}

func hasID(file string, id int) bool {
	b, err := os.ReadFile(file)
	if err != nil {
		return false
	}
	s := strconv.Itoa(id)
	for _, l := range strings.Split(string(b), "\n") {
		f := strings.Split(l, ":")
		if len(f) > 2 && f[2] == s {
			return true
		}
	}
	return false
}

func appendLine(file, line string) {
	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintln(f, line)
}

// sysdata writes stand-ins for /proc entries Android hides; proot binds
// them only when the real ones can't be read.
func sysdata(d string) {
	os.MkdirAll(filepath.Join(d, "empty"), 0o755)
	n := numCPU()
	var stat strings.Builder
	stat.WriteString("cpu  4000 0 3000 90000 200 0 100 0 0 0\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&stat, "cpu%d 500 0 400 11000 20 0 10 0 0 0\n", i)
	}
	fmt.Fprintf(&stat, "intr 0\nctxt 100000\nbtime %d\nprocesses 800\nprocs_running 1\nprocs_blocked 0\nsoftirq 0 0 0 0 0 0 0 0 0 0 0\n",
		time.Now().Unix()-5400)
	files := map[string]string{
		"loadavg":          "0.10 0.05 0.01 1/128 4242\n",
		"uptime":           "5400.00 20000.00\n",
		"version":          "Linux version 6.12.0-andronix (andronix@localhost) (clang) #1 SMP PREEMPT\n",
		"stat":             stat.String(),
		"vmstat":           "nr_free_pages 200000\nnr_inactive_anon 50000\nnr_active_anon 20000\npgpgin 0\npgpgout 0\npswpin 0\npswpout 0\npgfault 100000\npgmajfault 100\n",
		"cap_last_cap":     "40\n",
		"max_user_watches": "8192\n",
		"overflowuid":      "65534\n",
		"overflowgid":      "65534\n",
	}
	for k, v := range files {
		os.WriteFile(filepath.Join(d, k), []byte(v), 0o644)
	}
}

func numCPU() int {
	b, err := os.ReadFile("/sys/devices/system/cpu/present")
	if err == nil {
		s := strings.TrimSpace(string(b))
		if i := strings.LastIndex(s, "-"); i >= 0 {
			if n, err := strconv.Atoi(s[i+1:]); err == nil {
				return n + 1
			}
		}
	}
	return 8
}

// ShmLib is libandronix-shm.so inside the distro: file-backed System V
// shared memory (guest/preload/shm.c) that andronix-postgres preloads for
// PostgreSQL, instead of proot's --sysvipc helper.
const ShmLib = "/usr/local/lib/andronix/libandronix-shm.so"

// InstallShm puts ShmLib for this CPU into the distro.
func InstallShm(root string) error {
	b, err := andronix.Preload.ReadFile("guest/preload/" + string(sys.DetectArch()) + "/libandronix-shm.so")
	if err != nil {
		return err
	}
	p := filepath.Join(root, ShmLib)
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.Remove(p + ".new")
	if err := os.WriteFile(p+".new", b, 0o755); err != nil {
		return err
	}
	return os.Rename(p+".new", p)
}
