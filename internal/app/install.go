package app

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	andronix "github.com/AndronixApp/andronix-distros"
	"github.com/AndronixApp/andronix-distros/internal/conf"
	"github.com/AndronixApp/andronix-distros/internal/netx"
	"github.com/AndronixApp/andronix-distros/internal/oci"
	"github.com/AndronixApp/andronix-distros/internal/pkgmgr"
	"github.com/AndronixApp/andronix-distros/internal/proot"
	"github.com/AndronixApp/andronix-distros/internal/rootfs"
	"github.com/AndronixApp/andronix-distros/internal/sys"
	"github.com/AndronixApp/andronix-distros/internal/telemetry"
	"github.com/AndronixApp/andronix-distros/internal/termux"
	"github.com/AndronixApp/andronix-distros/internal/ui"
)

// InstallOpts are the install command's flags.
type InstallOpts struct {
	Distro, Desktop    string
	NoBrowser, NoStart bool
	Reinstall, NoUser  bool
	// Modded installs a paid Modded edition: Token (from the app) signs a
	// products-api download of the edition's tarball.
	Modded bool
	Token  string
	// Edition is a Modded edition id (editions.conf), e.g. legacy-debian.
	// It implies Modded and picks the distro and desktop.
	Edition string
	report  *installReport
}

// DefaultMirror is the public URL of the Andronix R2 bucket (decided:
// R2 is the default source, the registry the fallback). The domain is a
// proposal until the bucket's custom domain is set up.
const DefaultMirror = "https://dl.andronix.app"

// Mirror is where CI publishes rootfs tarballs and binaries:
//
//	<mirror>/rootfs/<id>/<version>/<id>-<version>-<arch>.tar.xz (+ .sha256)
//	<mirror>/bin/latest/andronix-<arch>, <mirror>/bin/latest/SHA256SUMS
func Mirror() string {
	if m := os.Getenv("ANDRONIX_MIRROR"); m != "" {
		return strings.TrimRight(m, "/")
	}
	return DefaultMirror
}

type source struct {
	kind  string // local | tarball | registry
	file  string
	url   string
	size  int64
	sha   string
	image *oci.Image
	// sumURL is where a Modded download's sha256 comes from (the same
	// token check, <file>.sha256).
	sumURL string
}

// Install is `andronix install`. It tells the Andronix app how the
// install went when the app asked (see report.go).
func Install(ctx context.Context, o InstallOpts) error {
	o.report = &installReport{distro: o.Distro, de: o.Desktop, modded: o.Modded || o.Edition != "", started: time.Now()}
	telemetry.Notice()
	err := install(ctx, o)
	if err != nil {
		o.report.err = err
		o.report.send("fail")
	}
	o.report.send("ok") // no-op if sent already (finish, or the failure above)
	return err
}

