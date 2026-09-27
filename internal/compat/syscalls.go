package compat

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/AndronixApp/andronix-distros/internal/sys"
)

// Syscalls are what the probe tries, in order.
var Syscalls = []string{"statx", "statx_mnt_id", "openat2", "faccessat2", "fchmodat2", "close_range", "open_tree",
	"name_to_handle_at", "memfd_create", "clone3", "seccomp"}

// RunAll runs every probe syscall in its own child (`self __probe one
// <name>`), so a seccomp kill costs only that child: it's recorded as the
// signal's name (SIGSYS). `andronix __probe` inside a distro prints one
// "name result" line each.
func RunAll(self string) map[string]string {
	out := map[string]string{}
	for _, n := range Syscalls {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		b, err := sys.CommandContext(ctx, self, "__probe", "one", n).Output()
		cancel()
		var ee *exec.ExitError
		switch {
		case err == nil:
			out[n] = strings.TrimSpace(string(b))
		case errors.As(err, &ee) && ee.ProcessState != nil:
			if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
				out[n] = unix.SignalName(ws.Signal())
			} else {
				out[n] = fmt.Sprintf("exit%d", ee.ExitCode())
			}
		case ctx.Err() != nil:
			out[n] = "timeout"
		default:
			out[n] = "error"
		}
		if out[n] == "" {
			out[n] = "error"
		}
	}
	return out
}

// ParseLines reads RunAll's "name result" lines.
func ParseLines(s string) map[string]string {
	out := map[string]string{}
	for _, l := range strings.Split(s, "\n") {
		if f := strings.Fields(l); len(f) == 2 {
			for _, n := range Syscalls {
				if f[0] == n {
					out[n] = f[1]
				}
			}
		}
	}
	return out
}
