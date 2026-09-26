# Void Linux (xbps) — port notes

Tested by port-b on 2026-09-25 in the Termux-like Docker box (see `arch.md` for the setup), aarch64, through `families/xbps.sh`.

## Rootfs

- `ghcr.io/void-linux/void-glibc:latest`; platforms amd64, 386, arm64, arm/v7, arm/v6. ghcr pulls need an anonymous bearer token (the registry fallback's ghcr token flow is untested).
- aarch64 export: 16 MB `.tar.xz`, 68 MB unpacked. No bash (busybox-free, `/bin/sh` is dash); bash comes from `DISTRO_BASE_PKGS`.
- `/etc/xbps.d/noextract*.conf` in the image skips man/info pages and python caches; keep it (saves space), but installs log a `won't be extracted, it matches a noextract pattern` line per file.

## Linux 6.6+ hosts: `fchmodat2` under proot (fixed)

Most Android 15/16 phones and the Android 16 emulator run 6.6; the colima VM used for all tests runs **6.8.0-117**.

### The problem
- glibc 2.39+ implements `lchmod()` / `fchmodat(..., AT_SYMLINK_NOFOLLOW)` with the `fchmodat2` syscall (452, Linux 6.6+), falling back to the old `O_PATH` + `/proc/self/fd` path only on `ENOSYS`. Void has glibc 2.41.
- termux/proot doesn't know `fchmodat2` (latest tag `v5.1.107.95`, 2026-09-24, and master `d4d2a19`; Termux's `proot` package is 5.1.107.95). The call reaches the kernel with the **untranslated** guest path and fails with `ENOENT`.
- libarchive lchmods every symlink it extracts (`ARCHIVE_EXTRACT_PERM`). pacman only warns (`Can't set permissions to 0777`); **xbps aborts**:
  ```
  ERROR: openssl-3.6.4_1: [unpack] failed to extract file `./etc/ssl/misc/tsget': No such file or directory
  ERROR: Transaction failed! see above for errors.
  ```
  Reproduced again 2026-09-26 on the 6.8 VM with stock Termux proot and no workaround: the very first update (openssl) fails.
- **How the first Void PASS happened:** those runs (2026-09-25, marked "k<6.6" in the old INDEX) used a Docker seccomp profile that makes `fchmodat2` return `ENOSYS`, i.e. they emulated a pre-6.6 kernel; they did not prove Void works on 6.6+. They are replaced by the runs below.

### Upstream status
- proot-me/proot fixed it in PR #408 "syscall: support fchmodat2" (merged 2026-09-15, `ea1551f`; issue #387). Not in termux/proot yet.
- That upstream patch reads the flags from `SYSARG_3`, which is the **mode**; `fchmodat2(dirfd, path, mode, flags)` has them in argument 4. With a mode that has 0400 set it translates the path as "don't follow". Our port below uses `SYSARG_4`; worth a follow-up upstream.

### Fix 1 (shipped): preload shim in the Void rootfs
- `guest/preload/fchmodat.c` → `libandronix-fchmodat.so` (built by `ci/build-preload.sh` for aarch64, arm, i686, x86_64; 5–18 KB, glibc-linked, 16 KB page alignment on aarch64).
- Overrides `fchmodat` and `lchmod`: with `AT_SYMLINK_NOFOLLOW` on a symlink it returns `EOPNOTSUPP` (what Linux does for symlinks; libarchive ignores it), otherwise it calls the old 3-argument `fchmodat` syscall, which proot translates.
- Installed host-side at configure time: `/usr/local/lib/andronix/libandronix-fchmodat.so` + `/etc/ld.so.preload` (xbps `fam_configure`). Go side: copy the per-CPU `.so` and write `ld.so.preload` before the first `xbps-install`.
- Tested on the 6.8 VM, stock Termux proot 5.1.107.95: fresh Void + XFCE (273 packages), LXQt and MATE all PASS, zero `set permissions` warnings, shim mapped in every process. Screenshots in `docs/screens/void-*` are from this setup.
- Works with any proot version and needs nothing from the user. Only Void gets it for now; Arch (glibc 2.43) would lose its harmless pacman warnings with it too.

### Fix 2 (proposed): patch proot
- `docs/ports/proot-fchmodat2.patch` (against termux/proot `v5.1.107.95`/master): adds `PR_fchmodat2` to `sysnums.list`, all `sysnums-*.h` (452), the seccomp filter list, and translates its path in `enter.c` like `fchmodat`, honouring `AT_SYMLINK_NOFOLLOW` from argument 4.
- Tested: termux/proot `v5.1.107.95` + this patch, on the 6.8 VM, **without** the shim: Void + XFCE installs (273 packages), zero permission warnings. Stock proot on the same VM fails.
- Shipping options: (a) PR to termux/proot (smallest; users get it with `pkg upgrade proot`, but old Termux installs never update); (b) ship our own proot binary per CPU with the installer (full control, but we then own proot updates and Android quirks). Recommendation: (a) plus Fix 1 as a safety net, since the shim works on every proot.
- Android's app seccomp policy may already block `fchmodat2` on some versions (then glibc sees `ENOSYS` or the process gets `SIGSYS`); still to be checked on the emulator.

## Commands (inside the distro, `sh -c`)

```sh
xbps-install -S && xbps-install -yu xbps          # refresh; xbps must update itself first
xbps-install -un | grep -cE ' (install|update) '  # upgrade count
xbps-install -yu                                  # upgrade
xbps-install -n PKGS | grep -cE ' (install|update) '   # install count
xbps-install -y PKGS                              # install
xbps-remove -yO                                   # clean cache
```
- If xbps itself is outdated, every other transaction fails with "The 'xbps' package must be updated", and `-un` prints nothing — so refresh must include `xbps-install -yu xbps`, before counting upgrades.
- Dry-run lines: `nano-9.2_1 install aarch64 https://repo-default.voidlinux.org/current/aarch64 3128278 763281`.
- Mirror: `repository=https://repo-default.voidlinux.org/current/aarch64` from `/usr/share/xbps.d/00-repository-main.conf` (Fastly CDN). Fine as is.

