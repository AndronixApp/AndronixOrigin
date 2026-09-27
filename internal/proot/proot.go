// Package proot builds the proot command line for an installed distro and
// runs commands inside it. Flags follow proot-distro's lessons (see
// DESIGN.md section 6); the code is our own.
package proot

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/AndronixApp/andronix-distros/internal/sys"
	"github.com/AndronixApp/andronix-distros/internal/termux"
)

// Target is an installed distro.
type Target struct {
	ID       string // distro id, exported as ANDRONIX_DISTRO
	Dir      string // ~/.andronix/distros/<id>
	Rootfs   string
	BindsDir string // legacy ~/<distro>-binds
	// Shell is the distro's login shell (exported as SHELL).
	Shell string
	// Lang is LANG inside (default C.UTF-8).
	Lang string
	// Login user; empty means root. UID/GID/Home come from the guest passwd.
	User           string
	UID, GID, Home string
	// Display is DISPLAY inside, e.g. ":0" for a Termux:X11 session.
	// Empty leaves it to the login profile (the VNC display, :1).
	Display string
	// Local runs commands directly, for andronix running inside the
	// distro (Rootfs is then "/").
	Local bool
}

var (
	helpOnce sync.Once
	helpText string
)

// Has reports whether this proot supports flag.
func Has(flag string) bool {
	helpOnce.Do(func() {
		c := sys.Command("proot", "--help")
		c.Env = hostEnv()
		out, _ := c.CombinedOutput()
		helpText = string(out)
	})
	if flag == "-L" {
		for _, l := range strings.Split(helpText, "\n") {
			if strings.TrimSpace(l) == "-L" || strings.HasPrefix(strings.TrimSpace(l), "-L,") {
				return true
			}
		}
		return false
	}
	return strings.Contains(helpText, flag)
}

func readable(p string) bool {
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	b := make([]byte, 1)
	_, err = f.Read(b)
	return err == nil || err == io.EOF
}

func exists(p string) bool { _, err := os.Lstat(p); return err == nil }

// Args returns proot's argument list (without the command to run).
func (t *Target) Args() []string {
	a := []string{"proot"}
	for _, f := range []string{"--kill-on-exit", "--link2symlink", "--sysvipc", "-L"} {
		if Has(f) {
			a = append(a, f)
		}
	}
	if Has("--kernel-release") {
		a = append(a, "--kernel-release=6.12.0-andronix")
	}
	if t.User != "" && t.UID != "" {
		a = append(a, "--change-id="+t.UID+":"+t.GID, "-w", t.Home)
	} else {
		a = append(a, "-0", "-w", "/root")
	}
	a = append(a, "-r", t.Rootfs, "-b", "/dev", "-b", "/proc", "-b", "/sys", "-b", "/dev/urandom:/dev/random")
	if !exists("/dev/fd") {
		a = append(a, "-b", "/proc/self/fd:/dev/fd")
	}
	for i, n := range []string{"stdin", "stdout", "stderr"} {
		if !exists("/dev/" + n) {
			a = append(a, "-b", "/proc/self/fd/"+string(rune('0'+i))+":/dev/"+n)
		}
	}
	if st, err := os.Stat(filepath.Join(t.Dir, "shm")); err == nil && st.IsDir() {
		a = append(a, "-b", filepath.Join(t.Dir, "shm")+":/dev/shm")
	}
	// Stand-ins for /proc entries newer Android hides from apps.
	sd := filepath.Join(t.Dir, "sysdata")
	for _, e := range [][2]string{
		{"/proc/loadavg", "loadavg"}, {"/proc/stat", "stat"}, {"/proc/uptime", "uptime"},
		{"/proc/version", "version"}, {"/proc/vmstat", "vmstat"},
		{"/proc/sys/kernel/cap_last_cap", "cap_last_cap"},
		{"/proc/sys/fs/inotify/max_user_watches", "max_user_watches"},
		{"/proc/sys/kernel/overflowuid", "overflowuid"}, {"/proc/sys/kernel/overflowgid", "overflowgid"},
	} {
		if !readable(e[0]) && exists(filepath.Join(sd, e[1])) {
			a = append(a, "-b", filepath.Join(sd, e[1])+":"+e[0])
		}
	}
	// apk 3 needs this stand-in always (see pkgmgr "apk").
	if exists(filepath.Join(sd, "memfd_noexec")) {
		a = append(a, "-b", filepath.Join(sd, "memfd_noexec")+":/proc/sys/vm/memfd_noexec")
	}
	if exists("/sys/fs/selinux") && exists(filepath.Join(sd, "empty")) {
		a = append(a, "-b", filepath.Join(sd, "empty")+":/sys/fs/selinux")
	}
	// Shared storage at /sdcard (after termux-setup-storage).
	if readable("/storage/emulated/0") || dirReadable("/storage/emulated/0") {
		a = append(a, "-b", "/storage/emulated/0:/sdcard")
		if dirReadable("/storage") {
			a = append(a, "-b", "/storage")
		}
	} else if dirReadable("/sdcard") {
		a = append(a, "-b", "/sdcard")
	}
	// Termux:X11's display socket, while its server runs, so GUI apps in
	// any session of the distro can reach DISPLAY=:0. Only the one socket:
	// VNC's own sockets stay in the distro's /tmp.
	if st, err := os.Stat(termux.X11Socket(termux.X11Display)); err == nil && st.Mode()&os.ModeSocket != 0 {
		a = append(a, "-b", fmt.Sprintf("%s:/tmp/.X11-unix/X%d", termux.X11Socket(termux.X11Display), termux.X11Display))
	}
	a = append(a, legacyBinds(t.BindsDir)...)
	for _, b := range strings.Fields(os.Getenv("ANDRONIX_BINDS")) {
		a = append(a, "-b", b)
	}
	return a
}

