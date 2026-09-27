package app

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Remove deletes only the distro's own downloads: ubuntu's aren't ubuntu24's.
func TestCacheEntries(t *testing.T) {
	d := t.TempDir()
	for _, n := range []string{
		"ubuntu-26.04-aarch64.tar.xz", "ubuntu-26.04-xfce-modded-aarch64.tar.xz.part", "legacy-ubuntu-kde-26.04-aarch64.tar.xz",
		"ubuntu-upstream-aarch64.tar.gz", "ubuntu24-24.04-aarch64.tar.xz", "debian-13-aarch64.tar.xz", "legacy-debian-13-aarch64.tar.xz",
	} {
		os.WriteFile(filepath.Join(d, n), []byte("x"), 0o644)
	}
	os.MkdirAll(filepath.Join(d, "layers-ubuntu"), 0o755)
	os.MkdirAll(filepath.Join(d, "layers-ubuntu24"), 0o755)
	var got []string
	for _, p := range cacheEntries(d, "ubuntu") {
		got = append(got, filepath.Base(p))
	}
	sort.Strings(got)
	want := "layers-ubuntu legacy-ubuntu-kde-26.04-aarch64.tar.xz ubuntu-26.04-aarch64.tar.xz ubuntu-26.04-xfce-modded-aarch64.tar.xz.part ubuntu-upstream-aarch64.tar.gz"
	if strings.Join(got, " ") != want {
		t.Errorf("got  %v\nwant %s", got, want)
	}
	if n := removeCache(cacheEntries(d, "ubuntu")); n != 4 {
		t.Errorf("freed %d, want 4", n)
	}
	if left, _ := os.ReadDir(d); len(left) != 4 {
		t.Errorf("%d left, want 4 (ubuntu24's and debian's)", len(left))
	}
}
