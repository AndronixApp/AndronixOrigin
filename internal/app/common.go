// Package app implements the andronix commands.
package app

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/log"

	"github.com/AndronixApp/andronix-distros/internal/conf"
	"github.com/AndronixApp/andronix-distros/internal/proot"
	"github.com/AndronixApp/andronix-distros/internal/sys"
	"github.com/AndronixApp/andronix-distros/internal/ui"
)

// Version is set at build time (-ldflags "-X .../app.Version=...").
var Version = "2.0.0-dev"

// VersionLabel is "v0.2.0" for releases, the git hash for dev builds.
func VersionLabel() string {
	if Version != "" && Version[0] >= '0' && Version[0] <= '9' {
		return "v" + Version
	}
	return Version
}

// LauncherMark tags start scripts we wrote, so we never touch others.
const LauncherMark = "ANDRONIX-LAUNCHER"

// Paths is the on-disk layout (DESIGN.md section 5).
type Paths struct {
	Home, Distros, Cache, Logs string
}

// GetPaths reads $ANDRONIX_HOME (default ~/.andronix).
func GetPaths() Paths {
	h := os.Getenv("ANDRONIX_HOME")
	if h == "" {
		h = filepath.Join(sys.Home(), ".andronix")
	}
	return Paths{Home: h, Distros: filepath.Join(h, "distros"), Cache: filepath.Join(h, "cache"), Logs: filepath.Join(h, "logs")}
}

// Inst is one installed (or partly installed) distro.
type Inst struct {
	D      *conf.Distro
	Dir    string
	Rootfs string
	State  string // install.conf
}

// Open returns the install paths for a distro.
func Open(d *conf.Distro) *Inst {
	dir := filepath.Join(GetPaths().Distros, d.ID)
	return &Inst{D: d, Dir: dir, Rootfs: filepath.Join(dir, "rootfs"), State: filepath.Join(dir, "install.conf")}
}

// Get reads a key from install.conf (same format the bash prototype uses).
func (in *Inst) Get(k string) string {
	b, err := os.ReadFile(in.State)
	if err != nil {
		return ""
	}
	return conf.Parse(b).Get(k)
}

// Set writes a key to install.conf.
func (in *Inst) Set(k, v string) error {
	os.MkdirAll(in.Dir, 0o755)
	vals := map[string]string{}
	var order []string
	if b, err := os.ReadFile(in.State); err == nil {
		sc := bufio.NewScanner(strings.NewReader(string(b)))
		for sc.Scan() {
			key, val, ok := strings.Cut(sc.Text(), "=")
			if ok {
				if _, seen := vals[key]; !seen {
					order = append(order, key)
				}
				vals[key] = val
			}
		}
	}
	if _, seen := vals[k]; !seen {
		order = append(order, k)
	}
	vals[k] = v
	var b strings.Builder
	for _, key := range order {
		fmt.Fprintf(&b, "%s=%s\n", key, vals[key])
	}
	tmp := in.State + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, in.State)
}

// Installed reports a finished install.
func (in *Inst) Installed() bool {
	_, err := os.Stat(in.Rootfs)
	return err == nil && in.Get("STAGE") == "done"
}

// Legacy returns the old installer's rootfs folders that exist.
func (in *Inst) Legacy() []string {
	var out []string
	for _, f := range in.D.LegacyFS {
		p := filepath.Join(sys.Home(), f)
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			out = append(out, p)
		}
	}
	return out
}

// Target is the proot view of this install, logging in as the default
// user (from first-boot setup) unless asRoot.
func (in *Inst) Target(asRoot bool) *proot.Target {
	t := &proot.Target{ID: in.D.ID, Dir: in.Dir, Rootfs: in.Rootfs, Shell: in.D.Shell, Lang: in.D.Lang}
	if in.D.BindsDir != "" {
		t.BindsDir = filepath.Join(sys.Home(), in.D.BindsDir)
	}
	if !asRoot {
		if b, err := os.ReadFile(filepath.Join(in.Rootfs, "etc/andronix/user")); err == nil {
			name := strings.TrimSpace(string(b))
			if uid, gid, home, ok := lookupUser(in.Rootfs, name); ok {
				t.User, t.UID, t.GID, t.Home = name, uid, gid, home
			}
		}
	}
	return t
}

func lookupUser(root, name string) (uid, gid, home string, ok bool) {
	b, err := os.ReadFile(filepath.Join(root, "etc/passwd"))
	if err != nil {
		return
	}
	for _, l := range strings.Split(string(b), "\n") {
		f := strings.Split(l, ":")
		if len(f) >= 6 && f[0] == name {
			return f[2], f[3], f[5], true
		}
	}
	return
}

