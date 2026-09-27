package compat

import (
	"embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// The rules ship in the binary (compat.json): a fix for a new kernel, ROM or
// Termux build is one rule, and reaches users with the next release.
//
//go:embed compat.json scripts/*.sh
var files embed.FS

// Table is compat.json.
type Table struct {
	Version int    `json:"version"`
	Rules   []Rule `json:"rules"`
}

// Rule applies its Do when every condition in When holds.
type Rule struct {
	ID   string `json:"id"`
	Why  string `json:"why"`
	When When   `json:"when"`
	Do   Do     `json:"do"`
}

// When: every field that is set must hold.
type When struct {
	KernelLT string              `json:"kernel_lt,omitempty"` // "4.8": older than 4.8
	KernelGE string              `json:"kernel_ge,omitempty"`
	SDKLT    int                 `json:"sdk_lt,omitempty"`
	SDKGE    int                 `json:"sdk_ge,omitempty"`
	RAMMBLT  int64               `json:"ram_mb_lt,omitempty"`
	Probe    map[string][]string `json:"probe,omitempty"`     // syscall -> results that match
	ProbeAny []string            `json:"probe_any,omitempty"` // any syscall with one of these results
	Distro   []string            `json:"distro,omitempty"`
	Family   []string            `json:"family,omitempty"`
	DE       []string            `json:"de,omitempty"`
	Desktop  *bool               `json:"desktop,omitempty"` // a desktop install (not --de none)
	Termux   []string            `json:"termux,omitempty"`  // f_droid, github, google_play_store
	ROM      []string            `json:"rom,omitempty"`
	Phantom  []string            `json:"phantom,omitempty"` // on, off, unknown
}

// Do is what a rule changes.
type Do struct {
	Env          map[string]string `json:"env,omitempty"`           // for every proot run
	Preload      string            `json:"preload,omitempty"`       // a guest/preload shim: fchmodat
	PreScript    string            `json:"pre_script,omitempty"`    // scripts/<name>.sh, as root after the refresh
	AptPin       []string          `json:"apt_pin,omitempty"`       // "package priority", e.g. "systemd -1"
	SkipOptional []string          `json:"skip_optional,omitempty"` // optional packages not to install
	Warn         string            `json:"warn,omitempty"`          // a message name (Messages)
	Refuse       string            `json:"refuse,omitempty"`        // a message name: stop the install
}

// Messages are what warn and refuse can say.
var Messages = map[string]string{
	"old_kernel_desktop": "This phone's Linux kernel (%s) is older than 4.8: desktops (XFCE, LXQt, MATE, KDE) stay black on it, in Termux:X11 and VNC alike. The command line works fully: install with --de none. Help: https://chat.andronix.app",
	"phantom_process":    "Android may stop long-running Linux processes (the phantom process killer). If an install or the desktop stops with 'signal 9', see docs.andronix.app: Phantom process killer.",
}

// Preloads are the shims preload can name (guest/preload/<cpu>/libandronix-<name>.so).
var Preloads = map[string]bool{"fchmodat": true, "mntid": true}

// Facts are what rules test.
type Facts struct {
	Probe              *Probe
	Distro, Family, DE string
}

// Default is the table in the binary.
func Default() (*Table, error) {
	b, err := files.ReadFile("compat.json")
	if err != nil {
		return nil, err
	}
	return Parse(b)
}

// Parse reads a table and checks that every name it uses exists.
func Parse(b []byte) (*Table, error) {
	var t Table
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&t); err != nil {
		return nil, fmt.Errorf("compat table: %w", err)
	}
	seen := map[string]bool{}
	for _, r := range t.Rules {
		if r.ID == "" || seen[r.ID] {
			return nil, fmt.Errorf("compat table: missing or repeated rule id %q", r.ID)
		}
		seen[r.ID] = true
		if err := r.Do.check(); err != nil {
			return nil, fmt.Errorf("compat rule %s: %w", r.ID, err)
		}
		for n := range r.When.Probe {
			if !isSyscall(n) {
				return nil, fmt.Errorf("compat rule %s: unknown syscall %q", r.ID, n)
			}
		}
	}
	return &t, nil
}

func (d Do) check() error {
	if d.Warn != "" && Messages[d.Warn] == "" {
		return fmt.Errorf("unknown message %q", d.Warn)
	}
	if d.Refuse != "" && Messages[d.Refuse] == "" {
		return fmt.Errorf("unknown message %q", d.Refuse)
	}
	if d.Preload != "" && !Preloads[d.Preload] {
		return fmt.Errorf("unknown preload %q", d.Preload)
	}
	if d.PreScript != "" {
		if _, err := Script(d.PreScript); err != nil {
			return fmt.Errorf("unknown script %q", d.PreScript)
		}
	}
	for _, p := range d.AptPin {
		if len(strings.Fields(p)) != 2 {
			return fmt.Errorf("apt_pin %q: want \"package priority\"", p)
		}
	}
	return nil
}

