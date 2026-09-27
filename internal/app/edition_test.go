package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/AndronixApp/andronix-distros/internal/conf"
	"github.com/AndronixApp/andronix-distros/internal/ui"
)

// fakeInstall makes ~/.andronix/distros/<id> with install.conf's EDITION.
func fakeInstall(t *testing.T, home, id, edition string) string {
	t.Helper()
	dir := filepath.Join(home, ".andronix/distros", id)
	os.MkdirAll(filepath.Join(dir, "rootfs/etc"), 0o755)
	os.WriteFile(filepath.Join(dir, "rootfs/etc/passwd"), []byte("root:x:0:0::/root:/bin/sh\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "install.conf"), []byte("DISTRO="+id+"\nEDITION="+edition+"\nDE=xfce\nSTAGE=done\n"), 0o644)
	return dir
}

// remove <edition-id> deletes only that edition: never the free distro or
// another edition in the same folder (the app's Modded uninstall).
func TestRemoveEdition(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ANDRONIX_HOME", "")
	ui.Yes = true
	defer func() { ui.Yes = false }()
	ctx := context.Background()

	// Free Ubuntu installed: removing ubuntu-xfce keeps it, exit 0.
	dir := fakeInstall(t, home, "ubuntu", "")
	if err := Remove(ctx, "ubuntu-xfce", true); err != nil || !exists(dir) {
		t.Fatalf("free Ubuntu: err %v, still there %v", err, exists(dir))
	}
	// Classic Ubuntu KDE installed: ubuntu-kde (Modded 2.0) keeps it too.
	os.RemoveAll(dir)
	dir = fakeInstall(t, home, "ubuntu", "legacy-ubuntu-kde")
	if err := Remove(ctx, "ubuntu-kde", false); err != nil || !exists(dir) {
		t.Fatalf("Classic KDE vs ubuntu-kde: err %v, still there %v", err, exists(dir))
	}
	// The installed edition goes.
	if err := Remove(ctx, "legacy-ubuntu-kde", false); err != nil || exists(dir) {
		t.Fatalf("legacy-ubuntu-kde: err %v, still there %v", err, exists(dir))
	}
	// Not installed at all: exit 0.
	if err := Remove(ctx, "kali-xfce", true); err != nil {
		t.Fatalf("kali-xfce not installed: %v", err)
	}
}

// --legacy with a Classic id also deletes the v8 Modded files of that
// product (andronix_os: ~/andronix-fs, ~/andronix-binds,
// ~/start-andronix.sh), and nothing else in home.
func TestRemoveEditionV8(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ANDRONIX_HOME", "")
	ui.Yes = true
	defer func() { ui.Yes = false }()
	for _, p := range []string{"andronix-fs/etc", "andronix-binds", "androkde-fs/etc", "ubuntu22-fs/etc"} {
		os.MkdirAll(filepath.Join(home, p), 0o755)
	}
	os.WriteFile(filepath.Join(home, "start-andronix.sh"), []byte("#!/bin/bash\nproot -r andronix-fs ...\n"), 0o755)
	os.WriteFile(filepath.Join(home, "start-androkde.sh"), []byte("#!/bin/bash\nproot -r androkde-fs ...\n"), 0o755)
	free := fakeInstall(t, home, "ubuntu", "")

	if err := Remove(context.Background(), "legacy-ubuntu-xfce", true); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"andronix-fs", "andronix-binds", "start-andronix.sh"} {
		if exists(filepath.Join(home, p)) {
			t.Errorf("%s left", p)
		}
	}
	for _, p := range []string{"androkde-fs", "start-androkde.sh", "ubuntu22-fs"} {
		if !exists(filepath.Join(home, p)) {
			t.Errorf("%s removed (another product or the free distro's old install)", p)
		}
	}
	if !exists(free) {
		t.Error("the free Ubuntu was removed")
	}
	// Without --legacy the v8 files stay.
	if err := Remove(context.Background(), "legacy-ubuntu-kde", false); err != nil || !exists(filepath.Join(home, "androkde-fs")) {
		t.Errorf("without --legacy: err %v, androkde-fs removed", err)
	}
}

// start/desktop <edition-id>: the distro when that edition is installed.
func TestEditionDistro(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ANDRONIX_HOME", "")
	fakeInstall(t, home, "debian", "debian-xfce")
	if d, err := resolveDistro("debian-xfce", "start"); err != nil || d.ID != "debian" {
		t.Errorf("debian-xfce: %v %v", d, err)
	}
	if _, err := resolveDistro("legacy-debian", "start"); err == nil {
		t.Error("legacy-debian isn't installed (debian-xfce is): want an error")
	}
	if _, err := resolveDistro("kali-xfce", "desktop"); err == nil {
		t.Error("kali-xfce not installed: want an error")
	}
	if ed, _ := conf.ResolveEdition("debian"); ed != nil {
		t.Error("a distro id must never resolve as an edition")
	}
}
