package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AndronixApp/andronix-distros/internal/conf"
)

func TestKernelNewer(t *testing.T) {
	for _, c := range []struct {
		rel, max string
		want     bool
	}{
		{"6.6.30-android15-8", "6.5", true}, {"6.1.75-android14", "6.5", false},
		{"5.10.0", "6.5", false}, {"6.5.0", "6.5", false}, {"7.0.1", "6.5", true}, {"", "6.5", false},
	} {
		if got := kernelNewer(c.rel, c.max); got != c.want {
			t.Errorf("%s vs %s: %v", c.rel, c.max, got)
		}
	}
}

func TestModdedSource(t *testing.T) {
	d, _ := conf.LoadDistro("ubuntu")
	de, _ := conf.ResolveDesktop("xfce")
	t.Setenv("ANDRONIX_API", "https://api.example")
	s, err := moddedSource(d, de, "aarch64", Paths{Cache: "/c"}, "k=KEY&e=123&h=abc%2Bdef", "")
	if err != nil {
		t.Fatal(err)
	}
	want := "https://api.example/v1/modded/download/ubuntu-26.04-xfce-modded-aarch64.tar.xz?e=123&h=abc%2Bdef&k=KEY"
	if s.url != want || s.kind != "modded" {
		t.Errorf("got %q", s.url)
	}
	if s, _ := moddedSource(d, de, "aarch64", Paths{}, "k=a&e=1&h=b&f=debmod.tar.xz", ""); !strings.Contains(s.url, "/download/debmod.tar.xz?") {
		t.Errorf("file override: %q", s.url)
	}
	for _, bad := range []string{"", "k=a&e=1", "k=a&e=1&h=b&f=../x"} {
		if _, err := moddedSource(d, de, "aarch64", Paths{}, bad, ""); err == nil {
			t.Errorf("token %q accepted", bad)
		}
	}
}

func TestIniSet(t *testing.T) {
	s := iniSet("", "Compositing", "Enabled", "false")
	s = iniSet(s, "Compositing", "Enabled", "false")
	s = iniSet(s, "Other", "A", "1")
	s = iniSet("[Compositing]\nBackend=OpenGL\n\n[Other]\nA=0\n", "Compositing", "Enabled", "false")
	want := "[Compositing]\nBackend=OpenGL\n\nEnabled=false\n[Other]\nA=0\n"
	if s != want {
		t.Errorf("got %q", s)
	}
}

func TestInstallReportArgs(t *testing.T) {
	r := &installReport{distro: "debian", de: "xfce", modded: true, step: "Downloading Debian 13"}
	for _, id := range []string{"", "0123456789abcde", "0123456789abcdefg", "0123456789abcdeg", "0123456789abcdef; rm -rf ~"} {
		t.Setenv("ANDRONIX_INSTALL_ID", id)
		if a := r.amArgs("fail"); a != nil {
			t.Errorf("id %q: want no broadcast, got %v", id, a)
		}
	}
	t.Setenv("ANDRONIX_INSTALL_ID", "0123456789ABCdef")
	got := strings.Join(r.amArgs("fail"), " ")
	want := "broadcast -n studio.com.techriz.andronix/.receivers.InstallResultReceiver -a studio.com.techriz.andronix.INSTALL_RESULT" +
		" --es id 0123456789ABCdef --es status fail --es distro debian --es de xfce --es step Downloading Debian 13 --ez modded true --es version " + Version
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	r.modded = false
	if got := strings.Join(r.amArgs("ok"), " "); strings.Contains(got, "step") || strings.Contains(got, "modded") {
		t.Errorf("ok: %s", got)
	}
}

func TestLegacyEditionFile(t *testing.T) {
	// The names the Modded builds export.
	for id, want := range map[string]string{
		"legacy-ubuntu-xfce": "legacy-ubuntu-xfce-26.04-aarch64.tar.xz",
		"legacy-ubuntu-kde":  "legacy-ubuntu-kde-26.04-aarch64.tar.xz",
		"legacy-debian":      "legacy-debian-13-aarch64.tar.xz",
		"legacy-manjaro":     "legacy-manjaro-stable-aarch64.tar.xz",
	} {
		ed, _ := conf.ResolveEdition(id)
		d, _ := conf.LoadDistro(ed.Distro)
		de, _ := conf.ResolveDesktop(ed.Desktop)
		s, err := moddedSource(d, de, "aarch64", Paths{Cache: "/c"}, "k=a&e=1&h=b", id)
		if err != nil || !strings.Contains(s.url, "/download/"+want+"?") {
			t.Errorf("%s: %v %v", id, s.url, err)
		}
	}
}

// Extra token fields reach products-api untouched; f (our file override) doesn't.
func TestModdedTokenAsIs(t *testing.T) {
	d, _ := conf.LoadDistro("ubuntu")
	de, _ := conf.ResolveDesktop("kde")
	s, _ := moddedSource(d, de, "arm", Paths{}, "k=K&e=E&h=H&p=modded_pass&v=2", "ubuntu-kde")
	if !strings.HasSuffix(s.url, "/download/ubuntu-26.04-kde-modded-arm.tar.xz?e=E&h=H&k=K&p=modded_pass&v=2") {
		t.Errorf("url: %s", s.url)
	}
}

