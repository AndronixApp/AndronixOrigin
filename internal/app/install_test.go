package app

import (
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