func install(ctx context.Context, o InstallOpts) error {
	rep := o.report
	var ed *conf.Edition
	if o.Edition != "" {
		var err error
		if ed, err = conf.ResolveEdition(o.Edition); err != nil {
			return ui.Errorf("Unknown edition '"+o.Edition+"'", "Andronix has no Modded edition called '"+o.Edition+"'.",
				"Copy the install command from the Andronix app again. Editions: "+strings.Join(conf.EditionIDs(), ", "))
		}
		o.Modded = true
		for _, f := range []struct {
			got        *string
			want, what string
		}{{&o.Distro, ed.Distro, "distro"}, {&o.Desktop, ed.Desktop, "desktop"}} {
			if *f.got == "" {
				*f.got = f.want
			} else if n := resolvedID(f.what, *f.got); n != f.want {
				return ui.Errorf("That edition doesn't match", "The "+ed.ID+" edition is "+ed.Distro+" with "+ed.Desktop+", not "+*f.got+".",
					"Copy the install command from the Andronix app again.")
			}
		}
	}
	// Pick a distro and desktop, asking when we can.
	if o.Distro == "" && ui.Interactive {
		var opts []ui.Option
		for _, id := range conf.DistroIDs() {
			d, _ := conf.LoadDistro(id)
			opts = append(opts, ui.Option{Label: d.Label(), Value: id})
		}
		v, err := ui.Choose("Which distro?", "You can install more later.", opts, "debian")
		if err != nil {
			return err
		}
		o.Distro = v
	}
	d, err := resolveDistro(o.Distro, "install")
	if err != nil {
		return err
	}
	rep.distro = d.ID
	// Old app keys (LXDE, the window managers) map to a desktop we offer;
	// if that one isn't offered on this distro, XFCE is.
	if de, notice, err := conf.ResolveDesktopCompat(o.Desktop); err == nil && notice != "" {
		if !de.Supports(d) {
			de, _ = conf.ResolveDesktop("xfce")
			notice = o.Desktop + " isn't offered any more. Installing XFCE instead."
		}
		ui.Warn(notice)
		o.Desktop = de.ID
	}
	if o.Desktop == "" {
		o.Desktop = "xfce"
		if ui.Interactive {
			var opts []ui.Option
			for _, id := range conf.DesktopIDs() {
				de, _ := conf.ResolveDesktop(id)
				if !de.Supports(d) {
					continue
				}
				opts = append(opts, ui.Option{Label: de.Name, Value: id})
			}
			if o.Desktop, err = ui.Choose("Which desktop?", "XFCE is light and works well on phones.", opts, "xfce"); err != nil {
				return err
			}
		}
	}
	de, err := conf.ResolveDesktop(o.Desktop)
	if err != nil {
		return ui.Errorf("Unknown desktop '"+o.Desktop+"'", "Andronix can't install '"+o.Desktop+"'.",
			"Available: "+strings.Join(conf.DesktopIDs(), ", "))
	}
	rep.de = de.ID
	fam, err := pkgmgr.Get(d.Family)
	if err != nil {
		return err
	}
	if !de.Supports(d) {
		fix := "Try --de xfce, or --de none for the command line only."
		if on := de.SupportedOn(); len(on) > 0 {
			fix = de.Name + " is available on " + strings.Join(on, ", ") + ". On " + d.Label() + ", try --de xfce."
		}
		return ui.Errorf(de.Name+" isn't available on "+d.Label(), d.Label()+" doesn't offer "+de.Name+" yet.", fix)
	}
	arch := sys.DetectArch()
	in := Open(d)
	deLabel := de.Name

	fmt.Print(ui.Banner("Linux on Android " + ui.GSep + " " + VersionLabel()))

	// The edition this install is: "" free, else a Modded edition id.
	edition := ""
	if ed != nil {
		edition = ed.ID
	} else if o.Modded {
		edition = d.ID + "-" + de.ID
	}
	have := installedEdition(in)
	if in.Installed() && !o.Reinstall && have != edition && (o.Modded || have != "") {
		name := func(e string) string {
			if e == "" {
				return "the free edition"
			}
			return editionName(e) + " (" + e + ")"
		}
		return ui.Errorf(d.Label()+" is already installed", "It's "+name(have)+"; this command installs "+name(edition)+".",
			"To replace it, add --reinstall (this deletes everything inside it; back it up first: andronix backup "+d.ID+").")
	}
	if in.Installed() && !o.Reinstall && (in.Get("DE") == de.ID || de.None()) {
		WriteLaunchers(d)             // refresh in case andronix moved
		rootfs.InstallSelf(in.Rootfs) // and keep the in-distro helper current
		if cur, err := conf.ResolveDesktop(in.Get("DE")); err == nil && !cur.None() {
			writeXstartup(in.Rootfs, cur)
		}
		fmt.Print(ui.Box(ui.BoxOK, d.Label()+" is already installed",
			ui.KV("Desktop", in.Get("DE_NAME")), ui.KV("Start", "./"+d.MainStart()), "",
			"To start over, run: andronix install "+d.ID+" --reinstall"))
		fmt.Println()
		return nil
	}
	if o.Reinstall {
		if _, err := os.Stat(in.Dir); err == nil {
			fmt.Print(ui.Box(ui.BoxWarn, "Reinstall "+d.Label()+"?",
				"This deletes everything inside "+d.Label()+", including your files in it. Files in /sdcard are safe."))
			ok, err := ui.Confirm("Delete it and install again?", "", false)
			if err != nil || !ok {
				ui.Note("Nothing changed.")
				return err
			}
			if err := rootfs.Remove(in.Dir); err != nil {
				return ui.Errorf("Couldn't delete the old files", err.Error(), "Exit every session of "+d.Name+", then try again.")
			}
		}
	}
	haveRootfs := false
	switch in.Get("STAGE") {
	case "configured", "desktop", "done":
		if _, err := os.Stat(in.Rootfs); err == nil {
			haveRootfs = true
		}
	}

	fmt.Print(ui.Box(ui.BoxBrand, d.Label()+" "+ui.GSep+" "+deLabel,
		ui.KV("CPU", arch.Label()), ui.KV("Desktop", deLabel), ui.KV("Location", ui.Tilde(in.Dir))))
	fmt.Println()
	ui.Note("Keep Termux open while this runs; it takes 5 to 30 minutes on a phone. Android may ask whether Termux can run in the background: tap Allow, so the install isn't stopped.")
	// This phone's compatibility rules (compat.json): what's known before
	// the probe (kernel, Android, Termux, a cached probe).
	cr := newCompat(d, de.ID)
	shown := map[string]bool{}
	if sys.IsTermux() {
		cr.warnings(shown)
		if err := cr.refuse(d); err != nil {
			return err
		}
	}
	fmt.Println()

	lg := NewLog("install-" + d.ID)
	defer lg.Close()
	lg.Printf("andronix %s install %s de=%s arch=%s termux=%v", Version, d.ID, de.ID, arch, sys.IsTermux())
	if sys.IsTermux() {
		sys.Command("termux-wake-lock").Run()
		defer sys.Command("termux-wake-unlock").Run()
	}

	paths := GetPaths()
	os.MkdirAll(paths.Cache, 0o755)
	var src source
	var lowRAM int64
	t := in.Target(true)
	run := func(r ui.Reporter, cmd string) error {
		return t.Run(ctx, cmd, nil, func(l string) { r.Line(l) })
	}

	var steps []ui.Step
	// proot comes from Termux's packages: install it here (with pkg's lock
	// and its recovery for a half-upgraded Termux) rather than asking.
	if _, err := sys.LookPath("proot"); err != nil && sys.IsTermux() {
		steps = append(steps, ui.Step{Label: "Installing proot", Run: func(ctx context.Context, r ui.Reporter) error {
			if err := termux.Install(ctx, r.Line, "proot"); err != nil {
				return &ui.UserError{Title: "Couldn't install proot", Class: "termux_pkg",
					What: "Termux's package manager stopped with an error.",
					Fix:  "Update Termux's packages and install it, then run the same command again: pkg upgrade -y && pkg install proot -y", Err: err}
			}
			return nil
		}})
	}
	steps = append(steps, []ui.Step{
		{Label: "Checking your phone", Run: func(ctx context.Context, r ui.Reporter) error {
			if _, err := sys.LookPath("proot"); err != nil {
				return ui.Errorf("proot is missing", "Andronix needs proot to run Linux inside Termux.", "Install it, then run the same command again: pkg install proot -y")
			}
			if d.MaxKernel != "" && kernelNewer(sys.KernelRelease(), d.MaxKernel) {
				return ui.Errorf(d.Label()+" can't run on this phone yet",
					d.Name+" needs an Android kernel up to "+d.MaxKernel+"; this phone has "+sys.KernelRelease()+". A fix in proot is in progress.",
					"Pick another distro for now, e.g. Debian or Alpine.")
			}
			if !d.HasArch(string(arch)) {
				return ui.Errorf(d.Label()+" isn't available here", d.Label()+" has no build for "+arch.Label()+" phones.",
					"Pick another distro in the Andronix app, e.g. Debian.")
			}
			need := int64(de.DiskMB + 300)
			if !haveRootfs {
				need += int64(d.DownloadMB*2 + d.DiskMB)
			}
			free := sys.FreeMB(sys.Home())
			lg.Printf("free=%dMB need=%dMB", free, need)
			if free >= 0 && free < need {
				return ui.Errorf("Not enough space", fmt.Sprintf("Your phone has %d MB free; %s with %s needs about %d MB.", free, d.Label(), deLabel, need),
					"Free up some space (old videos, app caches), then run the same command again.")
			}
			if de.MinRAMMB > 0 {
				if ram := sys.TotalRAMMB(); ram > 0 && ram < int64(de.MinRAMMB) {
					lg.Printf("ram=%dMB below %s's %dMB", ram, de.Name, de.MinRAMMB)
					r.Detail(fmt.Sprintf("%s · only %d MB RAM", arch.Label(), ram))
					lowRAM = ram
				}
			}
			if lowRAM == 0 {
				r.Detail(arch.Label())
			}
			os.MkdirAll(in.Dir, 0o755)
			in.Set("DISTRO", d.ID)
			in.Set("VERSION", d.Version)
			in.Set("ARCH", string(arch))
			in.Set("EDITION", edition)
			return nil
		}},
		{Label: "Finding the best download", Run: func(ctx context.Context, r ui.Reporter) error {
			if haveRootfs {
				return ui.Skip("already downloaded")
			}
			var s source
			var err error
			if o.Modded && os.Getenv("ANDRONIX_ROOTFS") == "" {
				if s, err = moddedSource(d, de, arch, paths, o.Token, edition); err == nil {
					if err = moddedChecksum(ctx, &s, lg, r.Line); err != nil {
						if e := earlyErr(err, ed); e != nil {
							err = e
						}
					}
				}
			} else {
				s, err = pickSource(ctx, d, arch, paths, lg)
			}
			if err != nil {
				return err
			}
			src = s
			switch s.kind {
			case "local":
				r.Detail("local file")
			case "tarball":
				r.Detail("Andronix mirror, " + ui.Bytes(s.size))
			case "upstream":
				r.Detail(d.Name + "'s own download, " + ui.Bytes(s.size))
			case "modded":
				r.Detail(editionName(edition))
			default:
				r.Detail("official image, " + ui.Bytes(s.image.Total))
			}
			return nil
		}},
		{Label: "Downloading " + d.Label(), Run: func(ctx context.Context, r ui.Reporter) error {
			if haveRootfs || src.kind == "local" {
				return ui.Skip("nothing to download")
			}
			if src.kind == "tarball" || src.kind == "upstream" || src.kind == "modded" {
				part := src.file + ".part"
				sum, err := netx.Download(ctx, src.url, part, nil, src.size, func(c, t int64) { r.Progress(c, t, "bytes") })
				if err != nil {
					if e := earlyErr(err, ed); e != nil {
						return e
					}
					return downloadErr(err)
				}
				if src.sha != "" && sum != src.sha {
					os.Remove(part)
					return ui.Errorf("Download is damaged", "The file's checksum doesn't match, so it was deleted.", "Run the same command again to download it fresh.")
				}
				r.Detail("sha256 checked")
				return os.Rename(part, src.file)
			}
			// Registry layers, each verified against its digest.
			dir := filepath.Join(paths.Cache, "layers-"+d.ID)
			os.MkdirAll(dir, 0o755)
			var done int64
			for _, l := range src.image.Layers {
				dst := filepath.Join(dir, strings.TrimPrefix(l.Digest, "sha256:"))
				if sum, _ := netx.FileSHA256(dst); "sha256:"+sum == l.Digest {
					done += l.Size
					continue
				}
				base := done
				sum, err := netx.Download(ctx, src.image.BlobURL(l), dst+".part", src.image.Headers(), l.Size,
					func(c, _ int64) { r.Progress(base+c, src.image.Total, "bytes") })
				if err != nil {
					return downloadErr(err)
				}
				if "sha256:"+sum != l.Digest {
					os.Remove(dst + ".part")
					return ui.Errorf("Download is damaged", "A layer's checksum doesn't match, so it was deleted.", "Run the same command again to download it fresh.")
				}
				os.Rename(dst+".part", dst)
				done += l.Size
			}
			r.Detail("digests checked")
			return nil
		}},
		{Label: "Unpacking files", Run: func(ctx context.Context, r ui.Reporter) error {
			if haveRootfs {
				return ui.Skip("already unpacked")
			}
			rootfs.Remove(in.Rootfs)
			os.MkdirAll(in.Rootfs, 0o755)
			x := &rootfs.Extractor{Root: in.Rootfs, Progress: func(c, t int64) { r.Progress(c, t, "bytes") }}
			var files []string
			if src.kind == "registry" {
				x.Layers = true
				x.Total = src.image.Total
				for _, l := range src.image.Layers {
					files = append(files, filepath.Join(paths.Cache, "layers-"+d.ID, strings.TrimPrefix(l.Digest, "sha256:")))
				}
			} else { // local, tarball or upstream: one archive
				st, _ := os.Stat(src.file)
				if st != nil {
					x.Total = st.Size()
				}
				files = []string{src.file}
			}
			for _, f := range files {
				if err := x.ExtractFile(f); err != nil {
					return ui.Errorf("Unpacking failed", "The files couldn't be unpacked: "+err.Error(), "Free up some space, then run the same command again.")
				}
			}
			x.Finish()
			for _, w := range x.Warnings {
				lg.Printf("extract: %s", w)
			}
			if _, err := os.Lstat(filepath.Join(in.Rootfs, "etc/passwd")); err != nil {
				return ui.Errorf("Unpacking failed", "The download doesn't contain a usable system.", "Run the same command again with --reinstall.")
			}
			if src.kind == "upstream" {
				// Not cleaned by our CI: strip kernel/firmware packages, default
				// users and passwords, and identity, like ci/build-rootfs.sh.
				if rm := d.UpstreamRemove(); len(rm) > 0 && fam.ID == "pacman" {
					run(r, "pkgs=$(pacman -Qq "+strings.Join(rm, " ")+" 2>/dev/null); [ -z \"$pkgs\" ] || pacman -Rns --noconfirm $pkgs; rm -rf /boot/*")
				}
				if err := run(r, rootfs.CleanScript); err != nil {
					lg.Printf("upstream clean: %v", err)
				}
			}
			// Whether the image itself has the desktop, before any package
			// run: a desktop install that stopped part way leaves the
			// session binary too, and mustn't count as prebuilt on resume.
			pb := "no"
			if sessionPresent(in, de) {
				pb = "yes"
			}
			in.Set("PREBUILT", pb)
			return in.Set("STAGE", "extracted")
		}},
		{Label: "Setting up " + d.Label(), Run: func(ctx context.Context, r ui.Reporter) error {
			// Measure this phone inside the rootfs, then apply the fixes
			// (idempotent: every install and resume runs it).
			compatFix := func(ctx context.Context) error {
				if !sys.IsTermux() {
					return nil
				}
				cr.probe(ctx, t, lg)
				if err := cr.refuse(d); err != nil {
					return err
				}
				cr.apply(in.Rootfs, arch, lg)
				return in.Set("COMPAT", strings.Join(cr.plan.IDs, " "))
			}
			if in.Get("STAGE") != "extracted" && haveRootfs {
				rootfs.InstallSelf(in.Rootfs) // keep the in-distro helper current
				// A resumed install (maybe begun by an older andronix) still
				// gets this phone's fixes before the package steps.
				if err := compatFix(ctx); err != nil {
					return err
				}
				return ui.Skip("already set up")
			}
			edition := "free"
			if moddedImage(in) || o.Modded {
				edition = "modded"
			}
			if err := rootfs.Configure(in.Dir, in.Rootfs, rootfs.Info{ID: d.ID, Label: d.Label(), Start: d.MainStart(),
				Version: Version, Family: d.Family, Edition: edition}); err != nil {
				return ui.Errorf("Setup failed", "Couldn't write settings into "+d.Label()+": "+err.Error(), "Run the same command again.")
			}
			if fam.Configure != nil {
				if err := fam.Configure(in.Rootfs); err != nil {
					lg.Printf("family configure: %v", err)
				}
			}
			if d.NoSnap {
				pkgmgr.NoSnap(in.Rootfs)
			}
			pkgmgr.SetMirror(in.Rootfs, d.Family, d.MirrorFor(string(arch)))
			if err := compatFix(ctx); err != nil {
				return err
			}
			if fam.FchmodatShim {
				if err := installShim(in.Rootfs, arch); err != nil {
					lg.Printf("fchmodat shim: %v", err)
				}
			}
			if err := pkgmgr.RewriteSources(in.Rootfs, d.MirrorRewrite[0], d.MirrorRewrite[1]); err != nil {
				lg.Printf("mirror rewrite: %v", err)
			}
			return in.Set("STAGE", "configured")
		}},
		{Label: "Refreshing package lists", Run: func(ctx context.Context, r ui.Reporter) error {
			if err := run(r, fam.Update); err != nil {
				return pkgErr("Couldn't reach the package servers", err)
			}
			if err := cr.preScripts(ctx, t, r); err != nil {
				return err
			}
			return preUpgrade(ctx, t, d, r)
		}},
		{Label: "Updating " + d.Label(), Run: func(ctx context.Context, r ui.Reporter) error {
			if prebuilt(in, de) {
				return ui.Skip("the image is up to date")
			}
			n := count(ctx, t, fam.SimUpgrade, fam)
			if n == 0 {
				return ui.Skip("already up to date")
			}
			r.Label(fmt.Sprintf("Updating %s", d.Label()))
			if err := pkgRun(ctx, t, fam, fam.Upgrade, n, r); err != nil {
				return pkgErr("Couldn't update "+d.Name, err)
			}
			r.Detail(fmt.Sprintf("%d packages", n))
			return nil
		}},
		{Label: installLabel(de), Run: func(ctx context.Context, r ui.Reporter) error {
			// A prebuilt image (a Modded edition, or a rootfs exported with
			// its desktop) already has everything: skip the package run.
			if prebuilt(in, de) {
				return ui.Skip("already in the image")
			}
			pkgs := append([]string{}, d.BasePkgs...)
			if !de.None() {
				pkgs = append(pkgs, de.Packages(d.Family)...)
				pkgs = append(pkgs, fam.VNC...)
				// Optional packages: only those this distro has.
				var opt []string
				for _, o := range de.OptionalPackages(d.Family) {
					if !cr.plan.SkipOptional[o] {
						opt = append(opt, o)
					}
				}
				if len(opt) > 0 {
					t.Run(ctx, fam.FilterAvailable(opt), nil, func(l string) {
						if l = strings.TrimSpace(l); l != "" && !strings.Contains(l, " ") {
							pkgs = append(pkgs, l)
						}
					})
				}
			}
			n := count(ctx, t, fam.SimInstall(pkgs), fam)
			if err := pkgRun(ctx, t, fam, fam.Install(pkgs), n, r); err != nil {
				return pkgErr("Couldn't install "+strings.ToLower(installLabel(de)[len("Installing "):]), err)
			}
			if n > 0 {
				r.Detail(fmt.Sprintf("%d packages", n))
			}
			run(r, fam.Clean)
			return nil
		}},
		{Label: "Installing a web browser", Run: func(ctx context.Context, r ui.Reporter) error {
			b := d.BrowserFor(string(arch))
			switch {
			case de.None() || o.NoBrowser:
				return ui.Skip("not requested")
			case b == "":
				return ui.Skip("none packaged for " + arch.Label())
			case prebuilt(in, de) && (moddedImage(in) || hasAny(in.Rootfs, "usr/bin/"+b, "usr/bin/firefox", "usr/lib/firefox")):
				return ui.Skip("already in the image")
			}
			r.Label("Installing " + strings.Title(strings.TrimSuffix(b, "-esr")))
			if err := browserRepo(ctx, d, in.Rootfs, t, fam); err != nil {
				lg.Printf("mozilla repo: %v", err)
				return ui.Skip("couldn't reach Mozilla; install it later")
			}
			n := count(ctx, t, fam.SimInstall([]string{b}), fam)
			if err := pkgRun(ctx, t, fam, fam.Install([]string{b}), n, r); err != nil {
				lg.Printf("browser: %v", err)
				return ui.Skip("failed; try later: install " + b)
			}
			installCodecs(ctx, t, fam, d, r, lg)
			run(r, fam.Clean)
			return nil
		}},
		{Label: "Setting up the desktop", Run: func(ctx context.Context, r ui.Reporter) error {
			if de.None() {
				return ui.Skip("command line only")
			}
			// Images ship without SSH host keys; make this phone's own if
			// an SSH server is present.
			run(r, "[ -d /etc/ssh ] && command -v ssh-keygen >/dev/null && ssh-keygen -A >/dev/null 2>&1; true")
			if err := setupDesktop(ctx, in, de, t, r); err != nil {
				return ui.Errorf("Desktop setup failed", err.Error(), "Run the same command again.")
			}
			in.Set("DE", de.ID)
			in.Set("DE_NAME", deLabel)
			rootfs.SetRelease(in.Rootfs, map[string]string{"ANDRONIX_DE": de.ID})
			return in.Set("STAGE", "desktop")
		}},
		{Label: "Writing " + d.MainStart(), Run: func(ctx context.Context, r ui.Reporter) error {
			if de.None() {
				in.Set("DE", "none")
				in.Set("DE_NAME", "Command line")
				rootfs.SetRelease(in.Rootfs, map[string]string{"ANDRONIX_DE": "none"})
			}
			renamed, err := WriteLaunchers(d)
			if err != nil {
				return ui.Errorf("Couldn't write the start script", err.Error(), "Check that Termux can write to its home folder.")
			}
			in.Set("RENAMED", strings.Join(renamed, " "))
			in.Set("SOURCE", src.kind)
			in.Set("STAGE", "done")
			os.RemoveAll(filepath.Join(paths.Cache, "layers-"+d.ID))
			// The image isn't needed any more (a Modded or Classic one
			// never stays on the phone longer than the install).
			if src.kind == "tarball" || src.kind == "modded" || src.kind == "upstream" {
				os.Remove(src.file)
			}
			return nil
		}},
	}...)
	for i := range steps {
		label, run := steps[i].Label, steps[i].Run
		steps[i].Run = func(ctx context.Context, r ui.Reporter) error {
			rep.step = label // the failed step, for the app
			return run(ctx, r)
		}
	}
	telemetry.Send("install_started", map[string]any{"distro": d.ID, "de": de.ID, "modded": o.Modded, "edition": edition})
	if err := ui.RunSteps(ctx, steps, lg); err != nil {
		if ue, ok := err.(*ui.UserError); ok && ue.Log == "" {
			ue.Log = lg.Path
		}
		return err
	}
	rep.step = ""

	// A normal user instead of root (asks here; otherwise at first login).
	user := in.Get("USER")
	if user == "" && !o.NoUser {
		if ui.Interactive {
			fmt.Println()
			if err := t.Interactive("/usr/local/bin/andronix setup-user", false); err == nil {
				if b, err := os.ReadFile(filepath.Join(in.Rootfs, "etc/andronix/user")); err == nil {
					user = strings.TrimSpace(string(b))
					in.Set("USER", user)
				}
			}
		} else {
			os.WriteFile(filepath.Join(in.Rootfs, "etc/andronix/firstboot"), []byte("1\n"), 0o644)
		}
	}
	if lowRAM > 0 {
		ui.Warn(fmt.Sprintf("%s needs about %d MB of RAM; this phone has %d MB. It may be slow or close apps; XFCE is lighter.", de.Name, de.MinRAMMB, lowRAM))
	}
	finish(in, de, user, o.NoStart, rep)
	return nil
}

