package termux

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakePulse puts pulseaudio, pactl and pkg stand-ins on PATH. They keep
// their state in dir: "running" (daemon up), "modules" (pactl's short
// module list) and "calls" (every invocation).
func fakePulse(t *testing.T, installed, running bool, modules string) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	os.MkdirAll(bin, 0o755)
	w := func(name, body string) {
		n := strings.TrimSuffix(name, ".new")
		os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\necho \""+n+" $*\" >>"+dir+"/calls\n"+body), 0o755)
	}
	pa := `case "$1" in
--check) [ -e ` + dir + `/running ] ;;
--start) touch ` + dir + `/running ;;
esac
`
	if installed {
		w("pulseaudio", pa)
	}
	w("pulseaudio.new", pa) // what apt-get installs
	os.Rename(filepath.Join(bin, "pulseaudio.new"), filepath.Join(dir, "pulseaudio.new"))
	w("apt-get", `case "$*" in *"install pulseaudio"*) cp `+dir+`/pulseaudio.new `+bin+`/pulseaudio && echo "Setting up pulseaudio" ;; esac
`)
	w("pactl", `case "$1" in
list) cat `+dir+`/modules ;;
load-module) printf '99\t%s\t%s\t\n' "$2" "$3" >>`+dir+`/modules ;;
unload-module) grep -v "^$2	" `+dir+`/modules >`+dir+`/m; mv `+dir+`/m `+dir+`/modules ;;
esac
`)
	os.WriteFile(filepath.Join(dir, "modules"), []byte(modules), 0o644)
	if running {
		os.WriteFile(filepath.Join(dir, "running"), nil, 0o644)
	}
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	t.Setenv("ANDRONIX_HOME", filepath.Join(dir, "home"))
	t.Setenv("PULSE_SERVER", "127.0.0.1")
	old := isTermux
	isTermux = func() bool { return true }
	t.Cleanup(func() { isTermux = old })
	return dir
}

func calls(dir string) string { b, _ := os.ReadFile(filepath.Join(dir, "calls")); return string(b) }

func TestEnsureAudioInstallsAndStarts(t *testing.T) {
	dir := fakePulse(t, false, false, "0\tmodule-device-restore\t\t\n")
	var lines []string
	did, err := EnsureAudio(context.Background(), true, func(l string) { lines = append(lines, l) })
	if err != nil {
		t.Fatal(err)
	}
	if did != "installed PulseAudio, started PulseAudio, enabled sound for distros" {
		t.Fatalf("did = %q", did)
	}
	c := calls(dir)
	for _, want := range []string{"apt-get -y -o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold install pulseaudio", "pulseaudio --start --exit-idle-time=-1", "pactl load-module module-native-protocol-tcp " + TCPArgs} {
		if !strings.Contains(c, want) {
			t.Errorf("missing call %q in:\n%s", want, c)
		}
	}
	if strings.Contains(c, "--load") {
		t.Errorf("started with --load (clashes with a TCP module in default.pa):\n%s", c)
	}
	if len(lines) == 0 {
		t.Error("pkg output not passed on")
	}
	// Second run: nothing to do.
	if did, err := EnsureAudio(context.Background(), true, nil); err != nil || did != "" {
		t.Fatalf("second run did %q, %v", did, err)
	}
	if n := strings.Count(calls(dir), "load-module"); n != 1 {
		t.Fatalf("module loaded %d times", n)
	}
	if b, _ := os.ReadFile(LogFile()); !strings.Contains(string(b), "sound: loaded module-native-protocol-tcp") {
		t.Fatalf("log: %s", b)
	}
}

func TestEnsureAudioKeepsSafeModule(t *testing.T) {
	dir := fakePulse(t, true, true, "7\tmodule-native-protocol-tcp\tauth-ip-acl=127.0.0.1\t\n")
	if did, err := EnsureAudio(context.Background(), true, nil); err != nil || did != "" {
		t.Fatalf("did %q, %v", did, err)
	}
	if strings.Contains(calls(dir), "load-module") || strings.Contains(calls(dir), "--start") {
		t.Fatalf("changed a working setup:\n%s", calls(dir))
	}
}

