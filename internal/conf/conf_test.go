package conf

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The app's Firestore keys must keep reaching the decided versions.
func TestAppKeys(t *testing.T) {
	for key, want := range map[string]string{
		"Debian": "debian", "debian": "debian",
		"Ubuntu22": "ubuntu", "ubuntu": "ubuntu", "resolute": "ubuntu",
		"Kali": "kali", "Fedora": "fedora",
		"Ubuntu20": "ubuntu24", "Ubuntu18": "ubuntu24", "noble": "ubuntu24", "ubuntu24": "ubuntu24",
	} {
		d, err := ResolveDistro(key)
		if err != nil || d.ID != want {
			t.Errorf("%s: got %v %v, want %s", key, d, err, want)
		}
	}
	u, _ := LoadDistro("ubuntu")
	if u.MainStart() != "start-ubuntu26.sh" || u.StartScripts[1] != "start-ubuntu22.sh" {
		t.Errorf("ubuntu 26.04 start scripts: %v", u.StartScripts)
	}
	u24, _ := LoadDistro("ubuntu24")
	if u24.MainStart() != "start-ubuntu.sh" || u24.StartScripts[1] != "start-ubuntu20.sh" {
		t.Errorf("ubuntu 24.04 start scripts: %v", u24.StartScripts)
	}
	k, _ := LoadDistro("kali")
	if k.MirrorRewrite[1] != "http://kali.download/kali/" {
		t.Errorf("kali mirror rewrite: %v", k.MirrorRewrite)
	}
	x, _ := ResolveDesktop("xfce")
	if len(x.AutostartHide) == 0 || len(x.Packages("dnf")) == 0 {
		t.Errorf("xfce: hide %v, dnf %v", x.AutostartHide, x.Packages("dnf"))
	}
	for _, k := range []string{"Node", "none", "xfce", "XFCE4", "lxqt", "mate"} {
		if _, err := ResolveDesktop(k); err != nil {
			t.Errorf("desktop %s: %v", k, err)
		}
	}
}

func TestPortBKeys(t *testing.T) {
	v, _ := LoadDistro("void")
	if v.MaxKernel != "" || v.Sudo != "nopasswd" {
		t.Errorf("void: %q %q", v.MaxKernel, v.Sudo)
	}
	mate, _ := ResolveDesktop("mate")
	if len(mate.DesktopEdits) != 2 || mate.DesktopEdits[0] != "caja.desktop:Exec=caja --force-desktop" {
		t.Errorf("mate edits: %q", mate.DesktopEdits)
	}
	a, _ := LoadDistro("arch")
	if a.UpstreamTarball("aarch64") == "" || len(a.UpstreamRemove()) == 0 || a.UpstreamTarball("x86_64") != "" {
		t.Errorf("arch upstream keys")
	}
	m, _ := LoadDistro("manjaro")
	if m.MirrorFor("aarch64") != "https://mirrors.manjaro.org/repo/arm-stable/$repo/$arch" || m.Lang != "en_US.UTF-8" {
		t.Errorf("manjaro: %q %q", m.MirrorFor("aarch64"), m.Lang)
	}
}

// Every UnModdedOS.<Distro>.Install.{DE,WM}.<Name> key in the app's links
// (testdata/app-keys.tsv, from the Firestore backup) must resolve, lead to
// a desktop offered on that distro, and have its installer in the shims map.
// ANDRONIX_LINKS_TSV points at a fresher export.
func TestFirestoreKeys(t *testing.T) {
	path := "testdata/app-keys.tsv"
	if p := os.Getenv("ANDRONIX_LINKS_TSV"); p != "" {
		path = p
	}
	rows, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	shims := map[string][2]string{}
	km, err := os.ReadFile("../../compat/keys.conf")
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range strings.Split(string(km), "\n") {
		if f := strings.Fields(l); len(f) == 3 && !strings.HasPrefix(l, "#") && strings.Contains(f[0], "/") {
			shims[f[0]] = [2]string{f[1], f[2]}
		}
	}
	n := 0
	for _, l := range strings.Split(strings.TrimSpace(string(rows)), "\n") {
		f := strings.Split(l, "\t")
		if len(f) != 4 {
			t.Fatalf("bad row %q", l)
		}
		key := "UnModdedOS." + f[0] + ".Install." + f[1] + "." + f[2]
		d, err := ResolveDistro(f[0])
		if err != nil {
			t.Errorf("%s: distro: %v", key, err)
			continue
		}
		de, _, err := ResolveDesktopCompat(f[2])
		if err != nil {
			t.Errorf("%s: desktop: %v", key, err)
			continue
		}
		if !de.Supports(d) {
			t.Errorf("%s: %s isn't offered on %s", key, de.ID, d.ID)
		}
		if f[3] == "-" {
			continue
		}
		n++
		s, ok := shims[f[3]]
		if !ok {
			t.Errorf("%s: %s missing from compat/keys.conf", key, f[3])
			continue
		}
		sd, _ := ResolveDistro(s[0])
		sde, _, _ := ResolveDesktopCompat(s[1])
		if sd == nil || sde == nil || sd.ID != d.ID || sde.ID != de.ID {
			t.Errorf("%s: shim installs %v, want %s %s", key, s, d.ID, de.ID)
		}
	}
	if n < 60 {
		t.Errorf("only %d installer keys checked", n)
	}
}

