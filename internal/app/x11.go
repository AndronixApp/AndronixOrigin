package app

// The Termux:X11 desktop: `andronix desktop [distro]` (or `andronix start
// <distro> --x11`) runs the distro's desktop on the Termux:X11 app instead
// of VNC. It uses the same session script as VNC (xstartup: dbus, the
// bwrap stand-in, per user), with DISPLAY=:0 and termux-x11's socket bound
// into the distro.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/AndronixApp/andronix-distros/internal/conf"
	"github.com/AndronixApp/andronix-distros/internal/rootfs"
	"github.com/AndronixApp/andronix-distros/internal/termux"
	"github.com/AndronixApp/andronix-distros/internal/ui"
)

// ErrExplained means the message was already shown (a box with the fix);
// exit 1 without an error box.
var ErrExplained = errors.New("explained")

// x11State is ~/.andronix/x11.state: the running Termux:X11 desktop.
func x11State() string { return filepath.Join(GetPaths().Home, "x11.state") }

type x11Run struct {
	Distro        string
	PID, Proot    int // andronix desktop, and its proot session
	Server        int // termux-x11, when we started it
	StartedServer bool
}

func readX11State() (x11Run, bool) {
	b, err := os.ReadFile(x11State())
	if err != nil {
		return x11Run{}, false
	}
	v := conf.Parse(b)
	r := x11Run{Distro: v.Get("DISTRO"), PID: v.Int("PID"), Proot: v.Int("PROOT"), Server: v.Int("SERVER"), StartedServer: v.Bool("STARTED_SERVER")}
	return r, termux.Alive(r.Proot)
}

func (r x11Run) write() error {
	os.MkdirAll(filepath.Dir(x11State()), 0o755)
	return os.WriteFile(x11State(), []byte(fmt.Sprintf("DISTRO=%s\nPID=%d\nPROOT=%d\nSERVER=%d\nSTARTED_SERVER=%t\n",
		r.Distro, r.PID, r.Proot, r.Server, r.StartedServer)), 0o644)
}

// desktopDistro picks the distro for `andronix desktop` with no name:
// the only one with a desktop, or a picker.
func desktopDistro() (string, error) {
	var opts []ui.Option
	for _, id := range conf.DistroIDs() {
		d, _ := conf.LoadDistro(id)
		in := Open(d)
		if in.Installed() && in.Get("DE") != "" && in.Get("DE") != "none" {
			opts = append(opts, ui.Option{Label: d.Label() + " (" + in.Get("DE_NAME") + ")", Value: id})
		}
	}
	switch {
	case len(opts) == 1:
		return opts[0].Value, nil
	case len(opts) == 0:
		return "", ui.Errorf("No desktop installed yet", "Termux:X11 shows a distro's desktop, and none of yours has one.",
			"Install one first, e.g. andronix install debian --de xfce")
	case !ui.Interactive:
		var ids []string
		for _, o := range opts {
			ids = append(ids, o.Value)
		}
		return "", ui.Errorf("Which distro?", "More than one distro has a desktop: "+strings.Join(ids, ", ")+".",
			"Name it, e.g. andronix desktop "+ids[0])
	}
	return ui.Choose("Which desktop should I open?", "", opts, opts[0].Value)
}

