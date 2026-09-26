package sys

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestLookPath(t *testing.T) {
	d := t.TempDir()
	os.WriteFile(filepath.Join(d, "tool"), []byte("#!/bin/sh\necho hi\n"), 0o755)
	os.WriteFile(filepath.Join(d, "data"), []byte("x"), 0o644)
	t.Setenv("PATH", d+string(os.PathListSeparator)+"/nonexistent")
	if p, err := LookPath("tool"); err != nil || p != filepath.Join(d, "tool") {
		t.Errorf("tool: %q %v", p, err)
	}
	if _, err := LookPath("data"); !errors.Is(err, exec.ErrNotFound) {
		t.Errorf("non-executable found: %v", err)
	}
	if out, err := Command("tool").Output(); err != nil || string(out) != "hi\n" {
		t.Errorf("Command: %q %v", out, err)
	}
	if err := Command("no-such-tool").Run(); !errors.Is(err, exec.ErrNotFound) {
		t.Errorf("missing command: %v", err)
	}
}
