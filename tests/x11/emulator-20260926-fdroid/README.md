# Termux:X11 with F-Droid Termux (andronix-a16, Android 16), Sept 26

- F-Droid Termux 0.118.3 (com.termux_1002.apk from f-droid.org, F-Droid-signed, not debuggable) and Termux:X11 `termux-x11-universal-debug.apk` (GitHub nightly 20260925).
- Driven by typing `sh /data/local/tmp/fd1.sh` / `fd2.sh` into the Termux terminal (no run-as on a non-debuggable app); logs written to /sdcard with storage permission granted over adb.
- Inside the app process: `Seccomp: 2`, `u:r:untrusted_app_27`.
- `install.txt`: the app's install template (android c579e30) against https://dl.andronix.app, `andronix install debian --de xfce --yes --no-start`: fast path (curl worked), proot installed, andronix 2.0.0, Debian 13 from the Andronix mirror (sha256 checked), 251 XFCE packages, Firefox; then a scripted first-boot user `alex`.
- `desktop.txt`: `andronix desktop debian`: installed PulseAudio and termux-x11-nightly, started the server on :0, opened the app.
- Android 16 asked "Allow Termux:X11 to send you notifications?" on the first open (tapped Allow).
- `xfce-on-termux-x11-fdroid-termux.png`: XFCE drawn as alex with the Andronix wallpaper, wordmark whole in portrait.
- Ctrl-C in the Termux session ended the desktop; no termux-x11, proot or dbus-daemon processes were left (`after-ctrl-c.png`). The "has stopped" line is missing from desktop.txt only because the test piped the output through tee, which Ctrl-C also stopped.
