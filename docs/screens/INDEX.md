# Screens

Made in the Termux-like Docker box (arm64, non-root, Termux's own proot). Pass or fail comes from the real checks, never from the pictures:

- the install exits 0;
- for desktops: login as the first-boot user, sudo works, and after `vncserver-start 1280x720` the window manager is running;
- for `none`: the distro starts and sudo works.

Files per combo: `<distro>-<de>-install.txt` (last 40 lines of the plain installer output), `-desktop.png`, `-menu.png`, `-apps.png` (terminal and file manager).

Script: `tests/screens.sh <distro> <de>`. Rows marked "bash prototype" were made with the phase-1 shell prototype, which isn't in this repository.

| Combo | Result | Date | Tested with | Packages · time | Note |
|---|---|---|---|---|---|
| ubuntu-xfce | PASS | 2026-09-25 | Go binary | 257 · 3m04s | Firefox from Mozilla APT; first desktop draw is slower than Debian; some menu category icons are blank (icon theme gap) |
| ubuntu24-xfce | PASS | 2026-09-25 | Go binary | 238 · 1m30s | XFCE 4.18, TigerVNC 1.13 (uses ~/.vnc directly; the symlink covers it); Firefox from Mozilla APT |
| kali-none | PASS | 2026-09-25 | bash prototype (port-a) | 16 · 33 s | CLI only; sudo and nano work. |
| kali-lxqt | PASS | 2026-09-25 | bash prototype (port-a) | 339 · 276 s | Main-menu button has no icon. The desktop icons carry a "!" (untrusted) emblem. The red ✗ in the tray is the volume plugin (no PulseAudio). |
| kali-mate | PASS | 2026-09-26 | bash prototype (port-a) | 473 · 360 s | Desktop is drawn (MATE gradient plus a Home icon) since the caja fix (`DE_DESKTOP_EDITS`). Tooltip in the apps shot left from the menu click. |
| fedora-none | PASS | 2026-09-25 | bash prototype (port-a) | 15 · 141 s | CLI only. dnf metadata alone takes about 50 s. |
| fedora-lxqt | PASS | 2026-09-25 | bash prototype (port-a) | 419 · 254 s | Fedora wallpaper, full menu. Red ✗ volume tray icon. |
| fedora-mate | PASS | 2026-09-26 | bash prototype (port-a) | 399 · 347 s | Desktop is drawn (gradient plus Computer, Home and Trash icons) since the caja fix. |
| kali-xfce | PASS | 2026-09-26 | Go binary | 259 · 1m45s | ok |
| fedora-xfce | PASS | 2026-09-26 | Go binary | 324 · 1m23s | ok |
| arch-lxqt | PASS | 2026-09-25 | bash prototype (port-b) |  | 69 pkgs. qterminal starts `sh`, not bash (no `$SHELL` in the session env). |
| manjaro-lxqt | PASS | 2026-09-25 | bash prototype (port-b) |  | 68 pkgs (LXQt 1.4, Qt5). qterminal starts `sh`. |
| manjaro-mate | PASS | 2026-09-25 | bash prototype (port-b) |  | 46 pkgs. MATE 1.26. |
| alpine-mate | PASS | 2026-09-25 | bash prototype (port-b) |  | 34 pkgs. First run FAIL: stale `/tmp/.X1-lock` with a reused pid, vncserver-start said "already running"; fixed in guest scripts, re-run PASS. |
| void-lxqt | PASS | 2026-09-26 | bash prototype (port-b) |  | 96 pkgs, on 6.8 with the shim. |
| void-mate | PASS | 2026-09-26 | bash prototype (port-b) |  | 100 pkgs, on 6.8 with the shim. Black desktop (predates the MATE caja fix). |
| alpine-xfce | PASS | 2026-09-26 | Go binary | 234 · 45s | ok |
| arch-xfce | PASS | 2026-09-26 | Go binary | 237 · 2m24s | ok |
| manjaro-xfce | PASS | 2026-09-26 | Go binary | 219 · 1m38s | no sudo (DISTRO_SUDO=none) |
| ubuntu-mate | PASS | 2026-09-26 | bash prototype (port-a) | 459 · 289 s | 26.04, --no-browser. Ubuntu's "familiar" panel needs brisk-menu, indicator and trash applets (now in the apt list). |
| debian-mate | PASS | 2026-09-26 | Go binary | 458 · 1m45s | caja fix (DE_DESKTOP_EDITS): desktop drawn |
| void-xfce | PASS | 2026-09-26 | Go binary | 268 · 1m45s | kernel 6.8 with the fchmodat2 shim; sudo is NOPASSWD (DISTRO_SUDO) |
| alpine-lxqt | PASS | 2026-09-26 | Go binary | 133 · 46s | ok |
| arch-mate | PASS | 2026-09-26 | Go binary | 289 · 3m01s | caja fix works (wallpaper + desktop icons); mate-terminal not captured: started outside the session D-Bus by the screenshot script (test limit, same as port-a); caja title says "(as aid_termux)" |
| debian-lxqt | PASS | 2026-09-26 | Go binary | 304 · 1m15s | menu opens (click); the main-menu button has no icon; red ✗ tray icon is the volume plugin (no PulseAudio in the box) |
| ubuntu-kde | PASS | 2026-09-26 | Go binary |  | ok |
| debian-xfce | PASS | 2026-09-26 | Go binary | 251 · 2m30s | ok |
