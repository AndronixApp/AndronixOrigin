package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/AndronixApp/andronix-distros/internal/conf"
	"github.com/AndronixApp/andronix-distros/internal/rootfs"
	"github.com/AndronixApp/andronix-distros/internal/sys"
	"github.com/AndronixApp/andronix-distros/internal/ui"
)

// Editions share their distro's folder (~/.andronix/distros/ubuntu holds
// free Ubuntu, Ubuntu XFCE, Ubuntu KDE or Classic Ubuntu: one at a time;
// installing another over it needs --reinstall). So the app can name the
// edition, and remove, start or desktop act only if that's the one there.

// v8Modded is where the old Modded scripts (moddedos-scripts V2/V3, run in
// Termux's home) put each Classic edition's product: ~/<name>-fs,
// ~/<name>-binds and ~/start-<name>.sh.
var v8Modded = map[string]string{
	"legacy-ubuntu-xfce": "andronix",  // andronix_os
	"legacy-ubuntu-kde":  "androkde",  // kde_ubuntu
	"legacy-debian":      "androdeb",  // debian_mod
	"legacy-manjaro":     "androjaro", // manjaro
}

// installedEdition is the edition in in's folder: "" for the free one.
func installedEdition(in *Inst) string {
	have := in.Get("EDITION")
	if have == "" && moddedImage(in) { // Modded installs from before EDITION was recorded
		have = conf.Parse(readFile(filepath.Join(in.Rootfs, "usr/share/andronix/modded/edition"))).Get("EDITION")
		if have == "" || have == "modded" {
			have = in.D.ID + "-" + in.Get("DE")
		}
	}
	return have
}

// editionLabel: "Andronix Modded 2.0 Ubuntu XFCE (ubuntu-xfce)", or
// "the free Ubuntu" for "".
func editionLabel(d *conf.Distro, id string) string {
	if id == "" {
		return "the free " + d.Name
	}
	de := ""
	if ed, err := conf.ResolveEdition(id); err == nil {
		if x, err := conf.ResolveDesktop(ed.Desktop); err == nil {
			de = " " + x.Name
		}
	}
	return editionName(id) + " " + d.Name + de + " (" + id + ")"
}

// removeCmdFor is the remove command for what's installed.
func removeCmdFor(d *conf.Distro, have string) string {
	if have == "" {
		return "andronix remove " + d.ID
	}
	return "andronix remove " + have
}

// editionDistro: for an edition id, its distro when that edition is the
// one installed; an error that says what's there instead otherwise.
func editionDistro(ed *conf.Edition, cmd string) (*conf.Distro, error) {
	d, err := conf.LoadDistro(ed.Distro)
	if err != nil {
		return nil, err
	}
	in := Open(d)
	if _, err := os.Stat(in.Rootfs); err != nil {
		return nil, ui.Errorf(editionLabel(d, ed.ID)+" isn't installed", "There's nothing to "+cmd+".",
			"Install it from the Andronix app.")
	}
	if have := installedEdition(in); have != ed.ID {
		return nil, ui.Errorf(editionLabel(d, ed.ID)+" isn't installed", "The "+d.Name+" that's installed is "+editionLabel(d, have)+".",
			"Use: andronix "+cmd+" "+d.ID)
	}
	return d, nil
}

// v8Leftovers are the old Modded scripts' files for a Classic edition
// that exist: the rootfs, the binds folder and the start script.
func v8Leftovers(id string) []string {
	n := v8Modded[id]
	if n == "" {
		return nil
	}
	var out []string
	home := sys.Home()
	for _, p := range []string{n + "-fs", n + "-binds"} {
		if st, err := os.Stat(filepath.Join(home, p)); err == nil && st.IsDir() {
			out = append(out, filepath.Join(home, p))
		}
	}
	// Only the old script's own launcher (it runs proot on <name>-fs).
	s := filepath.Join(home, "start-"+n+".sh")
	if b, err := os.ReadFile(s); err == nil && strings.Contains(string(b), n+"-fs") && !strings.Contains(string(b), LauncherMark) {
		out = append(out, s)
	}
	return out
}