func isSyscall(n string) bool {
	for _, s := range Syscalls {
		if s == n {
			return true
		}
	}
	return false
}

// Script is scripts/<name>.sh.
func Script(name string) (string, error) {
	if strings.ContainsAny(name, "/.") {
		return "", fmt.Errorf("bad script name")
	}
	b, err := files.ReadFile("scripts/" + name + ".sh")
	return string(b), err
}

// Match returns the rules that apply to f, in table order. Rules with
// probe conditions don't apply while the probe hasn't run.
func (t *Table) Match(f Facts) []Rule {
	var out []Rule
	for _, r := range t.Rules {
		if r.When.holds(f) {
			out = append(out, r)
		}
	}
	return out
}

func (w When) holds(f Facts) bool {
	p := f.Probe
	if p == nil {
		p = &Probe{}
	}
	ka, kb := kernelVersion(p.Kernel)
	kern := ka*1000 + kb
	ver := func(s string) int { a, b := kernelVersion(s); return a*1000 + b }
	switch {
	case w.KernelLT != "" && (p.Kernel == "" || kern >= ver(w.KernelLT)):
		return false
	case w.KernelGE != "" && (p.Kernel == "" || kern < ver(w.KernelGE)):
		return false
	case w.SDKLT != 0 && (p.SDK == 0 || p.SDK >= w.SDKLT):
		return false
	case w.SDKGE != 0 && p.SDK < w.SDKGE:
		return false
	case w.RAMMBLT != 0 && (p.RAMMB <= 0 || p.RAMMB >= w.RAMMBLT):
		return false
	case !in(w.Distro, f.Distro) || !in(w.Family, f.Family) || !in(w.DE, f.DE):
		return false
	case !in(w.Termux, p.Termux) || !in(w.ROM, p.ROM) || !in(w.Phantom, p.Phantom):
		return false
	case w.Desktop != nil && *w.Desktop != (f.DE != "" && f.DE != "none"):
		return false
	}
	for n, results := range w.Probe {
		if r, ok := p.Syscalls[n]; !ok || !in(results, r) {
			return false
		}
	}
	if len(w.ProbeAny) > 0 {
		hit := false
		for _, r := range p.Syscalls {
			if in(w.ProbeAny, r) {
				hit = true
			}
		}
		if !hit {
			return false
		}
	}
	return true
}

// in: an empty list allows anything; otherwise v must be listed.
func in(list []string, v string) bool {
	if len(list) == 0 {
		return true
	}
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// Plan is what the matching rules add up to.
type Plan struct {
	IDs          []string
	Env          map[string]string
	Preloads     []string
	PreScripts   []string
	AptPins      []string
	SkipOptional map[string]bool
	Warnings     []string // message names
	Refuse       string   // message name, or ""
}

// PlanFor merges the rules' actions (in order; later env values win).
func PlanFor(rules []Rule) *Plan {
	p := &Plan{Env: map[string]string{}, SkipOptional: map[string]bool{}}
	add := func(list []string, v string) []string {
		for _, x := range list {
			if x == v {
				return list
			}
		}
		return append(list, v)
	}
	for _, r := range rules {
		p.IDs = append(p.IDs, r.ID)
		for k, v := range r.Do.Env {
			p.Env[k] = v
		}
		if r.Do.Preload != "" {
			p.Preloads = add(p.Preloads, r.Do.Preload)
		}
		if r.Do.PreScript != "" {
			p.PreScripts = add(p.PreScripts, r.Do.PreScript)
		}
		for _, x := range r.Do.AptPin {
			p.AptPins = add(p.AptPins, x)
		}
		for _, x := range r.Do.SkipOptional {
			p.SkipOptional[x] = true
		}
		if r.Do.Warn != "" {
			p.Warnings = add(p.Warnings, r.Do.Warn)
		}
		if r.Do.Refuse != "" && p.Refuse == "" {
			p.Refuse = r.Do.Refuse
		}
	}
	return p
}

// EnvList is Env as KEY=value, sorted.
func (p *Plan) EnvList() []string {
	var out []string
	for k, v := range p.Env {
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return out
}

// Message is a message name's text (%s: the kernel).
func Message(name string, pr *Probe) string {
	m := Messages[name]
	if strings.Contains(m, "%s") {
		k := ""
		if pr != nil {
			k = pr.Kernel
		}
		return fmt.Sprintf(m, k)
	}
	return m
}
