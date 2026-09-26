# Kali Linux rolling (apt): port findings

Tested Sep 25, 2026 in the Termux-like Docker box (`tests/Dockerfile`: arm64, non-root user, Termux's proot with `--link2symlink --kill-on-exit --sysvipc -L --kernel-release`, then `-0`). Rootfs from `ci/build-rootfs.sh kali aarch64` (`kalilinux/kali-rolling:latest`, Kali 2026.3, 30 MB `.tar.xz`). Driver: `tests/port-test.sh kali <de>`.

## Result

All combos passed on the final run. The screenshots, install logs and notes are in `docs/screens/INDEX.md`.

| Desktop | Result | Packages | Install time* | Rootfs |
|---|---|---|---|---|
| none | pass | 16 | 33 s | 239 MB |
| XFCE | pass | 267 | 158 s | 1350 MB |
| LXQt | pass | 339 | 276 s | 1428 MB |
| MATE | pass, but see the caja quirk | 463 | 274 s | 1823 MB |

\* On a Mac over broadband, with 3 installs running in parallel.

- **Versions:** TigerVNC 1.15.0 (the `Xtigervnc` process), Firefox ESR 140, MATE 1.26.
- **CPUs:** the image has arm64, arm/v7, 386 and amd64, so `DISTRO_ARCHES="aarch64 arm i686 x86_64"`. Only aarch64 was tested.

## Commands

Kali uses the apt family unchanged: the same update, upgrade, install, simulate, progress regex, VNC packages (`tigervnc-standalone-server tigervnc-tools`) and `APT::Sandbox::User "root"` as Debian. master's `pkgmgr.go` apt entry is correct for Kali, plus **one required host-side fixup: swap the package source.**

- **Rewrite the source before the first `apt-get update`.** It's done host-side, as a literal replace in `etc/apt/sources.list.d/kali.sources`: `http://http.kali.org/kali/` becomes `http://kali.download/kali/`.
- **Data:** `DISTRO_MIRROR_REWRITE="http://http.kali.org/kali/ http://kali.download/kali/"` in `distros/kali.conf`, a new key documented in DESIGN.md section 8. The prototype applies it in `families/apt.sh` `fam_configure` with `sed` over `sources.list`, `sources.list.d/*.list` and `*.sources`.

Why:

- **The image.** `kali-rolling` ships **without `ca-certificates`**. Its only source is the deb822 file `/etc/apt/sources.list.d/kali.sources` (there is no `sources.list`), with `URIs: http://http.kali.org/kali/`.
- **The redirector.** `http.kali.org` answers each file with a 302 to a nearby mirror, and some of those mirrors are https-only. Without a CA store, those downloads fail:
  ```
  E: Failed to fetch https://kalimirror.velden.media/kali/pool/main/c/ca-certificates/ca-certificates_20260601_all.deb  SSL connection failed: error:0A000086:SSL routines::certificate verify failed
  ```
  The mirror's certificate itself is valid (checked with curl on the Mac).
- **Installing `ca-certificates` first isn't enough.** The first try installed it right after `update`, and it fixed a few runs. Then the redirector sent the `ca-certificates` download itself to the https mirror (the line above), and LXQt failed.
- **`kali.download` is Kali's own CDN** (Cloudflare). It serves plain http with no redirects: `HEAD /kali/dists/kali-rolling/InRelease` returns 200 directly.
  - After the rewrite, all 4 Kali combos passed.
  - Package integrity still comes from apt's GPG signatures (`kali-archive-keyring`), not from TLS.
- **The prototype also still installs `ca-certificates` right after `update`.** This is `fam_update` in `families/apt.sh`: `dpkg -s ca-certificates || apt-get install ca-certificates`. It's cheap and lets users add https sources later. `debian:13` lacks it too.

## Desktop package lists (verified; same lines serve Debian and Ubuntu)

| DE | `DE_PKGS_apt` | `DE_SESSION` |
|---|---|---|
| XFCE | unchanged from Debian: `xfce4 xfce4-terminal xfce4-whiskermenu-plugin dbus-x11 adwaita-icon-theme fonts-dejavu-core mousepad` | `startxfce4` |
| LXQt | `lxqt-core openbox breeze-icon-theme dbus-x11 featherpad fonts-dejavu-core` | `startlxqt` |
| MATE | `mate-desktop-environment-core dbus-x11 pluma fonts-dejavu-core` | `mate-session` |

- **`lxqt-core` depends on no window manager.** LXQt defaults to openbox, so it is listed explicitly.
- **`lxqt-core` doesn't pull the icon theme it configures** (`icon_theme=breeze`) when installed with `--no-install-recommends`. Without `breeze-icon-theme`, all icons are blank.
- **`mate-desktop-environment-core`** already includes caja, marco, mate-panel, mate-terminal, mate-polkit and mate-session-manager.
- **The browser is `firefox-esr`,** as on Debian.

## Users and sudo

```sh
useradd -m -s /bin/bash -G sudo alice && echo 'alice:pw' | chpasswd   # works
```

- **Membership in `sudo` is invisible to sudo under proot** ("alice is not in the sudoers file").
- **A sudoers drop-in works:** `echo 'alice ALL=(ALL:ALL) ALL' > /etc/sudoers.d/alice`. Then `sudo id -u` prints `0`.
- **sudo prints `sudo: unable to send audit message: Operation not permitted`** each time. Harmless.
- **Kali's `/usr/bin/sudo` is 4755,** so Fedora's exec-only problem doesn't apply.

## Quirks

- **GUI apps started from a second `./start-kali.sh` can't reach the desktop's D-Bus services.**
  - Seen from a second proot session exporting the session's `DBUS_SESSION_BUS_ADDRESS`: `mate-terminal` exits 1 silently, and `pcmanfm-qt /root` opens nothing.
  - The same commands work from the desktop's menu, or from a process inside the VNC session's own proot.
  - Launching from the menu (the normal path) works. The docs should say: start GUI apps from the desktop, not from another Termux tab.
- **`SHELL` was unset** (`env -i`), so qterminal opened `/bin/sh`. Fixed: `lib/launch.sh` passes `SHELL=$DISTRO_SHELL`. The Go launcher needs the same.
- **The Kali bash prompt's `㉿`** shows as a box: `fonts-dejavu-core` lacks the glyph. Cosmetic.
- **caja and mate-terminal title bars say "(as aid_termux)":** they see the real uid behind proot's fake root. Cosmetic.

- **XFCE has a black background,** as on Debian. Kali's wallpapers and theme (`kali-desktop-xfce`, `kali-themes`, `kali-wallpapers-*`) are not installed. The app-menu icon is the Kali dragon.
- **LXQt:**
  - The desktop icons carry a "!" emblem, because pcmanfm-qt marks the `.desktop` files as untrusted.
  - A red ✗ tray icon shows (the volume plugin: no PulseAudio in the box).
  - The panel's leftmost (main-menu) button shows no icon in the screenshot. Not investigated.
- **MATE: caja never stays up, so there are no desktop icons and no wallpaper.** The panel, menus, marco and the terminal work.
  - mate-session starts caja with `DESKTOP_AUTOSTART_ID`, and caja exits 0 at once.
  - The same `caja` stays up when started by hand, and then draws the default gradient background.
  - After the test opens a caja window, caja takes over the desktop and draws it.
  - Details and workaround candidates: `docs/ports/fedora.md` (same on both).
