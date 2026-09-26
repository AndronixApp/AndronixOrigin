package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AndronixApp/andronix-distros/internal/conf"
	"github.com/AndronixApp/andronix-distros/internal/pkgmgr"
	"github.com/AndronixApp/andronix-distros/internal/proot"
	"github.com/AndronixApp/andronix-distros/internal/rootfs"
	"github.com/AndronixApp/andronix-distros/internal/sys"
	"github.com/AndronixApp/andronix-distros/internal/telemetry"
	"github.com/AndronixApp/andronix-distros/internal/ui"
)

// Dev packs (docs/packs.md): `andronix pack list|add|remove [distro] <pack>...`.
// From Termux the package steps run as root through proot, like the
// installer's; inside a distro (`sudo andronix pack ...`) they run directly.
// /etc/andronix/packs/<id> records the packages a pack newly installed, so
// remove takes out exactly those, minus any another added pack also has.

const packState = "etc/andronix/packs"

// inGuest reports whether andronix runs inside an installed distro.
func inGuest() bool {
	_, err := os.Stat("/etc/andronix-release")
	return err == nil
}

// packPlace is where packs go: the target, its family and its label.
type packPlace struct {
	id    string // distro id
	t     *proot.Target
	fam   *pkgmgr.Family
	label string
	cmd   string // how to run pack commands here, for messages
}

// splitPackArgs takes an optional distro off the front of args.
func splitPackArgs(args []string) (distro string, packs []string) {
	if len(args) > 0 && !inGuest() {
		if _, err := conf.LoadPack(args[0]); err != nil {
			return args[0], args[1:]
		}
	}
	return "", args
}

func openPackPlace(distro, verb string, needRoot bool) (*packPlace, error) {
	if inGuest() {
		rel := conf.Parse(readFile("/etc/andronix-release"))
		fam := rel.Get("ANDRONIX_FAMILY")
		if fam == "" {
			for _, f := range [][2]string{{"apt-get", "apt"}, {"dnf", "dnf"}, {"pacman", "pacman"}, {"apk", "apk"}, {"xbps-install", "xbps"}} {
				if _, err := sys.LookPath(f[0]); err == nil {
					fam = f[1]
					break
				}
			}
		}
		f, err := pkgmgr.Get(fam)
		if err != nil {
			return nil, ui.Errorf("Unknown package manager", "andronix can't tell which package manager this distro uses.", "Run it from Termux instead: andronix pack "+verb+" <distro> <pack>")
		}
		if needRoot && os.Geteuid() != 0 {
			return nil, ui.Errorf("Packs need root", "Adding and removing packs installs packages.",
				"Run it with sudo: sudo andronix pack "+verb+" <pack>. Without sudo (Manjaro), run it from Termux: andronix pack "+verb+" "+rel.Get("ANDRONIX_DISTRO")+" <pack>")
		}
		name := strings.Trim(rel.Get("ANDRONIX_NAME"), `"`)
		if name == "" {
			name = "this distro"
		}
		return &packPlace{id: rel.Get("ANDRONIX_DISTRO"), t: &proot.Target{Rootfs: "/", Local: true}, fam: f, label: name, cmd: "sudo andronix pack"}, nil
	}
	if distro == "" {
		var got []string
		for _, id := range conf.DistroIDs() {
			d, _ := conf.LoadDistro(id)
			if Open(d).Installed() {
				got = append(got, id)
			}
		}
		if len(got) != 1 {
			return nil, ui.Errorf("Which distro?", "Add the distro's name to the command.",
				"For example: andronix pack "+verb+" debian python. 'andronix list' shows what's installed.")
		}
		distro = got[0]
	}
	d, err := resolveDistro(distro, "pack "+verb)
	if err != nil {
		return nil, err
	}
	in := Open(d)
	if !in.Installed() {
		return nil, ui.Errorf(d.Label()+" isn't installed", "Packs go on top of an installed distro.", "Install it first: andronix install "+d.ID)
	}
	f, err := pkgmgr.Get(d.Family)
	if err != nil {
		return nil, err
	}
	return &packPlace{id: d.ID, t: in.Target(true), fam: f, label: d.Label(), cmd: "andronix pack " + d.ID}, nil
}

func readFile(p string) []byte { b, _ := os.ReadFile(p); return b }

func (pp *packPlace) statePath(id string) string { return filepath.Join(pp.t.Rootfs, packState, id) }

// added reports a pack that was installed and passed its check.
func (pp *packPlace) added(id string) bool {
	b, err := os.ReadFile(pp.statePath(id))
	return err == nil && conf.Parse(b).Get("CHECKED") != "no"
}

// failed reports a pack whose packages are in but whose check failed (or
// never finished); adding it again retries.
func (pp *packPlace) failed(id string) bool {
	b, err := os.ReadFile(pp.statePath(id))
	return err == nil && conf.Parse(b).Get("CHECKED") == "no"
}

// setChecked records the check's result in the pack's state.
func (pp *packPlace) setChecked(id string, ok bool) {
	b, err := os.ReadFile(pp.statePath(id))
	if err != nil {
		return
	}
	var out []string
	for _, l := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		if !strings.HasPrefix(l, "CHECKED=") {
			out = append(out, l)
		}
	}
	v := "yes"
	if !ok {
		v = "no"
	}
	os.WriteFile(pp.statePath(id), []byte(strings.Join(append(out, "CHECKED="+v), "\n")+"\n"), 0o644)
}

// loadPacks resolves pack names, with a friendly error for unknown ones.
func loadPacks(names []string, verb string) ([]*conf.Pack, error) {
	if len(names) == 0 {
		return nil, ui.Errorf("Which pack?", "Name one or more packs.", "For example: andronix pack "+verb+" python. Packs: "+strings.Join(conf.PackIDs(), ", "))
	}
	var out []*conf.Pack
	for _, n := range names {
		p, err := conf.LoadPack(n)
		if err != nil {
			return nil, ui.Errorf("Unknown pack '"+n+"'", "There's no pack called '"+n+"'.", "Packs: "+strings.Join(conf.PackIDs(), ", "))
		}
		out = append(out, p)
	}
	return out, nil
}

// PackList is `andronix pack list [distro]`.
func PackList(args []string) error {
	distro, _ := splitPackArgs(args)
	var pp *packPlace
	if inGuest() || distro != "" {
		var err error
		if pp, err = openPackPlace(distro, "list", false); err != nil {
			return err
		}
	} else if p, err := openPackPlace("", "list", false); err == nil {
		pp = p // the only installed distro
	}
	fmt.Print(ui.Banner("Dev packs"))
	title := "Packs"
	if pp != nil {
		title = "Packs for " + pp.label
	}
	ui.Section(title)
	for _, id := range conf.PackIDs() {
		p, err := conf.LoadPack(id)
		if err != nil {
			continue
		}
		mark, desc := "  ", p.Desc
		if pp != nil && pp.added(id) {
			mark = ui.GOK + " "
		} else if pp != nil && pp.failed(id) {
			mark, desc = ui.GWarn+" ", p.Desc+" (its check failed; add it again)"
		}
		ui.Row(mark+id, desc, 10)
	}
	fmt.Println()
	how := "andronix pack add <distro> <pack>..."
	if pp != nil {
		how = pp.cmd + " add <pack>..."
		if !inGuest() {
			how = "andronix pack add " + strings.TrimPrefix(pp.cmd, "andronix pack ") + " <pack>..."
		}
	}
	ui.Note("Add: " + how)
	fmt.Println()
	return nil
}

