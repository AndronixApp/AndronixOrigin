package termux

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The extra-keys bar is hidden on the first desktop only: later runs leave
// the user's choice (a swipe down brings it back) alone.
func TestHideExtraKeysOnce(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	os.MkdirAll(bin, 0o755)
	// A stand-in that stores the value; "list" prints it back. The first
	// set is lost, like on a fresh install before the app first starts.
	os.WriteFile(filepath.Join(bin, "termux-x11-preference"), []byte(`#!/bin/sh
d=`+dir+`
if [ "$1" = list ]; then echo "\"additionalKbdVisible\"=\"$(cat $d/value 2>/dev/null || echo true)\""; exit 0; fi
echo "$*" >>$d/calls
if [ -e $d/started ]; then echo "${1#*:}" >$d/value; else touch $d/started; fi
`), 0o755)
	extraKeysWait = 0
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	t.Setenv("ANDRONIX_HOME", filepath.Join(dir, "home"))
	old := isTermux
	isTermux = func() bool { return true }
	t.Cleanup(func() { isTermux = old })

	if !HideExtraKeysOnce() {
		t.Fatal("first desktop: bar not hidden")
	}
	if HideExtraKeysOnce() {
		t.Fatal("second desktop changed the setting again")
	}
	b, _ := os.ReadFile(filepath.Join(dir, "calls"))
	if strings.Count(string(b), "additionalKbdVisible:false") != 2 {
		t.Fatalf("want a retry after the lost first set; calls: %q", b)
	}
}

// Never marked done when the setting doesn't stick: the next desktop
// tries again.
func TestHideExtraKeysNotMarkedOnFailure(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	os.MkdirAll(bin, 0o755)
	os.WriteFile(filepath.Join(bin, "termux-x11-preference"), []byte("#!/bin/sh\n[ \"$1\" = list ] && echo '\"additionalKbdVisible\"=\"true\"'\nexit 0\n"), 0o755)
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	t.Setenv("ANDRONIX_HOME", filepath.Join(dir, "home"))
	old := isTermux
	isTermux = func() bool { return true }
	t.Cleanup(func() { isTermux = old })
	extraKeysWait = 0
	if HideExtraKeysOnce() {
		t.Fatal("reported hidden although the setting reads true")
	}
	if _, err := os.Stat(filepath.Join(dir, "home", "x11-extra-keys-hidden")); err == nil {
		t.Fatal("marker written on failure")
	}
}

func TestPhantomNote(t *testing.T) {
	for _, c := range []struct {
		sdk     int
		monitor string
		want    string // substring; "" means no note
	}{
		{30, "", ""},                       // Android 11: no phantom killer
		{31, "", "Android 12 and 13"},      // Android 12
		{33, "false", "Android 12 and 13"}, // 12-13: the property doesn't apply
		{34, "", "#android-14-and-newer"},  // 14+, switch off
		{36, "true", "Disable child process restrictions"},
		{36, "false", ""}, // 14+, switch on
	} {
		got := phantomNote(c.sdk, c.monitor)
		if (c.want == "") != (got == "") || !strings.Contains(got, c.want) {
			t.Errorf("phantomNote(%d, %q) = %q, want %q", c.sdk, c.monitor, got, c.want)
		}
	}
}
