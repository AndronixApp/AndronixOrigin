package app

import (
	"context"
	"fmt"
	"github.com/AndronixApp/andronix-distros/internal/sys"
	"os"
	"path/filepath"
	"strings"

	"github.com/AndronixApp/andronix-distros/internal/conf"
	"github.com/AndronixApp/andronix-distros/internal/proot"
	"github.com/AndronixApp/andronix-distros/internal/rootfs"
	"github.com/AndronixApp/andronix-distros/internal/ui"
)

// WallpaperPath is where the default Andronix background lives.
const WallpaperPath = "/usr/share/backgrounds/andronix/andronix.png"

// PlasmaWallpaper is the same background as a Plasma wallpaper package:
// landscape and portrait (9:20, like the Modded editions') images, and
// Plasma shows the one closest to the screen's shape, also after the
// phone rotates.
const PlasmaWallpaper = "/usr/share/wallpapers/Andronix"

// xstartup starts the desktop inside VNC. @SESSION@ is the DE's command.
const xstartup = `#!/bin/sh
# Andronix VNC session: starts @NAME@. Output goes to the session log.
log=/tmp/andronix-session-$(id -u).log
exec >>"$log" 2>&1
echo "== $(date) starting @NAME@ as $(id -un) on $DISPLAY"
echo $$ >"/tmp/andronix-session-$(id -u).pid"
unset SESSION_MANAGER
unset DBUS_SESSION_BUS_ADDRESS
export PULSE_SERVER=127.0.0.1
# Firefox's sandboxes can't start under proot: without these, pages render
# no text (content) and no audio plays at all, AAC, Opus or MP3 (the RDD
# and utility decoder processes), so YouTube is silent or won't start.
export MOZ_DISABLE_CONTENT_SANDBOX=1 MOZ_DISABLE_RDD_SANDBOX=1 MOZ_DISABLE_UTILITY_SANDBOX=1
# Firefox's shared memory without memfd seals: on some phones its content
# processes all died otherwise ("Shared memory PlatformHandle is not safe to
# map": Firefox 156 on Ubuntu, and Firefox ESR from 153 on).
export MOZ_SHM_NO_SEALS=1
# No crash-reporter helper process (Android's phantom process killer).
export MOZ_CRASHREPORTER_DISABLE=1
export XDG_RUNTIME_DIR="${XDG_RUNTIME_DIR:-/tmp/runtime-$(id -u)}"
mkdir -p "$XDG_RUNTIME_DIR" && chmod 700 "$XDG_RUNTIME_DIR"
# Performance profile (andronix tune): the light layer goes first.
if [ "$(cat /etc/andronix/profile 2>/dev/null)" = light ] && [ -d /etc/xdg/andronix-light ]; then
    export XDG_CONFIG_DIRS="/etc/xdg/andronix-light:${XDG_CONFIG_DIRS:-/etc/xdg}"
fi
[ -r "$HOME/.Xresources" ] && xrdb "$HOME/.Xresources"
# Keyboard layout chosen at first boot (andronix setup-user).
[ -r /etc/andronix/keyboard ] && command -v setxkbmap >/dev/null && setxkbmap "$(cat /etc/andronix/keyboard)"
# dbus-run-session keeps the bus for the whole session; under proot,
# dbus-launch's --exit-with-session watcher can kill it at once (Arch).
# If dbus-run-session itself fails fast, fall back to dbus-launch.
if command -v dbus-run-session >/dev/null 2>&1; then
    t0=$(date +%s)
    dbus-run-session -- sh -c '/usr/local/bin/andronix session-prep 2>/dev/null; exec @SESSION@'
    rc=$?
    [ $(( $(date +%s) - t0 )) -gt 15 ] && exit $rc
    echo "== dbus-run-session ended after $(( $(date +%s) - t0 ))s (exit $rc); retrying with dbus-launch"
fi
exec dbus-launch --exit-with-session sh -c '/usr/local/bin/andronix session-prep 2>/dev/null; exec @SESSION@'
`

// setupDesktop writes the VNC session files (for root and, through
// /etc/skel, for users made later) and the default wallpaper.
func setupDesktop(ctx context.Context, in *Inst, de *conf.Desktop, t *proot.Target, r ui.Reporter) error {
	root := in.Rootfs
	xs := strings.NewReplacer("@SESSION@", de.Session, "@NAME@", de.Name).Replace(xstartup)
	// TigerVNC 1.14+ keeps its files in ~/.config/tigervnc; ~/.vnc (what
	// the docs use) is a symlink to it, which also stops TigerVNC's own
	// migration (it fails under proot).
	for _, home := range []string{"root", "etc/skel"} {
		dir := filepath.Join(root, home, ".config/tigervnc")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		vnc := filepath.Join(root, home, ".vnc")
		if st, err := os.Lstat(vnc); err == nil && st.IsDir() {
			entries, _ := os.ReadDir(vnc)
			for _, e := range entries {
				os.Rename(filepath.Join(vnc, e.Name()), filepath.Join(dir, e.Name()))
			}
			os.RemoveAll(vnc)
		}
		if _, err := os.Lstat(vnc); err != nil {
			os.Symlink(".config/tigervnc", vnc)
		}
		if err := os.WriteFile(filepath.Join(dir, "xstartup"), []byte(xs), 0o755); err != nil {
			return err
		}
		os.Chmod(filepath.Join(dir, "xstartup"), 0o755)
	}
	// Hide autostart entries that can't work under proot (they'd pop up
	// error dialogs): a user-level override with Hidden=true, for root and
	// (through /etc/skel) every user made later.
	for _, n := range de.AutostartHide {
		for _, home := range []string{"root", "etc/skel"} {
			dir := filepath.Join(root, home, ".config/autostart")
			os.MkdirAll(dir, 0o755)
			os.WriteFile(filepath.Join(dir, n+".desktop"),
				[]byte("[Desktop Entry]\nType=Application\nName="+n+"\nExec=true\nHidden=true\n"), 0o644)
		}
	}
	phoneDefaults(root)
	// Modded editions bring their own look; don't overwrite it.
	if !moddedImage(in) {
		if err := rootfs.Wallpaper(filepath.Join(root, WallpaperPath), 1920, 1080); err != nil {
			return err
		}
		if de.ID == "kde" {
			if err := plasmaWallpaper(root); err != nil {
				return err
			}
		}
		if err := setWallpaper(ctx, root, de.ID, t); err != nil {
			return err
		}
	}
	// DE_DESKTOP_EDITS: patched copies of .desktop files in
	// /usr/local/share/applications (first in XDG_DATA_DIRS, survives
	// package upgrades).
	for _, e := range de.DesktopEdits {
		file, kv, ok := strings.Cut(e, ":")
		key, _, ok2 := strings.Cut(kv, "=")
		if !ok || !ok2 {
			continue
		}
		dst := filepath.Join(root, "usr/local/share/applications", file)
		src := dst
		b, err := os.ReadFile(src)
		if err != nil {
			if b, err = os.ReadFile(filepath.Join(root, "usr/share/applications", file)); err != nil {
				r.Line("desktop edit: no " + file)
				continue
			}
		}
		lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
		done := false
		for i, l := range lines {
			if strings.HasPrefix(l, key+"=") {
				lines[i], done = kv, true
			}
		}
		if !done {
			lines = append(lines, kv)
		}
		os.MkdirAll(filepath.Dir(dst), 0o755)
		os.WriteFile(dst, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
	}
	// DE_XDG_CONFIG: KDE-style INI defaults in /etc/xdg.
	if err := writeXDGConfig(root, de.XDGConfig); err != nil {
		return err
	}
	// Performance profile: light by default on phones under 3 GB of RAM.
	profile := Profile(root)
	if _, err := os.Stat(filepath.Join(root, profileFile)); err != nil {
		if ram := sys.TotalRAMMB(); ram > 0 && ram < AutoLightBelow {
			profile = "light"
			r.Detail(fmt.Sprintf("light profile (%d MB RAM)", ram))
		}
	}
	if err := ApplyProfile(root, de, profile); err != nil {
		return err
	}
	// LXQt asks for a window manager at first login; preset openbox.
	if de.ID == "lxqt" {
		for _, home := range []string{"root", "etc/skel"} {
			p := filepath.Join(root, home, ".config/lxqt/session.conf")
			if _, err := os.Stat(p); err != nil {
				os.MkdirAll(filepath.Dir(p), 0o755)
				os.WriteFile(p, []byte("[General]\nwindow_manager=openbox\n"), 0o644)
			}
		}
	}
	// Fail here, not at first start, if the VNC server didn't install.
	// Xvnc is the common part: some distros have no vncserver wrapper.
	return t.Run(ctx, "command -v Xvnc >/dev/null && command -v vncpasswd >/dev/null", nil, func(l string) { r.Line(l) })
}

// writeXstartup (re)writes the session script for root, /etc/skel and
// every existing user, so fixes reach old installs too.
func writeXstartup(root string, de *conf.Desktop) {
	xs := strings.NewReplacer("@SESSION@", de.Session, "@NAME@", de.Name).Replace(xstartup)
	homes, _ := filepath.Glob(filepath.Join(root, "home/*"))
	homes = append(homes, filepath.Join(root, "root"), filepath.Join(root, "etc/skel"))
	for _, h := range homes {
		dir := filepath.Join(h, ".config/tigervnc")
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		os.WriteFile(filepath.Join(dir, "xstartup"), []byte(xs), 0o755)
		os.Chmod(filepath.Join(dir, "xstartup"), 0o755)
	}
}

// writeXDGConfig sets "<file>:<group>:<key>=<value>" entries in
// /etc/xdg/<file>, keeping whatever else the file holds.
func writeXDGConfig(root string, entries []string) error {
	return writeINI(filepath.Join(root, "etc/xdg"), entries)
}

// iniSet sets key=value in [group] of an INI text.
func iniSet(s, group, key, value string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		lines = nil
	}
	in, start, end := false, -1, len(lines)
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "[") {
			if in {
				end = i
				break
			}
			if t == "["+group+"]" {
				in, start = true, i
			}
		}
	}
	if start < 0 {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		return strings.Join(append(lines, "["+group+"]", key+"="+value), "\n") + "\n"
	}
	for i := start + 1; i < end; i++ {
		if k, _, ok := strings.Cut(lines[i], "="); ok && strings.TrimSpace(k) == key {
			lines[i] = key + "=" + value
			return strings.Join(lines, "\n") + "\n"
		}
	}
	out := append([]string{}, lines[:end]...)
	out = append(out, key+"="+value)
	out = append(out, lines[end:]...)
	return strings.Join(out, "\n") + "\n"
}