// removeEdition is `andronix remove <edition-id> [--legacy]`: deletes that
// edition, never the free distro or another edition in the same folder;
// --legacy also deletes a Classic edition's v8 Modded files. Nothing of
// it there: says so and exits 0 (like remove of a distro that isn't
// installed).
func removeEdition(ctx context.Context, ed *conf.Edition, legacy bool) error {
	d, err := conf.LoadDistro(ed.Distro)
	if err != nil {
		return err
	}
	in := Open(d)
	_, statErr := os.Stat(in.Dir)
	have := ""
	if statErr == nil {
		have = installedEdition(in)
	}
	mine := statErr == nil && have == ed.ID
	var old []string
	if legacy {
		old = v8Leftovers(ed.ID)
	}
	label := editionLabel(d, ed.ID)
	kept := func() {
		if statErr == nil && !mine {
			ui.Note("The " + d.Name + " that's installed is " + editionLabel(d, have) + "; it was kept. To remove that one: " + removeCmdFor(d, have))
		}
	}
	if !mine && len(old) == 0 {
		fmt.Println()
		ui.OK(label + " isn't installed, so there's nothing to remove.")
		kept()
		fmt.Println()
		return nil
	}
	lines := []string{}
	if mine {
		lines = append(lines, "Deletes "+label+" and everything inside it.",
			ui.KV("Folder", ui.Tilde(in.Dir)+" ("+ui.Bytes(diskSize(in.Dir))+")"), ui.KV("Launcher", "~/"+d.MainStart()))
		if n := cacheSize(cacheEntries(GetPaths().Cache, d.ID)); n > 0 {
			lines = append(lines, ui.KV("Downloads", ui.Bytes(n)+" in "+ui.Tilde(GetPaths().Cache)))
		}
	}
	if len(old) > 0 {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, "Also deletes the copy the old Andronix app installed:")
		for _, p := range old {
			lines = append(lines, ui.KV("", ui.Tilde(p)+" ("+ui.Bytes(diskSize(p))+")"))
		}
	}
	if statErr == nil && !mine {
		lines = append(lines, "", "Keeps "+editionLabel(d, have)+" in "+ui.Tilde(in.Dir)+".")
	}
	lines = append(lines, "", "Files in /sdcard are not touched.")
	fmt.Println()
	fmt.Print(ui.Box(ui.BoxWarn, "Remove "+label+"?", lines...))
	fmt.Println()
	ok, err := ui.Confirm("Remove it?", "", false)
	if err != nil {
		return err
	}
	if !ok {
		ui.Note("Nothing was removed.")
		return nil
	}
	lg := NewLog("remove-" + ed.ID)
	defer lg.Close()
	var steps []ui.Step
	if mine {
		steps = append(steps, ui.Step{Label: "Removing " + label, Run: func(ctx context.Context, r ui.Reporter) error {
			if err := rootfs.Remove(in.Dir); err != nil {
				return ui.Errorf("Couldn't remove everything", err.Error(), "Exit every "+d.Name+" session (and vncserver-stop), then try again.")
			}
			RemoveLaunchers(d)
			removeCache(cacheEntries(GetPaths().Cache, d.ID))
			return nil
		}})
	}
	if len(old) > 0 {
		steps = append(steps, ui.Step{Label: "Removing the old app's copy", Run: func(ctx context.Context, r ui.Reporter) error {
			for _, p := range old {
				if st, err := os.Stat(p); err == nil && st.IsDir() {
					if err := rootfs.Remove(p); err != nil {
						return ui.Errorf("Couldn't remove the old copy", err.Error(), "Exit every session of it, then try again.")
					}
				} else {
					os.Remove(p)
				}
			}
			return nil
		}})
	}
	fmt.Println()
	if err := ui.RunSteps(ctx, steps, lg); err != nil {
		return err
	}
	fmt.Println()
	ui.OK(label + " is gone. Reinstall any time from the Andronix app.")
	kept()
	fmt.Println()
	return nil
}