// installShim puts the fchmodat2 preload shim for this CPU into the
// distro and registers it in /etc/ld.so.preload, before any package run.
func installShim(root string, arch sys.Arch) error {
	b, err := andronix.Preload.ReadFile("guest/preload/" + string(arch) + "/libandronix-fchmodat.so")
	if err != nil {
		return err
	}
	lib := "/usr/local/lib/andronix/libandronix-fchmodat.so"
	if err := os.MkdirAll(filepath.Join(root, filepath.Dir(lib)), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, lib), b, 0o755); err != nil {
		return err
	}
	p := filepath.Join(root, "etc/ld.so.preload")
	cur, _ := os.ReadFile(p)
	if strings.Contains(string(cur), lib) {
		return nil
	}
	os.Remove(p)
	return os.WriteFile(p, append(cur, []byte(lib+"\n")...), 0o644)
}

// browserRepo adds the browser's own package repository (Mozilla's) and
// refreshes the package lists; nothing for distros without one.
func browserRepo(ctx context.Context, d *conf.Distro, root string, t *proot.Target, fam *pkgmgr.Family) error {
	if d.BrowserRepo != "mozilla" {
		return nil
	}
	key, _, err := netx.Get(ctx, pkgmgr.MozillaKeyURL, nil)
	if err == nil {
		err = pkgmgr.MozillaRepo(root, key)
	}
	if err == nil {
		err = t.Run(ctx, fam.Update, nil, nil)
	}
	return err
}

