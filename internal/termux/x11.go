package termux

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/AndronixApp/andronix-distros/internal/sys"
)

// X11Display is the display Termux:X11 serves. VNC inside a distro uses
// :1 and up, so the two never clash.
const X11Display = 0

// X11App is the Termux:X11 companion app's package.
const X11App = "com.termux.x11"

// X11AppURL is where the companion app comes from (termux/termux-x11:
// "termux-x11-universal-debug.apk" from the nightly release).
const X11AppURL = "https://github.com/termux/termux-x11/releases/tag/nightly"

// X11Package is the Termux package with the termux-x11 server (x11-repo).
const X11Package = "termux-x11-nightly"

// Errors from StartX11 that need their own message.
var (
	ErrX11AppMissing   = errors.New("the Termux:X11 app isn't installed")
	ErrX11AppSignature = errors.New("the Termux:X11 app and the termux-x11 package come from different builds")
	ErrX11OldAndroid   = errors.New("Termux:X11 needs Android 7 or newer")
)

// X11SocketDir is the X11 socket folder termux-x11 uses: $TMPDIR/.X11-unix,
// where TMPDIR is Termux's $PREFIX/tmp. Off Termux (tests) it's /tmp's.
func X11SocketDir() string {
	tmp := os.Getenv("TMPDIR")
	if tmp == "" {
		if p := sys.Prefix(); p != "" {
			tmp = filepath.Join(p, "tmp")
		} else {
			tmp = "/tmp"
		}
	}
	return filepath.Join(tmp, ".X11-unix")
}

// X11Socket is the socket of display :n on the Termux side.
func X11Socket(n int) string { return filepath.Join(X11SocketDir(), "X"+strconv.Itoa(n)) }

// X11Running reports whether an X server answers on display :n.
func X11Running(n int) bool {
	c, err := net.DialTimeout("unix", X11Socket(n), time.Second)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

// HasX11Server reports whether the termux-x11 command is installed.
func HasX11Server() bool { _, err := sys.LookPath("termux-x11"); return err == nil }

// InstallX11Server installs x11-repo, then termux-x11-nightly from it.
func InstallX11Server(ctx context.Context, onLine func(string)) error {
	Logf("x11: installing x11-repo and %s", X11Package)
	if err := pkgInstall(ctx, onLine, "x11-repo"); err != nil {
		return fmt.Errorf("pkg install x11-repo: %w", err)
	}
	// x11-repo adds a source; pkg refreshes the lists for the new repo.
	if err := pkgInstall(ctx, onLine, X11Package); err != nil {
		return fmt.Errorf("pkg install %s: %w", X11Package, err)
	}
	return nil
}

// X11AppInstalled asks Android's package manager whether the companion
// app is installed. known is false when pm can't answer (some ROMs block
// it for apps); StartX11 then finds out from termux-x11's own error.
func X11AppInstalled() (installed, known bool) {
	if !isTermux() {
		return true, false
	}
	out, err := sys.Command("pm", "list", "packages", X11App).Output()
	if err != nil {
		return false, false
	}
	for _, l := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(l) == "package:"+X11App {
			return true, true
		}
	}
	return false, true
}

