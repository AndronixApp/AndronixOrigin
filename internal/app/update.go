package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/AndronixApp/andronix-distros/internal/conf"
	"github.com/AndronixApp/andronix-distros/internal/netx"
	"github.com/AndronixApp/andronix-distros/internal/pkgmgr"
	"github.com/AndronixApp/andronix-distros/internal/rootfs"
	"github.com/AndronixApp/andronix-distros/internal/sys"
	"github.com/AndronixApp/andronix-distros/internal/telemetry"
	"github.com/AndronixApp/andronix-distros/internal/ui"
)

// UpdateOpts are the update command's flags.
type UpdateOpts struct {
	Distro   string
	NoSelf   bool   // skip updating the andronix binary
	SelfOnly bool   // only update the andronix binary
	Manifest string // Modded delta manifest (file or URL); DESIGN.md section 10
	Token    string // products-api token for Modded downloads (and the beta channel)
	// Channel "beta" updates andronix to the beta build (Premium: Token
	// from the app, GET /v2/beta/token); "stable" or "" the release.
	Channel string
}

// Update is `andronix update [<distro>]`: the andronix binary itself,
// then each distro's packages, then Modded delta updates.
func Update(ctx context.Context, o UpdateOpts) (err error) {
	telemetry.Notice()
	started, n := time.Now(), 0
	defer func() {
		telemetry.Send("update", map[string]any{"ok": err == nil, "distros": n, "self_only": o.SelfOnly,
			"error_class": telemetry.ErrorClass(err), "duration_ms": time.Since(started).Milliseconds()})
	}()
	fmt.Print(ui.Banner("Linux on Android " + ui.GSep + " " + VersionLabel()))
	lg := NewLog("update")
	defer lg.Close()

	var targets []*Inst
	if !o.SelfOnly {
		if o.Distro != "" {
			d, err := resolveDistro(o.Distro, "update")
			if err != nil {
				return err
			}
			in := Open(d)
			if !in.Installed() {
				return ui.Errorf(d.Label()+" isn't installed", "There's nothing to update.", "Install it first: andronix install "+d.ID)
			}
			targets = append(targets, in)
		} else {
			for _, id := range conf.DistroIDs() {
				d, _ := conf.LoadDistro(id)
				if in := Open(d); in.Installed() {
					targets = append(targets, in)
				}
			}
		}
	}

	n = len(targets)
	var steps []ui.Step
	if !o.NoSelf {
		updated := false
		self := ui.Step{Label: "Updating andronix", Run: func(ctx context.Context, r ui.Reporter) error {
			return selfUpdate(ctx, r, lg, &updated, o)
		}}
		if o.SelfOnly || len(targets) == 0 {
			steps = append(steps, self)
		} else {
			fmt.Println()
			if err := ui.RunSteps(ctx, []ui.Step{self}, lg); err != nil {
				return err
			}
			// The distros get the copy of andronix built into the new
			// binary, so let the new binary do the rest.
			if updated {
				if exe, err := sys.Executable(); err == nil {
					lg.Close()
					sys.Exec(exe, append(os.Args, "--no-self"), os.Environ())
				}
			}
		}
	}
	for _, in := range targets {
		in := in
		d := in.D
		fam, err := pkgmgr.Get(d.Family)
		if err != nil {
			return err
		}
		t := in.Target(true)
		steps = append(steps,
			ui.Step{Label: "Refreshing " + d.Label(), Run: func(ctx context.Context, r ui.Reporter) error {
				rootfs.InstallSelf(in.Rootfs) // the in-distro helper matches this binary
				if err := t.Run(ctx, fam.Update, nil, func(l string) { r.Line(l) }); err != nil {
					return pkgErr("Couldn't reach the package servers", err)
				}
				// Before the upgrade, also on installs made before it existed.
				return preUpgrade(ctx, t, d, r)
			}},
			ui.Step{Label: "Updating " + d.Label(), Run: func(ctx context.Context, r ui.Reporter) error {
				n := count(ctx, t, fam.SimUpgrade, fam)
				if n == 0 {
					return ui.Skip("already up to date")
				}
				if err := pkgRun(ctx, t, fam, fam.Upgrade, n, r); err != nil {
					return pkgErr("Couldn't update "+d.Name, err)
				}
				t.Run(ctx, fam.Clean, nil, nil)
				r.Detail(fmt.Sprintf("%d packages", n))
				return nil
			}},
		)
		if moddedVersion(in.Rootfs) != "" {
			steps = append(steps, ui.Step{Label: "Updating the Modded edition", Run: func(ctx context.Context, r ui.Reporter) error {
				return moddedUpdate(ctx, in, o, r, lg)
			}})
		}
		steps = append(steps, ui.Step{Label: "Tidying " + d.Label(), Run: func(ctx context.Context, r ui.Reporter) error {
			rootfs.EnsureBwrapShim(in.Rootfs)
			if de, err := conf.ResolveDesktop(in.Get("DE")); err == nil && !de.None() {
				writeXstartup(in.Rootfs, de)
			}
			return nil
		}})
	}
	fmt.Println()
	if err := ui.RunSteps(ctx, steps, lg); err != nil {
		if ue, ok := err.(*ui.UserError); ok && ue.Log == "" {
			ue.Log = lg.Path
		}
		return err
	}
	fmt.Println()
	ui.OK("Everything is up to date.")
	fmt.Println()
	return nil
}