// Per-run logs rotate per kind, by time stamp; the log just opened stays
// even when another kind has many newer-sorting names.
func TestRotateLogs(t *testing.T) {
	d := t.TempDir()
	touch := func(n string) { os.WriteFile(filepath.Join(d, n), nil, 0o644) }
	for i := 0; i < 12; i++ {
		touch(fmt.Sprintf("x11-20260926-1200%02d.log", i))
		touch(fmt.Sprintf("remove-debian-20260926-1300%02d.log", i))
	}
	touch("install-kali-20260926-110000.log") // older stamp, sorts first by name
	touch("termux.log")
	current := filepath.Join(d, "install-kali-20260926-140000.log")
	touch(filepath.Base(current))
	rotateLogs(d, 10, current)
	has := func(n string) bool { _, err := os.Stat(filepath.Join(d, n)); return err == nil }
	if !has(filepath.Base(current)) || !has("install-kali-20260926-110000.log") || !has("termux.log") {
		t.Error("the new install log, an older one of its kind, or a running log was removed")
	}
	if has("x11-20260926-120001.log") || !has("x11-20260926-120002.log") || !has("x11-20260926-120011.log") {
		t.Error("x11: want the newest 10 kept")
	}
	if has("remove-debian-20260926-130000.log") || !has("remove-debian-20260926-130011.log") {
		t.Error("remove-debian: want the newest 10 kept")
	}
	rotateLogs(d, 0, current) // even with nothing to keep, the current log stays
	if !has(filepath.Base(current)) {
		t.Error("current log removed")
	}
}

// A desktop install that stopped part way leaves startxfce4 behind; on
// resume that mustn't look like a prebuilt image (Kali from 2.0.0 skipped
// "Installing XFCE" and was left without dbus).
func TestPrebuilt(t *testing.T) {
	de, _ := conf.ResolveDesktop("xfce")
	d := t.TempDir()
	in := &Inst{Dir: d, Rootfs: filepath.Join(d, "rootfs"), State: filepath.Join(d, "install.conf")}
	os.MkdirAll(filepath.Join(in.Rootfs, "usr/bin"), 0o755)
	os.WriteFile(filepath.Join(in.Rootfs, "usr/bin/startxfce4"), nil, 0o755)
	in.Set("EDITION", "")
	if prebuilt(in, de) {
		t.Error("free install from 2.0.0 (no PREBUILT): want not prebuilt")
	}
	in.Set("EDITION", "legacy-debian")
	if !prebuilt(in, de) {
		t.Error("Classic from 2.0.0: want prebuilt")
	}
	in.Set("PREBUILT", "no")
	if prebuilt(in, de) {
		t.Error("PREBUILT=no: want not prebuilt")
	}
	in.Set("PREBUILT", "yes")
	if !prebuilt(in, de) {
		t.Error("PREBUILT=yes: want prebuilt")
	}
	os.Remove(filepath.Join(in.Rootfs, "usr/bin/startxfce4"))
	if prebuilt(in, de) {
		t.Error("no session binary: want not prebuilt")
	}
}

// Self-update only moves forward: 2.0.1-rc3 replaced itself with 2.0.0.
func TestVersionCmp(t *testing.T) {
	order := []string{"1.9.9", "2.0.0-beta1", "2.0.0-rc1", "2.0.0-rc2", "2.0.0-rc10", "2.0.0", "v2.0.0-5-gabc123", "v2.0.0-68-g9a0a0b4-dirty",
		"2.0.1-rc3", "v2.0.1", "2.0.10"}
	for i := range order {
		for j := range order {
			want := cmpInt(i, j)
			if got := versionCmp(order[i], order[j]); got != want {
				t.Errorf("versionCmp(%s, %s) = %d, want %d", order[i], order[j], got, want)
			}
		}
	}
	for _, junk := range []string{"", "dev", "andronix", "2.0"} {
		if versionCmp(junk, "1.0.0") >= 0 {
			t.Errorf("%q counts as newer than 1.0.0", junk)
		}
	}
}

func TestBinaryVersion(t *testing.T) {
	d := t.TempDir()
	bin := filepath.Join(d, "andronix")
	os.WriteFile(bin, []byte("#!/bin/sh\necho 'andronix 2.0.0 (go, android/arm64)'\n"), 0o755)
	if v := binaryVersion(context.Background(), bin); v != "2.0.0" {
		t.Errorf("got %q", v)
	}
	os.WriteFile(bin, []byte("#!/bin/sh\necho garbage\n"), 0o755)
	if v := binaryVersion(context.Background(), bin); v != "" {
		t.Errorf("garbage: got %q", v)
	}
}

// What Termux's am and Android's am print (a16, Sept 27).
func TestAmDelivered(t *testing.T) {
	for out, want := range map[string]bool{
		"Broadcasting: Intent { act=x (has extras) }\nBroadcast sent without waiting for result": true,
		"Broadcasting: Intent { act=x }\nBroadcast completed: result=0":                          true,
		"Broadcasting: Intent { act=x }\nException occurred while executing 'broadcast': java.lang.SecurityException: Permission Denial": false,
		"": false,
	} {
		if got := amDelivered(out); got != want {
			t.Errorf("%q: got %v", out, got)
		}
	}
}