func TestEnsureAudioTightensOpenAnonymousModule(t *testing.T) {
	dir := fakePulse(t, true, true, "7\tmodule-native-protocol-tcp\tauth-ip-acl=127.0.0.1 auth-anonymous=1\t\n")
	did, err := EnsureAudio(context.Background(), true, nil)
	if err != nil || did != "limited sound to this phone" {
		t.Fatalf("did %q, %v", did, err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "modules"))
	if strings.Contains(string(b), "7\t") || !strings.Contains(string(b), "listen=127.0.0.1") {
		t.Fatalf("modules now:\n%s", b)
	}
}

func TestEnsureAudioInstallBackoff(t *testing.T) {
	dir := fakePulse(t, false, false, "")
	os.Remove(filepath.Join(dir, "bin", "apt-get"))
	if _, err := EnsureAudio(context.Background(), true, nil); err == nil {
		t.Fatal("no error without apt-get")
	}
	if AudioInstallDue() {
		t.Fatal("install retried right after a failure")
	}
	if did, err := EnsureAudio(context.Background(), true, nil); err != nil || did != "" {
		t.Fatalf("during backoff: %q, %v", did, err)
	}
}

func TestEnsureAudioOffTermux(t *testing.T) {
	dir := fakePulse(t, false, false, "")
	isTermux = func() bool { return false }
	if did, err := EnsureAudio(context.Background(), true, nil); did != "" || err != nil || calls(dir) != "" {
		t.Fatalf("did %q, %v, calls %q", did, err, calls(dir))
	}
}

// A Termux with half-upgraded packages: the install fails until apt-get
// full-upgrade has run; pkgInstall recovers once, never through pkg.
func TestPkgInstallRecoversFromPartialUpgrade(t *testing.T) {
	dir := fakePulse(t, false, false, "")
	bin := filepath.Join(dir, "bin")
	os.WriteFile(filepath.Join(bin, "apt-get"), []byte(`#!/bin/sh
echo "apt-get $*" >>`+dir+`/calls
case "$*" in
*full-upgrade*) touch `+dir+`/upgraded ;;
*"install pulseaudio"*)
  [ -e `+dir+`/upgraded ] || { echo 'E: Unmet dependencies (libcurl needs a newer openssl)'; exit 100; }
  cp `+dir+`/pulseaudio.new `+bin+`/pulseaudio ;;
esac
`), 0o755)
	os.WriteFile(filepath.Join(bin, "pkg"), []byte("#!/bin/sh\necho \"pkg $*\" >>"+dir+"/calls\nexit 1\n"), 0o755)
	did, err := EnsureAudio(context.Background(), true, nil)
	if err != nil || !strings.HasPrefix(did, "installed PulseAudio") {
		t.Fatalf("did %q, %v\n%s", did, err, calls(dir))
	}
	c := calls(dir)
	if !strings.Contains(c, "apt-get update") || !strings.Contains(c, "--force-confold full-upgrade") ||
		strings.Count(c, "install pulseaudio") != 2 || strings.Contains(c, "pkg ") {
		t.Fatalf("calls:\n%s", c)
	}
}

// Command runs (install=false) never install packages; they only start
// and configure an installed PulseAudio.
func TestEnsureAudioNoInstallOnCommandRuns(t *testing.T) {
	dir := fakePulse(t, false, false, "")
	if did, err := EnsureAudio(context.Background(), false, nil); err != nil || did != "" {
		t.Fatalf("did %q, %v", did, err)
	}
	if c := calls(dir); c != "" {
		t.Fatalf("ran %s", c)
	}
}

// Two package runs at once: the second gives up at once.
func TestPkgInstallLock(t *testing.T) {
	fakePulse(t, false, false, "")
	unlock, err := pkgLock()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if err := pkgInstall(context.Background(), nil, "pulseaudio"); err == nil || !strings.Contains(err.Error(), "another andronix") {
		t.Fatalf("err = %v", err)
	}
}
