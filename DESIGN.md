# Andronix distros: installer design

Status: phase 2, Sept 2026. The installer is now a Go binary (`cmd/andronix`) built on the Charm stack. The bash prototype from phase 1 (`andronix`, `lib/`) still works for Debian and stays as a reference.

## 1. Goals

- Replace ~100 copied installers in `AndronixOrigin/Installer/` with one shared CLI, `andronix`.
- Feel like a product: branded colours, one clear step list, smooth progress bars, friendly errors with a fix.
- Keep today's contract:
  - the Firestore install commands
  - `start-<distro>.sh`
  - `vncserver-start`/`vncserver-stop`
  - `/sdcard` and `<distro>-binds/`
  - `PULSE_SERVER=127.0.0.1`
- Run on any Termux, old or new. The phone needs `proot` (and `curl` for the bootstrap). There's no Python, no Node and no proot-distro.
- **Free distros stay minimal:** the base distro, the chosen desktop, VNC, a browser, a normal user and the Andronix wallpaper. Themes and app bundles belong to Modded (section 8).

## 2. Where the rootfs comes from

| Option | Old Termux | Rate limits at 2.5M users | Hosting cost | Control |
|---|---|---|---|---|
| A. Pull from Docker Hub on the phone | Works | Bad. Anonymous pulls are limited per IP, and phone carriers put many users behind one CGNAT IP. | Free, but not ours | Upstream can change tags under us |
| B. Our own tarballs, built in CI from the same images | Works: one download | None on GitHub Releases, or on R2 behind Cloudflare | Low: about 8 distros x 4 CPUs x 30-60 MB; R2 has no egress fees | Full: pinned, checksummed, patchable |
| C. Require proot-distro | Fails: needs Python and a current Termux | Same as A, unless images are on ghcr | Free | Their UI and CLI, GPLv3, a second tool to support |

**Decided: B on Cloudflare R2, with A as an automatic fallback.**

- **CI build.** `ci/build-rootfs.sh` exports each image per CPU, cleans it, gates it with `ci/check-image.sh`, and publishes it to the R2 bucket. Layout:
  ```
  <mirror>/rootfs/<id>/<version>/<id>-<version>-<arch>.tar.xz (+ .sha256, .meta)
  <mirror>/bin/<version>/andronix-{android,linux}-<arch>, andronix-<arch>, SHA256SUMS
  <mirror>/bin/latest/...                              (what get.sh downloads)
  ```
- **Mirror URL.** `<mirror>` is `https://dl.andronix.app`, served from the public R2 bucket `andronix-dl` (live since Sept 26). `ANDRONIX_MIRROR` overrides it in both the binary and `get.sh`.
- **Paid images never go to the public bucket.** Modded images are served separately, behind the Andronix API. `ci/publish-r2.sh` is the only upload path to `andronix-dl`:
  - The bucket name is hard-coded, so it can't be pointed elsewhere by mistake.
  - Every object key must match the free allowlist (`rootfs/<id>/<ver>/<id>-<ver>-<arch>.{tar.xz,.sha256,.meta}` and `bin/<ver|latest>/{andronix-[android-|linux-]<arch>,SHA256SUMS}`).
  - Any key containing `modded`, `paid` or `premium`, and any distro conf marked `DISTRO_MODDED`/`DISTRO_PAID`, stops the whole upload before anything is sent.
  - `tests/publish-guard.sh` tests the guard.
- **Publishing.** The workflow drafts are `.github/workflows/rootfs.yml` (weekly, plus on demand) and `binaries.yml` (on `v*` tags).
  - They upload with the S3 API using an R2 token scoped to "Object Read & Write" on this one bucket, stored as GitHub secrets.
  - Tarballs go up first and checksums last, so a client never sees a checksum for a file that isn't there yet.
  - The bucket and domain are live. The scoped CI token is still a maintainer task.
- **Fallback.** If our tarball is missing, the binary pulls the OCI image straight from the registry named in the distro config (`internal/oci`).
  - It handles bearer tokens and picks the right CPU from the index.
  - It checks every layer's digest and applies whiteouts.
  - A `429` gets its own "network hit the limit" error.
- **Local override.** `ANDRONIX_ROOTFS=/path/file.tar.xz` installs from a local file (tests, offline users).

## 3. One shared installer

```
andronix install <distro> [--de xfce|lxqt|mate|kde|lxde|none] [--yes] [--no-browser] [--no-start] [--reinstall] [--plain]
andronix start   <distro> [--root] [-- command...]
andronix desktop [distro] | desktop stop   (Termux:X11; also start <distro> --x11)
andronix remove  <distro> [--yes] [--legacy]
andronix list | backup <distro> [file] | restore <file> | version | help
# inside a distro (the same binary is copied to /usr/local/bin/andronix):
andronix vnc start [WxH] [display]   (vncserver-start)
andronix vnc stop [display]          (vncserver-stop)
andronix setup-user                  (first-boot user setup)
```

