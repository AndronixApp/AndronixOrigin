// Command andronix installs and runs Linux distros in Termux.
//
//	andronix install <distro> [--de xfce|lxqt|mate|kde|lxde|none] [--yes] [--no-browser] [--no-start] [--reinstall]
//	andronix start   <distro> [--root] [-- command...]
//	andronix start   <distro> --x11 | desktop [distro] | desktop stop
//	andronix remove  <distro> [--yes] [--legacy]
//	andronix list | backup <distro> [file] | restore <file> | version | help
//
// Inside a distro: andronix vnc start [WxH] [display] | vnc stop [display]
// | setup-user | welcome.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"

	"github.com/AndronixApp/andronix-distros/internal/app"
	"github.com/AndronixApp/andronix-distros/internal/sys"
	"github.com/AndronixApp/andronix-distros/internal/telemetry"
	"github.com/AndronixApp/andronix-distros/internal/ui"
)

// args splits flags from positionals; flags may appear anywhere, and
// everything after "--" is passed through.
type args struct {
	pos   []string
	flags map[string]string
	rest  []string
}

func parse(in []string, withValue map[string]bool) args {
	a := args{flags: map[string]string{}}
	for i := 0; i < len(in); i++ {
		s := in[i]
		switch {
		case s == "--":
			a.rest = in[i+1:]
			return a
		case strings.HasPrefix(s, "--"):
			k, v, ok := strings.Cut(s[2:], "=")
			if !ok && withValue[k] && i+1 < len(in) {
				v = in[i+1]
				i++
			}
			a.flags[k] = v
		case s == "-y":
			a.flags["yes"] = ""
		case strings.HasPrefix(s, "-") && len(s) > 1:
			a.flags[strings.TrimLeft(s, "-")] = ""
		default:
			a.pos = append(a.pos, s)
		}
	}
	return a
}

func (a args) has(k string) bool { _, ok := a.flags[k]; return ok }
func (a args) arg(i int) string {
	if i < len(a.pos) {
		return a.pos[i]
	}
	return ""
}

// splitStart parses `start [--root|--x11] <distro> [--root|--x11] [--] [command...]`.
func splitStart(in []string) (name string, root, x11 bool, rest []string) {
	i := 0
	for ; i < len(in); i++ {
		switch in[i] {
		case "--root":
			root = true
			continue
		case "--x11":
			x11 = true
			continue
		case "--plain", "--yes", "-y", "--ascii", "--no-color":
			continue
		}
		if name == "" {
			name = in[i]
			i++
		}
		break
	}
	for i < len(in) && (in[i] == "--root" || in[i] == "--x11") {
		root, x11 = root || in[i] == "--root", x11 || in[i] == "--x11"
		i++
	}
	if i < len(in) && in[i] == "--" {
		i++
	}
	return name, root, x11, in[i:]
}

func help() {
	fmt.Print(ui.Banner("Linux on Android " + ui.GSep + " " + app.VersionLabel()))
	fmt.Println("  " + ui.Bold("Usage"))
	for _, c := range []string{"andronix install debian --de xfce", "andronix start debian", "andronix desktop debian", "andronix update",
		"andronix remove debian", "andronix list", "andronix backup debian --to storage", "andronix restore",
		"andronix tune debian --profile light", "andronix pack add debian python"} {
		ui.Cmd(c)
	}
	ui.Section("Install options")
	ui.Row("--de", "xfce, lxqt, mate, kde, lxde or none", 13)
	ui.Row("--no-browser", "skip the web browser", 13)
	ui.Row("--no-start", "don't open the distro when done", 13)
	ui.Row("--reinstall", "delete and install again", 13)
	ui.Row("--yes", "answer yes to questions", 13)
	ui.Row("--plain", "no colours or animation", 13)
	ui.Section("Desktop (Termux:X11)")
	ui.Row("desktop [distro]", "open it in the Termux:X11 app", 16)
	ui.Row("desktop stop", "stop it", 16)
	ui.Section("Inside a distro")
	ui.Row("vncserver-start", "or a VNC desktop (andronix vnc start)", 16)
	ui.Row("vncserver-stop", "stop it (andronix vnc stop)", 16)
	ui.Row("sudo andronix pack", "add dev packs: python, node, java, go, db", 16)
	ui.Footer()
}

