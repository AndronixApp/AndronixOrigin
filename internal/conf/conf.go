// Package conf reads the small KEY=value data files that describe each
// distro and desktop. The same files drive the bash prototype, so they
// stay plain shell assignments: KEY=value or KEY="value", # comments.
package conf

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	andronix "github.com/AndronixApp/andronix-distros"
)

// Values is one parsed file.
type Values map[string]string

// Parse reads KEY=value lines.
func Parse(data []byte) Values {
	v := Values{}
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if i := strings.Index(val, " #"); i >= 0 && !strings.HasPrefix(val, `"`) {
			val = val[:i]
		}
		val = strings.TrimSpace(val)
		if len(val) >= 2 && (val[0] == '"' || val[0] == '\'') && val[len(val)-1] == val[0] {
			val = val[1 : len(val)-1]
		}
		v[strings.TrimSpace(k)] = val
	}
	return v
}

func (v Values) Get(k string) string    { return v[k] }
func (v Values) List(k string) []string { return strings.Fields(v[k]) }
func (v Values) Int(k string) int       { n, _ := strconv.Atoi(v[k]); return n }
func (v Values) Bool(k string) bool {
	b := strings.ToLower(v[k])
	return b == "1" || b == "yes" || b == "true"
}

// Distro is distros/<id>.conf.
type Distro struct {
	ID, Name, Version, Codename, Family, Image, Shell string
	Arches, BasePkgs, StartScripts, LegacyFS, Aliases []string
	Browser, BrowserRepo                              string
	// MirrorRewrite is DISTRO_MIRROR_REWRITE="<from> <to>": a literal
	// replace in the image's package sources (Kali's redirector sends
	// downloads to https mirrors before a CA store exists).
	MirrorRewrite [2]string
	BrowserArches []string
	// BrowserCodecs are DISTRO_BROWSER_CODECS: libraries the browser loads
	// for H.264 (YouTube falls back to it on phones), installed with it.
	BrowserCodecs      []string
	NoSnap             bool
	DownloadMB, DiskMB int
	BindsDir           string
	// Lang is DISTRO_LANG (default C.UTF-8).
	Lang string
	// Sudo is DISTRO_SUDO: "" (password), "nopasswd" (Void: password
	// rules fail under proot) or "none" (Manjaro's sudo refuses).
	Sudo string
	// PreUpgrade is DISTRO_PRE_UPGRADE: shell run as root after each
	// package refresh, before any upgrade (Ubuntu 26.04: GNU coreutils).
	PreUpgrade string
	// MaxKernel is DISTRO_MAX_KERNEL, e.g. "6.5": newer host kernels
	// can't run this distro yet (Void: glibc's fchmodat2 vs proot).
	MaxKernel string
	raw       Values
}

// UpstreamTarball is DISTRO_UPSTREAM_TARBALL_<arch>: the distro's own
// rootfs tarball for CPUs with no OCI image (Arch Linux ARM).
func (d *Distro) UpstreamTarball(arch string) string {
	return d.raw.Get("DISTRO_UPSTREAM_TARBALL_" + arch)
}

// UpstreamRemove is DISTRO_UPSTREAM_REMOVE: packages to strip from an
// upstream tarball (kernel, firmware, network daemons).
func (d *Distro) UpstreamRemove() []string { return d.raw.List("DISTRO_UPSTREAM_REMOVE") }

// MirrorFor is DISTRO_MIRROR_<arch>: the package mirror to use, if any.
func (d *Distro) MirrorFor(arch string) string { return d.raw.Get("DISTRO_MIRROR_" + arch) }

// Label is "Debian 13".
func (d *Distro) Label() string { return d.Name + " " + d.Version }

// MainStart is the first start script, e.g. start-debian.sh.
func (d *Distro) MainStart() string {
	if len(d.StartScripts) == 0 {
		return "start-" + d.ID + ".sh"
	}
	return d.StartScripts[0]
}

// HasArch reports whether the distro ships for arch.
func (d *Distro) HasArch(arch string) bool { return contains(d.Arches, arch) }

// BrowserFor returns the browser package for arch, or "".
func (d *Distro) BrowserFor(arch string) string {
	if d.Browser == "" {
		return ""
	}
	if len(d.BrowserArches) > 0 && !contains(d.BrowserArches, arch) {
		return ""
	}
	return d.Browser
}

