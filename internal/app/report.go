package app

import (
	"os"
	"path/filepath"
	"regexp"
	"syscall"
	"time"

	"github.com/AndronixApp/andronix-distros/internal/sys"
	"github.com/AndronixApp/andronix-distros/internal/telemetry"
)

// Install result for the Andronix app (the android contract). The app
// runs `export ANDRONIX_INSTALL_ID=<16 hex>; <install command>`; at the end
// of the install we broadcast the result to its receiver with Termux's am.
// Best effort: silent, in the background, capped at 5 s, errors ignored.
const (
	appReceiver = "studio.com.techriz.andronix/.receivers.InstallResultReceiver"
	appAction   = "studio.com.techriz.andronix.INSTALL_RESULT"
)

// The ID can be pasted by the user, so only exactly 16 hex digits go out.
var installIDRe = regexp.MustCompile(`^[0-9a-fA-F]{16}$`)

type installReport struct {
	distro, de string
	modded     bool
	step       string // the running step; on failure, the one that failed
	sent       bool
	started    time.Time
	err        error // the failure, for its telemetry class
}

// amArgs is the am command line for status "ok" or "fail", or nil when
// the app didn't ask (or the ID isn't valid).
func (r *installReport) amArgs(status string) []string {
	id := os.Getenv("ANDRONIX_INSTALL_ID")
	if !installIDRe.MatchString(id) {
		return nil
	}
	a := []string{"broadcast", "-n", appReceiver, "-a", appAction,
		"--es", "id", id, "--es", "status", status, "--es", "distro", r.distro, "--es", "de", r.de}
	if status == "fail" && r.step != "" {
		a = append(a, "--es", "step", r.step)
	}
	if r.modded {
		a = append(a, "--ez", "modded", "true")
	}
	return append(a, "--es", "version", Version)
}

// send broadcasts the result once. am runs in its own session under a
// small sh wrapper that kills it after 5 s, so neither a slow am nor this
// process exec'ing into the distro shell (or exiting) affects the other.
func (r *installReport) send(status string) {
	if r == nil || r.sent {
		return
	}
	r.sent = true
	props := map[string]any{"ok": status == "ok", "distro": r.distro, "de": r.de, "modded": r.modded,
		"duration_ms": time.Since(r.started).Milliseconds()}
	if status != "ok" {
		props["step"], props["error_class"] = r.step, telemetry.ErrorClass(r.err)
	}
	telemetry.Send("install_result", props)
	args := r.amArgs(status)
	if args == nil {
		return
	}
	am := filepath.Join(sys.Prefix(), "bin/am")
	if sys.Prefix() == "" || !fileExists(am) {
		var err error
		if am, err = sys.LookPath("am"); err != nil {
			return // no am (not Termux, or termux-am missing): say nothing
		}
	}
	sh, err := sys.LookPath("sh")
	if err != nil {
		return
	}
	c := sys.Command(sh, append([]string{"-c",
		`"$0" "$@" & p=$!; (sleep 5; kill $p) & k=$!; wait $p; kill $k`, am}, args...)...)
	c.Stdin, c.Stdout, c.Stderr = nil, nil, nil
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if c.Start() == nil {
		go c.Wait()
	}
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }
