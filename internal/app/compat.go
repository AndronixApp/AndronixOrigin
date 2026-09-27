package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	andronix "github.com/AndronixApp/andronix-distros"
	"github.com/AndronixApp/andronix-distros/internal/compat"
	"github.com/AndronixApp/andronix-distros/internal/conf"
	"github.com/AndronixApp/andronix-distros/internal/proot"
	"github.com/AndronixApp/andronix-distros/internal/rootfs"
	"github.com/AndronixApp/andronix-distros/internal/sys"
	"github.com/AndronixApp/andronix-distros/internal/telemetry"
	"github.com/AndronixApp/andronix-distros/internal/ui"
)

// Adaptive compatibility (DESIGN.md section 13): what this phone does,
// measured, and the fixes the rules in compat.json pick for it.

// compatRun is this run's facts, rules and plan.
type compatRun struct {
	table *compat.Table
	facts compat.Facts
	plan  *compat.Plan
}

func init() {
	// Every proot run gets the rules' env (e.g. PROOT_NO_SECCOMP), from
	// what's known without a distro: the host and a cached probe.
	proot.EnvHook = func() []string {
		if !sys.IsTermux() {
			return nil
		}
		return newCompat(nil, "").plan.EnvList()
	}
}

// newCompat loads the rules and the facts: the host, and the cached probe
// if it's still for this kernel, Android and Termux.
func newCompat(d *conf.Distro, de string) *compatRun {
	c := &compatRun{}
	var err error
	if c.table, err = compat.Default(); err != nil {
		c.table = &compat.Table{} // a broken table must never stop an install (tests catch it)
	}
	// The host facts (ROM, phantom setting, RAM) are read fresh every
	// time; only the syscall results come from the cache.
	host := compat.Host()
	if p := compat.Cached(GetPaths().Home, host); p != nil {
		host.Syscalls = p.Syscalls
	}
	c.facts = compat.Facts{Probe: host, DE: de}
	if d != nil {
		c.facts.Distro, c.facts.Family = d.ID, d.Family
	}
	c.replan()
	return c
}

func (c *compatRun) replan() {
	c.plan = compat.PlanFor(c.table.Match(c.facts))
	proot.SetEnv(c.plan.EnvList())
}

// probe runs `andronix __probe` inside the distro (unless a probe for
// this phone is cached), then plans again with the results.
func (c *compatRun) probe(ctx context.Context, t *proot.Target, lg *Logger) {
	if len(c.facts.Probe.Syscalls) > 0 {
		return
	}
	// The prober is the in-distro copy of this andronix: make sure it's current.
	if !t.Local {
		rootfs.InstallSelf(t.Rootfs)
	}
	var out strings.Builder
	err := t.Run(ctx, "/usr/local/bin/andronix __probe", nil, func(l string) { out.WriteString(l + "\n") })
	res := compat.ParseLines(out.String())
	lg.Printf("compat probe: %v %s", err, strings.TrimSpace(out.String()))
	if len(res) == 0 {
		return // the old checks stay in charge (proot's fallback)
	}
	c.facts.Probe.Syscalls = res
	c.facts.Probe.Save(GetPaths().Home)
	c.replan()
	p := c.facts.Probe
	telemetry.Send("compat", map[string]any{"probe": p.Summary(), "kernel": p.KernelMajorMinor(), "sdk": p.SDK, "rom": p.ROM,
		"phantom": p.Phantom, "ram": p.RAMBucket(), "termux": p.Termux, "rules": strings.Join(c.plan.IDs, " "),
		"distro": c.facts.Distro, "de": c.facts.DE})
}

// warnings shows the plan's warnings (once each).
func (c *compatRun) warnings(shown map[string]bool) {
	for _, w := range c.plan.Warnings {
		if !shown[w] {
			shown[w] = true
			ui.Warn(compat.Message(w, c.facts.Probe))
		}
	}
}

// desktopNote warns before a desktop starts where the rules say it won't
// draw (old_kernel_desktop: kernels before 4.8), so a black screen isn't
// the first sign. Only the kernel is needed; inside a distro uname is the
// phone's too.
func desktopNote() {
	t, err := compat.Default()
	if err != nil {
		return
	}
	pr := &compat.Probe{Kernel: sys.KernelRelease()}
	for _, w := range compat.PlanFor(t.Match(compat.Facts{Probe: pr, DE: "xfce"})).Warnings {
		if w == "old_kernel_desktop" {
			fmt.Println()
			ui.Warn(compat.Message(w, pr))
		}
	}
}

