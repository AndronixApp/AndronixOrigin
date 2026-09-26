# andronix

`andronix` installs and runs Linux distributions on Android, inside [Termux](https://termux.dev), with no root. It's the installer behind the [Andronix](https://andronix.app) app.

```sh
curl -fsSL https://dl.andronix.app/get.sh -o $PREFIX/tmp/get.sh && sh $PREFIX/tmp/get.sh install debian --de xfce
```

It works with Termux from F-Droid, GitHub and Google Play; Play Termux needs Android 11 or newer. The installer adds proot itself if it's missing, and repairs a half-upgraded Termux. Every download is checked against its sha256.

## What you get

- **9 free distros:**

  | Distro | Version |
  |---|---|
  | Ubuntu | 26.04 LTS and 24.04 LTS |
  | Debian | 13 |
  | Kali | rolling |
  | Fedora | 44 |
  | Arch Linux ARM | |
  | Manjaro ARM | |
  | Alpine | 3.24 |
  | Void | |

- **Desktops:**
  - XFCE, LXQt and MATE.
  - KDE Plasma on Ubuntu, Debian and Kali; it needs 4 GB of RAM.
  - `--de none` for the command line only.
- **The desktop in the Termux:X11 app:** `andronix desktop debian`. Sound works out of the box. Get Termux:X11 from [its nightly release](https://github.com/termux/termux-x11/releases/tag/nightly) (`termux-x11-universal-debug.apk`). VNC works too: `vncserver-start` inside a distro.
- **A normal user account** with sudo; Firefox on desktop installs.

## Commands

```
andronix install <distro> [--de xfce|lxqt|mate|kde|none]
andronix start <distro> [-- command...]      ./start-<distro>.sh works too
andronix desktop <distro>                    the desktop in Termux:X11
andronix update [<distro>]                   the installer and your distros
andronix backup <distro> --to storage        andronix restore
andronix pack add <distro> python|node|java|go|db
andronix tune <distro> --profile light|balanced
andronix remove <distro>
andronix telemetry off|on|status
```

`andronix help` lists everything. Docs: [docs.andronix.app](https://docs.andronix.app).

## How it works

- **The installer:** one Go binary per CPU (aarch64, arm, i686, x86_64).
  - The Termux build is `GOOS=android`. It has been tested on Android 9, 11, 16 and 17.
  - A static Linux build of the same code goes inside each distro, for the desktop and user setup.
- **Distros and desktops are data:** `distros/*.conf` and `desktops/*.conf` (plain `KEY=value`). The dev packs are data too, in `mods/packs/*.conf`.
- **Where the rootfs comes from:** clean tarballs on dl.andronix.app (`ci/build-rootfs.sh`, checked by `ci/check-image.sh`). If a tarball isn't there, the official Docker image is used.
- **Running it:** each distro runs under proot, with fixes for Android kernels and seccomp.

[DESIGN.md](DESIGN.md) has the details, and [CHANGELOG.md](CHANGELOG.md) has the releases.

## Build and test

```sh
ci/build-go.sh                    # all binaries into dist/ (arm, i686 and x86_64 need the Android NDK or Docker)
NO_NDK=1 ci/build-go.sh           # quick: linux builds + android aarch64
go test ./...
tests/smoke.sh                    # end to end in a Termux-like Docker box (tests/Dockerfile)
tests/screens.sh debian xfce      # install a desktop and take screenshots
ci/build-rootfs.sh debian aarch64 # a clean rootfs tarball
```

The test box turns telemetry off (`ANDRONIX_NO_TELEMETRY=1`). `tests/emulator/` runs the installer inside the real Termux app on Android emulators.

## Privacy

`andronix` sends anonymous install events: distro, desktop, CPU, Android version, result, and a random id. It never sends names, emails, paths or commands. A notice shows the first time. Turn them off with `andronix telemetry off` or `ANDRONIX_NO_TELEMETRY=1`.

## Modded editions

Andronix Modded 2.0 and the Classic editions are paid images, built and hosted separately. This repository has only the installer's side (`--edition`, `editions.conf`).

## License

MIT, see [LICENSE](LICENSE). Each distro and package keeps its own license.

## Help

- Discord: [chat.andronix.app](https://chat.andronix.app)
- Email: support@andronix.app
