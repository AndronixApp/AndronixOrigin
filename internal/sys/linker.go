package sys

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Play Store Termux (targetSdk 29 and up) may not execve files in its data
// directory (Android's W^X). Its termux-exec LD_PRELOAD runs them through
// the system linker instead:
//
//	execve("/system/bin/linker64", [argv0, <path>, args...])
//
// with TERMUX_EXEC__PROC_SELF_EXE=<path>, and scripts as
// [interpreter, <prefixed interpreter>, [arg], <script>, args...]. Go
// execs without libc, so termux-exec never sees andronix's own execs;
// when andronix itself was started through the linker it does the same.

var (
	linkerPath string // the linker that started us; "" when started directly
	selfPath   string // our own path then (/proc/self/exe is the linker)
)

// FixLinkerArgs must run first in main. Started through the linker, Go
// sees the program's path as os.Args[1] (bionic moves argv only for C
// programs); it is dropped so commands parse as usual.
func FixLinkerArgs() {
	exe, _ := os.Readlink("/proc/self/exe")
	os.Args = fixLinkerArgs(exe, os.Getenv("TERMUX_EXEC__PROC_SELF_EXE"), os.Args)
}

func fixLinkerArgs(exe, self string, args []string) []string {
	if b := filepath.Base(exe); b != "linker" && b != "linker64" {
		return args
	}
	linkerPath = exe
	if len(args) > 1 && (args[1] == self || self == "" && isELF(args[1])) {
		selfPath, _ = filepath.Abs(args[1])
		return append([]string{args[0]}, args[2:]...)
	}
	return args
}

// appData is where W^X applies (a variable for tests).
var appData = "/data/"

// Executable is os.Executable, except that when started through the
// linker it is andronix's own path, not the linker's.
func Executable() (string, error) {
	if selfPath != "" {
		return selfPath, nil
	}
	return os.Executable()
}

// viaLinker rewrites an exec of path the way termux-exec would, when we
// were started through the linker and path is in app data. It returns the
// file to exec, its argv, and the TERMUX_EXEC__PROC_SELF_EXE value ("" for
// none).
func viaLinker(path string, argv []string) (string, []string, string) {
	if linkerPath == "" {
		return path, argv, ""
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if !strings.HasPrefix(path, appData) {
		return path, argv, ""
	}
	if interp, arg, ok := shebang(path); ok {
		ip := prefixed(interp)
		a := []string{interp}
		if !strings.HasPrefix(ip, appData) {
			a = []string{ip}
		}
		if arg != "" {
			a = append(a, arg)
		}
		a = append(append(a, path), argv[1:]...)
		if !strings.HasPrefix(ip, appData) {
			return ip, a, "" // e.g. #!/system/bin/sh
		}
		return linkerPath, append([]string{interp, ip}, a[1:]...), ip
	}
	if !isELF(path) {
		return path, argv, "" // let execve report it
	}
	return linkerPath, append([]string{argv[0], path}, argv[1:]...), path
}

// prefixed maps /bin/x and /usr/bin/x to Termux's, as termux-exec does.
func prefixed(p string) string {
	pre := Prefix()
	if pre == "" {
		return p
	}
	for _, d := range []string{"/bin/", "/usr/bin/"} {
		if strings.HasPrefix(p, d) {
			return filepath.Join(pre, "bin", strings.TrimPrefix(p, d))
		}
	}
	return p
}

// shebang reads "#!interpreter [arg]" from the start of a script.
func shebang(path string) (interp, arg string, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", false
	}
	defer f.Close()
	buf := make([]byte, 256)
	n, _ := f.Read(buf)
	buf = buf[:n]
	if !bytes.HasPrefix(buf, []byte("#!")) {
		return "", "", false
	}
	line, _, _ := bytes.Cut(buf[2:], []byte("\n"))
	fields := strings.SplitN(strings.TrimSpace(string(line)), " ", 2)
	if fields[0] == "" {
		return "", "", false
	}
	if len(fields) == 2 {
		arg = strings.TrimSpace(fields[1])
	}
	return fields[0], arg, true
}

func isELF(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	b := make([]byte, 4)
	n, _ := f.Read(b)
	return n == 4 && string(b) == "\x7fELF"
}

// Exec is syscall.Exec, through the linker when needed (see viaLinker).
func Exec(path string, argv, env []string) error {
	p, a, self := viaLinker(path, argv)
	if self != "" {
		env = append(withoutSelfExe(env), "TERMUX_EXEC__PROC_SELF_EXE="+self)
	}
	return syscall.Exec(p, a, env)
}

func withoutSelfExe(env []string) []string {
	out := make([]string, 0, len(env)+1)
	for _, e := range env {
		if !strings.HasPrefix(e, "TERMUX_EXEC__PROC_SELF_EXE=") {
			out = append(out, e)
		}
	}
	return out
}