// refuse is the plan's stop, if it has one.
func (c *compatRun) refuse(d *conf.Distro) error {
	if c.plan.Refuse == "" {
		return nil
	}
	return ui.Errorf(d.Label()+" can't run on this phone yet", compat.Message(c.plan.Refuse, c.facts.Probe),
		"Tell us on Discord (https://chat.andronix.app) which phone you have.")
}

// apply writes the plan's host-side fixes into the rootfs: preload shims
// and apt pins.
func (c *compatRun) apply(root string, arch sys.Arch, lg *Logger) {
	for _, name := range c.plan.Preloads {
		if err := installPreload(root, arch, name); err != nil {
			lg.Printf("compat preload %s: %v", name, err)
		}
	}
	if len(c.plan.AptPins) > 0 && c.facts.Family == "apt" {
		var b strings.Builder
		b.WriteString("# Andronix compat rules (" + strings.Join(c.plan.IDs, " ") + ")\n")
		for _, pin := range c.plan.AptPins {
			f := strings.Fields(pin)
			fmt.Fprintf(&b, "\nPackage: %s\nPin: release *\nPin-Priority: %s\n", f[0], f[1])
		}
		p := filepath.Join(root, "etc/apt/preferences.d/andronix-compat")
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(b.String()), 0o644)
	}
}

// preScripts runs the plan's scripts as root (after the package refresh).
func (c *compatRun) preScripts(ctx context.Context, t *proot.Target, r ui.Reporter) error {
	for _, name := range c.plan.PreScripts {
		s, err := compat.Script(name)
		if err != nil {
			continue
		}
		if err := t.Run(ctx, s, nil, func(l string) { r.Line(l) }); err != nil {
			return pkgErr("Couldn't prepare the packages for this phone ("+name+")", err)
		}
	}
	return nil
}

// Doctor is `andronix doctor [distro] [--probe]`: what this phone does and
// which fixes apply.
// Doctor always measures again (install and start use the cache).
func Doctor(ctx context.Context, name string) error {
	var d *conf.Distro
	if name != "" {
		var err error
		if d, err = resolveDistro(name, "doctor"); err != nil {
			return err
		}
	} else {
		for _, id := range conf.DistroIDs() {
			dd, _ := conf.LoadDistro(id)
			if Open(dd).Installed() {
				d = dd
				break
			}
		}
	}
	de := ""
	var in *Inst
	if d != nil {
		in = Open(d)
		de = in.Get("DE")
	}
	c := newCompat(d, de)
	c.facts.Probe.Syscalls = nil
	if in != nil && in.Installed() {
		lg := NewLog("doctor")
		defer lg.Close()
		rootfs_ := in.Target(true)
		c.probe(ctx, rootfs_, lg)
	}
	p := c.facts.Probe
	fmt.Print(ui.Banner("Doctor"))
	ui.Section("This phone")
	ui.Row("Kernel", p.Kernel, 10)
	ui.Row("Android", fmt.Sprintf("SDK %d, ROM %s", p.SDK, p.ROM), 10)
	ui.Row("Termux", strings.TrimSpace(p.Termux+" "+p.TermuxV), 10)
	ui.Row("RAM", fmt.Sprintf("%d MB (free space %d MB)", p.RAMMB, p.FreeMB), 10)
	ui.Row("Phantom", "process killer "+p.Phantom, 10)
	ui.Section("Syscalls under proot")
	if len(p.Syscalls) == 0 {
		ui.Note("Not measured: install a distro first (the probe runs inside one).")
	}
	for _, n := range compat.Syscalls {
		if r, ok := p.Syscalls[n]; ok {
			ui.Row(n, r, 18)
		}
	}
	ui.Section("Fixes that apply")
	if len(c.plan.IDs) == 0 {
		ui.Note("None needed.")
	}
	for _, r := range c.table.Match(c.facts) {
		ui.Row(r.ID, r.Why, 20)
	}
	fmt.Println()
	return nil
}

// installPreload puts guest/preload/<cpu>/libandronix-<name>.so into the
// distro and lists it in /etc/ld.so.preload.
func installPreload(root string, arch sys.Arch, name string) error {
	if name == "fchmodat" {
		return installShim(root, arch)
	}
	b, err := andronix.Preload.ReadFile("guest/preload/" + string(arch) + "/libandronix-" + name + ".so")
	if err != nil {
		return err
	}
	lib := "/usr/local/lib/andronix/libandronix-" + name + ".so"
	os.MkdirAll(filepath.Join(root, filepath.Dir(lib)), 0o755)
	os.Remove(filepath.Join(root, lib))
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