// installCodecs installs the browser's video codecs (DISTRO_BROWSER_CODECS),
// if any are missing. Optional: if the set fails, each on its own (Fedora's
// openh264 comes from Cisco's repo, which may be off). It reports whether
// it installed anything.
func installCodecs(ctx context.Context, t *proot.Target, fam *pkgmgr.Family, d *conf.Distro, r ui.Reporter, lg *Logger) bool {
	cs := d.BrowserCodecs
	if len(cs) == 0 {
		return false
	}
	n := count(ctx, t, fam.SimInstall(cs), fam)
	if n == 0 {
		return false
	}
	r.Label("Installing video codecs")
	if err := pkgRun(ctx, t, fam, fam.Install(cs), n, r); err != nil {
		lg.Printf("browser codecs: %v", err)
		for _, c := range cs {
			if err := t.Run(ctx, fam.Install([]string{c}), nil, func(l string) { r.Line(l) }); err != nil {
				lg.Printf("browser codec %s: %v", c, err)
			}
		}
	}
	return true
}

// prebuilt reports whether the unpacked image already contains this
// desktop (its session command is installed): a Modded edition or an
// exported rootfs. Then the package steps are skipped.
func prebuilt(in *Inst, de *conf.Desktop) bool {
	if !sessionPresent(in, de) {
		return false
	}
	switch in.Get("PREBUILT") {
	case "yes":
		return true
	case "no":
		return false
	}
	// Unpacked by an andronix before PREBUILT (2.0.0): only a Modded or
	// Classic image is prebuilt; a free one has its desktop from apt,
	// maybe half-installed.
	return moddedImage(in) || in.Get("EDITION") != ""
}

