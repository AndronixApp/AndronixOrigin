package main

import (
	"github.com/AndronixApp/andronix-distros/internal/conf"
	"reflect"
	"testing"
)

func TestLegacyModded(t *testing.T) {
	for _, c := range []struct {
		in   []string
		want []string
	}{
		{[]string{"-v", "2", "-h", "H", "-k", "K", "-e", "a@b.c", "-p", "andronix_os.tar.xz"},
			[]string{"install", "--edition", "legacy-ubuntu-xfce", "--token", "e=a%40b.c&h=H&k=K"}},
		{[]string{"install", "-v", "2", "-p", "kde_ubuntu.tar.xz", "-k", "K", "-e", "E", "-h", "H", "--yes"},
			[]string{"install", "--edition", "legacy-ubuntu-kde", "--token", "e=E&h=H&k=K", "--yes"}},
		{[]string{"-v", "2", "-h", "H", "-k", "K", "-e", "E", "-p", "debian_mod.tar.xz"},
			[]string{"install", "--edition", "legacy-debian", "--token", "e=E&h=H&k=K"}},
		{[]string{"-v", "2", "-h", "H", "-k", "K", "-e", "E", "-p", "manjaro"},
			[]string{"install", "--edition", "legacy-manjaro", "--token", "e=E&h=H&k=K"}},
		{[]string{"-v", "2", "-h", "H", "-k", "K", "-e", "E", "-p", "kali_xfce.tar.xz"},
			[]string{"install", "kali", "--de", "xfce", "--modded", "--token", "e=E&h=H&k=K"}},
	} {
		if got, ok := legacyModded(c.in); !ok || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%v: got %v %v, want %v", c.in, got, ok, c.want)
		}
	}
	for _, in := range [][]string{{"-v"}, {"version"}, {"install", "debian"}, {"-v", "2", "-p", "x.tar.xz"}} {
		if _, ok := legacyModded(in); ok {
			t.Errorf("%v: translated, want untouched", in)
		}
	}
}

// Every old product maps to an edition the installer knows.
func TestLegacyFilesAreEditions(t *testing.T) {
	for file, id := range legacyFiles {
		if ed, err := conf.ResolveEdition(id); err != nil || !ed.Legacy() {
			t.Errorf("%s -> %s: %v", file, id, err)
		}
	}
}