## Progress output (stdout not a tty)

```
[*] Downloading packages
nano-9.2_1.aarch64.xbps: [745KB 0%] 186MB/s ETA: 00m00s
nano-9.2_1: verifying RSA signature...
[*] Unpacking packages
libcrypto3-3.6.3_1: updating to 3.6.4_1 ...
nano-9.2_1: unpacking ...
[*] Configuring unpacked packages
nano-9.2_1: configuring ...
nano-9.2_1: installed successfully.
```
Regex: `^[^ ]+: (verifying RSA signature|unpacking |configuring )`, **3 events per package**. Download progress lines (`ETA:`) are noisy in the log.

## Packages

- Base extras: `bash sudo nano wget ca-certificates tzdata procps-ng shadow`
- VNC: `tigervnc xauth`. Void's tigervnc has `Xvnc`, `vncpasswd`, `vncsession`, but **no `vncserver`** at all; `guest/vncserver-start` starts Xvnc directly (see `arch.md`). dbus: `dbus dbus-x11`.
- Browser: `firefox`.
- XFCE: **not** the `xfce4` meta, which pulls `xfce-polkit` (error dialog "XFCE PolicyKit Agent" at login: no system bus), `elogind`, `upower`, `xfce4-screensaver`, `parole`. Explicit list in `desktops/xfce.conf` → 270 packages, ~2.5 min.
- LXQt: the `lxqt` meta also pulls `elogind`, `upower`, `lxqt-policykit`, `lxqt-powermanagement`; explicit list used.

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
- Void has no `useradd`/`chpasswd`/`su` and no `/etc/shadow` in the image. `shadow` is now in `DISTRO_BASE_PKGS`; run `pwconv` once before `chpasswd`.
- Result: **partial**: `NAME ALL=(ALL:ALL) NOPASSWD: ALL` works; with a password rule sudo always says `Sorry, try again` (PAM `system-auth` path under proot; not solved).

## Quirks

- Version: rolling, so `DISTRO_VERSION="rolling"` (tarball `void-rolling-aarch64.tar.xz`).

## Firefox

- `firefox` 156.0 from the distro's own repo (Mozilla's own repo serves .deb/.rpm only; these distros package Firefox themselves, no snap involved).
- Under proot the content sandbox fails: pages render with `[GFX1]: no fonts` and `--headless --screenshot` writes nothing. With `MOZ_DISABLE_CONTENT_SANDBOX=1` it renders (`docs/screens/void-firefox.png`, headless, example.com). `guest/xstartup` and `guest/profile.sh` now export it.