// StartX11 starts `termux-x11 :n -ac` detached, logging to logf, and
// waits for its socket. Extra server options come from ANDRONIX_X11_ARGS
// (e.g. -legacy-drawing for a black screen, -force-bgra for swapped
// colours). Returns the server's pid.
func StartX11(n int, logf string) (int, error) {
	os.MkdirAll(X11SocketDir(), 0o1777)
	// A stale socket from a killed server would make the new one fail.
	os.Remove(X11Socket(n))
	os.Remove(filepath.Join(filepath.Dir(X11SocketDir()), fmt.Sprintf(".X%d-lock", n)))
	args := append([]string{fmt.Sprintf(":%d", n), "-ac"}, strings.Fields(os.Getenv("ANDRONIX_X11_ARGS"))...)
	lg, err := os.Create(logf)
	if err != nil {
		return 0, err
	}
	defer lg.Close()
	c := sys.Command("termux-x11", args...)
	c.Stdout, c.Stderr = lg, lg
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	// termux-exec's LD_PRELOAD is unset by the script itself; nothing
	// else of ours leaks into the server.
	if err := c.Start(); err != nil {
		return 0, err
	}
	Logf("x11: started termux-x11 %s (pid %d)", strings.Join(args, " "), c.Process.Pid)
	exited := make(chan error, 1)
	go func() { exited <- c.Wait() }()
	for i := 0; i < 40; i++ {
		select {
		case err := <-exited:
			b, _ := os.ReadFile(logf)
			Logf("x11: termux-x11 exited early (%v): %s", err, lastLines(string(b), 4))
			return 0, classifyX11(string(b), err)
		case <-time.After(250 * time.Millisecond):
		}
		if X11Running(n) {
			return c.Process.Pid, nil
		}
	}
	c.Process.Kill()
	b, _ := os.ReadFile(logf)
	Logf("x11: no socket at %s after 10 s: %s", X11Socket(n), lastLines(string(b), 4))
	return 0, fmt.Errorf("termux-x11 didn't open display :%d: %s", n, lastLines(string(b), 3))
}

// classifyX11 turns termux-x11's early exit into one of our errors, using
// the messages of its loader (shell-loader/build.gradle) and script.
func classifyX11(out string, err error) error {
	switch {
	case strings.Contains(out, "Termux:X11 application is not found"),
		strings.Contains(out, "NameNotFoundException") && strings.Contains(out, X11App):
		return ErrX11AppMissing
	case strings.Contains(out, "Signature verification of target application"):
		return ErrX11AppSignature
	case strings.Contains(out, "requires at least Android 7"):
		return ErrX11OldAndroid
	}
	return fmt.Errorf("termux-x11 stopped (%v): %s", err, lastLines(out, 3))
}

// OpenX11App brings the Termux:X11 app to the front.
func OpenX11App() {
	if !isTermux() {
		return
	}
	out, err := sys.Command("am", "start", "--user", "0", "-n", X11App+"/"+X11App+".MainActivity").CombinedOutput()
	if err != nil {
		Logf("x11: am start %s failed: %v: %s", X11App, err, lastLines(string(out), 2))
		return
	}
	Logf("x11: opened the Termux:X11 app")
}

// StopX11 stops the Termux:X11 server: pid (when we know it), every
// termux-x11 process, and the app (its own ACTION_STOP broadcast). It
// then clears the display's socket. Returns whether anything was running.
func StopX11(n int, pid int) bool {
	stopped := false
	pids := x11Pids()
	if Alive(pid) {
		pids = append(pids, pid)
	}
	for _, p := range pids {
		if syscall.Kill(p, syscall.SIGTERM) == nil {
			stopped = true
			Logf("x11: stopped termux-x11 (pid %d)", p)
		}
	}
	if isTermux() {
		sys.Command("am", "broadcast", "-a", X11App+".ACTION_STOP", "-p", X11App).Run()
	}
	for i := 0; i < 20 && X11Running(n); i++ {
		time.Sleep(100 * time.Millisecond)
	}
	for _, p := range pids {
		if Alive(p) {
			syscall.Kill(p, syscall.SIGKILL)
		}
	}
	if !X11Running(n) {
		os.Remove(X11Socket(n))
	}
	return stopped
}

// x11Pids finds termux-x11 servers: app_process renames itself
// "termux-x11 com.termux.x11 :0 ..." (the package's script).
func x11Pids() []int {
	var out []int
	entries, _ := os.ReadDir("/proc")
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == os.Getpid() {
			continue
		}
		b, err := os.ReadFile("/proc/" + e.Name() + "/cmdline")
		if err != nil {
			continue
		}
		cmd := strings.ReplaceAll(string(b), "\x00", " ")
		if strings.HasPrefix(cmd, "termux-x11 ") && strings.Contains(cmd, X11App) {
			out = append(out, pid)
		}
	}
	return out
}

// Alive reports whether pid is a live (non-zombie) process.
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	// The state follows the command name in parentheses.
	s := string(b)
	if i := strings.LastIndexByte(s, ')'); i >= 0 && i+2 < len(s) {
		return s[i+2] != 'Z'
	}
	return true
}
