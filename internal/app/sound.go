package app

import (
	"context"
	"fmt"
	"os"

	"github.com/AndronixApp/andronix-distros/internal/sys"
	"github.com/AndronixApp/andronix-distros/internal/termux"
	"github.com/AndronixApp/andronix-distros/internal/ui"
)

// startSound makes sure Termux's PulseAudio is up for the distro
// (termux.EnsureAudio). It never stops a start: a problem is one line on
// stderr, with the details in the Termux log. stepList (an interactive
// start, andronix desktop) may install pulseaudio, shown as a step.
// Command runs (./start-debian.sh cmd, scripts) never install packages:
// that can take minutes on an out-of-date Termux, and two starts at once
// would fight over apt. They only start sound when it's installed, and
// keep stdout clean.
func startSound(ctx context.Context, stepList bool) {
	if !sys.IsTermux() {
		return
	}
	install := termux.AudioInstallDue()
	if install && stepList {
		ui.RunSteps(ctx, []ui.Step{{Label: "Setting up sound (PulseAudio)", Run: func(ctx context.Context, r ui.Reporter) error {
			did, err := termux.EnsureAudio(ctx, true, r.Line)
			if err != nil {
				return ui.Skip("no sound for now; details in " + ui.Tilde(termux.LogFile()))
			}
			r.Detail(did)
			return nil
		}}}, nil)
		return
	}
	did, err := termux.EnsureAudio(ctx, false, nil)
	switch {
	case err != nil:
		fmt.Fprintf(os.Stderr, "andronix: sound isn't set up (%v). Details: %s\n", err, ui.Tilde(termux.LogFile()))
	case did != "":
		fmt.Fprintf(os.Stderr, "andronix: sound: %s\n", did)
	}
}