func sessionPresent(in *Inst, de *conf.Desktop) bool {
	if de.None() || de.Session == "" {
		return false
	}
	cmd := strings.Fields(de.Session)[0]
	return hasAny(in.Rootfs, "usr/bin/"+cmd, "bin/"+cmd, "usr/local/bin/"+cmd)
}

// moddedImage reports a Modded edition (its layer's files are present).
func moddedImage(in *Inst) bool { return hasAny(in.Rootfs, "usr/share/andronix/modded") }

func hasAny(root string, rels ...string) bool {
	for _, r := range rels {
		if _, err := os.Stat(filepath.Join(root, r)); err == nil {
			return true
		}
	}
	return false
}

// API is products-api, which signs Modded downloads.
func API() string {
	if a := os.Getenv("ANDRONIX_API"); a != "" {
		return strings.TrimRight(a, "/")
	}
	return "https://products.andronix.xyz"
}

// moddedSource turns the app's token into a products-api download:
// GET <api>/v1/modded/download/<file>?k&e&h answers with a redirect to a
// short-lived bucket URL (the same flow the Modded scripts use today).
// The token is "k=<key>&e=<expiry>&h=<hash>", optionally with "&f=<file>";
// the default file is <id>-<version>-<desktop>-modded-<arch>.tar.xz.
func moddedSource(d *conf.Distro, de *conf.Desktop, arch sys.Arch, p Paths, token, edition string) (source, error) {
	if token == "" {
		return source{}, ui.Errorf("Missing download token", "Modded editions need the token the Andronix app gives you after purchase.",
			"Copy the install command from the Andronix app again; it includes --token.")
	}
	q, err := url.ParseQuery(strings.TrimPrefix(strings.TrimSpace(token), "?"))
	if err != nil || q.Get("k") == "" || q.Get("e") == "" || q.Get("h") == "" {
		return source{}, ui.Errorf("That token doesn't look right", "The --token value is incomplete.",
			"Copy the whole install command from the Andronix app again.")
	}
	file := q.Get("f")
	if file == "" {
		file = fmt.Sprintf("%s-%s-%s-modded-%s.tar.xz", d.ID, d.Version, de.ID, arch)
		if strings.HasPrefix(edition, "legacy-") {
			file = fmt.Sprintf("%s-%s-%s.tar.xz", edition, d.Version, arch)
		}
	}
	if strings.ContainsAny(file, "/\\") || strings.HasPrefix(file, ".") {
		return source{}, ui.Errorf("That token doesn't look right", "The file name in the token isn't valid.", "Copy the install command from the Andronix app again.")
	}
	// The token proves the purchase of a product, not a file: it goes to
	// products-api as given (every field), with this CPU's file name.
	q.Del("f")
	base := API() + "/v1/modded/download/"
	return source{kind: "modded", url: base + url.PathEscape(file) + "?" + q.Encode(),
		sumURL: base + url.PathEscape(file+".sha256") + "?" + q.Encode(), file: filepath.Join(p.Cache, file)}, nil
}

