# Alpine 3.24 (apk) — port notes

Tested by port-b on 2026-09-25 in the Termux-like Docker box (see `arch.md` for the setup), aarch64, through `families/apk.sh`.

## Rootfs

- `docker.io/library/alpine:3.24`; platforms amd64, 386, arm64v8, armv7, armv6 (+ ppc64le, riscv64, s390x).
- aarch64 export: 3.2 MB `.tar.xz`. busybox only: `/bin/sh` is ash, no bash; bash comes from `DISTRO_BASE_PKGS` (the VNC helpers and the login welcome need it).
- musl libc; apk-tools **3.0.8** (not 2.x). Repos: `https://dl-cdn.alpinelinux.org/alpine/v3.24/{main,community}` (Fastly CDN), fine as is.

## Required fixup: apk scripts and memfd under proot

- apk 3 writes each package script (install scripts **and triggers**) to a `memfd_create(MFD_EXEC)` and runs it with `execve("/proc/self/fd/N")` (`apk-tools src/package.c`, `database.c: apk_db_run_script`). Under proot that exec fails and apk **silently** carries on, printing `Executing glib-2.88.1-r1.trigger` as if it worked.
- Effect: no `gschemas.compiled`, no gdk-pixbuf `loaders.cache`, no mime/icon caches. XFCE starts, every app logs `Cannot get the default GSettingsSchemaSource`, at-spi dies with SIGTRAP and xfce4-session exits within 3 s (black screen).
- apk only uses the memfd when `/proc/sys/vm/memfd_noexec` reads `0` or `1`; with `2` (or no such file, kernels < 6.3) it writes scripts to `/lib/apk/exec/` and they run fine. Fix: bind a file containing `2` over it on every launch:
  ```sh
  echo 2 > ~/.andronix/distros/alpine/sysdata/memfd_noexec
  proot ... -b ~/.andronix/distros/alpine/sysdata/memfd_noexec:/proc/sys/vm/memfd_noexec ...
  ```
  In the bash prototype `fam_configure` writes the file and `lib/launch.sh` binds it when present. Verified: `apk fix glib` then produces `gschemas.compiled`, and a clean install gets a working XFCE. The bind is needed from the first `apk` run on (packages installed without it need `apk fix <pkg>`).
- apk 3 has `--force-no-chroot` but it doesn't help (root is `/` already; the problem is the memfd exec).

## Commands (inside the distro, `sh -c`)

```sh
APK="apk --no-progress"
$APK update                                                     # refresh
$APK upgrade --available --simulate | grep -cE '^\([0-9]+/[0-9]+\) '   # upgrade count
$APK upgrade --available                                        # upgrade
$APK add --simulate PKGS | grep -cE '^\([0-9]+/[0-9]+\) '       # install count
$APK add PKGS                                                   # install
rm -rf /var/cache/apk/*                                         # clean
```

## Progress output (stdout not a tty)

```
(1/3) Installing ncurses-terminfo-base (6.6_p20260516-r0)
(2/3) Installing libncursesw (6.6_p20260516-r0)
(3/3) Installing nano (9.2-r0)
Executing busybox-1.37.0-r31.trigger
OK: 9259 KiB in 19 packages
```
Regex `^\([0-9]+/[0-9]+\) ` (Installing / Upgrading / Replacing), **1 event per package**; the `N` in `(i/N)` is also the total, so the count query could be skipped.

## Packages

- Base extras: `bash sudo nano wget ca-certificates tzdata procps shadow` (`shadow` gives `useradd`/`usermod`/`chpasswd` with the usual flags; busybox has only `adduser`/`addgroup`).
- VNC: `tigervnc xauth`. Alpine's tigervnc 1.16.2 has upstream's `vncserver` (display only), `Xvnc`, `vncpasswd`; `guest/vncserver-start` starts Xvnc directly. musl naming: no `tigervnc-standalone-server`/`-tools` split.
- dbus: `dbus` (has `dbus-run-session`) and `dbus-x11` (`dbus-launch`).
- Browser: `firefox` (151; `firefox-esr` 140 also exists).
- XFCE: `xfce4 xfce4-terminal mousepad dbus dbus-x11 adwaita-icon-theme font-dejavu gsettings-desktop-schemas librsvg` → 238 packages, ~40 s. `gsettings-desktop-schemas` and `librsvg` (SVG icons) are not pulled in by `xfce4` but XFCE needs them. There is no `xfce4-whiskermenu-plugin` in 3.24.
- MATE: explicit list, **not** `mate-desktop-environment` (it pulls `mate-screensaver`, which would lock the VNC desktop behind root's unset password, plus `mate-power-manager`, `mate-polkit`).

## User and admin group

Tested 2026-09-25 (user `andy`, uid 1001). Under proot two things differ from a real system:
- **Log in with `proot -i UID:GID`, not `-0` + `su`.** After `su` from fake root, sudo sees files owned by the emulated uid and refuses. With `-i` the tested sudo versions work, except where noted.
- **Grant sudo by user name, not `%wheel`.** The process's supplementary groups come from the host (Termux's gids), so the guest's `wheel` membership is invisible to sudo (`andy is not in the sudoers file`). `usermod -aG wheel` is still fine for other tools.

```sh
useradd -m -G wheel -s /bin/bash NAME
echo 'NAME:PASSWORD' | chpasswd
echo 'NAME ALL=(ALL:ALL) ALL' > /etc/sudoers.d/NAME && chmod 440 /etc/sudoers.d/NAME
# then log in with: proot ... -i UID:GID ... (instead of -0)
```
- Result: **works** with `sudo -S` and a password (sudo 1.9.17p2). Needs `shadow` for `useradd`/`chpasswd`, already in `DISTRO_BASE_PKGS`.

## Quirks

- `DISTRO_SHELL=/bin/bash` only exists after the base packages are in; a half-finished install that stopped before that can't `start` (Go side: fall back to `/bin/sh` when the shell is missing).
- The old Andronix Alpine installer used Xvfb + x11vnc with password "andronix"; the new one uses TigerVNC like the other distros.

## Firefox

- `firefox` 151.0.3 from the distro's own repo (Mozilla's own repo serves .deb/.rpm only; these distros package Firefox themselves, no snap involved).
- Under proot the content sandbox fails: pages render with `[GFX1]: no fonts` and `--headless --screenshot` writes nothing. With `MOZ_DISABLE_CONTENT_SANDBOX=1` it renders (`docs/screens/alpine-firefox.png`, headless, example.com). `guest/xstartup` and `guest/profile.sh` now export it.