**Two builds per CPU** (`ci/build-go.sh`). The builds follow the SIGSYS decision: Option A for all four CPUs.
- `andronix-android-<cpu>` (GOOS=android, PIE) runs in Termux.
  - Go's android runtime and standard library avoid syscalls that Android 8-11's seccomp answers with SIGSYS.
  - Play Store Termux runs only PIE executables, through `/system/bin/linker64`.
  - aarch64 builds without cgo. arm, i686 and x86_64 use cgo through the NDK (API 24), from `ANDROID_NDK_HOME` or the build container `ci/build.Dockerfile`.
- `andronix-linux-<cpu>` (static GOOS=linux) is the copy inside distros: vnc, setup-user, the bwrap stand-in, packs.
  - Each android build embeds its CPU's linux build, gzipped (`selfbin/`). Builds without one download it from the mirror and check its sha256.
- `andronix-<cpu>` is the android build under its old name.
- **Exec helpers:** every exec goes through `sys.Command` or `sys.Exec`.
  - The command lookup uses `stat` instead of faccessat2.
  - When andronix was itself started through the linker (Play Store Termux, where W^X forbids execve in app data), they exec through the linker the way termux-exec does. This covers scripts via their shebang.
  - `sys.FixLinkerArgs` drops the program path that the linker leaves in `os.Args[1]`.
- **File copies** use `sys.Copy`, which uses plain reads and writes instead of copy_file_range.

**Downloads resolve through products-api first** (2.0.1):
- The request is `GET /v1/installer/resolve?item=bin|rootfs&arch&flavor&distro&ver&v[&t=1]`, answered with `{url, sha256, size, alt[], version}`. The API can pin, roll back or move a download without an installer release.
- A 3 s timeout, a 400/404/503, or an answer without a 64-hex sha256 or an https URL falls back to the built-in dl.andronix.app URL.
- The sha256 is always checked. A mirror tarball without its `.sha256` isn't used; the registry is used instead.
- `t=1` is sent only with telemetry on.
- `ANDRONIX_MIRROR` and `ANDRONIX_NO_RESOLVE=1` skip the resolver.
- It covers rootfs downloads, self-update, the in-distro linux copy and get.sh's binary download. Modded downloads keep products-api's signed download.

**Dev packs.** `andronix pack list|add|remove [distro] <pack>...` adds toolsets (python, node, java, go, db) from `mods/packs/*.conf`, which are embedded like the other confs. It works from Termux (as root through proot) or inside a distro (`sudo andronix pack ...`). See `docs/packs.md`.

**Supported combos are data.** A desktop is offered on every distro whose package family has a list in the desktop's conf. `DE_DISTROS="…"` narrows that to named distros; KDE uses `ubuntu debian kali`. The picker lists only the desktops a distro offers. An unsupported `--de` fails with a message that names the distros offering it.

**Window managers are dropped.** i3, Awesome and Openbox are not offered in this release. Old app versions still show WM buttons, so their Firestore `UnModdedOS.*.Install.WM.*` links must keep working after launch. There are two options, and the user decides later:

- (a) Leave the old AndronixOrigin WM scripts in place.
- (b) Point the WM links at the new installer with `--de xfce` and a one-line notice. `andronix install` already maps `--de i3|awesome|openbox` to XFCE with a warning.

### Code layout (Go)