// setWallpaper makes the Andronix background each desktop's default,
// using the system-wide defaults each one reads for new users.
func setWallpaper(ctx context.Context, root, de string, t *proot.Target) error {
	w := func(rel, s string) error {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		return os.WriteFile(p, []byte(s), 0o644)
	}
	switch de {
	case "xfce":
		// xfdesktop keys backdrops by monitor name: VNC's is "VNC-0",
		// Termux:X11's "builtin".
		var mons strings.Builder
		for _, m := range []string{"monitorVNC-0", "monitorbuiltin", "monitor0", "monitorscreen", "monitorDisplayPort-0", "monitordefault"} {
			// Zoomed (5) fills a landscape screen. Termux:X11 is usually a
			// portrait phone, where zoom crops the wordmark: scaled (4) on
			// the wallpaper's own base colour (#15110e) instead.
			style := `          <property name="image-style" type="int" value="5"/>
`
			if m == "monitorbuiltin" {
				style = `          <property name="image-style" type="int" value="4"/>
          <property name="rgba1" type="array">
            <value type="double" value="0.082353"/>
            <value type="double" value="0.066667"/>
            <value type="double" value="0.054902"/>
            <value type="double" value="1"/>
          </property>
`
			}
			mons.WriteString(`      <property name="` + m + `" type="empty">
        <property name="workspace0" type="empty">
          <property name="last-image" type="string" value="` + WallpaperPath + `"/>
` + style + `          <property name="color-style" type="int" value="0"/>
        </property>
      </property>
`)
		}
		return w("etc/xdg/xfce4/xfconf/xfce-perchannel-xml/xfce4-desktop.xml", `<?xml version="1.0" encoding="UTF-8"?>
<channel name="xfce4-desktop" version="1.0">
  <property name="backdrop" type="empty">
    <property name="screen0" type="empty">
`+mons.String()+`    </property>
  </property>
</channel>
`)
	case "lxqt":
		// Fitted on its own base colour: zoom cropped the wordmark on a
		// portrait phone (pcmanfm-qt has one wallpaper for any shape).
		return w("etc/xdg/pcmanfm-qt/lxqt/settings.conf", "[Desktop]\nWallpaper="+WallpaperPath+"\nWallpaperMode=fit\nBgColor=#15110e\n")
	case "lxde":
		return w("etc/xdg/pcmanfm/LXDE/desktop-items-0.conf", "[*]\nwallpaper_mode=crop\nwallpaper_common=1\nwallpaper="+WallpaperPath+"\n")
	case "mate":
		if err := w("usr/share/glib-2.0/schemas/90_andronix.gschema.override",
			"[org.mate.background]\npicture-filename='"+WallpaperPath+"'\npicture-options='zoom'\n"); err != nil {
			return err
		}
		return t.Run(ctx, "glib-compile-schemas /usr/share/glib-2.0/schemas", nil, nil)
	case "kde":
		// plasmashell runs each update script once per user, inside the
		// shell, after the layout exists. The old autostart entry called
		// plasma-apply-wallpaperimage before plasmashell was on D-Bus and
		// left Plasma's default wallpaper.
		os.Remove(filepath.Join(root, "etc/xdg/autostart/andronix-wallpaper.desktop"))
		updates := "usr/share/plasma/shells/org.kde.plasma.desktop/contents/updates/"
		set := "    d.wallpaperPlugin = \"org.kde.image\";\n" +
			"    d.currentConfigGroup = [\"Wallpaper\", \"org.kde.image\", \"General\"];\n" +
			"    d.writeConfig(\"Image\", \"file://" + PlasmaWallpaper + "/\");\n" +
			"    d.writeConfig(\"FillMode\", 2);\n"
		if err := w(updates+"andronix-wallpaper.js", "// Andronix: the Andronix wallpaper on every desktop, once per user.\n"+
			"desktops().forEach(function (d) {\n"+set+"});\n"); err != nil {
			return err
		}
		// Users who already ran the one above had the flat 16:9 image
		// (cropped on a portrait phone): move them to the package. Runs
		// before it for new users, and then changes nothing.
		return w(updates+"andronix-wallpaper-2.js", "// Andronix: the portrait-aware wallpaper for users still on the old one.\n"+
			"desktops().forEach(function (d) {\n"+
			"    d.currentConfigGroup = [\"Wallpaper\", \"org.kde.image\", \"General\"];\n"+
			"    if (d.readConfig(\"Image\", \"\") != \"file://"+WallpaperPath+"\") return;\n"+set+"});\n")
	}
	return nil
}

