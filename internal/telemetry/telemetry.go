// Package telemetry sends anonymous installer events to products-api
// (POST /v1/events), so broken installs show up without a support ticket.
//
// What goes out: the event name, a random install id (made once, kept in
// ~/.andronix/telemetry), the time, and props such as distro, desktop, CPU,
// Android SDK level, Termux build, andronix version, duration, and for a
// failure the step and an error class. Never paths, user names, tokens,
// emails or command lines.
//
// It never blocks or fails anything: each event is posted by a detached
// copy of andronix (`andronix __telemetry <json>`) with a 2 s timeout, so
// the installer can exit or exec into the distro shell straight away.
// Off with `andronix telemetry off`, ANDRONIX_NO_TELEMETRY=1 or
// DO_NOT_TRACK=1; a one-line notice is shown the first time.
package telemetry

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/AndronixApp/andronix-distros/internal/conf"
	"github.com/AndronixApp/andronix-distros/internal/sys"
	"github.com/AndronixApp/andronix-distros/internal/ui"
)

// DefaultURL is products-api's events endpoint.
const DefaultURL = "https://products.andronix.xyz/v1/events"

var (
	home, version string
	mu            sync.Mutex
)

// Init tells the package where andronix keeps its files and its version.
func Init(andronixHome, ver string) { home, version = andronixHome, ver }

func stateFile() string { return filepath.Join(home, "telemetry") }

func state() conf.Values {
	b, _ := os.ReadFile(stateFile())
	return conf.Parse(b)
}

func save(v conf.Values) {
	os.MkdirAll(home, 0o755)
	var b strings.Builder
	for _, k := range []string{"ID", "ENABLED", "NOTICE"} {
		if v[k] != "" {
			b.WriteString(k + "=" + v[k] + "\n")
		}
	}
	os.WriteFile(stateFile(), []byte(b.String()), 0o644)
}

// Enabled reports whether events may be sent: not turned off by the user,
// by the environment, or because this runs inside a distro.
func Enabled() bool {
	if home == "" || os.Getenv("ANDRONIX_NO_TELEMETRY") != "" || os.Getenv("DO_NOT_TRACK") == "1" {
		return false
	}
	if _, err := os.Stat("/etc/andronix-release"); err == nil {
		return false // inside a distro; the Termux side reports
	}
	return state().Get("ENABLED") != "no"
}

// SetEnabled is `andronix telemetry on|off`.
func SetEnabled(on bool) {
	mu.Lock()
	defer mu.Unlock()
	v := state()
	v["ENABLED"] = map[bool]string{true: "yes", false: "no"}[on]
	v["NOTICE"] = "shown"
	save(v)
}

// id is the anonymous install id, made on first use.
func id() string {
	mu.Lock()
	defer mu.Unlock()
	v := state()
	if len(v.Get("ID")) != 32 {
		b := make([]byte, 16)
		rand.Read(b)
		v["ID"] = hex.EncodeToString(b)
		save(v)
	}
	return v.Get("ID")
}

// Notice prints the one-line notice the first time events are sent.
func Notice() {
	if !Enabled() {
		return
	}
	mu.Lock()
	v := state()
	shown := v.Get("NOTICE") == "shown"
	if !shown {
		v["NOTICE"] = "shown"
		save(v)
	}
	mu.Unlock()
	if !shown {
		ui.Note("Andronix sends anonymous install events (distro, desktop, result) to find broken installs. Turn off: andronix telemetry off")
	}
}

// Event is one telemetry event.
type Event struct {
	Event     string         `json:"event"`
	InstallID string         `json:"install_id"`
	Time      string         `json:"ts"`
	Props     map[string]any `json:"props"`
}

// Send posts an event in the background (see the package comment).
func Send(name string, props map[string]any) {
	if !Enabled() {
		return
	}
	b, err := json.Marshal(build(name, props))
	if err != nil {
		return
	}
	self, err := sys.Executable()
	if err != nil {
		return
	}
	c := sys.Command(self, "__telemetry", string(b))
	c.Stdin, c.Stdout, c.Stderr = nil, nil, nil
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if c.Start() == nil {
		go c.Wait()
	}
}

func build(name string, props map[string]any) Event {
	p := map[string]any{
		"arch":          string(sys.DetectArch()),
		"android_sdk":   androidSDK(),
		"termux_flavor": termuxFlavor(),
		"version":       version,
	}
	for k, v := range props {
		p[k] = v
	}
	return Event{Event: name, InstallID: id(), Time: time.Now().UTC().Format(time.RFC3339), Props: p}
}

// Post sends one event now, with a 2 s timeout (`andronix __telemetry`).
func Post(body string) error {
	url := os.Getenv("ANDRONIX_TELEMETRY_URL")
	if url == "" {
		url = DefaultURL
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBufferString(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "andronix/"+version)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

var sdkOnce sync.Once
var sdk int

// androidSDK is ro.build.version.sdk (0 off Android).
func androidSDK() int {
	sdkOnce.Do(func() {
		if out, err := sys.Command("getprop", "ro.build.version.sdk").Output(); err == nil {
			for _, c := range strings.TrimSpace(string(out)) {
				if c < '0' || c > '9' {
					return
				}
				sdk = sdk*10 + int(c-'0')
			}
		}
	})
	return sdk
}

// termuxFlavor is which Termux build: google_play_store, f_droid, github,
// or "" off Termux (TERMUX_APK_RELEASE, else TERMUX_VERSION).
func termuxFlavor() string {
	if r := os.Getenv("TERMUX_APK_RELEASE"); r != "" {
		return strings.ToLower(r)
	}
	if v := os.Getenv("TERMUX_VERSION"); strings.HasPrefix(v, "googleplay") {
		return "google_play_store"
	} else if v != "" {
		return "unknown"
	}
	return ""
}

var slug = regexp.MustCompile(`[^a-z0-9]+`)

// ErrorClass names a failure without its details: "cancelled", a class
// the error carries (download, package_manager, disk, ...), else a slug of
// its title.
func ErrorClass(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ui.ErrCancelled) || errors.Is(err, context.Canceled):
		return "cancelled"
	}
	var ue *ui.UserError
	if errors.As(err, &ue) {
		if ue.Class != "" {
			return ue.Class
		}
		s := strings.Trim(slug.ReplaceAllString(strings.ToLower(ue.Title), "_"), "_")
		if len(s) > 48 {
			s = s[:48]
		}
		return s
	}
	return "other"
}