// selfUpdate replaces this binary with the mirror's latest build for this
// CPU when its checksum differs, and refreshes the copies inside distros.
func selfUpdate(ctx context.Context, r ui.Reporter, lg *Logger, updated *bool, o UpdateOpts) error {
	beta := o.Channel == "beta"
	if o.Channel != "" && o.Channel != "beta" && o.Channel != "stable" {
		return ui.Errorf("Unknown channel '"+o.Channel+"'", "The update channels are stable and beta.", "Run: andronix update --channel beta --token '<token from the app>'")
	}
	if !beta && !strings.HasPrefix(VersionLabel(), "v") && os.Getenv("ANDRONIX_SELF_UPDATE") == "" {
		return ui.Skip("development build (" + Version + ")")
	}
	arch := sys.DetectArch()
	var urls []string
	want := ""
	if beta {
		res, err := betaResolve(ctx, o.Token)
		if err != nil {
			return err
		}
		urls, want = res.urls(), res.SHA256
		lg.Printf("self-update: beta %s (%s)", res.URL, res.Version)
		r.Label("Updating andronix to beta " + res.Version)
	} else if res := resolve(ctx, "bin", url.Values{"flavor": {binFlavor()}}); res != nil {
		urls, want = res.urls(), res.SHA256
		lg.Printf("self-update: resolved %s (%s)", res.URL, res.Version)
	} else {
		// This build's kind: andronix-android-<cpu> in Termux (andronix-<cpu>
		// on mirrors from before the split), andronix-linux-<cpu> elsewhere.
		name := "andronix-" + binFlavor() + "-" + string(arch)
		base := Mirror() + "/bin/latest/"
		sums, _, err := netx.Get(ctx, base+"SHA256SUMS", nil)
		if err != nil {
			lg.Printf("self-update: %v", err)
			return ui.Skip("couldn't reach the download server")
		}
		want = sumFor(sums, name)
		if want == "" && runtime.GOOS == "android" {
			name = "andronix-" + string(arch)
			want = sumFor(sums, name)
		}
		urls = []string{base + name}
	}
	if want == "" {
		return ui.Skip("no build for " + arch.Label() + " on the server")
	}
	self, err := sys.Executable()
	if err != nil {
		return err
	}
	self, _ = filepath.EvalSymlinks(self)
	if have, _ := netx.FileSHA256(self); have == want {
		return ui.Skip("already the latest")
	}
	tmp := self + ".new"
	var got string
	for _, u := range urls {
		os.Remove(tmp)
		if got, err = netx.Download(ctx, u, tmp, nil, 0, func(c, t int64) { r.Progress(c, t, "bytes") }); err == nil {
			break
		}
		lg.Printf("self-update: %s: %v", u, err)
	}
	if err != nil {
		os.Remove(tmp)
		return downloadErr(err)
	}
	if got != want {
		os.Remove(tmp)
		return ui.Errorf("Update is damaged", "The downloaded andronix doesn't match its checksum, so it wasn't installed.", "Run andronix update again.")
	}
	os.Chmod(tmp, 0o755)
	if err := os.Rename(tmp, self); err != nil {
		os.Remove(tmp)
		return ui.Errorf("Couldn't replace andronix", err.Error(), "Check that Termux can write to "+filepath.Dir(self)+".")
	}
	r.Detail("new version installed")
	*updated = true
	return nil
}

// sumFor finds name's checksum in a SHA256SUMS file.
func sumFor(sums []byte, name string) string {
	for _, l := range strings.Split(string(sums), "\n") {
		if f := strings.Fields(l); len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			return f[0]
		}
	}
	return ""
}