// moddedChecksum fetches a Modded download's sha256 (<file>.sha256, same
// token). Missing (404: products-api not serving it yet) means unchecked,
// logged; anything malformed is an error, never a silent pass.
func moddedChecksum(ctx context.Context, s *source, lg *Logger, warn func(string)) error {
	b, _, err := netx.Get(ctx, s.sumURL, nil) // products-api answers 302 to the file
	var se *netx.StatusError
	switch {
	case errors.As(err, &se) && se.Code == 404:
		lg.Printf("modded: no checksum served for %s; downloading unchecked", filepath.Base(s.file))
		if warn != nil {
			warn("The server has no checksum for this edition yet; the download can't be verified.")
		}
		return nil
	case err != nil:
		return downloadErr(err)
	}
	f := strings.Fields(string(b))
	if len(f) == 0 || !sha256Re.MatchString(strings.ToLower(f[0])) {
		return ui.Errorf("Download check failed", "The server's checksum for this edition isn't valid.", "Run the same command again; if it keeps happening, tell us on Discord.")
	}
	s.sha = strings.ToLower(f[0])
	return nil
}

// kernelNewer reports whether release (e.g. "6.6.30-android15") is newer
// than max (e.g. "6.5"), comparing major.minor.
func kernelNewer(release, max string) bool {
	num := func(s string) (int, int) {
		var a, b int
		fmt.Sscanf(s, "%d.%d", &a, &b)
		return a, b
	}
	ra, rb := num(release)
	ma, mb := num(max)
	return ra > ma || ra == ma && rb > mb
}

