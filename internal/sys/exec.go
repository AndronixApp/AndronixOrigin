package sys

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// LookPath, Command and CommandContext stand in for os/exec's. Go's
// exec.LookPath checks with faccessat2, which Android's seccomp policy
// before Android 12 answers with SIGSYS instead of ENOSYS, killing the
// process. The GOOS=android build avoids that in the standard library;
// these keep the GOOS=linux copy inside distros (and any future change
// there) safe too: a stat and the mode bits are all Android needs, since
// nothing there runs setuid.
func LookPath(file string) (string, error) {
	if strings.Contains(file, "/") {
		if executable(file) {
			return file, nil
		}
		return "", &exec.Error{Name: file, Err: exec.ErrNotFound}
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			dir = "."
		}
		if p := filepath.Join(dir, file); executable(p) {
			if !filepath.IsAbs(p) {
				p = "./" + p
			}
			return p, nil
		}
	}
	return "", &exec.Error{Name: file, Err: exec.ErrNotFound}
}

func executable(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular() && fi.Mode().Perm()&0o111 != 0
}

// Command is exec.Command with the lookup above.
func Command(name string, arg ...string) *exec.Cmd {
	return CommandContext(nil, name, arg...)
}

// CommandContext is exec.CommandContext with the lookup above (ctx may be
// nil for no context).
func CommandContext(ctx context.Context, name string, arg ...string) *exec.Cmd {
	p, err := LookPath(name)
	if err != nil {
		// Start reports err, the same as exec's own failed lookup.
		return &exec.Cmd{Path: name, Args: append([]string{name}, arg...), Err: err}
	}
	p, argv, self := viaLinker(p, append([]string{name}, arg...))
	var c *exec.Cmd
	if ctx == nil {
		c = exec.Command(p, argv[1:]...)
	} else {
		c = exec.CommandContext(ctx, p, argv[1:]...)
	}
	c.Args = argv
	if self != "" {
		// Callers that set their own Env keep theirs (proot's drops
		// LD_PRELOAD anyway).
		c.Env = append(withoutSelfExe(os.Environ()), "TERMUX_EXEC__PROC_SELF_EXE="+self)
	}
	return c
}

// Copy is io.Copy with plain reads and writes. Between two files, io.Copy
// uses copy_file_range (on kernels from 5.3), which Android's seccomp
// policy may not allow on older releases: SIGSYS rather than ENOSYS.
func Copy(dst io.Writer, src io.Reader) (int64, error) {
	return io.Copy(struct{ io.Writer }{dst}, struct{ io.Reader }{src})
}