// Desktop is desktops/<id>.conf.
type Desktop struct {
	ID, Name, Session string
	DiskMB, MinRAMMB  int
	// Distros is DE_DISTROS: the distros this desktop is offered on
	// (empty: every distro whose family has a package list).
	Distros []string
	// The light profile (andronix tune): DE_LIGHT_AUTOSTART_HIDE,
	// DE_LIGHT_XDG_CONFIG ("<file>:<group>:<key>=<value>;..."),
	// DE_LIGHT_XFCONF ("<channel>:/prop/path:<type>:<value>;...") and
	// DE_LIGHT_GSETTINGS ("<schema> <key> <value>;...").
	LightAutostartHide                          []string
	LightXDGConfig, LightXfconf, LightGSettings []string
	// XDGConfig is DE_XDG_CONFIG="<file>:<group>:<key>=<value>;...":
	// defaults written to /etc/xdg/<file> (KDE's system-wide config).
	XDGConfig []string
	// DesktopEdits is DE_DESKTOP_EDITS="<file.desktop>:<Key>=<value>;...":
	// copies of /usr/share/applications entries in
	// /usr/local/share/applications with keys changed (MATE: caja).
	DesktopEdits []string
	// AutostartHide lists /etc/xdg/autostart entries that fail under proot
	// (no polkitd, logind or upower); they are hidden for every user.
	AutostartHide []string
	raw           Values
}

// Packages for a package-manager family.
func (d *Desktop) Packages(family string) []string { return d.raw.List("DE_PKGS_" + family) }

// OptionalPackages are installed only where the distro has them.
func (d *Desktop) OptionalPackages(family string) []string {
	return d.raw.List("DE_PKGS_OPTIONAL_" + family)
}

// Supports reports whether this desktop is offered on distro d.
func (de *Desktop) Supports(d *Distro) bool {
	if de.None() {
		return true
	}
	if len(de.Distros) > 0 && !contains(de.Distros, d.ID) {
		return false
	}
	return len(de.Packages(d.Family)) > 0
}

// SupportedOn lists the distros offering this desktop (labels).
func (de *Desktop) SupportedOn() []string {
	var out []string
	for _, id := range DistroIDs() {
		if d, err := LoadDistro(id); err == nil && de.Supports(d) {
			out = append(out, d.Label())
		}
	}
	return out
}

// None is the command-line-only desktop.
func (d *Desktop) None() bool { return d.ID == "none" }

// The data is built into the binary. ANDRONIX_DATA=<dir> (with distros/
// and desktops/ inside) adds to it and overrides same-named files, for
// testing a new .conf without a rebuild. Nothing else on disk is read.
func overlay() string { return os.Getenv("ANDRONIX_DATA") }

func read(dir, id string) (Values, error) {
	if o := overlay(); o != "" {
		if b, err := os.ReadFile(filepath.Join(o, dir, id+".conf")); err == nil {
			return Parse(b), nil
		}
	}
	b, err := fs.ReadFile(andronix.Data, path.Join(dir, id+".conf"))
	if err != nil {
		return nil, err
	}
	return Parse(b), nil
}

