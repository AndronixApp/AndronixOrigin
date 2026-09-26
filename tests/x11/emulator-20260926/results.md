# Termux:X11 + sound on the emulator, 2026-09-26

Build: `feat-x11-audio` @ c1db7b9. AVD `andronix-a16` (Android 16, API 36), Termux 0.118.3 (GitHub debug build) after `apt full-upgrade` (termux-exec 2.5.0), Termux:X11 nightly `termux-x11-universal-debug.apk`. Debian 13 XFCE, user `alex`.

Commands were typed into a real Termux session (not `adb run-as`), because the app's seccomp filter only applies there.

| Step | Result | Notes |
|---|---|---|
| 0. Run anything from a real Termux session | **FAIL (blocker, also on master)** | `./start-debian.sh true` dies with `SIGSYS: bad system call` in `syscall.faccessat2` from `exec.LookPath("proot")` (`internal/app/manage.go:40`). Go 1.26's LookPath calls faccessat2, which Android's app seccomp filter traps. The harness uses `adb run-as`, which has no seccomp, so every earlier PASS missed this. Steps 2–6 below ran on a scratch build that resolves commands with `os.Stat` instead of `exec.LookPath`. |
| 1. Install + setup-user | PASS | The first sound install failed because this Termux had the old curl/openssl mismatch (`CANNOT LINK EXECUTABLE ... SSL_set_quic_tls_early_data_enabled`). `apt full-upgrade` fixed it; the 24 h backoff then held further retries, as designed. |
| 2. Sound | PASS | Run 1: `installing PulseAudio for sound (only this once)...` then `sound: installed PulseAudio, started PulseAudio, enabled sound for distros`. `pactl` shows `module-native-protocol-tcp listen=127.0.0.1 auth-ip-acl=127.0.0.1 auth-anonymous=1`. Run 2 prints nothing. `termux.log` lists each action. |
| 3. No app | PASS | Installs `termux-x11-nightly`, shows the orange "One more app: Termux:X11" box, exits 1. `step3-no-app.png` |
| 4. Termux:X11 desktop | **FAIL, fix verified** | Termux:X11 opens but stays black (`step4-xfce-black.png`). xfce4-session and every GTK app hang in a futex after Mesa loads libvulkan (DRI3/zink path). X itself works: native `xclock` draws (`diag-termux-xclock-ok.png`). With `LIBGL_DRI3_DISABLE=1` in the guest env, full XFCE draws as alex (`step4-xfce-with-LIBGL_DRI3_DISABLE.png`). The wallpaper is XFCE's default, not the Andronix one (VNC shows Andronix's). |
| 5. Second session + stop | PASS | `./start-debian.sh 'DISPLAY=:0 mousepad'` shows on the X11 desktop (`step5-mousepad-on-x11.png`). With a trailing `&` the app dies at once (proot `--kill-on-exit`). `andronix desktop stop`: "Stopped the Termux:X11 desktop.", session 1 prints "The Debian 13 desktop has stopped." (`step5-session1-has-stopped.png`), and no termux-x11, proot, xfce4 or dbus-daemon is left. |
| 6. `--x11` and VNC | PASS | `./start-debian.sh --x11` starts the X11 desktop (`step6-start-x11-flag.png`). `vncserver-start` still works, with the Andronix wallpaper (`step6-vnc-desktop.png`). |
| 6b. Ubuntu KDE | not run | |

Also seen: under `adb run-as` with termux-exec 2.5.0, `env -u LD_PRELOAD andronix` fails with `has unexpected e_type: 2` (static non-PIE binary run through the system linker). A real session on this Termux build was fine, but Termux builds that need linker exec (Play Store, targetSdk ≥ 29) may hit it; worth checking a `-buildmode=pie` build.

## Rerun on ace4fe3 (09:20–09:31)

`feat-x11-audio` @ ace4fe3, plus the same scratch faccessat2 workaround (resolve commands with `os.Stat`). Fresh Debian 13 XFCE install; everything typed into real Termux sessions. Screenshots carry an `-ace4fe3` suffix.

| Step | Result | Notes |
|---|---|---|
| 1. Install + setup-user | PASS | |
| 2. Sound | PASS | Installed PulseAudio on run 1, TCP module on 127.0.0.1, run 2 silent. The PulseAudio daemon from the earlier run was still up after `pkg uninstall`, so the summary said only "installed PulseAudio". |
| 3. No app | PASS | Orange box, exit 1 (`step3-no-app-ace4fe3.png`). |
| 4. Termux:X11 desktop | PASS | Full XFCE as alex with the Andronix wallpaper (`step4-xfce-x11-ace4fe3.png`). Cosmetic: in portrait the wallpaper is zoomed, so "ANDRONIX." is cropped to "NDRON". |
| 5. Second session + stop | PASS | `./start-debian.sh 'DISPLAY=:0 mousepad'` (no `&`) shows on the X11 desktop and stays responsive (`step5-mousepad-on-x11-ace4fe3.png`). `andronix desktop stop` from a third session stops everything: the mousepad session ends, session 1 prints "has stopped" (`step5-session1-has-stopped-ace4fe3.png`), and no termux-x11, proot, xfce4, mousepad or dbus-daemon is left. |
| 6. `--x11` and VNC | PASS | `--x11` starts the X11 desktop (`step6-start-x11-flag-ace4fe3.png`); VNC at 1280x720 shows the Andronix wallpaper (`step6-vnc-desktop-ace4fe3.png`). |