func installLabel(de *conf.Desktop) string {
	if de.None() {
		return "Installing the basics"
	}
	return "Installing " + de.Name + " desktop"
}

func pickSource(ctx context.Context, d *conf.Distro, arch sys.Arch, p Paths, lg *Logger) (source, error) {
	if f := os.Getenv("ANDRONIX_ROOTFS"); f != "" {
		if _, err := os.Stat(f); err != nil {
			return source{}, ui.Errorf("Rootfs file not found", "ANDRONIX_ROOTFS points at '"+f+"', which doesn't exist.", "Fix the path or unset ANDRONIX_ROOTFS.")
		}
		s := source{kind: "local", file: f}
		if b, err := os.ReadFile(f + ".sha256"); err == nil {
			if fs := strings.Fields(string(b)); len(fs) > 0 {
				s.sha = fs[0]
			}
		}
		return s, nil
	}
	mode := os.Getenv("ANDRONIX_SOURCE")
	if mode != "registry" {
		name := fmt.Sprintf("%s-%s-%s.tar.xz", d.ID, d.Version, arch)
		// The API's answer first (see resolve.go), then the built-in URL.
		if res := resolve(ctx, "rootfs", url.Values{"distro": {d.ID}, "ver": {d.Version}}); res != nil {
			for _, u := range res.urls() {
				code, size, err := netx.Head(ctx, u)
				lg.Printf("resolved tarball %s (%s): %d %v", u, res.Version, code, err)
				if err == nil && code == 200 {
					if res.Size > 0 {
						size = res.Size
					}
					return source{kind: "tarball", url: u, size: size, sha: res.SHA256, file: filepath.Join(p.Cache, name)}, nil
				}
			}
		}
		u := fmt.Sprintf("%s/rootfs/%s/%s/%s", Mirror(), d.ID, d.Version, name)
		code, size, err := netx.Head(ctx, u)
		lg.Printf("tarball %s: %d %v", u, code, err)
		if err == nil && code == 200 {
			s := source{kind: "tarball", url: u, size: size, file: filepath.Join(p.Cache, name)}
			if b, _, err := netx.Get(ctx, u+".sha256", nil); err == nil {
				if fs := strings.Fields(string(b)); len(fs) > 0 && sha256Re.MatchString(fs[0]) {
					s.sha = fs[0]
				}
			}
			// Never unchecked: without a checksum, the registry (whose
			// layers are checked by digest) is used instead.
			if s.sha != "" {
				return s, nil
			}
			lg.Printf("tarball %s: no checksum, using the registry", u)
		}
		if mode == "tarball" {
			return source{}, ui.Errorf("Download not available", "The "+d.Label()+" download isn't reachable right now.", "Check your connection and try again in a few minutes.")
		}
	}
	a, v := arch.OCI()
	im, err := oci.Resolve(ctx, d.Image, a, v)
	lg.Printf("registry %s: %v", d.Image, err)
	if err != nil {
		var np *oci.ErrNoPlatform
		var se *netx.StatusError
		switch {
		case errors.As(err, &np) && d.UpstreamTarball(string(arch)) != "":
			// No OCI image for this CPU (Arch Linux ARM): the distro's own tarball.
			url := d.UpstreamTarball(string(arch))
			_, size, _ := netx.Head(ctx, url)
			s := source{kind: "upstream", url: url, size: size, file: filepath.Join(p.Cache, d.ID+"-upstream-"+string(arch)+filepath.Ext(url))}
			if b, _, err := netx.Get(ctx, url+".sha256", nil); err == nil {
				if f := strings.Fields(string(b)); len(f) > 0 && len(f[0]) == 64 {
					s.sha = f[0]
				}
			}
			return s, nil
		case errors.As(err, &np):
			return source{}, ui.Errorf("No build for your CPU", d.Label()+" has no image for "+arch.Label()+".", "Pick another distro in the Andronix app.")
		case errors.As(err, &se) && se.Code == 429:
			return source{}, ui.Errorf("The image server is busy", "Docker Hub limits downloads per network, and your network hit the limit.",
				"Wait an hour, or switch between Wi-Fi and mobile data, then run the same command again.")
		}
		return source{}, ui.Errorf("Couldn't reach the download servers", err.Error(), "Check your internet connection, then run the same command again.")
	}
	return source{kind: "registry", image: im}, nil
}