func ids(dir string) []string {
	seen := map[string]bool{}
	add := func(name string) {
		if strings.HasSuffix(name, ".conf") {
			seen[strings.TrimSuffix(name, ".conf")] = true
		}
	}
	entries, _ := fs.ReadDir(andronix.Data, dir)
	for _, e := range entries {
		add(e.Name())
	}
	if o := overlay(); o != "" {
		disk, _ := os.ReadDir(filepath.Join(o, dir))
		for _, e := range disk {
			add(e.Name())
		}
	}
	var out []string
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// DistroIDs lists every distro we know.
func DistroIDs() []string { return ids("distros") }

// DesktopIDs lists every desktop we know.
func DesktopIDs() []string { return ids("desktops") }

// LoadDistro reads distros/<id>.conf.
func LoadDistro(id string) (*Distro, error) {
	v, err := read("distros", id)
	if err != nil {
		return nil, fmt.Errorf("unknown distro %q", id)
	}
	d := &Distro{
		ID: v.Get("DISTRO_ID"), Name: v.Get("DISTRO_NAME"), Version: v.Get("DISTRO_VERSION"),
		Codename: v.Get("DISTRO_CODENAME"), Family: v.Get("DISTRO_FAMILY"), Image: v.Get("DISTRO_IMAGE"),
		Shell: v.Get("DISTRO_SHELL"), Arches: v.List("DISTRO_ARCHES"), BasePkgs: v.List("DISTRO_BASE_PKGS"),
		StartScripts: v.List("DISTRO_START_SCRIPTS"), LegacyFS: v.List("DISTRO_LEGACY_FS"),
		Aliases: v.List("DISTRO_ALIASES"), Browser: v.Get("DISTRO_BROWSER"),
		BrowserRepo: v.Get("DISTRO_BROWSER_REPO"), BrowserArches: v.List("DISTRO_BROWSER_ARCHES"),
		BrowserCodecs: v.List("DISTRO_BROWSER_CODECS"),
		NoSnap:        v.Bool("DISTRO_NO_SNAP"), DownloadMB: v.Int("DISTRO_DOWNLOAD_MB"), DiskMB: v.Int("DISTRO_DISK_MB"),
		BindsDir: v.Get("DISTRO_BINDS_DIR"), Lang: v.Get("DISTRO_LANG"), Sudo: v.Get("DISTRO_SUDO"),
		PreUpgrade: v.Get("DISTRO_PRE_UPGRADE"),
		MaxKernel:  v.Get("DISTRO_MAX_KERNEL"), raw: v,
	}
	if f := v.List("DISTRO_MIRROR_REWRITE"); len(f) == 2 {
		d.MirrorRewrite = [2]string{f[0], f[1]}
	}
	if d.ID == "" {
		d.ID = id
	}
	if d.Shell == "" {
		d.Shell = "/bin/sh"
	}
	return d, nil
}

// ResolveDistro accepts ids, aliases and the app's keys (any case).
func ResolveDistro(name string) (*Distro, error) {
	want := strings.ToLower(strings.TrimSpace(name))
	if want == "" {
		return nil, fmt.Errorf("no distro given")
	}
	for _, id := range DistroIDs() {
		if id == want {
			return LoadDistro(id)
		}
	}
	for _, id := range DistroIDs() {
		d, err := LoadDistro(id)
		if err != nil {
			continue
		}
		for _, a := range d.Aliases {
			if strings.ToLower(a) == want {
				return d, nil
			}
		}
	}
	return nil, fmt.Errorf("unknown distro %q", name)
}

// ResolveDesktopCompat is ResolveDesktop plus the app's retired options:
// LXDE installs LXQt, and Enlightenment and the window managers (i3,
// Awesome, Openbox) install XFCE. notice says so ("" when nothing was
// substituted).
func ResolveDesktopCompat(name string) (de *Desktop, notice string, err error) {
	switch n := strings.ToLower(strings.TrimSpace(name)); n {
	case "lxde":
		de, err = ResolveDesktop("lxqt")
		return de, "LXDE isn't offered any more. Installing LXQt instead: the same light, classic desktop, still maintained.", err
	case "enlightenment", "el", "i3", "awesome", "openbox":
		de, err = ResolveDesktop("xfce")
		return de, strings.Title(n) + " isn't offered any more. Installing XFCE instead, which works the same way.", err
	}
	de, err = ResolveDesktop(name)
	return de, "", err
}

// ResolveDesktop accepts ids and the app's names ("Node" is CLI only).
func ResolveDesktop(name string) (*Desktop, error) {
	want := strings.ToLower(strings.TrimSpace(name))
	switch want {
	case "", "node", "cli", "no", "none":
		want = "none"
	case "xfce4":
		want = "xfce"
	}
	v, err := read("desktops", want)
	if err != nil {
		return nil, fmt.Errorf("unknown desktop %q", name)
	}
	return &Desktop{ID: v.Get("DE_ID"), Name: v.Get("DE_NAME"), Session: v.Get("DE_SESSION"),
		DiskMB: v.Int("DE_DISK_MB"), MinRAMMB: v.Int("DE_MIN_RAM_MB"), AutostartHide: v.List("DE_AUTOSTART_HIDE"),
		Distros:            v.List("DE_DISTROS"),
		DesktopEdits:       splitNonEmpty(v.Get("DE_DESKTOP_EDITS"), ";"),
		XDGConfig:          splitNonEmpty(v.Get("DE_XDG_CONFIG"), ";"),
		LightAutostartHide: v.List("DE_LIGHT_AUTOSTART_HIDE"), LightXDGConfig: splitNonEmpty(v.Get("DE_LIGHT_XDG_CONFIG"), ";"),
		LightXfconf: splitNonEmpty(v.Get("DE_LIGHT_XFCONF"), ";"), LightGSettings: splitNonEmpty(v.Get("DE_LIGHT_GSETTINGS"), ";"),
		raw: v}, nil
}

func splitNonEmpty(s, sep string) []string {
	var out []string
	for _, p := range strings.Split(s, sep) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// Pack is a dev pack, mods/packs/<id>.conf (docs/packs.md).
type Pack struct {
	ID, Name, Desc string
	// Files are PACK_FILES entries: src (in mods/packs/files), dest, mode.
	Files                  []PackFile
	Post, Check, PreRemove string
	// Purge is PACK_PURGE: data the pack's programs made (databases),
	// deleted only by remove --purge.
	Purge  []string
	Hint   string
	DiskMB int
	raw    Values
}

// PackFile is one PACK_FILES entry.
type PackFile struct {
	Src, Dest string
	Mode      os.FileMode
}

// Packages is PACK_PKGS_<family>: tokens like "a|b" (the first one the
// distro has) and "x?" (optional).
func (p *Pack) Packages(family string) []string { return p.raw.List("PACK_PKGS_" + family) }

const packDir = "mods/packs"

// PackIDs lists every dev pack.
func PackIDs() []string { return ids(packDir) }

// LoadPack reads mods/packs/<id>.conf.
func LoadPack(id string) (*Pack, error) {
	v, err := read(packDir, strings.ToLower(id))
	if err != nil {
		return nil, fmt.Errorf("unknown pack %q", id)
	}
	p := &Pack{ID: v.Get("PACK_ID"), Name: v.Get("PACK_NAME"), Desc: v.Get("PACK_DESC"),
		Post: v.Get("PACK_POST"), Check: v.Get("PACK_CHECK"), PreRemove: v.Get("PACK_PRE_REMOVE"),
		Hint: v.Get("PACK_HINT"), DiskMB: v.Int("PACK_DISK_MB"), raw: v}
	for _, d := range v.List("PACK_PURGE") {
		// Globs only in the last element, never a bare "*".
		if !strings.HasPrefix(d, "/") || strings.Count(strings.Trim(d, "/"), "/") < 1 || strings.Contains(d, "..") ||
			strings.ContainsAny(filepath.Dir(d), "*?[") || strings.Trim(filepath.Base(d), "*?") == "" {
			return nil, fmt.Errorf("pack %s: PACK_PURGE entry %q must be an absolute path two levels deep or more", id, d)
		}
		p.Purge = append(p.Purge, d)
	}
	for _, f := range v.List("PACK_FILES") {
		s := strings.SplitN(f, ":", 3)
		if len(s) != 3 || strings.Contains(s[0], "/") || !strings.HasPrefix(s[1], "/") {
			return nil, fmt.Errorf("pack %s: bad PACK_FILES entry %q", id, f)
		}
		m, err := strconv.ParseUint(s[2], 8, 32)
		if err != nil {
			return nil, fmt.Errorf("pack %s: bad mode in %q", id, f)
		}
		p.Files = append(p.Files, PackFile{Src: s[0], Dest: s[1], Mode: os.FileMode(m)})
	}
	return p, nil
}

// PackFileData returns a PACK_FILES source from mods/packs/files.
func PackFileData(name string) ([]byte, error) {
	if o := overlay(); o != "" {
		if b, err := os.ReadFile(filepath.Join(o, packDir, "files", name)); err == nil {
			return b, nil
		}
	}
	return fs.ReadFile(andronix.Data, path.Join(packDir, "files", name))
}

// Edition is a Modded edition from editions.conf.
type Edition struct {
	ID, Distro, Desktop string
	// Early marks an edition in early access (Premium and Modded Pass
	// owners first): "<distro> <desktop> early" in editions.conf.
	Early bool
}

// Legacy reports an archived edition (legacy-*).
func (e *Edition) Legacy() bool { return strings.HasPrefix(e.ID, "legacy-") }

func editions() Values {
	if o := overlay(); o != "" {
		if b, err := os.ReadFile(filepath.Join(o, "editions.conf")); err == nil {
			return Parse(b)
		}
	}
	b, _ := fs.ReadFile(andronix.Data, "editions.conf")
	return Parse(b)
}

// EditionIDs lists the Modded editions.
func EditionIDs() []string {
	var out []string
	for id := range editions() {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// ResolveEdition reads one edition from editions.conf.
func ResolveEdition(id string) (*Edition, error) {
	id = strings.ToLower(strings.TrimSpace(id))
	f := strings.Fields(editions()[id])
	if len(f) < 2 || len(f) > 3 || len(f) == 3 && f[2] != "early" {
		return nil, fmt.Errorf("unknown edition %q", id)
	}
	return &Edition{ID: id, Distro: f[0], Desktop: f[1], Early: len(f) == 3}, nil
}