func main() {
	// Play Store Termux starts programs through the system linker.
	sys.FixLinkerArgs()
	// Inside a distro, /usr/local/bin/bwrap links here (see app.BwrapShim).
	if app.IsBwrap() {
		if err := app.BwrapShim(os.Args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	sys.FixDNS()
	all := os.Args[1:]
	// A detached copy posting one telemetry event (telemetry.Send).
	if len(all) == 2 && all[0] == "__telemetry" {
		telemetry.Post(all[1])
		return
	}
	telemetry.Init(app.GetPaths().Home, app.Version)
	if legacy, ok := legacyModded(all); ok {
		all = legacy
	}
	cmd := "help"
	if len(all) > 0 && !strings.HasPrefix(all[0], "-") {
		cmd, all = all[0], all[1:]
	}
	a := parse(all, map[string]bool{"de": true, "desktop": true, "wm": true, "user": true, "token": true, "to": true, "profile": true, "manifest": true, "edition": true, "channel": true})
	if a.has("yes") {
		ui.Yes = true
	}
	if a.has("ascii") {
		os.Setenv("ANDRONIX_ASCII", "1")
	}
	if a.has("no-color") {
		os.Setenv("NO_COLOR", "1")
	}
	ui.Init(a.has("plain"))
	if a.has("version") || a.has("v") {
		cmd = "version"
	}
	if a.has("help") || a.has("h") {
		cmd = "help"
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch cmd {
	case "install", "i":
		de := a.flags["de"]
		if de == "" {
			de = a.flags["desktop"]
		}
		if de == "" {
			de = a.flags["wm"]
		}
		err = app.Install(ctx, app.InstallOpts{Distro: a.arg(0), Desktop: de, NoBrowser: a.has("no-browser"),
			NoStart: a.has("no-start"), Reinstall: a.has("reinstall") || a.has("force"), NoUser: a.has("no-user"),
			Modded: a.has("modded"), Token: a.flags["token"], Edition: a.flags["edition"]})
	case "start", "login", "run":
		// Everything after the distro name is the command, verbatim
		// (flags included), so argv reaches the distro unchanged.
		name, root, x11, rest := splitStart(all)
		if x11 {
			err = app.Desktop(ctx, name)
		} else {
			err = app.Start(name, root, rest)
		}
	case "desktop", "x11":
		if a.arg(0) == "stop" {
			err = app.DesktopStop()
		} else {
			err = app.Desktop(ctx, a.arg(0))
		}
	case "remove", "uninstall", "rm":
		err = app.Remove(ctx, a.arg(0), a.has("legacy"))
	case "update", "upgrade":
		err = app.Update(ctx, app.UpdateOpts{Distro: a.arg(0), NoSelf: a.has("no-self"), SelfOnly: a.has("self"),
			Manifest: a.flags["manifest"], Token: a.flags["token"], Channel: a.flags["channel"]})
	case "list", "ls":
		err = app.List()
	case "backup":
		err = app.Backup(ctx, a.arg(0), a.arg(1), a.flags["to"] == "storage")
	case "restore":
		err = app.Restore(ctx, a.arg(0))
	case "vnc":
		switch a.arg(0) {
		case "start":
			err = app.VNCStart(a.pos[1:], a.has("lan"))
		case "stop":
			err = app.VNCStop(a.pos[1:])
		default:
			ui.Cmd("vncserver-start [WIDTHxHEIGHT] [display] [--lan]")
			ui.Cmd("vncserver-stop [display]")
		}
	case "pack", "packs":
		switch rest := a.pos[min(1, len(a.pos)):]; a.arg(0) {
		case "add", "install":
			err = app.PackAdd(ctx, rest)
		case "remove", "rm", "uninstall":
			err = app.PackRemove(ctx, rest, a.has("purge"))
		default:
			err = app.PackList(rest)
		}
	case "telemetry":
		switch a.arg(0) {
		case "off":
			telemetry.SetEnabled(false)
			ui.OK("Telemetry is off. Nothing is sent.")
		case "on":
			telemetry.SetEnabled(true)
			ui.OK("Telemetry is on: anonymous install events (distro, desktop, result) go to Andronix.")
		default:
			if telemetry.Enabled() {
				ui.Info("Telemetry is on: anonymous install events (distro, desktop, result). Turn off: andronix telemetry off")
			} else {
				ui.Info("Telemetry is off. Turn on: andronix telemetry on")
			}
		}
	case "tune":
		err = app.Tune(a.arg(0), a.flags["profile"])
	case "session-prep":
		err = app.SessionPrep()
	case "setup-user":
		err = app.SetupUser(a.flags["user"])
	case "welcome":
		err = app.Welcome()
	case "version":
		fmt.Printf("andronix %s (go, %s/%s)\n", app.Version, runtime.GOOS, runtime.GOARCH)
	case "help":
		help()
	default:
		err = ui.Errorf("Unknown command '"+cmd+"'", "andronix doesn't have a '"+cmd+"' command.", "Run 'andronix help' to see what it can do.")
	}
	if errors.Is(err, app.ErrExplained) {
		os.Exit(1)
	}
	if err != nil {
		if errors.Is(err, ui.ErrCancelled) || errors.Is(err, context.Canceled) {
			fmt.Println()
			ui.Note("Stopped. Run the same command again to pick up where you left off.")
			os.Exit(130)
		}
		ui.ShowError(err)
		os.Exit(1)
	}
}