| Package | Job |
|---|---|
| `internal/ui` | Charm UI, described in the next table. |
| `internal/conf` | Reads `distros/*.conf` and `desktops/*.conf`. Optional keys: `DISTRO_MIRROR_REWRITE="<from> <to>"` (a literal replace in the package sources; Kali uses it to reach `kali.download`), `DISTRO_BROWSER_REPO`, `DISTRO_BROWSER_ARCHES`, `DISTRO_NO_SNAP`, and `DE_AUTOSTART_HIDE` (autostart entries that fail under proot, hidden for every user). From port-b:
  - `DISTRO_UPSTREAM_TARBALL_<arch>` + `DISTRO_UPSTREAM_REMOVE`: Arch Linux ARM has no OCI image. CI imports the upstream tarball, strips kernel and firmware packages, and cleans it. The installer's last-resort fallback downloads it directly and runs the same strip and clean (`rootfs.CleanScript`).
  - `DISTRO_MIRROR_<arch>` (Manjaro's pacman mirror) and `DISTRO_LANG`.
  - `DE_DESKTOP_EDITS`: patched `.desktop` copies in `/usr/local/share/applications`. MATE uses it to run `caja --force-desktop` in the Panel phase; otherwise the desktop is black under proot.
  - The xbps family installs an embedded `fchmodat2` preload shim (`guest/preload`), because proot doesn't translate that syscall on Linux 6.6+.
  - Added on master: `DISTRO_SUDO=nopasswd|none` (Void, Manjaro), and `DISTRO_MAX_KERNEL`, a generic guard that refuses a distro on newer kernels (currently unused). | These are plain `KEY=value` data files embedded into the binary; they're the stable interface shared with the bash prototype and the porting agents. |
| `internal/pkgmgr` | One `Family` per package manager (apt, dnf, pacman, apk, xbps). apt installs `ca-certificates` right after the first update. dnf runs `PostInstall` (restoring sudo's setuid bit, which rpm loses under proot) and counts `[n/N]` lines in two phases. Each family has: update/upgrade/install commands, a simulate command to size the progress bar, progress regexes, VNC packages, admin group, add-user command, and host-side fixups (apt sandbox user, pacman `DisableSandbox`, the no-snap pin, the Mozilla repo). |
| `internal/rootfs` | A tar/gz/xz/zstd extractor that resolves every path inside the rootfs and never follows a symlink out of it, copies hard links when Android refuses them, and applies OCI whiteouts. Also rootfs fixups and the generated wallpaper. |
| `internal/proot` | Builds the proot command line (section 6). This includes the `memfd_noexec=2` stand-in that apk 3 needs to run its package scripts under proot, logs in as the first-boot user, and streams command output. |
| `internal/netx`, `internal/oci`, `internal/sys` | Resumable downloads, the registry client, CPU detection and the Android quirks. |
| `internal/app` | The commands above. |

The UI pieces:

| Part | What it does |
|---|---|
| lipgloss | The theme: brand colours with truecolor, 256 and 16-colour fallbacks, and `NO_COLOR`; the wordmark; boxes that fit 40 columns. |
| bubbletea step list | Finished steps print above. Only the running step redraws: spinner, a `#ff8b25` to `#ffbc57` gradient bar, and the last 3 lines of apt output. |
| huh | Pickers, confirmations and the user setup. |
| charmbracelet/log | The install log in `~/.andronix/logs/`. |
| Plain mode | One stable line per event (`run:`, `ok:`, `skip:`, `fail:`, `error:`, `fix:`). It's used automatically when stdout isn't a terminal, and with `--plain`. |

### Install steps

Each step ends with ✓, – (skipped) or ✗, plus its time:

1. Check the phone.
2. Pick a source.
3. Download.
4. Unpack.
5. Set up the rootfs.
6. Refresh package lists.
7. Update.
8. Install the desktop and VNC.
9. Install the browser. This step can't fail the install; if it fails, the step is skipped with a hint.
10. Set up the desktop (VNC files, wallpaper).
11. Write `start-<distro>.sh`.

Then, on a terminal, `setup-user` creates a normal user with sudo and the VNC password. Without a terminal, it runs at first interactive login instead. The ready box says the user is being taken into the distro now, and how to start it next time.

### How the app reaches it (no app release needed)

- **Shims.** The Firestore `UnModdedOS.<Distro>.Install.*` commands keep their shape. Each `Installer/<Distro>/<distro>-<de>.sh` in AndronixOrigin becomes a small shim (`compat/`).
  - The shim fetches `get.sh`, a POSIX sh stub.
  - `get.sh` detects the CPU and the OS, downloads `andronix-android-<cpu>` in Termux (`andronix-linux-<cpu>` elsewhere), checks it against `SHA256SUMS`, installs it to `$PREFIX/bin`, and runs `andronix install <distro> --de <de>`.
- **Key map:**

  | App key | Installs |
  |---|---|
  | Ubuntu22 | `ubuntu` (26.04 LTS), writing `start-ubuntu26.sh` and `start-ubuntu22.sh` (decided) |
  | Ubuntu18, Ubuntu20 | `ubuntu24` (24.04 LTS), writing `start-ubuntu.sh` and `start-ubuntu20.sh`, the names the docs use (decided) |
  | Debian | `debian` (13) |
  | Other distros | Their lower-case name |
  | DE `Node` | `--de none` |
  | Enlightenment and the WMs | XFCE, with a notice |

- **Uninstall.** `UnModdedOS.<Distro>.Uninstall` should run `andronix remove <distro> --yes` when `andronix` exists. The old `UNI-*.sh` deletes `start-debian.sh` but not `~/.andronix/distros/debian`.

## 4. Backward compatibility

- **Old installs stay untouched.** `~/debian-fs` and the other `~/<distro>-fs` folders are left alone, and `andronix list` shows them as "old installer".
- **Start scripts keep their names:** `start-debian.sh`, `start-ubuntu.sh` and `start-ubuntu20.sh` (24.04), `start-ubuntu22.sh`/`start-ubuntu26.sh` (26.04), `start-kali.sh`, `start-fedora.sh`, `start-arch.sh`, `start-manjaro.sh`, `start-alpine.sh` and `start-void.sh`.
  - Each is a small wrapper, `exec andronix start <distro> "$@"`, tagged `ANDRONIX-LAUNCHER`.
  - `./start-debian.sh cmd` still runs `cmd` inside.
- **An old launcher with the same name** is renamed to `start-<distro>-old.sh`. It uses relative paths, so it still boots the old rootfs.
- **`<distro>-binds/`** snippets (`command+=" -b ..."`) are still honoured. `/sdcard` is bound by default.
- **`vncserver-start` and `vncserver-stop`** keep their names and display `:1` (port 5901).
  - Debian's `vncserver` wrapper is used where it exists (Debian, Ubuntu, Kali, Fedora).
  - Elsewhere, `Xvnc` and the session start directly. Arch, Manjaro and Alpine ship upstream's wrapper, and Void has none.
  - `xstartup` prefers `dbus-run-session` and sets `MOZ_DISABLE_CONTENT_SANDBOX=1`; without it, Firefox renders pages with no text under proot.
  - A lock file only counts as a running desktop if its pid is alive and is actually a VNC server.
  - VNC now listens on localhost only by default. `vncserver-start --lan` is the explicit opt-in for other devices; old builds used `-localhost no`.
  - Pressing Enter picks Auto size.
  - `vncserver-start 1920x1080` doesn't prompt.
  - `ANDRONIX_VNC_PASSWORD` sets the password for scripted runs.
  - `~/.vnc` is a symlink to `~/.config/tigervnc`, where TigerVNC 1.15 keeps its files. TigerVNC's own migration fails under proot, so the symlink sidesteps it.
- **Audio:** `PULSE_SERVER=127.0.0.1` is exported inside the distro. Every `andronix start` (and `andronix desktop`) makes sure Termux's PulseAudio runs (`internal/termux`), with no user steps:
  - installs `pulseaudio` with `pkg` if it's missing, but only on an interactive start or `andronix desktop`, shown as a step. Command runs (`./start-debian.sh cmd`, scripts) never install packages: on an out-of-date Termux that means an `apt full-upgrade` first, and two starts at once would fight over apt. A failed install is retried after 24 h; package runs take `~/.andronix/pkg.lock` and time out after 20 minutes;
  - if `pkg` itself is broken by a half-upgraded Termux (a new libcurl on an old OpenSSL), runs `apt update && apt full-upgrade` once with `--force-confdef --force-confold`, as `pkg` advises, and retries;
  - starts it with `--exit-idle-time=-1`, without `--load`, so a TCP line in the user's `default.pa` (older Andronix docs) can't stop the daemon;
  - then checks `pactl list short modules` and loads `module-native-protocol-tcp listen=127.0.0.1 auth-ip-acl=127.0.0.1 auth-anonymous=1` if no safe one is loaded. `listen=127.0.0.1` matters: with `auth-anonymous=1` the IP list doesn't restrict anything, so an old module on every interface (anyone on the Wi-Fi could play or record) is swapped for the loopback one;
  - logs each action to `~/.andronix/logs/termux.log`; a change is one line on stderr, so a command's stdout stays clean.
- **Termux:X11** (`andronix desktop [distro]`, `andronix start <distro> --x11`, `./start-<distro>.sh --x11`, `andronix desktop stop`): the desktop in the Termux:X11 app instead of VNC.
  - Installs `x11-repo` and `termux-x11-nightly` when missing. Checks the companion app with `pm list packages com.termux.x11`, and also recognises the loader's own errors (app not found, signature mismatch, Android < 7). A missing app gets a branded box with the nightly download link.
  - Starts `termux-x11 :0 -ac` detached (`ANDRONIX_X11_ARGS` adds options such as `-legacy-drawing`), waits for `$PREFIX/tmp/.X11-unix/X0`, and opens the app with `am start`.
  - The session is the same `xstartup` as VNC (dbus-run-session, `andronix session-prep`, the light profile, the bwrap stand-in), run as the first-boot user with `DISPLAY=:0`. Display `:0` never clashes with VNC's `:1`.
  - Only the one socket is bound into proot (`$PREFIX/tmp/.X11-unix/X0` to `/tmp/.X11-unix/X0`), and on every start while the server runs, so a second `./start-<distro>.sh` can open apps on `:0`. VNC's sockets stay in the distro's `/tmp`.
  - `andronix desktop` stays in the foreground. Logging out, Ctrl-C or `andronix desktop stop` (from another session) ends it; the server is stopped only if this run started it (`stop` always stops it, plus the app's `ACTION_STOP`). State is in `~/.andronix/x11.state`, the server log in `~/.andronix/logs/termux-x11.log`.

## 5. On-disk layout

```
$PREFIX/bin/andronix                     # the binary (8 MB, static, one per CPU)
~/.andronix/                             # data ($ANDRONIX_HOME)
  distros/<id>/rootfs/                   # the distro; /usr/local/bin/andronix is a copy of the binary
  distros/<id>/install.conf              # DISTRO, VERSION, ARCH, DE, USER, SOURCE, STAGE
  distros/<id>/sysdata/                  # fake /proc files, bound only when the real ones are unreadable
  distros/<id>/shm/                      # bound to /dev/shm
  cache/, logs/
~/start-<distro>.sh, ~/<distro>-binds/   # compat
```

- **Relocatable rootfs, with one catch.** The Go extractor copies hard links instead of using `proot --link2symlink tar`, so a freshly unpacked rootfs has no absolute paths. But later package runs under `proot --link2symlink` create `.l2s.*` files plus symlinks that hold this phone's host path (for example after `apt install perl`). That's handled in three places:
  - **Backup is portable.** Each `.l2s` group is written as one file plus tar hard links, other host-path symlinks become guest paths, and `.l2s` files are left out. Restore unpacks the hard links as copies where Android refuses links.
  - **Restore repairs old backups.** Host-path links (from another phone or an old location) are pointed at the new rootfs (`rootfs.RepairHostLinks`).
  - **Exports.** `ci/clean-rootfs.sh` also undoes link2symlink: `.l2s` symlinks become hard links again, host paths become guest paths, and the leftovers are removed. `ci/check-image.sh` fails on host-path symlinks or `.l2s` files.
  - **Tests.** Smoke checks install `perl`, back up, restore to another `ANDRONIX_HOME`, and run `perlbug --version` and `dpkg --verify perl`. `tests/check-image-test.sh` covers the clean step.
- **Resume.** `STAGE` in `install.conf` lets a killed install pick up where it stopped.

## 6. Launch (proot)

- **proot flags.** These follow proot-distro's approach, reimplemented rather than copied. Each flag is used only if the local `proot --help` lists it:
  - `--kill-on-exit`
  - `--link2symlink`
  - `--sysvipc`
  - `-L`
  - `--kernel-release`
- **Identity.** `-0` for root. For the first-boot user it's `--change-id=uid:gid` with that user's home.
- **Sudo.** Under proot, a process's supplementary groups are Android's, so sudo never sees the admin group. `sudoers.d/andronix` therefore names the user as well as the group.
- **Binds:**
  - `/dev`, `/proc` and `/sys`, plus `/dev/urandom:/dev/random`.
  - `/proc/self/fd` for `/dev/fd` and the std streams, when they're missing.
  - `shm/` at `/dev/shm`.
  - Fake `/proc` entries, but only when the real ones are unreadable.
  - `/sdcard`, the legacy binds and `ANDRONIX_BINDS`.
- **Environment.** A clean `env -i` with `HOME`, `USER`, `PATH`, `TERM`, `LANG=C.UTF-8`, `PULSE_SERVER` and `ANDRONIX_DISTRO`.
- **No sandboxes inside the distro.** bubblewrap needs unprivileged user namespaces, which Android (and proot) don't give apps. glycin (GdkPixbuf's newer image loading) runs its image loaders through `bwrap`.
  - **The stand-in.** `/usr/local/lib/andronix/bwrap` is a small POSIX sh script. It's linked from `/usr/local/bin/bwrap`, and the distro's own `/usr/bin/bwrap` is replaced by a link to it; the original is kept as `bwrap.andronix-real`.
    - It answers glycin's probe (`--unshare-all ... /usr/bin/true`) the way bwrap does on a system without user namespaces.
    - glycin then runs its loaders unsandboxed.
    - Any other caller gets its command run directly.
  - **Why sh, not Go.** glycin 3 (Fedora 44) caps sandboxed loaders with `RLIMIT_AS`, set to 80% of (MemAvailable + SwapFree − 200 MB). That's too little on a phone, and it also applies to the probe. A Go stand-in can't even start under that cap, so the probe failed without a known error, glycin assumed the sandbox worked, and every image load failed: a black desktop on Fedora.
  - **Self-healing.** `/usr/bin/bwrap` is re-pointed at install, after every package step, and on every `andronix start`, in case a package update restores the real binary.
  - **Log.** Calls are recorded in `/tmp/andronix-bwrap.log`.
- **Android specifics for a static Go binary.** `/etc/resolv.conf` is missing, so DNS falls back to `$PREFIX/etc/resolv.conf` or 8.8.8.8/1.1.1.1. TLS roots come from `/system/etc/security/cacerts`, which Go already reads.

## 7. Risks

- **Phantom process killer.** Android 12+ can kill long installs. `termux-wake-lock` is taken during installs; the UI warns that Android may ask to let Termux run in the background, and the error hint links the docs page.
- **Old Termux (Play Store build).** Its package repos are dead, so the Firestore `pkg install` prefix can fail before we even run. The binary itself needs nothing from `pkg` except proot.
- **Big desktops.** KDE needs about 4 GB of RAM; desktop configs carry `DE_MIN_RAM_MB`.

## 8. Modded editions (installer side)

Modded editions are paid images of the free distros with the Andronix look. They are built and hosted separately; this repository only has the installer's side:

- `editions.conf` lists the edition ids with their distro and desktop. `early` marks early-access editions.
- **Install command:** `andronix install --edition <id> --token '<token>'`. The Andronix app gives the command, and the token proves the purchase; single-quote it, because it contains `&`.
- **Download and checks:** the installer downloads `/v1/modded/download/<file>?<token>` from the Andronix API. It checks the file against `<file>.sha256` from the same place, then runs the normal install steps. The package steps are skipped because the desktop is already in the image.
- **Old app commands:** the old `-v 2 -h -k -e -p` form is translated to `--edition`.
- **Refused downloads** get clear messages: an expired link, or early access.

## 9. Image hygiene (every free and modded image)

New images are clean by construction; old images are left as they are.

| Rule | How it's enforced |
|---|---|
| No browser profiles, caches, history or cookies in `/etc/skel`, `/root` or `/home` | CI clean step. The installer only ever writes `xstartup` and a `.vnc` symlink there. |
| No preset passwords: root locked, no `~/.vnc/passwd` | CI locks root (`passwd -l`). The user sets their own password and the VNC password at first boot (`setup-user`). Root stays locked and admin work goes through sudo. |
| Empty `/etc/machine-id`, no SSH host keys | CI empties and removes them. The installer writes a fresh random machine-id per install, and runs `ssh-keygen -A` if an SSH server is present. |
| VNC on localhost only by default | `vncserver-start` passes `-localhost yes`. `--lan` is the opt-in, and the ready box says who can reach the desktop. |
| No build leftovers: shell history, package caches, `/tmp`, logs, builder or upstream default users | CI clean step, which also deletes Ubuntu's `ubuntu` user and `/home/*`. |

**Exported installs.** A rootfs made by the installer (for example, the base of a modded build) is cleaned with `ci/clean-rootfs.sh <dir>` before export. That script is the shared, host-side version of the CI clean step. It also removes what an install adds per phone:
- the `aid_*` Android ids;
- the first-boot user and their home;
- the user's sudoers file and `/etc/andronix/user`.

The gate fails on any `aid_*` entry, so this can't be skipped. `tests/check-image-test.sh` covers these cases:
- Fedora's mode-0000 `/etc/shadow`, which is read straight from the tarball;
- Void with no shadow and `x` in passwd (locked);
- an empty or real root password (fail);
- an installed rootfs before and after `clean-rootfs.sh`.

**The gate is `ci/check-image.sh <tarball|dir>`.** It checks all five rules on the file list, without unpacking or needing root, and exits 1 on any hit. `ci/build-rootfs.sh` runs it after each build and refuses to publish a dirty image. The modded pipeline must run it too.

## 10. Updates (`andronix update [<distro>]`)

1. **The andronix binary.** The command reads `<mirror>/bin/latest/SHA256SUMS`.
   - If the checksum for this CPU's build of the same kind (`andronix-android-<arch>`, falling back to the old name `andronix-<arch>`) differs from the running binary, it downloads it, verifies it, and renames it over itself.
   - It then refreshes the copy inside each distro.
   - Development builds skip this step (`ANDRONIX_SELF_UPDATE=1` forces it). `--self` updates only the binary; `--no-self` skips it.
2. **Packages.** For each installed distro, or the one named: the family's update and upgrade, with the same progress UI as install. Then a tidy step re-points `/usr/bin/bwrap` and rewrites `xstartup`.
3. **Modded editions: delta updates.** A Modded install carries `/etc/andronix-modded` with `EDITION_VERSION`.
   - **Manifest.** Each edition has a manifest per distro, desktop and CPU:
     ```json
     {"edition":"modded","distro":"ubuntu","de":"xfce","arch":"aarch64","latest":"2026.11",
      "deltas":[{"from":"2026.10","to":"2026.11","file":"ubuntu-26.04-xfce-modded-aarch64-2026.10-2026.11.tar.xz",
                 "sha256":"…","size":123}]}
     ```
   - **Delta files.** A delta is a tarball of added and changed files, with OCI whiteouts (`.wh.<name>`) for removed ones. The installer applies it with the same extractor as image layers, then bumps `EDITION_VERSION`. Deltas chain from the installed version to `latest`; if there's no path, the user is told to reinstall.
   - **Server side (design only; no server or R2 changes made).** CI builds consecutive edition versions and diffs the two trees into a delta tarball with whiteouts. The Andronix API serves the manifest and signs each file, exactly like full images: `/v1/modded/download/<file>?k&e&h`. The app hands the user `andronix update <d> --manifest <signed manifest URL> --token <t>`.
   - **Client today.** It works against a local manifest (`--manifest FILE`, files next to it) or a URL plus `--token`. Tested: a local manifest took 2026.10 to 2026.11, adding a file and removing one through a whiteout.

## 11. Backups to phone storage

- **Writing.** `andronix backup <d> --to storage` writes `~/storage/shared/Andronix/backups/andronix-<d>-<date>.tar.gz`, running `termux-setup-storage` first if Termux has no storage access. Those files survive uninstalling Termux and are visible in the phone's file manager. `ANDRONIX_STORAGE` overrides the folder.
- **Restoring.** `andronix restore` with no file lists the backups there and in `~`, newest first. On a terminal it offers a picker.
- **Portable.** Backups are portable (section 5): no `.l2s` files or host paths, so they restore on another phone.
- **Cloud backup (design only).**
  - Upload: the same tarball goes up through products-api to a per-user prefix in a private bucket, using a signed upload URL. Uploads are resumable and use multipart for large files.
  - Restore: the file comes back through a signed download.
  - Encryption: the tarball is encrypted on the phone before upload with a key derived from a user passphrase (age or XChaCha20-Poly1305), so the server never sees plaintext.
  - Quota and retention are enforced by products-api.

## 12. Performance profiles (`andronix tune <d> --profile light|balanced`)

- **Data-driven.** Each desktop conf carries its light settings:
  - `DE_LIGHT_AUTOSTART_HIDE`: extra autostart entries to hide (accessibility bus, tracker, power manager and so on).
  - `DE_LIGHT_XDG_CONFIG`: KDE and LXQt INI settings (animations, previews, thumbnails).
  - `DE_LIGHT_XFCONF`: XFCE settings (compositor off, thumbnails off). These are merged into a copy of the channel's existing system defaults, so a Modded edition's look survives.
  - `DE_LIGHT_GSETTINGS`: MATE (Marco compositing, Caja thumbnails, animations). Applied, or reset under balanced, by `andronix session-prep` at session start on the session's D-Bus.
- **Layered, not edited.** Light writes only under `/etc/xdg/andronix-light`, and `xstartup` puts that folder first in `XDG_CONFIG_DIRS`. Nothing the distro or Modded edition ships is modified, and balanced just removes the folder.
- **VNC.** Light also means 16-bit colour, and 1280x720 as the default screen size.
- **Automatic.** A desktop install on a phone with less than 3 GB of RAM starts in light. `andronix tune <d>` shows the current profile and the RAM.

## 13. Adaptive compatibility: probe the phone, fixes as data (design, 2.0.1+)

Today's fixes are scattered version checks:
- `proot.OldKernel` (below 4.8: `PROOT_NO_SECCOMP=1` and a desktop warning);
- `DISTRO_MAX_KERNEL` with `kernelNewer`;
- `Family.FchmodatShim`;
- the Kali systemd swap.

They guess from version numbers. The replacement **measures what this phone does** and applies **fixes listed as data** in the binary, so a fix is a one-line change.

### Probe (measure, cached)

- **When:** at the first install on a phone, after the rootfs is unpacked and before any package step. It runs again when the cache key changes, and `andronix doctor --probe` forces it.
- **Cache key:** kernel release + Android SDK + Termux version + probe version. The result is kept in `~/.andronix/probe.json`.
- **Inside proot:** one proot run of the in-distro binary, `andronix __probe`. Each syscall runs in its own child process, so a seccomp kill (SIGSYS) only loses that child and is recorded as `SIGSYS`. The syscalls tested:
  - statx, openat2, faccessat2, fchmodat2, close_range;
  - open_tree, name_to_handle_at, memfd_create;
  - clone3, called with invalid arguments so it never forks;
  - seccomp (`GET_ACTION_AVAIL`).

  Each result is one of: `ok`, `ENOSYS`, `EPERM`, `EUNATCH`, `EINVAL`, `SIGSYS`, or another errno name. About 1–2 s in total.
- **Host side, without proot:**
  - kernel release;
  - SDK;
  - ROM family: `lineage`, `miui`/`hyperos`, `oneui`, `stock`, or `other`, taken from getprop. The raw fingerprint is never recorded;
  - the phantom-process setting (`/system/bin/settings get global settings_enable_monitor_phantom_procs` where Termux may read it, else inferred from the SDK: on for 31 and newer);
  - RAM and free space;
  - Termux version and source (`TERMUX_APK_RELEASE`: F-Droid, GitHub or Play).
- **Fixtures:** recorded probes live in `internal/compat/testdata/*.json`: the emulators a9, a11, a12, a16 and a17, plus the Redmi Note 7 Pro on kernel 4.14.

### Rules (fixes as data)

- **Where they live:** `compat.json`, embedded in the binary. Each rule has an `id`, a `when` and a `do`. A rule applies when every condition in `when` holds.
  - Conditions:
    - `kernel_lt` and `kernel_ge` (major.minor);
    - `sdk_lt` and `sdk_ge`;
    - `probe` (syscall to the results that match, e.g. `{"fchmodat2": ["ENOSYS", "EPERM", "SIGSYS"]}`);
    - `distro`, `family`, `de`, `termux`, `rom`;
    - `ram_mb_lt`, `phantom` (true/false).
  - Actions:
    - `env`: variables for proot, e.g. `PROOT_NO_SECCOMP=1`;
    - `preload`: a shim from `guest/preload/` (fchmodat2, later faccessat2);
    - `pre_script`: shell run as root after the refresh, before any package step (e.g. the machine-id and systemctl shim, the Kali systemd swap);
    - `apt_pin` and `hold`: package preferences;
    - `skip_optional`: heavy optional packages;
    - `warn`: a message shown before the install, such as the phantom-process advice or the old-kernel desktop warning;
    - `refuse`: a friendly stop, which replaces `DISTRO_MAX_KERNEL`.
- **Examples:**
  ```json
  {"id": "old-syscall-order", "when": {"kernel_lt": "4.8"}, "do": {"env": {"PROOT_NO_SECCOMP": "1"}, "warn": "old_kernel_desktop"}}
  {"id": "fchmodat2-shim", "when": {"probe": {"fchmodat2": ["ENOSYS", "EPERM", "SIGSYS"]}, "family": ["xbps"]}, "do": {"preload": "fchmodat"}}
  {"id": "systemd-eunatch", "when": {"probe": {"open_tree": ["EUNATCH"]}, "family": ["apt"]}, "do": {"pre_script": "systemd-standalone"}}
  ```
- **Named scripts:** `pre_script`, `warn`, `refuse` and `preload` refer to names shipped in the binary (`internal/compat/scripts/*.sh`, the message catalogue, `guest/preload`). The table is checked on load, and a test fails on an unknown name.
- **The Kali systemd fix:** stays unconditional in `kali.conf` for 2.0.1, since it's cheap and safe. Once the Redmi fixture shows which syscall fails, it can become a probe rule.
- **Reporting:** `andronix doctor` prints the probe and the rules that apply. The applied rule ids are saved in `install.conf` (`COMPAT=`).

### Updates

- **Shipping:** the table ships only in the binary. A new or changed rule reaches users with the next installer release: `get.sh` fetches `bin/latest`, and `andronix update` updates the installer itself.
- **Nothing from the server:** there is no server-side table and no signing (dropped by the owner, Sept 26).

### Telemetry (existing opt-out)

- **A `compat` event after each probe:** the probe as a compact string (e.g. `openat2:ENOSYS,clone3:SIGSYS`), kernel major.minor, SDK, ROM family, Termux source, a RAM bucket, and the rule ids applied.
- **`install_result`** also gets `compat_rules` and the failing step, which it has already.
- **Never sent:** the full kernel string, build fingerprint, device model, or anything that identifies a person.

### Tests

- **Unit:** the rule engine run over each fixture, with the expected rule ids (for example, the Redmi 4.14 fixture gets `systemd-eunatch` and `old-syscall-order` doesn't apply).
- **Table checks:**
  - the embedded table parses;
  - every `pre_script` and `warn` name exists;
- **Emulator matrix:** `andronix doctor --probe` on a9, a11, a16 and a17 (app context), saved as fixtures.
- **Redmi:** the lead's device agent records its probe.

### Migration

Rules take over the existing checks:
- `proot.OldKernel` and `oldSyscallOrder` become `old-syscall-order`;
- `DISTRO_MAX_KERNEL` becomes `refuse` rules;
- `Family.FchmodatShim` becomes `fchmodat2-shim`.

The old code stays as a fallback when the probe can't run (no rootfs yet, or the probe failed), so behaviour never gets worse than 2.0.0.
