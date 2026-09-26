# Manjaro ARM (pacman) — port notes

Tested by port-b on 2026-09-25 in the Termux-like Docker box, aarch64. Manjaro uses the same pacman logic as Arch: read `arch.md` first. Only the differences are here.

## Rootfs

- `docker.io/manjarolinux/base:latest`; platforms arm64 and amd64. The arm64 image (built 2026-03-22) is 315 MB compressed, 1.35 GB unpacked; our `.tar.xz` export is 214 MB.
- The image ships an **initialised** keyring in `/etc/pacman.d/gnupg` (241 keys, plus `private-keys-v1.d`: the local master key, identical for everyone who uses the image, and stale `S.gpg-agent*` sockets). Delete the directory host-side before first use and run `pacman-key --init && pacman-key --populate` like on Arch (keyrings: `archlinux archlinuxarm manjaro manjaro-arm`).

## Repos are stale

Checked 2026-09-25 against `https://mirror.init7.net/manjaro/<branch>/`:

| Branch | pacman | glibc | firefox | tigervnc | xfce4-session | tzdata |
|---|---|---|---|---|---|---|
| arm-stable | 6.0.2 | 2.35 | 123.0 | 1.13.1 | 4.18.3 | 2024a |
| arm-testing | 7.0.0 | 2.41 | 141.0.3 | 1.15.0 | 4.20.3 | 2025b |
| arm-unstable | same as testing | | | | | |

- arm-stable has effectively been frozen since early 2024 (its `state` file says 2025-09-24, the packages are older). The x86_64 `stable` branch is current (2026-09).
- We ship arm-stable (what the image uses). It works, but users get Firefox 123 and no security updates.

## Mirrors

- The image's mirrorlist already uses the moved layout, `Server = https://<mirror>/manjaro/arm-stable/$repo/$arch`, but lists only five US mirrors.
- Replaced with the Manjaro CDN (CDN77, global): `Server = https://mirrors.manjaro.org/repo/arm-stable/$repo/$arch` (x86_64: `.../repo/stable/$repo/$arch`). New conf keys `DISTRO_MIRROR_<arch>`, single-quoted so `$repo`/`$arch` stay literal. Written to `/etc/pacman.d/mirrorlist` host-side.
- `pacman-mirrors` is not in the base image; we don't need it.

## pacman 6 differences

- `DisableSandbox` is unknown to pacman 6 and produces a warning on every command → don't write it (the installer checks `/var/lib/pacman/local/pacman-<ver>`). If a user later moves to arm-testing (pacman 7), they need to add it.
- Everything else (commands, `-Sp --print-format`, progress lines) behaves the same as on Arch.

## Packages

- Same names as Arch for base, VNC (`tigervnc xorg-xauth`), browser and all three desktops. tigervnc 1.13.1's `vncserver` is also upstream's display-only script; Xvnc is started directly.
- XFCE → 224 packages, ~1.5 min. Nothing to upgrade on a fresh arm-stable image.

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
- Result: **sudo fails** even with `-i`: sudo 1.9.15p5 checks `/etc/sudo.conf is owned by uid 1001, should be 0` and `/usr/sbin/sudo must be owned by uid 0 and have the setuid bit set`, also with NOPASSWD. `useradd`/`chpasswd` work. Options: root-only on Manjaro ARM stable, or wait for a newer sudo (arm-testing).

## Firefox

- `firefox` 123.0 from the distro's own repo (Mozilla's own repo serves .deb/.rpm only; these distros package Firefox themselves, no snap involved).
- Under proot the content sandbox fails: pages render with `[GFX1]: no fonts` and `--headless --screenshot` writes nothing. With `MOZ_DISABLE_CONTENT_SANDBOX=1` it renders (`docs/screens/manjaro-firefox.png`, headless, example.com). `guest/xstartup` and `guest/profile.sh` now export it.
