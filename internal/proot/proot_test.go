package proot

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TmpDir's cleanup must keep every file of a live proot (another session
// of the same distro), whatever its kind, and drop the dead ones.
func TestTmpDirKeepsLiveSessions(t *testing.T) {
	if _, err := os.Stat("/proc/self"); err != nil {
		t.Skip("needs /proc (Linux, Android)")
	}
	home := t.TempDir()
	t.Setenv("ANDRONIX_HOME", home)
	dir := filepath.Join(home, "tmp")
	os.MkdirAll(dir, 0o700)
	live, dead := os.Getpid(), 1<<22+7 // above pid_max
	var keep, drop []string
	for _, kind := range []string{"proot", "prooted", "prootshm"} {
		keep = append(keep, fmt.Sprintf("%s-%d-AbC123", kind, live))
		drop = append(drop, fmt.Sprintf("%s-%d-AbC123", kind, dead))
	}
	drop = append(drop, "stray-file")
	for _, n := range append(append([]string{}, keep...), drop...) {
		os.WriteFile(filepath.Join(dir, n), nil, 0o600)
	}
	if got := TmpDir(); got != dir {
		t.Fatalf("TmpDir() = %s", got)
	}
	for _, n := range keep {
		if _, err := os.Stat(filepath.Join(dir, n)); err != nil {
			t.Errorf("removed a live session's %s", n)
		}
	}
	for _, n := range drop {
		if _, err := os.Stat(filepath.Join(dir, n)); err == nil {
			t.Errorf("kept %s", n)
		}
	}
}

func TestOldSyscallOrder(t *testing.T) {
	for rel, want := range map[string]bool{
		"4.4.302-gcc24b0a8f68a": true, "3.18.140": true, "4.8.0": false,
		"4.14.186-perf": false, "5.10.66-android12": false, "6.6.30": false, "": false,
	} {
		if got := oldSyscallOrder(rel); got != want {
			t.Errorf("oldSyscallOrder(%q) = %v", rel, got)
		}
	}
}
