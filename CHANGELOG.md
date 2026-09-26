# Changelog

Changes to the `andronix` installer and the free distro images it installs. The app, the website and the docs have their own release notes. See https://docs.andronix.app for how to use the installer.

## 2.0.1 (unreleased)

- **Termux:X11 first.** After an install, and when you log in to a distro, the installer now suggests `andronix desktop <distro>` to open the desktop in the Termux:X11 app. VNC is shown second.
- **Downloads resolve through the Andronix API first.** If the API is slow or down, the installer falls back to dl.andronix.app after 3 seconds. Every download is still checked against its sha256.
- **Beta channel for Premium:** `andronix update --channel beta --token '<token from the app>'`. `andronix update --channel stable` goes back to the release.
- **Early-access Modded editions.** Premium and Modded Pass owners can install them before everyone else. Anyone else sees "Early access: Premium" instead of a download error.
- **Modded downloads are checked too:** the installer fetches the edition's sha256 from the Andronix API, behind the same purchase token, and verifies the download.
- **Kali on older kernels:** Kali's systemd 261 failed to configure under proot on older phone kernels ("Failed to enable units: Protocol driver not attached", seen on a Redmi Note 7 Pro with 4.14), which left sudo, dbus and the desktop unconfigured. Kali now uses the standalone tmpfiles and sysusers instead of the full systemd, and `andronix update` repairs installs where this already happened.
- The unused TigerVNC wrapper path was removed; the VNC desktop starts the same way on every distro.

## 2.0.0 (2026-09-26)

Andronix 2.0: a new installer, written in Go, published at dl.andronix.app.

### Install

- **One command:**

  ```sh
  curl -fsSL https://dl.andronix.app/get.sh -o $PREFIX/tmp/get.sh && sh $PREFIX/tmp/get.sh install <distro> --de <de>
  ```

- **Termux:** it works with Termux from F-Droid, GitHub and Google Play. Play Termux needs Android 11 or newer.
- **proot and a half-upgraded Termux:** the installer installs proot itself if it's missing. It also repairs a half-upgraded Termux (where curl can't start) with one package upgrade that keeps your edited settings.
- **Every download is checksummed.**

### Distros and desktops

- **9 free distros:** Ubuntu 26.04 LTS, Ubuntu 24.04 LTS, Debian 13, Kali rolling, Fedora 44, Arch Linux ARM, Manjaro ARM, Alpine 3.24 and Void.
- **Desktops:** XFCE, LXQt, MATE and KDE Plasma. KDE is offered on Ubuntu, Debian and Kali, and needs 4 GB of RAM.
- **Firefox** is included.
- **A normal user account** is created, so you no longer run everything as root.
- **Window managers are retired.** Old window-manager installs now install XFCE.

### Desktop and commands

- **Termux:X11:** `andronix desktop <distro>` opens the desktop in the Termux:X11 app. It's smoother than VNC, and sound works out of the box because PulseAudio is set up automatically. Termux:X11 comes from GitHub (termux-x11 nightly, the universal-debug APK).
- **VNC** still works: `vncserver-start` inside a distro.
- **New commands:**
  - `andronix update`: updates the installer and your distros.
  - `andronix backup --to storage`: backs up to phone storage.
  - `andronix tune`: performance profile.
  - Dev packs: `andronix pack add python|node|java|go|db`. PostgreSQL works under proot.

### Modded OS

- **Modded 2.0 editions**, installed with `--edition <id>`: Ubuntu 26.04 XFCE, Ubuntu 26.04 KDE (Plasma 6), Debian 13 XFCE, Manjaro XFCE and Kali XFCE. They share one Andronix look, and updates are included.
- **Classic editions:** the original Modded Ubuntu XFCE, Ubuntu KDE, Debian and Manjaro. They keep the original look, rebuilt on current systems. The old Modded install commands install the matching Classic edition.

### Fixes

- **Android 8 to 11:** the installer no longer crashes (SIGSYS).
- **Android 9 on kernel 4.4:** proot no longer aborts.
- **Ubuntu 26.04 on Play Store Termux:** updates no longer break the system. It switches to GNU coreutils first.

### Privacy

- **Optional, anonymous install events:** distro, desktop, result. There are no names and no emails.
- **Turn them off** with `andronix telemetry off` or `ANDRONIX_NO_TELEMETRY=1`.

### Known issue

- On some Android 9 phones with a 4.4 kernel, desktops may show a black screen. The command line works, and the installer warns you before a desktop install.