func dirReadable(p string) bool {
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	f.Close()
	return true
}

// legacyBinds sources the old installers' ~/<distro>-binds/* snippets
// (`command+=" -b a:b"`) with bash and returns their arguments.
func legacyBinds(dir string) []string {
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		return nil
	}
	script := `command=""; for f in "$1"/*; do [ -f "$f" ] && . "$f"; done; printf '%s\n' $command`
	out, err := sys.Command("bash", "-c", script, "binds", dir).Output()
	if err != nil {
		return nil
	}
	return strings.Fields(string(out))
}

// Env is the clean environment inside the distro.
func (t *Target) Env() []string {
	home, user := "/root", "root"
	if t.User != "" {
		home, user = t.Home, t.User
	}
	shell := t.Shell
	if shell == "" {
		shell = "/bin/sh"
	}
	lang := t.Lang
	if lang == "" {
		lang = "C.UTF-8"
	}
	term := os.Getenv("TERM")
	if term == "" {
		term = "xterm-256color"
	}
	env := []string{"/usr/bin/env", "-i",
		"HOME=" + home, "USER=" + user, "LOGNAME=" + user, "SHELL=" + shell, "TERM=" + term, "LANG=" + lang,
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/usr/local/games:/usr/games",
		"PULSE_SERVER=127.0.0.1", "TMPDIR=/tmp", "ANDRONIX_DISTRO=" + t.ID}
	if t.Display != "" {
		env = append(env, "DISPLAY="+t.Display)
	}
	// Mesa over Termux:X11 takes the DRI3 path (zink, with no Vulkan
	// driver) and deadlocks: GTK apps hang and XFCE stays black. Every
	// session gets it, so apps opened on :0 from a second session work too;
	// Xvnc has no DRI3, so VNC is unchanged. ANDRONIX_DRI3=1 keeps DRI3
	// for GPU setups (turnip, zink with a Vulkan driver).
	if os.Getenv("ANDRONIX_DRI3") != "1" {
		env = append(env, "LIBGL_DRI3_DISABLE=1")
	}
	for _, k := range []string{"COLORTERM", "NO_COLOR", "COLUMNS", "ANDRONIX_ASCII", "ANDRONIX_PLAIN"} {
		if v := os.Getenv(k); v != "" {
			env = append(env, k+"="+v)
		}
	}
	return env
}

// Login replaces this process with a login shell (or `shell -c cmd`,
// which is how the old start scripts passed arguments).
func (t *Target) Login(shell string, cmd []string) error {
	os.Unsetenv("LD_PRELOAD")
	argv := append(t.Args(), t.Env()...)
	argv = append(argv, shell, "-l")
	switch {
	case len(cmd) == 1:
		// One argument is a shell command line, like the old start
		// scripts: ./start-debian.sh "apt update && apt upgrade".
		argv = append(argv, "-c", cmd[0])
	case len(cmd) > 1:
		// Several arguments reach the distro unchanged (after the login
		// profile has run): ./start-debian.sh sh -c "a && b".
		argv = append(argv, "-c", `exec "$@"`, "andronix")
		argv = append(argv, cmd...)
	}
	bin, err := sys.LookPath("proot")
	if err != nil {
		return err
	}
	return sys.Exec(bin, argv, hostEnv())
}

// Run runs a shell command inside the distro as root, streaming each
// output line to onLine. stdin may be nil.
func (t *Target) Run(ctx context.Context, cmd string, stdin io.Reader, onLine func(string)) error {
	root := *t
	root.User = ""
	var c *exec.Cmd
	if t.Local {
		c = sys.CommandContext(ctx, "/bin/sh", "-c", cmd)
	} else {
		argv := append(root.Args(), root.Env()...)
		argv = append(argv, "/bin/sh", "-c", cmd)
		c = sys.CommandContext(ctx, argv[0], argv[1:]...)
		c.Env = hostEnv()
	}
	c.Stdin = stdin
	pr, pw := io.Pipe()
	c.Stdout, c.Stderr = pw, pw
	if err := c.Start(); err != nil {
		return err
	}
	done := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			if onLine != nil {
				onLine(sc.Text())
			}
		}
		io.Copy(io.Discard, pr)
		close(done)
	}()
	err := c.Wait()
	pw.Close()
	<-done
	return err
}

