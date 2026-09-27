# Changelog

Changes to the `andronix` installer and the free distro images it installs. The app, the website and the docs have their own release notes. See https://docs.andronix.app for how to use the installer.

## 2.0.1 (unreleased)

### Works on more phones

- **Phone compatibility, automatic.** The installer measures what the phone's kernel and Termux can do (inside the distro, under proot) and applies the fixes that phone needs, from rules shipped in the installer. `andronix doctor` shows the results and the fixes that apply. No personal data is read; with telemetry on, only the result summary is sent.
- **Kali (and other systemd 256+ distros) on kernels before 5.8.** systemd couldn't read its own config under proot there ("Protocol driver not attached"), which left sudo, dbus and the desktop unconfigured (seen on a Redmi Note 7 Pro, kernel 4.14). A small shim now gives it what it needs, and Kali uses the standalone tmpfiles and sysusers.
- **Resuming an install works across installer versions.** An install that stopped part way (also one started by 2.0.0) gets this phone's fixes and finishes its desktop when you run `andronix install <distro>` again. `andronix update` on an unfinished install now says to run `andronix install`.
- **Kernels before 4.8 (many Android 8 and 9 phones):** the command line works. Desktops stay black there, and the installer now says so at install and before starting a desktop, and suggests `--de none`.
- **LineageOS** is recognised in `andronix doctor`.

### Desktop

- **Termux:X11 first.** After an install, and when you log in to a distro, the installer suggests `andronix desktop <distro>` to open the desktop in the Termux:X11 app. VNC is shown second.
- **Termux:X11's extra-keys bar** is hidden on the first desktop, so the desktop's bottom panel shows. Swipe down with three fingers to bring it back.
- **Fewer processes, so Android 12+ stops the desktop less often** (the phantom process killer counts an app's processes): Firefox runs in fewer processes and without its crash reporter, the distro doesn't start its own PulseAudio, and autostart programs that don't work on a phone are off. On Android 12 and newer, `andronix desktop` prints one line with how to prevent "signal 9".
- **Firefox plays H.264 video** (YouTube falls back to it on phones): video codecs are installed with Firefox on Debian, Kali, Ubuntu and Fedora.
- **XFCE's Web Browser button opens Firefox** on Debian and Kali (it asked for a "preferred application" before).
- **Wallpapers fit portrait phones** on LXQt and KDE Plasma, without cropping the logo.

### Downloads and storage

- **Downloads resolve through the Andronix API first.** If the API is slow or down, the installer falls back to dl.andronix.app after 3 seconds. Every download is still checked against its sha256.
- **Modded downloads are checked too:** the installer fetches the edition's sha256 from the Andronix API, behind the same purchase token, and verifies the download.
- **Less storage used.** A finished install deletes its download, Modded and Classic images included. `andronix remove` also deletes the distro's leftover downloads, and the new `andronix clean` empties the download cache.
- **get.sh retries a download that was cut off** mid-transfer on a bad connection.
- **Removing a Modded or Classic edition** (the app's uninstall): `andronix remove <edition-id> --legacy` removes only that edition, never the free distro or another edition in the same folder. With a Classic id, `--legacy` also removes the copy the old app's Modded scripts installed. The confirmation shows each folder's size.

### Premium

- **Beta channel:** `andronix update --channel beta --token '<token from the app>'`. `andronix update --channel stable` goes back to the release.
- **Early-access Modded editions.** Premium and Modded Pass owners can install them before everyone else. Anyone else sees "Early access: Premium" instead of a download error.
- A Modded token for a different edition now says so, instead of "link expired".

### Smaller fixes

- Output fits the terminal's width (a stale `COLUMNS` made it wrap at 56 columns).
- Logs rotate per kind, and the log of the running command is never deleted.
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
