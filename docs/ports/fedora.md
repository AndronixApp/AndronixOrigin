# Fedora 44 (dnf5): port findings

Tested Sep 25, 2026 in the Termux-like Docker box (`tests/Dockerfile`: arm64, non-root user, Termux's proot with `--link2symlink --kill-on-exit --sysvipc -L --kernel-release`, then `-0`). Rootfs from `ci/build-rootfs.sh fedora aarch64` (`fedora:44`, 40 MB `.tar.xz`). Driver: `tests/port-test.sh fedora <de>`.

## Result

All combos passed on the final run. The screenshots, install logs and notes are in `docs/screens/INDEX.md`.

| Desktop | Result | Packages | Install time* | Rootfs |
|---|---|---|---|---|
| none | pass | 15 | 141 s | 355 MB |
| XFCE | pass | 366 | 212 s | 1278 MB |
| LXQt | pass | 419 | 254 s | 1586 MB |
| MATE | pass | 399 | 347 s | 1491 MB |

\* On a Mac over broadband, with 3 installs running in parallel. The first `makecache` alone takes 40 to 60 s (about 47 MB of metadata).

The installed versions were dnf5 5.4.3, rpm 6.0.2, TigerVNC 1.16.2, xfce4-session 4.20.3 and Firefox 156. Images exist for arm64 and amd64 only, so `DISTRO_ARCHES="aarch64 x86_64"`.

## dnf under proot

- **It works unchanged.** proot needs no sandbox or rpm-lock workaround: no `%_netsharedpath`, no `--nodeps` and no `tsflags` changes. rpm's sysusers scriptlets run fine.
- **Harmless noise:** `>>> Failed to preset unit: Unit systemd-tmpfiles-clear.service does not exist`.
- **The container image already sets `tsflags=nodocs`** in `/etc/dnf/dnf.conf`, so there are no man pages. That is fine for us.

## Commands (verified)

```sh
# update (metadata)
dnf -y --setopt=install_weak_deps=False makecache
# upgrade
dnf -y --setopt=install_weak_deps=False upgrade
# install; no weak deps is the twin of apt's --no-install-recommends
dnf -y --setopt=install_weak_deps=False install PKGS...
# clean: keep metadata, drop downloaded rpms
dnf clean packages
```

- **Keep `install_weak_deps=False` on the command line; don't set it in a config file.** Users running `dnf install` later should get Fedora's normal behaviour.
- **`dnf clean all`** also deletes about 99 MB of metadata in `/var/cache/libdnf5`, and the user's next `dnf` then downloads about 47 MB again. `clean packages` is nearly a no-op, since `keepcache=0` already deletes rpms after a good transaction. Pick based on disk space vs. data use.

### Simulate: how many packages will change

```sh
dnf --assumeno --setopt=install_weak_deps=False install PKGS... 2>&1 |
  awk '/^ (Installing|Upgrading|Downgrading|Reinstalling): +[0-9]/ { n += $2 } END { print n + 0 }'
dnf --assumeno upgrade 2>&1 | awk '...same...'
```

- **The `Transaction Summary` block** looks like ` Installing:        15 packages` or ` Upgrading:         26 packages`. When there is nothing to do, it prints `Nothing to do.` (count 0). `--assumeno` exits 1, so ignore the exit status.
- **master's `pkgmgr.go` counts table rows** with `^ \S+\s+\S+\s+\S+`. That also matches the summary lines (` Installing:  15 packages` has three fields), so it overcounts by 1 or 2. That's harmless, but the awk above is exact.

### Progress output (non-tty, captured from the log)

```
[ 1/15] wget2-wget-0:2.2.1-2.fc44.aarch 100% | 103.5 KiB/s |   9.3 KiB |  00m00s   <- one per download
...
[15/15] Total                           100% |   1.6 MiB/s |  13.7 MiB |  00m09s
Running transaction
[ 1/17] Verify package files            100% | ...
[ 2/17] Prepare transaction             100% | ...
[ 3/17] Installing gpgme-0:2.0.1-5.fc44 100% | ...                                <- one per package
>>> Running sysusers scriptlet: ...                                               <- scriptlet lines, no counter
```

For an upgrade of N packages, the transaction has 2N+2 items: `Upgrading X` plus `Removing X-old` for each package, plus Verify and Prepare. The measured counts were:

| Operation | N | Download lines | Transaction lines | Total `^\[ *\d+/\d+\]` lines |
|---|---|---|---|---|
| install | 366 | 366 + 1 `Total` | 368 (`N+2`) | 735 ≈ 2N |
| upgrade | 26 | 26 + 1 `Total` | 54 (`2N+2`) | 81 ≈ 3N |

- **The counter restarts between phases,** and the transaction total differs from N. master's `Counter` regex (`^\[\s*(?P<cur>\d+)/(?P<total>\d+)\]`) therefore makes the bar run 0→100% for downloads, then again 0→100% for the transaction.
- **What we use instead: step mode.** The regex `^\[ *[0-9]+/[0-9]+\] ` with Phases=2 against the simulated count gives one smooth bar. It overshoots slightly on upgrades; the UI clamps. A counter can still work with a phase weight: the first time `cur` resets, switch to the second half of the bar.

## VNC and dbus packages

- **`tigervnc-server` pulls in `tigervnc-x11-server` and `tigervnc-server-common`.** `tigervnc-server-common` provides `vncpasswd`. Note that `rpm -q tigervnc-server` says "not installed", because the name is only a Provides; check with `command -v vncserver`.
- **`/usr/bin/vncserver` still exists** (the perl wrapper). It prints `WARNING: vncserver has been replaced by a systemd unit and is now considered deprecated`, but it works with the same arguments as Debian's (`:1 -geometry WxH -localhost no -xstartup ...`).
- **The X server process is `Xvnc`,** not `Xtigervnc` as on Debian and Kali. Anything that runs `pgrep` for the server must accept both.
- **dbus:** `dbus-x11` exists and provides `dbus-launch`, so `exec dbus-launch --exit-with-session <session>` works as on Debian.

## Desktop package lists (verified)

| DE | `DE_PKGS_dnf` | `DE_SESSION` |
|---|---|---|
| XFCE | `xfce4-session xfwm4 xfce4-panel xfdesktop xfce4-settings xfce4-terminal Thunar xfce4-whiskermenu-plugin xfce4-appfinder dbus-x11 adwaita-icon-theme dejavu-sans-fonts dejavu-sans-mono-fonts mousepad` | `startxfce4` |
| LXQt | `lxqt-session lxqt-x11-session lxqt-panel lxqt-runner lxqt-config lxqt-notificationd lxqt-globalkeys lxqt-qtplugin lxqt-themes pcmanfm-qt qterminal featherpad openbox dbus-x11 dejavu-sans-fonts dejavu-sans-mono-fonts` | `startlxqt` |
| MATE | `mate-session-manager marco mate-panel caja mate-settings-daemon mate-control-center mate-terminal mate-menus mate-themes mate-icon-theme mate-notification-daemon pluma dbus-x11 dejavu-sans-fonts dejavu-sans-mono-fonts` | `mate-session` |

- **Don't use the comps groups** (`@xfce-desktop-environment`, `@xfce-desktop`, and so on). They pull in NetworkManager and its VPN plugins, lightdm, abrt, blueman, firewall-config and initial-setup: hundreds of useless packages under proot. The old installers used `dnf groupinstall xfce`.
- **`dejavu-sans-mono-fonts` must be listed.** Without it, `fc-match monospace` gives DejaVu Sans, and mate-terminal spaced its text widely. XFCE happened to get it through dependencies.
- **`startlxqt` is in `lxqt-x11-session`,** not in `lxqt-session`. Without it, VNC shows a black screen (`Couldn't exec startlxqt`).
- **Fedora's default wallpapers** (`desktop-backgrounds`) come in as dependencies, so XFCE and LXQt get a real background. Debian and Kali XFCE are black.
- **The browser is `firefox`** (Fedora's own build, arm64 OK, no snap).

## Fixups

1. **`/root` is mode `0550` in the Fedora image.**
   - proot `-0` lets the guest write there, but the host side runs as the Termux user and gets `EACCES`.
   - In the prototype, the `~/.vnc` → `.config/tigervnc` symlink silently failed, so `vncserver-start` couldn't save a password.
   - Fix: host-side `chmod u+rwx <rootfs>/root` right after extraction (in `families/dnf.sh` `fam_configure`). The Go side should do this for every distro; it is harmless where `/root` is already 0700.
2. **`xfce4-session` hard-requires `xfce-polkit`.**
   - Its autostart entry pops up "XFCE PolicyKit Agent" with an error on every login, because there is no polkitd.
   - Fix: hide it with `DE_AUTOSTART_HIDE="xfce-polkit"` (new key, see DESIGN.md section 8). The installer writes `/root/.config/autostart/xfce-polkit.desktop` and `/etc/skel/.config/autostart/xfce-polkit.desktop` containing `Type=Application`, `Exec=true`, `Hidden=true`.
   - `Exec=true` is needed: mate-session ignores an override without `Exec` (it logs `GsmAutostartApp returned NULL`) and starts the system entry.
3. **Don't lower dnf's network settings.**
   - dnf5 defaults to `retries=10`, `timeout=30`, `minrate=1000`.
   - master's `Configure` writes `retries=5` to `/etc/dnf/libdnf5.conf.d/90-andronix.conf`. That drop-in path works (checked with `dnf --dump-main-config`), but 5 *lowers* the default of 10.
   - Suggest writing nothing, or only `max_parallel_downloads`.

## Users and sudo

```sh
useradd -m -s /bin/bash -G wheel alice    # works; wheel is gid 10
echo 'alice:pw' | chpasswd                 # works
```

- **`/usr/bin/sudo` is mode `0711` (`-rwx--x--x`)**, execute-only, and the setuid bit is lost on extraction as non-root.
  - Under proot, a non-root guest user gets `bash: /usr/sbin/sudo: Permission denied`, because proot has to *read* the ELF to load it.
  - Fix: `chmod 4755 /usr/bin/sudo`, run inside the guest.
  - Do it after every dnf transaction: a sudo upgrade resets the mode to 4111.
  - `/usr/bin/sudoreplay` is the only other exec-only file in the base image.
  - Generic form: `find / -xdev -type f -perm -o+x ! -perm -o+r -exec chmod o+r {} +`.
- **Membership in `wheel` is invisible to sudo under proot** ("alice is not in the sudoers file"), the same as `sudo` on apt distros. A sudoers drop-in works: `echo 'alice ALL=(ALL:ALL) ALL' > /etc/sudoers.d/alice`. With that and the chmod, `sudo id -u` prints `0`.
- **There is no `su` in the image.** It is in `util-linux`, which isn't installed. Use `sudo -u alice ...`, or add `util-linux` to the base packages if first-boot needs `su`.

## Quirks

- **GUI apps started from a second `./start-fedora.sh` can't reach the desktop's D-Bus services.** From the menu they work. Details are in `docs/ports/kali.md`; it's the same on both.
- **`SHELL` was unset** (`env -i`), so qterminal opened `/bin/sh`. Fixed: `lib/launch.sh` passes `SHELL=$DISTRO_SHELL`. The Go launcher needs the same.

- **MATE black desktop: fixed.** See "MATE: caja and the desktop" below.
- **caja and mate-terminal title bars say "(as aid_termux)"** (they see the real uid behind proot's fake root). Cosmetic.
- **LXQt shows a red ✗ tray icon** (the volume plugin: no PulseAudio in the box).
- **Every GTK app prints `WARNING: Glycin running without sandbox.`** (the image loaders can't use bwrap under proot). Harmless.

## MATE: caja and the desktop (fixed; same on Kali, Debian and Ubuntu)

**Symptom.** A black desktop with no icons; the panel, menus and apps work.

**Root cause** (from caja's source, `src/caja-application.c`):

- `init_desktop()` sets `no_desktop = TRUE` when `geteuid () == 0`. Under proot `-0`, everything is root.
- mate-session starts caja (a required component, `filemanager`) with `DESKTOP_AUTOSTART_ID`, and that sets `no_default_window`.
- As root, caja also skips `g_application_hold()`.
- So it opens nothing, exits 0 at once, and `X-MATE-AutoRestart` respawns it every second. A D-Bus trace showed `RegisterClient` followed straight by `ClientRemoved`; there is no Stop from mate-session.

**Checked on Kali, Fedora, Debian 13 and Ubuntu 26.04.** Without the fix, `pgrep caja` shows a new pid every second. With it, one stable `caja --force-desktop`.

**Fix: start caja with `--force-desktop`,** which skips the root check. Also move it to the `Panel` startup phase:

- mate-panel never registers with mate-session under proot. Its `GetClients` list stays without it, while marco does register.
- So the `Desktop` phase would only start after the 30 s phase timeout.
- In the Panel phase, the desktop is drawn about 4 s after login.
- `mate-session` still logs `Application 'mate-panel.desktop' failed to register before timeout`, and apps in the later `Application` phase (none in our lists) still start 30 s late.

**As data:**

```
DE_DESKTOP_EDITS="caja.desktop:Exec=caja --force-desktop;caja.desktop:X-MATE-Autostart-Phase=Panel"
```

After the desktop packages install, the installer copies `/usr/share/applications/caja.desktop` to `/usr/local/share/applications/caja.desktop` with those two keys replaced. `/usr/local/share` comes first in the default `XDG_DATA_DIRS`, and mate-session finds a required component by its desktop-file name there. Package upgrades don't touch the copy. The implementation is `desktop_file_edits` in `lib/desktop.sh`; the key is documented in DESIGN.md section 8.

**The Go port needs:**
- parse `DE_DESKTOP_EDITS` (entries split on `;`, each `<file>:<Key>=<value>`);
- replace the key's line, or append it;
- write the copy.

The caja file on Fedora 44 and Kali has `Exec=/usr/bin/caja` and `X-MATE-Autostart-Phase=Desktop`; both are replaced.

**Rejected alternatives:**
- Wrapping `DE_SESSION` to start caja before mate-session: caja stays outside the session, and mate-session keeps respawning its own caja.
- Setting `X-MATE-Autostart-Notify=false` on mate-panel: mate-session ignores it for the Panel phase.
- A gsettings schema override: needs `glib-compile-schemas` and is still a file.

**Ubuntu only: the panel layout.** Ubuntu's default MATE panel layout (`familiar`) references the brisk menu, the indicator applet and the trash applet. Without them there is no app menu, and each missing applet shows an error dialog (`The panel encountered a problem loading "IndicatorAppletCompleteFactory::IndicatorAppletComplete"`). The fix is in `DE_PKGS_apt`: `mate-applet-brisk-menu mate-indicator-applet mate-applets`. That is about 10 extra packages; they exist on Debian, Kali and Ubuntu and are harmless where the layout doesn't use them.