// PackAdd is `andronix pack add [distro] <pack>...`.
func PackAdd(ctx context.Context, args []string) error {
	distro, names := splitPackArgs(args)
	packs, err := loadPacks(names, "add")
	if err != nil {
		return err
	}
	pp, err := openPackPlace(distro, "add", true)
	if err != nil {
		return err
	}
	fmt.Print(ui.Banner("Dev packs " + ui.GSep + " " + pp.label))
	var todo []*conf.Pack
	need := 0
	for _, p := range packs {
		if pp.added(p.ID) {
			ui.OK(p.Name + " is already added.")
			continue
		}
		todo = append(todo, p)
		need += p.DiskMB
	}
	if len(todo) == 0 {
		fmt.Println()
		return nil
	}
	if free := sys.FreeMB(pp.t.Rootfs); free >= 0 && free < int64(need+200) {
		return ui.Errorf("Not enough space", fmt.Sprintf("These packs need about %d MB; there's %d MB free.", need+200, free),
			"Free some space on the phone, or add fewer packs at once.")
	}
	lg := NewLog("pack")
	defer lg.Close()
	telemetry.Notice()
	started := time.Now()
	var ids []string
	for _, p := range todo {
		ids = append(ids, p.ID)
	}
	t, fam := pp.t, pp.fam
	steps := []ui.Step{{Label: "Refreshing package lists", Run: func(ctx context.Context, r ui.Reporter) error {
		if err := t.Run(ctx, fam.Update, nil, func(l string) { r.Line(l) }); err != nil {
			return pkgErr("Couldn't reach the package servers", err)
		}
		return nil
	}}}
	for _, p := range todo {
		p := p
		steps = append(steps,
			ui.Step{Label: "Installing " + p.Name, Run: func(ctx context.Context, r ui.Reporter) error {
				return packInstall(ctx, pp, p, r)
			}},
			ui.Step{Label: "Checking " + p.Name, Run: func(ctx context.Context, r ui.Reporter) error {
				if p.Check == "" {
					return ui.Skip("no check")
				}
				// A check can hang (a server that never comes up): 10 minutes.
				cctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
				defer cancel()
				err := t.Run(cctx, p.Check, nil, func(l string) { r.Line(l) })
				pp.setChecked(p.ID, err == nil)
				if err != nil {
					if cctx.Err() == context.DeadlineExceeded {
						err = fmt.Errorf("the check didn't finish in 10 minutes")
					}
					return &ui.UserError{Title: p.Name + " was installed, but its check failed",
						What: "The pack's packages are in place, but a quick test of them didn't pass.",
						Fix:  "Try removing and adding it again: " + pp.cmd + " remove " + p.ID + ", then add " + p.ID + ".", Err: err}
				}
				return nil
			}})
	}
	fmt.Println()
	err = ui.RunSteps(ctx, steps, lg)
	telemetry.Send("pack_add", map[string]any{"packs": strings.Join(ids, " "), "distro": pp.id, "ok": err == nil,
		"error_class": telemetry.ErrorClass(err), "duration_ms": time.Since(started).Milliseconds()})
	if err != nil {
		if ue, ok := err.(*ui.UserError); ok && ue.Log == "" {
			ue.Log = lg.Path
		}
		return err
	}
	fmt.Println()
	for _, p := range todo {
		ui.OK(p.Name + " is ready.")
		if p.Hint != "" {
			ui.Hint(p.Hint)
		}
	}
	fmt.Println()
	return nil
}

// resolvePackages turns PACK_PKGS tokens into this distro's package names:
// "a|b" is the first available, "x?" is skipped when missing.
func resolvePackages(tokens []string, available map[string]bool) (pkgs, skipped []string, missing string) {
	for _, tok := range tokens {
		opt := strings.HasSuffix(tok, "?")
		tok = strings.TrimSuffix(tok, "?")
		pick := ""
		for _, alt := range strings.Split(tok, "|") {
			if available[alt] {
				pick = alt
				break
			}
		}
		switch {
		case pick != "":
			pkgs = append(pkgs, pick)
		case opt:
			skipped = append(skipped, tok)
		default:
			return nil, nil, tok
		}
	}
	return pkgs, skipped, ""
}

// lines runs cmd and returns its single-word output lines as a set.
func lines(ctx context.Context, t *proot.Target, cmd string) map[string]bool {
	out := map[string]bool{}
	t.Run(ctx, cmd, nil, func(l string) {
		if l = strings.TrimSpace(l); l != "" && !strings.Contains(l, " ") {
			out[l] = true
		}
	})
	return out
}

