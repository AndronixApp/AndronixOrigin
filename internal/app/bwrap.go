package app

import (
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/AndronixApp/andronix-distros/internal/sys"
)

// Bubblewrap needs unprivileged user namespaces, which Android (and proot)
// don't give apps. glycin (GdkPixbuf's newer image loading) runs every
// image loader through bwrap, so on Android icons fail to load, GTK
// asserts, and xfce4-panel/xfdesktop abort: a black or white desktop.
// Inside the distro, /usr/local/bin/bwrap (first in PATH) is this binary;
// BwrapShim drops the sandbox options and runs the command directly.

// Options that take arguments, and how many.
var bwrapArgs = map[string]int{
	"--args": 1, "--argv0": 1, "--bind": 2, "--bind-try": 2, "--bind-data": 2, "--block-fd": 1,
	"--cap-add": 1, "--cap-drop": 1, "--chdir": 1, "--chmod": 2, "--dev": 1, "--dev-bind": 2,
	"--dev-bind-try": 2, "--dir": 1, "--exec-label": 1, "--file": 2, "--file-label": 1, "--gid": 1,
	"--hostname": 1, "--info-fd": 1, "--json-status-fd": 1, "--lock-file": 1, "--mqueue": 1,
	"--overlay": 3, "--overlay-src": 1, "--perms": 1, "--pidns": 1, "--proc": 1, "--remount-ro": 1,
	"--ro-bind": 2, "--ro-bind-data": 2, "--ro-bind-try": 2, "--ro-overlay": 1, "--seccomp": 1,
	"--add-seccomp-fd": 1, "--setenv": 2, "--size": 1, "--symlink": 2, "--sync-fd": 1, "--tmp-overlay": 1,
	"--tmpfs": 1, "--uid": 1, "--unsetenv": 1, "--userns": 1, "--userns2": 1, "--userns-block-fd": 1,
}

// IsBwrap reports whether we were started as bwrap.
func IsBwrap() bool {
	a := os.Args[0]
	return a == "bwrap" || strings.HasSuffix(a, "/bwrap")
}

// BwrapShim runs `bwrap [options] [--] command args...` without a sandbox.
func BwrapShim(args []string) error {
	env := os.Environ()
	chdir := ""
	i := 0
	for i < len(args) {
		a := args[i]
		if a == "--" {
			i++
			break
		}
		if !strings.HasPrefix(a, "--") {
			break
		}
		n := bwrapArgs[a]
		if i+n >= len(args) {
			return fmt.Errorf("bwrap (andronix): %s needs %d argument(s)", a, n)
		}
		switch a {
		case "--clearenv":
			env = nil
		case "--setenv":
			env = setEnv(env, args[i+1], args[i+2])
		case "--unsetenv":
			env = setEnv(env, args[i+1], "\x00")
		case "--chdir":
			chdir = args[i+1]
		}
		i += 1 + n
	}
	cmd := args[i:]
	if len(cmd) == 0 {
		return fmt.Errorf("bwrap (andronix): no command given")
	}
	// glycin 3 probes bwrap with `... --unshare-all ... /usr/bin/true`.
	// Answer the way bwrap itself does on a system without user
	// namespaces, which is the truth here: glycin then runs its loaders
	// unsandboxed, and without the RLIMIT_AS cap it puts on sandboxed
	// loaders (80% of MemAvailable+SwapFree minus 200 MB; on a phone that
	// is too small and every image load fails: black desktop on Fedora).
	if cmd[0] == "/usr/bin/true" && len(cmd) == 1 && contains(args, "--unshare-all") {
		logShim("probe", cmd)
		fmt.Fprintln(os.Stderr, "bwrap: Creating new namespace failed: Operation not permitted (andronix: no user namespaces under proot)")
		os.Exit(1)
	}
	logShim("run", cmd)
	if chdir != "" {
		if err := os.Chdir(chdir); err != nil {
			return err
		}
	}
	path := cmd[0]
	if !strings.Contains(path, "/") {
		p, err := sys.LookPath(path)
		if err != nil {
			return err
		}
		path = p
	}
	return syscall.Exec(path, cmd, env)
}

// logShim records what the shim did in /tmp/andronix-bwrap.log (<64 KB).
func logShim(what string, cmd []string) {
	const logf = "/tmp/andronix-bwrap.log"
	if st, err := os.Stat(logf); err == nil && st.Size() > 64<<10 {
		os.Remove(logf)
	}
	if f, err := os.OpenFile(logf, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o666); err == nil {
		fmt.Fprintf(f, "%s uid=%d %s\n", what, os.Getuid(), strings.Join(cmd, " "))
		f.Close()
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func setEnv(env []string, k, v string) []string {
	out := env[:0:0]
	for _, e := range env {
		if !strings.HasPrefix(e, k+"=") {
			out = append(out, e)
		}
	}
	if v != "\x00" {
		out = append(out, k+"="+v)
	}
	return out
}
