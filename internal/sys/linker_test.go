package sys

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLinkerExec(t *testing.T) {
	d := t.TempDir()
	elf := filepath.Join(d, "proot")
	os.WriteFile(elf, []byte("\x7fELF rest"), 0o755)
	script := filepath.Join(d, "termux-wake-lock")
	os.WriteFile(script, []byte("#!/bin/sh -e\necho hi\n"), 0o755)
	sysScript := filepath.Join(d, "sys")
	os.WriteFile(sysScript, []byte("#!/system/bin/sh\necho hi\n"), 0o755)
	t.Setenv("PREFIX", d+"/com.termux/usr")
	defer func(a string) { appData, linkerPath, selfPath = a, "", "" }(appData)
	appData = d + "/"

	// Started directly: nothing changes.
	args := []string{"andronix", "version"}
	if got := fixLinkerArgs("/data/x/andronix", "", args); !reflect.DeepEqual(got, args) || linkerPath != "" {
		t.Fatalf("direct: %v %q", got, linkerPath)
	}
	if p, a, _ := viaLinker(elf, []string{"proot", "-0"}); p != elf || len(a) != 2 {
		t.Errorf("direct exec rewritten: %s %v", p, a)
	}

	// Through the linker: our path is dropped from the arguments.
	got := fixLinkerArgs("/system/bin/linker64", elf, []string{"andronix", elf, "version"})
	if !reflect.DeepEqual(got, []string{"andronix", "version"}) || selfPath != elf {
		t.Fatalf("linker: %v %q", got, selfPath)
	}
	if p, _ := Executable(); p != elf {
		t.Errorf("Executable = %s", p)
	}
	p, a, self := viaLinker(elf, []string{"proot", "-0", "sh"})
	if p != "/system/bin/linker64" || !reflect.DeepEqual(a, []string{"proot", elf, "-0", "sh"}) || self != elf {
		t.Errorf("elf: %s %v %s", p, a, self)
	}
	wantInterp := d + "/com.termux/usr/bin/sh"
	p, a, self = viaLinker(script, []string{"termux-wake-lock", "x"})
	if p != "/system/bin/linker64" || !reflect.DeepEqual(a, []string{"/bin/sh", wantInterp, "-e", script, "x"}) || self != wantInterp {
		t.Errorf("script: %s %v %s", p, a, self)
	}
	p, a, _ = viaLinker(sysScript, []string{"sys"})
	if p != "/system/bin/sh" || !reflect.DeepEqual(a, []string{"/system/bin/sh", sysScript}) {
		t.Errorf("system script: %s %v", p, a)
	}
	if p, _, _ := viaLinker("/system/bin/getprop", []string{"getprop"}); p != "/system/bin/getprop" {
		t.Errorf("system binary rewritten: %s", p)
	}
}