func packInstall(ctx context.Context, pp *packPlace, p *conf.Pack, r ui.Reporter) error {
	t, fam := pp.t, pp.fam
	tokens := p.Packages(fam.ID)
	if len(tokens) == 0 {
		return ui.Errorf(p.Name+" isn't available on "+pp.label, "The "+p.ID+" pack has no package list for "+pp.label+".", "")
	}
	var alts []string
	for _, tok := range tokens {
		alts = append(alts, strings.Split(strings.TrimSuffix(tok, "?"), "|")...)
	}
	pkgs, skipped, missing := resolvePackages(tokens, lines(ctx, t, fam.FilterAvailable(alts)))
	if missing != "" {
		return ui.Errorf(p.Name+" isn't available on "+pp.label, "Its package '"+missing+"' isn't in "+pp.label+"'s repositories.",
			"Update the distro and try again: andronix update. If it still fails, this pack doesn't support "+pp.label+" yet.")
	}
	for _, s := range skipped {
		r.Line("optional " + s + " isn't available here; skipped")
	}
	// Record only what this pack brings in: remove mustn't take out
	// packages the system (or another pack) already had.
	had := lines(ctx, t, fam.FilterInstalled(pkgs))
	var fresh []string
	for _, q := range pkgs {
		if !had[q] {
			fresh = append(fresh, q)
		}
	}
	n := count(ctx, t, fam.SimInstall(pkgs), fam)
	if err := pkgRun(ctx, t, fam, fam.Install(pkgs), n, r); err != nil {
		return pkgErr("Couldn't install "+p.Name, err)
	}
	t.Run(ctx, fam.Clean, nil, nil)
	for _, f := range p.Files {
		b, err := conf.PackFileData(f.Src)
		if err != nil {
			return err
		}
		dst := filepath.Join(t.Rootfs, f.Dest)
		os.MkdirAll(filepath.Dir(dst), 0o755)
		os.Remove(dst)
		if err := os.WriteFile(dst, b, f.Mode); err != nil {
			return err
		}
		os.Chmod(dst, f.Mode)
	}
	if p.Post != "" {
		if err := t.Run(ctx, p.Post, nil, func(l string) { r.Line(l) }); err != nil {
			return &ui.UserError{Title: "Couldn't finish setting up " + p.Name, What: "The pack's setup command failed.",
				Fix: "Run the same command again.", Err: err}
		}
	}
	os.MkdirAll(filepath.Join(t.Rootfs, packState), 0o755)
	if err := pp.record(p.ID, fresh, pkgs, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return err
	}
	pp.setChecked(p.ID, false) // until the check passes
	rootfs.EnsureBwrapShim(t.Rootfs)
	rootfs.InstallShm(t.Rootfs) // andronix-postgres preloads it
	r.Detail(fmt.Sprintf("%d packages", len(fresh)))
	return nil
}

// PackRemove is `andronix pack remove [distro] <pack>...`.
func PackRemove(ctx context.Context, args []string, purge bool) error {
	distro, names := splitPackArgs(args)
	packs, err := loadPacks(names, "remove")
	if err != nil {
		return err
	}
	pp, err := openPackPlace(distro, "remove", true)
	if err != nil {
		return err
	}
	fmt.Print(ui.Banner("Dev packs " + ui.GSep + " " + pp.label))
	lg := NewLog("pack")
	defer lg.Close()
	var steps []ui.Step
	for _, p := range packs {
		p := p
		if !pp.added(p.ID) && !pp.failed(p.ID) {
			ui.Note(p.Name + " isn't added.")
			continue
		}
		steps = append(steps, ui.Step{Label: "Removing " + p.Name, Run: func(ctx context.Context, r ui.Reporter) error {
			return packRemove(ctx, pp, p, purge, r)
		}})
	}
	if len(steps) == 0 {
		fmt.Println()
		return nil
	}
	fmt.Println()
	if err := ui.RunSteps(ctx, steps, lg); err != nil {
		if ue, ok := err.(*ui.UserError); ok && ue.Log == "" {
			ue.Log = lg.Path
		}
		return err
	}
	fmt.Println()
	if !purge {
		for _, p := range packs {
			var kept []string
			for _, d := range p.Purge {
				m, _ := filepath.Glob(filepath.Join(pp.t.Rootfs, d))
				for _, f := range m {
					kept = append(kept, "/"+strings.TrimPrefix(strings.TrimPrefix(f, pp.t.Rootfs), "/"))
				}
			}
			if len(kept) > 0 {
				ui.Note("Your " + p.Name + " data is kept in " + strings.Join(kept, ", ") + ". To delete it too: " + pp.cmd + " remove --purge " + p.ID)
			}
		}
	}
	return nil
}

