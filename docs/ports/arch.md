# Arch Linux (pacman) — port notes

Tested by port-b on 2026-09-25 in the Termux-like Docker box (`tests/Dockerfile`: Debian 13 arm64, non-root user, Termux's proot fork with `--link2symlink --kill-on-exit --sysvipc -L`), aarch64 only. Everything below ran through the bash prototype (`families/pacman.sh`), so the commands are exactly what was executed. Manjaro shares all of this; see `manjaro.md` for its differences.

## Rootfs

| CPU | Source |
|---|---|
| x86_64 | `docker.io/library/archlinux:latest` (the only platform that image has) |
| aarch64 | `http://os.archlinuxarm.org/os/ArchLinuxARM-aarch64-latest.tar.gz` (829 MB gzip, has a `.md5`) |
| arm | `http://os.archlinuxarm.org/os/ArchLinuxARM-armv7-latest.tar.gz` (not built or tested) |
| i686 | none (Arch dropped it) |

- `ci/build-rootfs.sh arch aarch64` downloads the tarball, checks the `.md5`, `docker import`s it, then removes kernel, firmware and network tools and exports:
  ```sh
  pkgs=$(pacman -Qq linux-aarch64 linux-armv7 linux-firmware dhcpcd netctl openssh net-tools 2>/dev/null)
  pacman -Rns --noconfirm $pkgs        # only installed names, or pacman refuses the transaction
  rm -rf /boot/* /var/cache/pacman/pkg/* /var/lib/pacman/sync/*
  ```
  The removal needs no network and no keyring. Result: 138 MB `.tar.xz` (from 1.9 GB `/usr`, of which 960 MB firmware and 188 MB modules).
- There is no OCI image for ARM, so **no registry fallback** on aarch64/arm: the CI tarball must be published. (Alternative: push our export to `ghcr.io/andronixapp/arch` and point the fallback there.)
- Conf keys added for this (documented in DESIGN.md): `DISTRO_UPSTREAM_TARBALL_<arch>`, `DISTRO_UPSTREAM_REMOVE`.
- The ALARM tarball ships an empty `/etc/machine-id` and **no keyring** (`/etc/pacman.d/gnupg` missing). pacman 7.1.0, glibc 2.43 (Aug 2026 build).

## Host-side fixups (before first pacman run)

Edit `/etc/pacman.conf`:
```sh
sed -i 's/^CheckSpace/#CheckSpace/' /etc/pacman.conf
# pacman >= 7 only (check /var/lib/pacman/local/pacman-<ver>): add under [options]
DisableSandbox
```
- Both are **precautions**: in Docker, `CheckSpace` and pacman 7's sandbox (Landlock + `DownloadUser = alpm`) both worked under proot. On Android, `/proc/mounts` entries can be unreadable, and Android's app seccomp policy may not allow Landlock syscalls; untested on a phone.
- pacman 6 prints `warning: config file /etc/pacman.conf, line N: directive 'DisableSandbox' in section 'options' not recognized.` on **every** command, so only add it for 7+ (Manjaro ARM stable is on 6.0.2).
- Mirror: keep the image's `Server = http://mirror.archlinuxarm.org/$arch/$repo` (a GeoIP redirector). Arch x86_64 image: its mirrorlist is fine.

## Commands (inside the distro, `sh -c`)

```sh
PAC="pacman --noconfirm --noprogressbar --color never"
# keyring, once (about 10 s on the Mac; ~6 keys signed, 38 disabled)
[ -s /etc/pacman.d/gnupg/pubring.gpg ] || { pacman-key --init && pacman-key --populate; }
$PAC -Syy                                   # refresh
pacman -Qu | grep -c .                      # upgrade count (NOT with --noprogressbar: "invalid option")
$PAC -Su                                    # upgrade
$PAC -Sp --needed --print-format %n PKGS | grep -vc '^:: '   # install count (groups expand)
$PAC -S --needed PKGS                       # install
rm -f /var/cache/pacman/pkg/*               # clean
```
- `pacman-key --populate` with no argument populates every keyring in `/usr/share/pacman/keyrings` (archlinuxarm here).
- Don't use `-Sy PKG` (partial upgrade); refresh, `-Su`, then `-S`.

## Progress output (stdout not a tty)

```
:: Retrieving packages...
 sudo-1.9.17.p2-6-aarch64 downloading...
checking keyring...
...
:: Processing package changes...
upgrading tzdata...
installing sudo...
```
Regex used: `^ .*-(any|aarch64|armv7h|x86_64) downloading|^(installing|upgrading|reinstalling) ` with **2 events per package** (download + install). Already-cached packages skip the download line, so the bar can end below 100% — harmless. The `-Sp` count includes group members and dependencies.

## Packages

- Base extras (`DISTRO_BASE_PKGS`): `sudo nano wget ca-certificates tzdata procps-ng which`
- VNC: `tigervnc xorg-xauth`. dbus: `dbus` (has `dbus-run-session` and `dbus-launch`).
- Browser: `firefox`.
- XFCE: `xfce4 xfce4-whiskermenu-plugin mousepad dbus adwaita-icon-theme ttf-dejavu` → 240 packages, 2m40s.
- LXQt / MATE: see `desktops/*.conf` (explicit lists, not the groups).

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
- Result: **works** with `sudo -S` and a password (sudo 1.9.17p2).

## VNC

- Arch's TigerVNC 1.16 `vncserver` is **upstream's**: `usage: vncserver <display>`, options only from `~/.config/tigervnc/config`, runs in the foreground (`exec xinit`), no `-xstartup`, `-geometry` or `-kill`. `guest/vncserver-start` now detects that (no `-xstartup` in the script) and starts Xvnc itself:
  ```sh
  Xvnc :1 -desktop remote-desktop -localhost=0 -rfbport 5901 -rfbauth ~/.config/tigervnc/passwd \
       -SecurityTypes VncAuth -depth 24 [-geometry WxH] &
  # wait for /tmp/.X11-unix/X1, then
  DISPLAY=:1 ~/.config/tigervnc/xstartup &
  ```
  `vncserver-stop` kills the pid in `/tmp/.X1-lock` when `vncserver -kill` fails.
- **dbus:** `dbus-launch --exit-with-session CMD` loses its bus immediately under proot (the watcher kills the daemon; plain `dbus-launch` keeps it). XFCE then shows "Unable to contact settings server". `guest/xstartup` now uses `exec dbus-run-session -- SESSION` when available (every distro here has it).
- LXQt first login asks for a window manager; the installer writes `/root/.config/lxqt/session.conf` with `[General]\nwindow_manager=openbox`.

## Quirks (harmless)

- `warning: warning given when extracting /usr/bin/sudoedit (Can't set permissions to 0777)`: libarchive's lchmod on symlinks. glibc 2.39+ calls `fchmodat2`, which proot doesn't translate (see `void.md`); pacman only warns.
- Hooks print `Skipped: Current root is not booted.` and a systemd-tmpfiles `uninitialized /etc/ detected` line.
- MATE: caja exits and is respawned by mate-session several times in the first ~30-40 s (systemd1 activation fails), then stays up and draws the desktop.

## Firefox

- `firefox` 156.0.1 from the distro's own repo (Mozilla's own repo serves .deb/.rpm only; these distros package Firefox themselves, no snap involved).
- Under proot the content sandbox fails: pages render with `[GFX1]: no fonts` and `--headless --screenshot` writes nothing. With `MOZ_DISABLE_CONTENT_SANDBOX=1` it renders (`docs/screens/arch-firefox.png`, headless, example.com). `guest/xstartup` and `guest/profile.sh` now export it.