// refreshWallpaper brings an existing free LXQt or KDE install to the
// portrait-safe wallpaper (andronix update). Modded editions keep theirs;
// a wallpaper the user picked is left alone.
func refreshWallpaper(ctx context.Context, root string, de *conf.Desktop) {
	if hasAny(root, "usr/share/andronix/modded") || !hasAny(root, strings.TrimPrefix(WallpaperPath, "/")) {
		return
	}
	switch de.ID {
	case "kde":
		plasmaWallpaper(root)
		setWallpaper(ctx, root, de.ID, nil)
	case "lxqt":
		setWallpaper(ctx, root, de.ID, nil)
		homes, _ := filepath.Glob(filepath.Join(root, "home/*"))
		for _, h := range append(homes, filepath.Join(root, "root")) {
			p := filepath.Join(h, ".config/pcmanfm-qt/lxqt/settings.conf")
			b, err := os.ReadFile(p)
			if err != nil || !strings.Contains(string(b), "Wallpaper="+WallpaperPath+"\n") || !strings.Contains(string(b), "WallpaperMode=zoom") {
				continue
			}
			s := iniSet(string(b), "Desktop", "WallpaperMode", "fit")
			os.WriteFile(p, []byte(iniSet(s, "Desktop", "BgColor", "#15110e")), 0o644)
		}
	}
}