func downloadErr(err error) error {
	if errors.Is(err, context.Canceled) {
		return ui.ErrCancelled
	}
	var se *netx.StatusError
	if errors.As(err, &se) && se.Code == 403 {
		// products-api: a genuine token for another edition.
		return &ui.UserError{Title: "That token is for another edition", Class: "token_other_edition",
			What: "The download token was made for a different Modded edition than this command installs.",
			Fix:  "Copy the install command for this edition from the Andronix app.", Err: err}
	}
	if errors.As(err, &se) && se.Code == 401 {
		return &ui.UserError{Title: "Download link expired", Class: "token_refused", What: "The download token was refused (it lasts a limited time, and each purchase has a daily limit).",
			Fix: "Copy a fresh install command from the Andronix app and run it again.", Err: err}
	}
	return &ui.UserError{Title: "Download failed", Class: "download", What: "The download stopped before it finished (" + err.Error() + ").",
		Fix: "Check your internet connection, then run the same command again. It resumes where it stopped.", Err: err}
}

func pkgErr(title string, err error) error {
	if errors.Is(err, context.Canceled) {
		return ui.ErrCancelled
	}
	return &ui.UserError{Title: title, What: "The package manager stopped with an error.", Class: "package_manager",
		Fix: "Run the same command again; downloads resume. If Android closed Termux (signal 9), see docs.andronix.app on the phantom process killer.", Err: err}
}

// count runs a simulate command and counts the packages it would touch.
func count(ctx context.Context, t *proot.Target, cmd string, f *pkgmgr.Family) int {
	n := 0
	t.Run(ctx, cmd, nil, func(l string) {
		if f.SimLine.MatchString(l) {
			n++
		}
	})
	return n
}

// pkgRun runs a package command with a progress bar read from its output.
func pkgRun(ctx context.Context, t *proot.Target, f *pkgmgr.Family, cmd string, n int, r ui.Reporter) error {
	defer rootfs.EnsureBwrapShim(t.Rootfs) // a package may have (re)installed bwrap
	if f.PostInstall != "" {
		defer t.Run(ctx, f.PostInstall, nil, nil)
	}
	steps := 0
	return t.Run(ctx, cmd, nil, func(l string) {
		r.Line(l)
		if cur, tot, ok := f.Parse(l); ok {
			r.Progress(int64(cur), int64(tot), "packages")
			return
		}
		if f.Step != nil && n > 0 && f.Step.MatchString(l) {
			steps++
			total := int64(n * f.Phases)
			r.Progress(int64(steps), total, "")
		}
	})
}

func finish(in *Inst, de *conf.Desktop, user string, noStart bool, rep *installReport) {
	d := in.D
	start := ui.Interactive && !noStart
	lines := []string{}
	// The desktop is Termux:X11 (owner decision); VNC is the other way.
	if !de.None() {
		lines = append(lines, ui.KV("Desktop", "andronix desktop "+d.ID), "  in Termux; it opens in the Termux:X11 app",
			"  (github.com/termux/termux-x11, nightly: termux-x11-universal-debug.apk)", "")
	}
	if start {
		lines = append(lines, "You're going into "+d.Name+"'s terminal now (type exit to leave).", "Next time, from Termux: ./"+d.MainStart(), "")
	} else {
		lines = append(lines, ui.KV("Terminal", "./"+d.MainStart()))
	}
	if !de.None() {
		lines = append(lines, ui.KV("Or VNC", "vncserver-start inside "+d.Name+", then localhost:1 in a VNC viewer"))
	}
	if user != "" {
		lines = append(lines, ui.KV("User", user+" (sudo works)"))
	}
	lines = append(lines, ui.KV("Remove", "andronix remove "+d.ID))
	if r := in.Get("RENAMED"); r != "" {
		lines = append(lines, "", "Your older "+d.Name+" is untouched. Start it with ./"+strings.Fields(r)[0])
	}
	fmt.Println()
	fmt.Print(ui.Box(ui.BoxOK, d.Label()+" is ready", lines...))
	ui.Footer()
	rep.send("ok") // before the shell: Login replaces this process
	if start {
		startSound(context.Background(), true)
		if err := in.Target(false).Login(d.Shell, nil); err != nil {
			ui.ShowError(err)
		}
	}
}

// resolvedID is the id a distro or desktop name resolves to (the name
// itself if it doesn't).
func resolvedID(what, name string) string {
	if what == "distro" {
		if d, err := conf.ResolveDistro(name); err == nil {
			return d.ID
		}
	} else if de, _, err := conf.ResolveDesktopCompat(name); err == nil {
		return de.ID
	}
	return name
}

// preUpgrade runs DISTRO_PRE_UPGRADE (after a refresh, before upgrades).
func preUpgrade(ctx context.Context, t *proot.Target, d *conf.Distro, r ui.Reporter) error {
	if d.PreUpgrade == "" {
		return nil
	}
	if err := t.Run(ctx, d.PreUpgrade, nil, func(l string) { r.Line(l) }); err != nil {
		return pkgErr("Couldn't prepare "+d.Label()+" for updates", err)
	}
	return nil
}

// editionName is what users see for a Modded edition id (owner naming):
// "Modded 2.0" for the lineup, "Classic" for the archived legacy-* ones.
func editionName(id string) string {
	if strings.HasPrefix(id, "legacy-") {
		return "Andronix Classic"
	}
	return "Andronix Modded 2.0"
}

// earlyErr explains a refused download of an early-access edition (nil
// for anything else).
func earlyErr(err error, ed *conf.Edition) error {
	var se *netx.StatusError
	if ed == nil || !ed.Early || !errors.As(err, &se) || se.Code != 401 && se.Code != 403 {
		return nil
	}
	return &ui.UserError{Title: "Early access: Premium", Class: "early_access",
		What: editionName(ed.ID) + " (" + ed.ID + ") is in early access for Andronix Premium and Modded Pass owners, before everyone gets it.",
		Fix:  "Get Premium or the Modded Pass in the Andronix app, then copy the install command again.", Err: err}
}
