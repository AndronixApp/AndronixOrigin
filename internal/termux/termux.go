// Package termux is the Termux side of a running desktop: PulseAudio for
// sound and the Termux:X11 display server. Everything here runs in
// Termux, never inside a distro.
package termux

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/AndronixApp/andronix-distros/internal/sys"
)

// isTermux is a variable so tests can run the Termux paths off a phone.
var isTermux = sys.IsTermux

// logPath is where the Termux-side helpers record what they did
// (~/.andronix/logs/termux.log), one timestamped line per action.
func logPath() string {
	base := os.Getenv("ANDRONIX_HOME")
	if base == "" {
		base = filepath.Join(sys.Home(), ".andronix")
	}
	return filepath.Join(base, "logs", "termux.log")
}

// Logf appends one line to the Termux helpers' log. The file is cut back
// to its newer half once it passes 256 KB.
func Logf(format string, args ...any) {
	p := logPath()
	os.MkdirAll(filepath.Dir(p), 0o755)
	if st, err := os.Stat(p); err == nil && st.Size() > 256<<10 {
		if b, err := os.ReadFile(p); err == nil {
			b = b[len(b)/2:]
			if i := strings.IndexByte(string(b), '\n'); i >= 0 {
				b = b[i+1:]
			}
			os.WriteFile(p, b, 0o644)
		}
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, args...))
}

// LogFile is the path of the helpers' log, for error hints.
func LogFile() string { return logPath() }

// pkgInstall installs Termux packages with apt-get (not pkg: pkg runs
// curl first, which can't start on a half-upgraded Termux), sending each
// output line to onLine. It refreshes the lists first (x11-repo adds a
// source), installs non-interactively keeping user-edited conffiles, and
// if that fails runs one apt-get full-upgrade, as pkg itself advises for
// a Termux whose packages were upgraded only in part (a new libcurl on an
// old OpenSSL), then tries again.
func pkgInstall(ctx context.Context, onLine func(string), pkgs ...string) error {
	if _, err := sys.LookPath("apt-get"); err != nil {
		return fmt.Errorf("apt-get not found")
	}
	// One package run at a time (two sessions starting together), and
	// never forever: a dead mirror can stall apt's download.
	unlock, err := pkgLock()
	if err != nil {
		return err
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(ctx, pkgTimeout)
	defer cancel()
	keep := []string{"-o", "Dpkg::Options::=--force-confdef", "-o", "Dpkg::Options::=--force-confold"}
	install := append(append([]string{"-y"}, keep...), append([]string{"install"}, pkgs...)...)
	termuxRun(ctx, onLine, "apt-get", "update") // stale lists still let install try
	if _, err = termuxRun(ctx, onLine, "apt-get", install...); err == nil {
		return nil
	}
	Logf("apt-get: install failed; upgrading Termux's packages (apt-get full-upgrade), then retrying")
	if _, err := termuxRun(ctx, onLine, "apt-get", append(append([]string{"-y"}, keep...), "full-upgrade")...); err != nil {
		return err
	}
	_, err = termuxRun(ctx, onLine, "apt-get", install...)
	return err
}

// Install is pkgInstall for other packages (the installer's proot).
func Install(ctx context.Context, onLine func(string), pkgs ...string) error {
	return pkgInstall(ctx, onLine, pkgs...)
}

// pkgTimeout bounds one pkgInstall, recovery included.
var pkgTimeout = 20 * time.Minute

// pkgLock takes ~/.andronix/pkg.lock, or fails at once if another andronix
// holds it.
func pkgLock() (func(), error) {
	p := filepath.Join(filepath.Dir(filepath.Dir(logPath())), "pkg.lock")
	os.MkdirAll(filepath.Dir(p), 0o755)
	f, err := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		Logf("pkg: another andronix is installing Termux packages; not waiting")
		return nil, fmt.Errorf("another andronix is installing Termux packages right now")
	}
	return func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
}

// termuxRun runs a Termux package command non-interactively, logging and
// passing on each output line, and returns the output.
func termuxRun(ctx context.Context, onLine func(string), name string, args ...string) (string, error) {
	c := sys.CommandContext(ctx, name, args...)
	c.Env = append(c.Environ(), "DEBIAN_FRONTEND=noninteractive")
	b, err := c.CombinedOutput()
	out := string(b)
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			Logf("%s: %s", name, l)
			if onLine != nil {
				onLine(l)
			}
		}
	}
	return out, err
}

// lastLines is the last n non-empty lines of s, joined with " / ".
func lastLines(s string, n int) string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return strings.Join(out, " / ")
}