// KDE is offered only where it's verified; the others everywhere.
func TestSupports(t *testing.T) {
	kde, _ := ResolveDesktop("kde")
	for id, want := range map[string]bool{"debian": true, "ubuntu": true, "kali": true, "ubuntu24": false, "fedora": false, "arch": false} {
		d, _ := LoadDistro(id)
		if kde.Supports(d) != want {
			t.Errorf("kde on %s: want %v", id, want)
		}
	}
	for _, id := range DistroIDs() {
		d, _ := LoadDistro(id)
		for _, de := range []string{"xfce", "lxqt", "mate", "none"} {
			if x, _ := ResolveDesktop(de); !x.Supports(d) {
				t.Errorf("%s on %s not offered", de, id)
			}
		}
	}
}

// The Firestore links backup, when it's on this machine, must not hold a
// key testdata/app-keys.tsv lacks (regenerate the TSV if it does).
func TestFirestoreBackup(t *testing.T) {
	path := os.Getenv("ANDRONIX_LINKS_JSON") // an export of the app's links, if you have one
	if path == "" {
		t.Skip("set ANDRONIX_LINKS_JSON to check an export of the app's links")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Skip("no links backup:", err)
	}
	var links struct {
		UnModdedOS map[string]struct {
			Install map[string]map[string]any
		}
	}
	if err := json.Unmarshal(b, &links); err != nil {
		t.Fatal(err)
	}
	tsv, _ := os.ReadFile("testdata/app-keys.tsv")
	for distro, v := range links.UnModdedOS {
		for kind, names := range v.Install {
			for name := range names {
				if !strings.Contains(string(tsv), distro+"\t"+kind+"\t"+name+"\t") {
					t.Errorf("UnModdedOS.%s.Install.%s.%s missing from testdata/app-keys.tsv", distro, kind, name)
				}
			}
		}
	}
}

// ModdedOS.Legacy.<Name>.{Install,Uninstall} in compat/keys.conf: every
// archived edition has both, installs name a known edition, and uninstall
// removes that edition's distro.
func TestModdedKeys(t *testing.T) {
	km, err := os.ReadFile("../../compat/keys.conf")
	if err != nil {
		t.Fatal(err)
	}
	install, remove := map[string]*Edition{}, map[string]string{}
	for _, l := range strings.Split(string(km), "\n") {
		f := strings.Fields(l)
		if len(f) < 2 || !strings.HasPrefix(f[0], "ModdedOS.") {
			continue
		}
		switch {
		case strings.HasSuffix(f[0], ".Install"):
			name := strings.TrimSuffix(strings.TrimPrefix(f[0], "ModdedOS."), ".Install")
			var ed *Edition
			for i := 1; i+1 < len(f); i++ {
				if f[i] == "--edition" {
					if ed, err = ResolveEdition(f[i+1]); err != nil {
						t.Errorf("%s: %v", f[0], err)
					}
				}
			}
			if f[1] != "install" || ed == nil || !strings.Contains(l, "--token '@TOKEN@'") {
				t.Errorf("%s: want install --edition <id> --token '@TOKEN@': %q", f[0], l)
			}
			install[name] = ed
		case strings.HasSuffix(f[0], ".Uninstall"):
			if len(f) != 3 || f[1] != "remove" {
				t.Errorf("%s: want remove <distro>: %q", f[0], l)
			}
			remove[strings.TrimSuffix(strings.TrimPrefix(f[0], "ModdedOS."), ".Uninstall")] = f[len(f)-1]
		}
	}
	for _, n := range []string{"Legacy.UbuntuXfce", "Legacy.UbuntuKde", "Legacy.Manjaro", "Legacy.Debian"} {
		ed := install[n]
		if ed == nil {
			t.Errorf("ModdedOS.%s.Install missing", n)
			continue
		}
		if !ed.Legacy() || remove[n] != ed.Distro {
			t.Errorf("ModdedOS.%s: edition %s, uninstall removes %q", n, ed.ID, remove[n])
		}
	}
	for _, id := range EditionIDs() {
		ed, _ := ResolveEdition(id)
		d, err := LoadDistro(ed.Distro)
		de, err2 := ResolveDesktop(ed.Desktop)
		if err != nil || err2 != nil || !de.Supports(d) {
			t.Errorf("edition %s: %s with %s isn't offered", id, ed.Distro, ed.Desktop)
		}
	}
}

// Ubuntu 26.04 switches to GNU coreutils before upgrades (proot and the
// Rust coreutils' multicall check); 24.04 already is GNU.
func TestPreUpgrade(t *testing.T) {
	u, _ := LoadDistro("ubuntu")
	if !strings.Contains(u.PreUpgrade, "coreutils-from-gnu coreutils-from-uutils-") || !strings.Contains(u.PreUpgrade, "--allow-remove-essential") {
		t.Errorf("ubuntu PreUpgrade: %q", u.PreUpgrade)
	}
	for _, id := range DistroIDs() {
		if d, _ := LoadDistro(id); id != "ubuntu" && d.PreUpgrade != "" {
			t.Errorf("%s: unexpected PreUpgrade %q", id, d.PreUpgrade)
		}
	}
}

func TestEarlyEdition(t *testing.T) {
	d := t.TempDir()
	os.WriteFile(filepath.Join(d, "editions.conf"), []byte("fedora-xfce=\"fedora xfce early\"\nbad=\"debian xfce soon\"\nplain=\"debian xfce\"\n"), 0o644)
	t.Setenv("ANDRONIX_DATA", d)
	if e, err := ResolveEdition("fedora-xfce"); err != nil || !e.Early || e.Distro != "fedora" {
		t.Errorf("early: %+v %v", e, err)
	}
	if e, err := ResolveEdition("plain"); err != nil || e.Early {
		t.Errorf("plain: %+v %v", e, err)
	}
	if _, err := ResolveEdition("bad"); err == nil {
		t.Error("a third field other than 'early' must be refused")
	}
}