func init() { rootfs.FetchLinux = fetchLinux }

// fetchLinux downloads the GOOS=linux build for inside distros, for
// android builds that don't carry one (development builds): this
// version's, else the latest.
func fetchLinux() ([]byte, error) {
	ctx := context.Background()
	if res := resolve(ctx, "bin", url.Values{"flavor": {"linux"}}); res != nil {
		for _, u := range res.urls() {
			if bin, _, err := netx.Get(ctx, u, nil); err == nil {
				if h := sha256.Sum256(bin); hex.EncodeToString(h[:]) == res.SHA256 {
					return bin, nil
				}
			}
		}
	}
	name := "andronix-linux-" + string(sys.DetectArch())
	dirs := []string{"latest"}
	if strings.HasPrefix(VersionLabel(), "v") {
		dirs = []string{Version, "latest"}
	}
	var err error
	for _, dir := range dirs {
		base := Mirror() + "/bin/" + dir + "/"
		var sums, bin []byte
		if sums, _, err = netx.Get(ctx, base+"SHA256SUMS", nil); err != nil {
			continue
		}
		want := sumFor(sums, name)
		if want == "" {
			err = fmt.Errorf("%s isn't on the mirror", name)
			continue
		}
		if bin, _, err = netx.Get(ctx, base+name, nil); err != nil {
			continue
		}
		if h := sha256.Sum256(bin); hex.EncodeToString(h[:]) != want {
			err = fmt.Errorf("%s doesn't match its checksum", name)
			continue
		}
		return bin, nil
	}
	return nil, err
}

// moddedVersion is EDITION_VERSION from /etc/andronix-modded ("" if the
// install isn't a Modded edition).
func moddedVersion(root string) string {
	b, err := os.ReadFile(filepath.Join(root, "etc/andronix-modded"))
	if err != nil {
		return ""
	}
	return conf.Parse(b).Get("EDITION_VERSION")
}

// Manifest describes a Modded edition's updates (DESIGN.md section 10).
type Manifest struct {
	Edition string  `json:"edition"`
	Distro  string  `json:"distro"`
	DE      string  `json:"de"`
	Arch    string  `json:"arch"`
	Latest  string  `json:"latest"`
	Deltas  []Delta `json:"deltas"`
}