// Desktop is `andronix desktop [distro]`: the desktop on Termux:X11. It
// stays in the foreground until the desktop ends (log out, Ctrl-C, or
// `andronix desktop stop` from another session).
func Desktop(ctx context.Context, name string) error {
	if os.Getenv("ANDRONIX_DISTRO") != "" {
		return ui.Errorf("Run this in Termux", "andronix desktop starts Termux:X11, which lives in Termux, not inside a distro.",
			"Type exit, then run: andronix desktop "+os.Getenv("ANDRONIX_DISTRO"))
	}
	if name == "" {
		var err error
		if name, err = desktopDistro(); err != nil {
			return err
		}
	}
	d, err := resolveDistro(name, "desktop")
	if err != nil {
		return err
	}
	desktopNote()
	in := Open(d)
	if !in.Installed() {
		return ui.Errorf(d.Label()+" isn't installed", "There's no desktop to show yet.", "Install it first: andronix install "+d.ID+" --de xfce")
	}
	de, err := conf.ResolveDesktop(in.Get("DE"))
	if err != nil || de.None() {
		return ui.Errorf(d.Label()+" has no desktop", "It was installed as command line only.", "Add one: andronix install "+d.ID+" --de xfce")
	}
	if run, ok := readX11State(); ok {
		if run.Distro != d.ID {
			return ui.Errorf("Another desktop is on Termux:X11", "The "+run.Distro+" desktop is running there now.",
				"Stop it first: andronix desktop stop")
		}
		termux.OpenX11App()
		fmt.Println()
		ui.OK("The " + d.Label() + " desktop is already running. Switch to the Termux:X11 app.")
		ui.Note("To stop it: andronix desktop stop")
		fmt.Println()
		return nil
	}
	// First boot: the user doesn't exist yet. Set it up now, like the
	// first login would.
	if _, err := os.Stat(filepath.Join(in.Rootfs, "etc/andronix/firstboot")); err == nil && ui.Interactive {
		if err := in.Target(true).Interactive("/usr/local/bin/andronix setup-user", false); err != nil {
			return ui.Errorf("User setup didn't finish", err.Error(), "Run ./"+d.MainStart()+" once to set up your user, then andronix desktop "+d.ID)
		}
	}
	t := in.Target(false)
	home := t.Home
	if t.User == "" {
		home = "/root"
	}
	// The same session script as VNC (xstartup: dbus-run-session, then
	// andronix session-prep and the light profile's XDG_CONFIG_DIRS), so
	// both displays behave the same. Scripts from before session-prep are
	// refreshed, as install and update do.
	xs := filepath.Join(in.Rootfs, home, ".config/tigervnc/xstartup")
	if b, err := os.ReadFile(xs); err != nil || !strings.Contains(string(b), "andronix session-prep") {
		writeXstartup(in.Rootfs, de)
		if _, err := os.Stat(xs); err != nil {
			return ui.Errorf("The desktop session is missing", "There's no session script at ~/.config/tigervnc/xstartup for "+nameOr(t.User, "root")+".",
				"Reinstall the desktop: andronix install "+d.ID+" --de "+de.ID)
		}
	}

	// Installs from before Termux:X11 lack its monitor ("builtin") in the
	// XFCE wallpaper defaults, or have it zoomed (rgba1 came with the
	// portrait-safe style). Modded editions bring their own look.
	if de.ID == "xfce" && !moddedImage(in) {
		xml := filepath.Join(in.Rootfs, "etc/xdg/xfce4/xfconf/xfce-perchannel-xml/xfce4-desktop.xml")
		if b, err := os.ReadFile(xml); err == nil && strings.Contains(string(b), WallpaperPath) && !strings.Contains(string(b), "rgba1") {
			setWallpaper(ctx, in.Rootfs, de.ID, nil)
		}
	}

	lg := NewLog("x11-" + d.ID)
	defer lg.Close()
	fmt.Println()
	startSound(ctx, true)
	run := x11Run{Distro: d.ID, PID: os.Getpid()}
	serverLog := filepath.Join(GetPaths().Logs, "termux-x11.log")
	steps := []ui.Step{
		{Label: "Installing Termux:X11 (" + termux.X11Package + ")", Run: func(ctx context.Context, r ui.Reporter) error {
			if termux.HasX11Server() {
				return ui.Skip("already installed")
			}
			if err := termux.InstallX11Server(ctx, r.Line); err != nil {
				return ui.Errorf("Couldn't install Termux:X11", err.Error(),
					"Check your internet, then run: pkg install x11-repo && pkg install "+termux.X11Package)
			}
			return nil
		}},
		{Label: "Starting Termux:X11", Run: func(ctx context.Context, r ui.Reporter) error {
			if installed, known := termux.X11AppInstalled(); known && !installed {
				return termux.ErrX11AppMissing
			}
			if termux.X11Running(termux.X11Display) {
				termux.Logf("x11: a server already answers on :%d; using it", termux.X11Display)
				return ui.Skip("already running")
			}
			pid, err := termux.StartX11(termux.X11Display, serverLog)
			if err != nil {
				return err
			}
			run.Server, run.StartedServer = pid, true
			r.Detail("display :" + strconv.Itoa(termux.X11Display))
			return nil
		}},
	}
	if err := ui.RunSteps(ctx, steps, lg); err != nil {
		return x11Error(err, serverLog)
	}
	termux.OpenX11App()
	// After the app is open (see HideExtraKeysOnce); it can take a few
	// seconds, so the session starts meanwhile.
	hidden := make(chan bool, 1)
	go func() { hidden <- termux.HideExtraKeysOnce() }()
	rootfs.EnsureBwrapShim(in.Rootfs)

	t.Display = fmt.Sprintf(":%d", termux.X11Display)
	c := t.Command("/bin/sh", "-c", `x="$HOME/.config/tigervnc/xstartup"; [ -x "$x" ] || x="$HOME/.vnc/xstartup"; exec "$x"`)
	if f, err := os.OpenFile(serverLog, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644); err == nil {
		defer f.Close()
		c.Stdout, c.Stderr = f, f // proot's own warnings; the session logs inside
	}
	start := time.Now()
	if err := c.Start(); err != nil {
		stopOurServer(run)
		return ui.Errorf("The desktop didn't start", err.Error(), "Run andronix desktop "+d.ID+" again.")
	}
	run.Proot = c.Process.Pid
	run.write()
	termux.Logf("x11: %s session started (proot pid %d, user %s)", d.ID, run.Proot, nameOr(t.User, "root"))
	vncNote := ""
	if pidFileAlive(filepath.Join(in.Rootfs, "tmp/.X1-lock")) {
		vncNote = "The VNC desktop is running too. For the best speed, stop it: vncserver-stop"
	}
	lines := []string{ui.KV("Desktop", de.Name+" on Termux:X11"), ui.KV("User", nameOr(t.User, "root")), "",
		"Switch to the Termux:X11 app to use it. If it didn't open, open Termux:X11 from your app drawer.",
		"Keep this Termux session open while you use the desktop.", "",
		ui.KV("Stop", "log out, press Ctrl-C here,"), ui.KV("", "or run: andronix desktop stop")}
	if <-hidden {
		lines = append(lines, "", "Termux:X11's extra-keys bar is hidden so the desktop's bottom panel shows. Swipe down with three fingers in Termux:X11 to bring it back.")
	}
	if vncNote != "" {
		lines = append(lines, "", vncNote)
	}
	fmt.Println()
	fmt.Print(ui.Box(ui.BoxOK, "Desktop is running", lines...))
	if n := termux.PhantomNote(); n != "" {
		ui.Note(n)
	}
	fmt.Println()

	done := make(chan error, 1)
	go func() { done <- c.Wait() }()
	var werr error
	select {
	case werr = <-done:
	case <-ctx.Done():
		go stopSession(run.Proot)
		werr = <-done
	}
	// `andronix desktop stop` removes the state file first.
	_, statErr := os.Stat(x11State())
	requested := ctx.Err() != nil || os.IsNotExist(statErr)
	os.Remove(x11State())
	stopOurServer(run)
	termux.Logf("x11: %s session ended after %s (%v)", d.ID, time.Since(start).Round(time.Second), werr)
	if !requested && time.Since(start) < 15*time.Second {
		uid := t.UID
		if t.User == "" {
			uid = "0"
		}
		b, _ := os.ReadFile(filepath.Join(in.Rootfs, "tmp/andronix-session-"+uid+".log"))
		tail := strings.Split(strings.TrimSpace(string(b)), "\n")
		if len(tail) > 3 {
			tail = tail[len(tail)-3:]
		}
		return ui.Errorf("The desktop session stopped", "The desktop quit right after starting: "+strings.Join(tail, " / "),
			"Run andronix desktop "+d.ID+" again. If it happens again, send us /tmp/andronix-session-"+uid+".log from inside "+d.Name+" (Discord or email).")
	}
	fmt.Println()
	ui.OK("The " + d.Label() + " desktop has stopped.")
	fmt.Println()
	return nil
}

// stopOurServer stops termux-x11 if this run started it; a server the
// user started themselves is left alone.
func stopOurServer(run x11Run) {
	if run.StartedServer {
		termux.StopX11(termux.X11Display, run.Server)
	}
}

// stopSession ends a proot session: its processes first (every tracee
// of that proot), then proot. Killing proot alone would leave them
// running untraced (a dbus-daemon spinning in the background).
func stopSession(proot int) {
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGKILL} {
		for _, p := range tracees(proot) {
			syscall.Kill(p, sig)
		}
		for i := 0; i < 30 && termux.Alive(proot); i++ {
			time.Sleep(100 * time.Millisecond)
		}
		if !termux.Alive(proot) {
			return
		}
	}
	syscall.Kill(proot, syscall.SIGKILL)
}

// tracees are the processes proot traces: everything in its session.
func tracees(tracer int) []int {
	var out []int
	want := "TracerPid:\t" + strconv.Itoa(tracer) + "\n"
	entries, _ := os.ReadDir("/proc")
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || tracer <= 0 {
			continue
		}
		if b, err := os.ReadFile("/proc/" + e.Name() + "/status"); err == nil && strings.Contains(string(b), want) {
			out = append(out, pid)
		}
	}
	return out
}

// DesktopStop is `andronix desktop stop`: ends the Termux:X11 desktop and
// its server from any Termux session.
func DesktopStop() error {
	run, ok := readX11State()
	os.Remove(x11State())
	stopped := false
	if ok {
		stopSession(run.Proot)
		termux.Logf("x11: stopped the %s session (proot pid %d)", run.Distro, run.Proot)
		stopped = true
	}
	if termux.StopX11(termux.X11Display, run.Server) {
		stopped = true
	}
	fmt.Println()
	if stopped {
		ui.OK("Stopped the Termux:X11 desktop.")
	} else {
		ui.OK("Nothing was running on Termux:X11. Cleaned up, ready for andronix desktop.")
	}
	fmt.Println()
	return nil
}

// x11Error explains a failed Termux:X11 start; the missing app gets a
// branded box with the download link instead of an error.
func x11Error(err error, serverLog string) error {
	switch {
	case errors.Is(err, termux.ErrX11AppMissing):
		fmt.Println()
		fmt.Print(ui.Box(ui.BoxBrand, "One more app: Termux:X11",
			"Termux:X11 shows your Linux desktop as a normal Android app, faster than VNC. It's free and made by the Termux team.", "",
			"1. Open "+termux.X11AppURL,
			"2. Download termux-x11-universal-debug.apk and install it (allow installs from your browser if Android asks).",
			"3. Come back to Termux and run the same command again."))
		ui.Footer()
		return ErrExplained
	case errors.Is(err, termux.ErrX11AppSignature):
		return ui.Errorf("Termux:X11 doesn't match", "The Termux:X11 app and the termux-x11 package come from different builds.",
			"Install the app again from "+termux.X11AppURL+", then run: pkg reinstall "+termux.X11Package)
	case errors.Is(err, termux.ErrX11OldAndroid):
		return ui.Errorf("Termux:X11 needs Android 7+", "This phone's Android is too old for Termux:X11.",
			"Use VNC instead: start the distro and run vncserver-start")
	}
	var ue *ui.UserError
	if errors.As(err, &ue) || errors.Is(err, ui.ErrCancelled) || errors.Is(err, context.Canceled) {
		return err
	}
	return &ui.UserError{Title: "Termux:X11 didn't start", What: err.Error(),
		Fix: "Run andronix desktop stop, then try again. For a black screen, try ANDRONIX_X11_ARGS=-legacy-drawing andronix desktop.", Log: serverLog}
}

func nameOr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// pidFileAlive reports whether the pid in an X lock file is alive.
func pidFileAlive(p string) bool {
	b, err := os.ReadFile(p)
	if err != nil {
		return false
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return termux.Alive(pid)
}