func packRemove(ctx context.Context, pp *packPlace, p *conf.Pack, purge bool, r ui.Reporter) error {
	t, fam := pp.t, pp.fam
	pkgs := conf.Parse(readFile(pp.statePath(p.ID))).List("PACKAGES")
	// Keep packages another added pack brought in or uses. A kept package
	// this pack installed passes to a pack that uses it, so removing that
	// one later takes it out.
	keep := map[string]string{}
	others, _ := os.ReadDir(filepath.Join(t.Rootfs, packState))
	for _, o := range others {
		if o.Name() != p.ID {
			v := conf.Parse(readFile(pp.statePath(o.Name())))
			for _, q := range append(v.List("PACKAGES"), v.List("NEEDS")...) {
				keep[q] = o.Name()
			}
		}
	}
	var drop []string
	for _, q := range pkgs {
		if heir := keep[q]; heir == "" {
			drop = append(drop, q)
		} else {
			pp.inherit(heir, q)
		}
	}
	if p.PreRemove != "" {
		t.Run(ctx, p.PreRemove, nil, func(l string) { r.Line(l) })
	}
	if len(drop) > 0 {
		// Only the ones still installed; the user may have removed some.
		var present []string
		have := lines(ctx, t, fam.FilterInstalled(drop))
		for _, q := range drop {
			if have[q] {
				present = append(present, q)
			}
		}
		if len(present) > 0 {
			if err := pkgRun(ctx, t, fam, fam.Remove(present), 0, r); err != nil {
				return pkgErr("Couldn't remove "+p.Name, err)
			}
		}
		drop = present
	}
	for _, f := range p.Files {
		os.Remove(filepath.Join(t.Rootfs, f.Dest))
	}
	if purge {
		for _, d := range p.Purge {
			m, _ := filepath.Glob(filepath.Join(t.Rootfs, d))
			for _, f := range m {
				os.RemoveAll(f)
			}
		}
	}
	os.Remove(pp.statePath(p.ID))
	r.Detail(fmt.Sprintf("%d packages", len(drop)))
	return nil
}

// inherit adds pkg to pack id's PACKAGES (see packRemove).
func (pp *packPlace) inherit(id, pkg string) {
	v := conf.Parse(readFile(pp.statePath(id)))
	have := v.List("PACKAGES")
	for _, q := range have {
		if q == pkg {
			return
		}
	}
	pp.record(id, append(have, pkg), v.List("NEEDS"), v.Get("ADDED"))
	if c := v.Get("CHECKED"); c != "" {
		pp.setChecked(id, c == "yes")
	}
}

// record writes /etc/andronix/packs/<id>. PACKAGES is what the pack
// installed (remove takes these out); NEEDS is everything it uses, so
// removing another pack keeps those.
func (pp *packPlace) record(id string, installed, needs []string, added string) error {
	rec := fmt.Sprintf("PACK_ID=%s\nPACKAGES=%q\nNEEDS=%q\nADDED=%s\n", id, strings.Join(installed, " "), strings.Join(needs, " "), added)
	return os.WriteFile(pp.statePath(id), []byte(rec), 0o644)
}