// Logger writes the install log.
type Logger struct {
	*log.Logger
	Path string
	f    *os.File
}

// Printf satisfies ui.Logger.
func (l *Logger) Printf(format string, args ...any) { l.Logger.Printf(format, args...) }

// Close flushes the log.
func (l *Logger) Close() {
	if l.f != nil {
		l.f.Close()
	}
}

// NewLog opens ~/.andronix/logs/<name>-<time>.log and keeps the 10 newest.
func NewLog(name string) *Logger {
	p := GetPaths()
	os.MkdirAll(p.Logs, 0o755)
	path := filepath.Join(p.Logs, name+"-"+time.Now().Format("20060102-150405")+".log")
	f, err := os.Create(path)
	if err != nil {
		return &Logger{Logger: log.New(os.Stderr)}
	}
	lg := log.NewWithOptions(f, log.Options{ReportTimestamp: true, TimeFormat: time.TimeOnly})
	// Only the per-run logs (name-YYYYMMDD-HHMMSS.log) rotate; running
	// logs like termux.log and termux-x11.log stay.
	old, _ := filepath.Glob(filepath.Join(p.Logs, "*-[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]-[0-9][0-9][0-9][0-9][0-9][0-9].log"))
	sort.Strings(old)
	for len(old) > 10 {
		os.Remove(old[0])
		old = old[1:]
	}
	return &Logger{Logger: lg, Path: path, f: f}
}

// WriteLaunchers writes ~/start-<distro>.sh wrappers. An old installer's
// launcher of the same name is kept working as start-<distro>-old.sh.
// Returns the names of renamed launchers.
func WriteLaunchers(d *conf.Distro) ([]string, error) {
	self, err := sys.Executable()
	if err != nil {
		return nil, err
	}
	self, _ = filepath.EvalSymlinks(self)
	if p := os.Getenv("ANDRONIX_BIN"); p != "" {
		self = p
	}
	var renamed []string
	for _, name := range d.StartScripts {
		p := filepath.Join(sys.Home(), name)
		if b, err := os.ReadFile(p); err == nil && !strings.Contains(string(b), LauncherMark) {
			old := filepath.Join(sys.Home(), strings.TrimSuffix(name, ".sh")+"-old.sh")
			if _, err := os.Stat(old); err == nil {
				old = filepath.Join(sys.Home(), strings.TrimSuffix(name, ".sh")+"-old-"+time.Now().Format("20060102150405")+".sh")
			}
			if err := os.Rename(p, old); err != nil {
				return renamed, err
			}
			renamed = append(renamed, filepath.Base(old))
		}
		shell := "/data/data/com.termux/files/usr/bin/sh"
		if pre := sys.Prefix(); pre != "" {
			shell = filepath.Join(pre, "bin/sh")
		} else {
			shell = "/bin/sh"
		}
		script := fmt.Sprintf("#!%s\n# %s: %s. Written by andronix; runs 'andronix start %s'.\n"+
			"# Extra folders: put bind files in ~/%s/, e.g. a file containing\n"+
			"#   command+=\" -b /sdcard/Download:/root/Download\"\nexec %q start %s \"$@\"\n",
			shell, LauncherMark, d.Label(), d.ID, d.BindsDir, self, d.ID)
		os.Remove(p)
		if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
			return renamed, err
		}
	}
	return renamed, nil
}

// RemoveLaunchers deletes only the start scripts we wrote.
func RemoveLaunchers(d *conf.Distro) {
	for _, name := range d.StartScripts {
		p := filepath.Join(sys.Home(), name)
		if b, err := os.ReadFile(p); err == nil && strings.Contains(string(b), LauncherMark) {
			os.Remove(p)
		}
	}
}

// resolveDistro turns a user-given name into a distro, or explains.
func resolveDistro(name, cmd string) (*conf.Distro, error) {
	if name == "" {
		return nil, ui.Errorf("Which distro?", "Add the distro's name to the command.",
			"Run 'andronix list' to see them, e.g. andronix "+cmd+" debian")
	}
	d, err := conf.ResolveDistro(name)
	if err != nil {
		return nil, ui.Errorf("Unknown distro '"+name+"'", "Andronix doesn't have a distro called '"+name+"' yet.",
			"Run 'andronix list' to see the ones you can install.")
	}
	return d, nil
}