// Interactive runs a command inside the distro on this terminal (for
// prompts like vncpasswd or the first-boot setup).
func (t *Target) Interactive(cmd string, asUser bool) error {
	tt := *t
	if !asUser {
		tt.User = ""
	}
	argv := append(tt.Args(), tt.Env()...)
	argv = append(argv, "/bin/sh", "-c", cmd)
	c := sys.Command(argv[0], argv[1:]...)
	c.Env = hostEnv()
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}

// Command is a command inside the distro (as the login user unless the
// target is root), ready to start. The caller sets stdio.
func (t *Target) Command(argv ...string) *exec.Cmd {
	a := append(t.Args(), t.Env()...)
	a = append(a, argv...)
	c := sys.Command(a[0], a[1:]...)
	c.Env = hostEnv()
	return c
}

// hostEnv is the environment for proot itself: no LD_PRELOAD (termux-exec
// breaks proot) and our own PROOT_TMP_DIR, since Termux clears
// $PREFIX/tmp on its own and proot fails when its files vanish.
func hostEnv() []string {
	out := []string{}
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "LD_PRELOAD=") && !strings.HasPrefix(e, "PROOT_TMP_DIR=") {
			out = append(out, e)
		}
	}
	out = append(out, "PROOT_TMP_DIR="+TmpDir())
	// The compat rules' env (e.g. PROOT_NO_SECCOMP on old kernels); the
	// user's own settings win.
	for _, e := range compatEnv() {
		if k, _, _ := strings.Cut(e, "="); os.Getenv(k) == "" {
			out = append(out, e)
		}
	}
	// Fallback when the rules couldn't load.
	if os.Getenv("PROOT_NO_SECCOMP") == "" && oldSyscallOrder(sys.KernelRelease()) && !hasKey(compatEnv(), "PROOT_NO_SECCOMP") {
		out = append(out, "PROOT_NO_SECCOMP=1")
	}
	return out
}

// EnvHook returns environment for every proot run (the compat rules, set
// by the app). Called once, lazily.
var (
	EnvHook func() []string
	envMu   sync.Mutex
	envSet  bool
	extra   []string
)

// compatEnv is the compat env: set by SetEnv, else from EnvHook once. The
// hook runs without the lock held, since it may call SetEnv itself.
func compatEnv() []string {
	envMu.Lock()
	set, env := envSet, extra
	envMu.Unlock()
	if set || EnvHook == nil {
		return env
	}
	env = EnvHook()
	envMu.Lock()
	if !envSet {
		envSet, extra = true, env
	}
	env = extra
	envMu.Unlock()
	return env
}

// SetEnv replaces the compat env (the installer, once it has probed).
func SetEnv(env []string) {
	envMu.Lock()
	envSet, extra = true, env
	envMu.Unlock()
}

func hasKey(env []string, key string) bool {
	for _, e := range env {
		if k, _, _ := strings.Cut(e, "="); k == key {
			return true
		}
	}
	return false
}

// OldKernel reports whether this phone's kernel is older than 4.8, where
// proot runs without seccomp acceleration (see oldSyscallOrder). Desktops
// there can hang at start (xfwm4 on Android 9, kernel 4.4).
func OldKernel() bool { return oldSyscallOrder(sys.KernelRelease()) }

// oldSyscallOrder reports a kernel before 4.8, where seccomp stops come
// before ptrace's syscall-enter stop. proot's seccomp acceleration then
// aborts ("assertion IS_IN_SYSENTER(tracee) failed") on the first apt run:
// Android 9 on kernel 4.4, in the app and under run-as alike. Plain
// ptrace is slower but works.
func oldSyscallOrder(release string) bool {
	var major, minor int
	if n, _ := fmt.Sscanf(release, "%d.%d", &major, &minor); n < 2 {
		return false
	}
	return major < 4 || major == 4 && minor < 8
}

var tmpOnce sync.Once

// TmpDir is ~/.andronix/tmp (or $ANDRONIX_HOME/tmp). The first call
// creates it and removes files left by proot processes that are gone;
// live sessions' files are kept.
func TmpDir() string {
	base := os.Getenv("ANDRONIX_HOME")
	if base == "" {
		base = filepath.Join(sys.Home(), ".andronix")
	}
	dir := filepath.Join(base, "tmp")
	tmpOnce.Do(func() {
		os.MkdirAll(dir, 0o700)
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			// proot names them <kind>-<pid>-<random>: proot- (glue),
			// prooted- (its loader, when not in libexec) and prootshm-
			// (--sysvipc). Removing a live session's loader makes every
			// exec in it fail with ENOENT.
			parts := strings.SplitN(e.Name(), "-", 3)
			if len(parts) == 3 && strings.HasPrefix(parts[0], "proot") {
				if _, err := os.Stat("/proc/" + parts[1]); err == nil {
					continue
				}
			}
			os.RemoveAll(filepath.Join(dir, e.Name()))
		}
	})
	return dir
}
