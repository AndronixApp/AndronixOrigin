package app

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/AndronixApp/andronix-distros/internal/sys"
	"github.com/AndronixApp/andronix-distros/internal/telemetry"
	"github.com/AndronixApp/andronix-distros/internal/termux"
	"github.com/AndronixApp/andronix-distros/internal/ui"
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

// send broadcasts the result once, in the foreground (up to ~12 s per
// try; Termux's am starts a Java VM, which took over the 5 s the 2.0.0
// background send allowed on a slow phone, and nothing was logged). It
// tries Termux's am, then Android's own; every try goes to termux.log
// (which outlives the per-run logs), and a line tells the user if both
// failed.
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
	if id := os.Getenv("ANDRONIX_INSTALL_ID"); id != "" && !installIDRe.MatchString(id) {
		termux.Logf("install result: ANDRONIX_INSTALL_ID %q isn't 16 hex digits; not sent", id)
	}
	args := r.amArgs(status)
	if args == nil {
		return
	}
	var tried []string
	if sys.Prefix() != "" {
		tried = append(tried, filepath.Join(sys.Prefix(), "bin/am"))
	}
	tried = append(tried, "/system/bin/am")
	for _, am := range tried {
		if !fileExists(am) {
			termux.Logf("install result: %s missing", am)
			continue
		}
		a := args
		if strings.HasPrefix(am, "/system/") {
			// Android's am defaults to the current user, which an app may
			// not name (INTERACT_ACROSS_USERS); its own user it may.
			a = append([]string{args[0], "--user", strconv.Itoa(os.Getuid() / 100000)}, args[1:]...)
		}
		out, err := runAm(am, a)
		termux.Logf("install result %s via %s: %v %s", status, am, err, strings.Join(strings.Fields(out), " "))
		if err == nil && amDelivered(out) {
			return
		}
	}
	ui.Note("The Andronix app wasn't told that the install finished; it still works. Details: ~/.andronix/logs/termux.log")
}

// runAm runs am with a 12 s limit and returns what it printed.
func runAm(am string, args []string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	out, err := sys.CommandContext(ctx, am, args...).CombinedOutput()
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	return string(out), err
}

// amDelivered: am said it sent the broadcast ("Broadcast completed", or
// Termux's am: "Broadcast sent without waiting for result"), and no error.
func amDelivered(out string) bool {
	return (strings.Contains(out, "Broadcast completed") || strings.Contains(out, "Broadcast sent")) &&
		!strings.Contains(out, "Exception") && !strings.Contains(out, "Error")
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }
