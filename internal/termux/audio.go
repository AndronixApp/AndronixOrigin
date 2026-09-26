package termux

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AndronixApp/andronix-distros/internal/sys"
)

// TCPArgs are the arguments of the PulseAudio module the distros play
// through (PULSE_SERVER=127.0.0.1). listen=127.0.0.1 matters: with
// auth-anonymous=1 every client that can reach the port is let in (the
// IP list doesn't restrict it), so the port must not face the network.
const TCPArgs = "listen=127.0.0.1 auth-ip-acl=127.0.0.1 auth-anonymous=1"

const tcpModule = "module-native-protocol-tcp"

// installBackoff stops a failed `pkg install pulseaudio` (no network,
// broken mirror) from being retried on every start.
const installBackoff = 24 * time.Hour

func backoffFile() string {
	return filepath.Join(filepath.Dir(filepath.Dir(logPath())), "pulseaudio-install-failed")
}

// pulseEnv is env without PULSE_SERVER, so pactl talks to the local
// daemon over its unix socket even if the user exported
// PULSE_SERVER=127.0.0.1 in Termux.
func pulseEnv(in []string) []string {
	var env []string
	for _, e := range in {
		if !strings.HasPrefix(e, "PULSE_SERVER=") {
			env = append(env, e)
		}
	}
	return env
}

func pulse(ctx context.Context, name string, args ...string) (string, error) {
	c := sys.CommandContext(ctx, name, args...)
	c.Env = pulseEnv(c.Environ())
	out, err := c.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// AudioInstallDue reports whether EnsureAudio would install pulseaudio:
// it's missing and no failed install is being waited out.
func AudioInstallDue() bool {
	if !isTermux() {
		return false
	}
	if _, err := sys.LookPath("pulseaudio"); err == nil {
		return false
	}
	st, err := os.Stat(backoffFile())
	return err != nil || time.Since(st.ModTime()) >= installBackoff
}

// EnsureAudio makes sure Termux's PulseAudio runs with the TCP module on
// 127.0.0.1, installing pulseaudio first when it's missing and install is
// set (a package install can take minutes, so callers only allow it where
// the user sees progress). It is idempotent and quiet: it returns a short
// summary of what it changed ("" when sound was already set up) and logs
// every action. onLine gets pkg's output while installing (may be nil).
// Off Termux it does nothing.
func EnsureAudio(ctx context.Context, install bool, onLine func(string)) (string, error) {
	if !isTermux() {
		return "", nil
	}
	var did []string
	if _, err := sys.LookPath("pulseaudio"); err != nil {
		if !install {
			Logf("sound: pulseaudio missing; it's installed at the next interactive start")
			return "", nil
		}
		if !AudioInstallDue() {
			Logf("sound: pulseaudio missing; the last install failed less than %s ago, not retrying yet", installBackoff)
			return "", nil
		}
		Logf("sound: pulseaudio missing, installing it")
		if err := pkgInstall(ctx, onLine, "pulseaudio"); err != nil {
			Logf("sound: pkg install pulseaudio failed: %v", err)
			os.MkdirAll(filepath.Dir(backoffFile()), 0o755)
			os.WriteFile(backoffFile(), []byte(time.Now().Format(time.RFC3339)+"\n"), 0o644)
			return "", err
		}
		os.Remove(backoffFile())
		did = append(did, "installed PulseAudio")
	}
	if _, err := pulse(ctx, "pulseaudio", "--check"); err != nil {
		// Start it without --load: a TCP module in the user's default.pa
		// (older Andronix docs) would clash on port 4713 and stop the
		// daemon. The module is checked and loaded below instead.
		out, err := pulse(ctx, "pulseaudio", "--start", "--exit-idle-time=-1")
		if err != nil {
			Logf("sound: pulseaudio --start failed: %v: %s", err, lastLines(out, 3))
			return strings.Join(did, ", "), err
		}
		Logf("sound: started pulseaudio (--exit-idle-time=-1)")
		did = append(did, "started PulseAudio")
	}
	changed, err := ensureTCP(ctx)
	if err != nil {
		Logf("sound: %v", err)
		return strings.Join(did, ", "), err
	}
	if changed != "" {
		did = append(did, changed)
	}
	if len(did) == 0 {
		Logf("sound: pulseaudio already running with the TCP module")
	}
	return strings.Join(did, ", "), nil
}

// ensureTCP loads the TCP module on 127.0.0.1 unless a safe one is
// loaded. A TCP module with anonymous access on every interface (the old
// default.pa line) is swapped for the loopback-only one.
func ensureTCP(ctx context.Context) (string, error) {
	out, err := pulse(ctx, "pactl", "list", "short", "modules")
	if err != nil {
		return "", &pulseError{"pactl list short modules", err, out}
	}
	changed := ""
	for _, l := range strings.Split(out, "\n") {
		f := strings.Split(l, "\t")
		if len(f) < 2 || f[1] != tcpModule {
			continue
		}
		args := ""
		if len(f) > 2 {
			args = f[2]
		}
		if !strings.Contains(args, "auth-anonymous=1") || loopbackOnly(args) {
			Logf("sound: TCP module already loaded (%s)", strings.TrimSpace(args))
			return "", nil
		}
		// Anonymous and open to the network: replace it.
		if o, err := pulse(ctx, "pactl", "unload-module", f[0]); err != nil {
			return "", &pulseError{"pactl unload-module " + f[0], err, o}
		}
		Logf("sound: unloaded TCP module %s (%s): anonymous access on every interface", f[0], args)
		changed = "limited sound to this phone"
	}
	if o, err := pulse(ctx, "pactl", "load-module", tcpModule, TCPArgs); err != nil {
		return "", &pulseError{"pactl load-module " + tcpModule, err, o}
	}
	Logf("sound: loaded %s %s", tcpModule, TCPArgs)
	if changed == "" {
		changed = "enabled sound for distros"
	}
	return changed, nil
}

// loopbackOnly reports whether module args bind the socket to loopback.
func loopbackOnly(args string) bool {
	for _, a := range strings.Fields(args) {
		if v, ok := strings.CutPrefix(a, "listen="); ok {
			return v == "127.0.0.1" || v == "localhost" || v == "::1"
		}
	}
	return false
}

type pulseError struct {
	cmd string
	err error
	out string
}

func (e *pulseError) Error() string {
	return e.cmd + " failed: " + e.err.Error() + ": " + lastLines(e.out, 3)
}