// plasmaWallpaper writes the Plasma package (PlasmaWallpaper).
func plasmaWallpaper(root string) error {
	dir := filepath.Join(root, PlasmaWallpaper)
	for _, s := range [][2]int{{1920, 1080}, {1440, 3200}} {
		if err := rootfs.Wallpaper(filepath.Join(dir, "contents/images", fmt.Sprintf("%dx%d.png", s[0], s[1])), s[0], s[1]); err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(`{
    "KPlugin": {
        "Id": "Andronix",
        "Name": "Andronix",
        "License": "CC-BY-SA-4.0"
    }
}
`), 0o644)
}

// firefoxPrefs keep Firefox to a few processes. Android's phantom process
// killer stops Termux, and the whole desktop with it, once the app has
// more than 32 processes (Redmi, Android 16: a desktop and Firefox with a
// few tabs). Site isolation (fission) stays on, as Firefox ships it: at
// most one content process per site (webIsolated), and few other
// processes. Defaults only; a user can change them in about:config.
const firefoxPrefs = `// Andronix: fewer Firefox processes, for Android's phantom process killer.
pref("dom.ipc.processCount", 2);
pref("dom.ipc.processCount.webIsolated", 1);
pref("dom.ipc.processPrelaunch.enabled", false);
pref("browser.preferences.defaultPerformanceSettings.enabled", false);
pref("network.process.enabled", false);
pref("layers.gpu-process.enabled", false);
// Media needs its decoder processes: without RDD every video fails
// (NotSupportedError) and without the utility process AAC audio never
// plays, on Debian's ESR 140, Ubuntu and Kali. They only run while media
// plays. Set true to repair files from 2.0.1-rc1, which turned them off.
pref("media.rdd-process.enabled", true);
pref("media.utility-process.enabled", true);
`

// phoneDefaults writes the settings that keep a desktop's process count
// down: Firefox's (in every Firefox's defaults/pref, which package
// upgrades leave alone) and no PulseAudio of the distro's own (sound goes
// to Termux's, through PULSE_SERVER).
func phoneDefaults(root string) {
	dirs, _ := filepath.Glob(filepath.Join(root, "usr/lib*/firefox*/defaults/pref"))
	for _, d := range dirs {
		os.WriteFile(filepath.Join(d, "andronix-phone.js"), []byte(firefoxPrefs), 0o644)
	}
	d := filepath.Join(root, "etc/pulse/client.conf.d")
	if _, err := os.Stat(filepath.Join(root, "etc/pulse")); err == nil {
		os.MkdirAll(d, 0o755)
		os.WriteFile(filepath.Join(d, "00-andronix.conf"), []byte("# Andronix: sound goes to Termux's PulseAudio (PULSE_SERVER); never start one in the distro.\nautospawn = no\n"), 0o644)
	}
}