// Delta takes an edition from one version to the next: a tarball of added
// and changed files, with OCI whiteouts (.wh.<name>) for removed ones.
type Delta struct {
	From   string `json:"from"`
	To     string `json:"to"`
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

func moddedUpdate(ctx context.Context, in *Inst, o UpdateOpts, r ui.Reporter, lg *Logger) error {
	src := o.Manifest
	if src == "" {
		src = os.Getenv("ANDRONIX_MODDED_MANIFEST")
	}
	if src == "" {
		return ui.Skip("Modded updates come with a link from the Andronix app")
	}
	var raw []byte
	var err error
	remote := strings.HasPrefix(src, "https://") || strings.HasPrefix(src, "http://")
	if remote {
		raw, _, err = netx.Get(ctx, src, nil)
	} else {
		raw, err = os.ReadFile(src)
	}
	if err != nil {
		return ui.Errorf("Couldn't read the update list", err.Error(), "Get a fresh update link from the Andronix app.")
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return ui.Errorf("The update list is damaged", err.Error(), "Get a fresh update link from the Andronix app.")
	}
	arch := string(sys.DetectArch())
	if (m.Distro != "" && m.Distro != in.D.ID) || (m.Arch != "" && m.Arch != arch) || (m.DE != "" && m.DE != in.Get("DE")) {
		return ui.Errorf("Wrong update list", fmt.Sprintf("This list is for %s %s on %s, not this install.", m.Distro, m.DE, m.Arch),
			"Get the update link for this edition from the Andronix app.")
	}
	cur := moddedVersion(in.Rootfs)
	if cur == m.Latest {
		return ui.Skip("already " + cur)
	}
	// Follow the chain of deltas from the installed version to the latest.
	var chain []Delta
	for v := cur; v != m.Latest; {
		next := -1
		for i, d := range m.Deltas {
			if d.From == v {
				next = i
				break
			}
		}
		if next < 0 || len(chain) > 50 {
			return ui.Errorf("No update path", "There's no update from "+cur+" to "+m.Latest+".",
				"Reinstall the edition from the Andronix app to get "+m.Latest+".")
		}
		chain = append(chain, m.Deltas[next])
		v = m.Deltas[next].To
	}
	cache := GetPaths().Cache
	os.MkdirAll(cache, 0o755)
	for _, d := range chain {
		if strings.ContainsAny(d.File, "/\\") || strings.HasPrefix(d.File, ".") {
			return ui.Errorf("The update list is damaged", "Bad file name "+d.File+".", "Get a fresh update link from the Andronix app.")
		}
		r.Label(fmt.Sprintf("Updating the Modded edition to %s", d.To))
		file := filepath.Join(cache, d.File)
		switch {
		case remote && o.Token != "":
			ms, err := moddedFileSource(d.File, o.Token)
			if err != nil {
				return err
			}
			if _, err := netx.Download(ctx, ms, file+".part", nil, d.Size, func(c, t int64) { r.Progress(c, t, "bytes") }); err != nil {
				return downloadErr(err)
			}
			os.Rename(file+".part", file)
		case remote:
			return ui.Errorf("Missing download token", "Modded updates need the token from the Andronix app.", "Copy the update command from the app; it includes --token.")
		default: // a local manifest: files sit next to it
			file = filepath.Join(filepath.Dir(src), d.File)
		}
		if sum, _ := netx.FileSHA256(file); d.SHA256 != "" && sum != d.SHA256 {
			return ui.Errorf("Update is damaged", d.File+" doesn't match its checksum.", "Run andronix update again.")
		}
		st, _ := os.Stat(file)
		x := &rootfs.Extractor{Root: in.Rootfs, Layers: true, Progress: func(c, t int64) { r.Progress(c, t, "bytes") }}
		if st != nil {
			x.Total = st.Size()
		}
		if err := x.ExtractFile(file); err != nil {
			return ui.Errorf("Couldn't apply the update", err.Error(), "Free up some space, then run andronix update again.")
		}
		x.Finish()
		for _, w := range x.Warnings {
			lg.Printf("delta %s: %s", d.File, w)
		}
		setModdedVersion(in.Rootfs, d.To)
		if remote {
			os.Remove(file)
		}
	}
	r.Detail(cur + " → " + m.Latest)
	return nil
}

func setModdedVersion(root, v string) {
	p := filepath.Join(root, "etc/andronix-modded")
	b, _ := os.ReadFile(p)
	var out []string
	found := false
	for _, l := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		if strings.HasPrefix(l, "EDITION_VERSION=") {
			l, found = "EDITION_VERSION="+v, true
		}
		out = append(out, l)
	}
	if !found {
		out = append(out, "EDITION_VERSION="+v)
	}
	os.WriteFile(p, []byte(strings.Join(out, "\n")+"\n"), 0o644)
}

// moddedFileSource is the products-api download URL for a Modded file.
func moddedFileSource(file, token string) (string, error) {
	d := &conf.Distro{}
	s, err := moddedSource(d, &conf.Desktop{}, "", Paths{}, token+"&f="+file, "")
	if err != nil {
		return "", err
	}
	return s.url, nil
}

// betaResolve asks the resolver for the beta build. The app's token (an
// HMAC like the Modded one, product "beta", from GET /v2/beta/token) goes
// along as given; there is no built-in fallback for beta.
func betaResolve(ctx context.Context, token string) (*Resolved, error) {
	get := "Copy the command again from the Andronix app (Premium, Early access): andronix update --channel beta --token '<token>'"
	q, err := url.ParseQuery(strings.TrimPrefix(strings.TrimSpace(token), "?"))
	if err != nil || token == "" {
		return nil, ui.Errorf("Beta needs a token", "The beta channel is for Andronix Premium; the app gives you the command with its token.", get)
	}
	q.Del("f")
	q.Set("channel", "beta")
	q.Set("flavor", binFlavor())
	res, err := resolveQuery(ctx, "bin", q)
	var se *netx.StatusError
	switch {
	case err == nil:
		return res, nil
	case errors.As(err, &se) && (se.Code == 401 || se.Code == 403):
		return nil, &ui.UserError{Title: "Beta access: Premium", Class: "beta_refused",
			What: "The token was refused (it's for Andronix Premium and Modded Pass owners, and lasts a limited time).", Fix: get, Err: err}
	case errors.As(err, &se) && se.Code == 404:
		return nil, &ui.UserError{Title: "No beta right now", Class: "beta_none",
			What: "There's no beta build for this phone at the moment.", Fix: "Stay on the release: andronix update", Err: err}
	}
	return nil, &ui.UserError{Title: "Couldn't reach the beta channel", Class: "download",
		What: "The Andronix server didn't answer.", Fix: "Check your connection and run the same command again.", Err: err}
}
