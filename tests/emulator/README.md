# Emulator matrix

`test-matrix.sh` runs the installer in real Termux on Android emulators. See the header of the script for its options and requirements; the lead's runs use `--avd andronix-a16` (Android 16, kernel 6.6).

## Real app context (`--app-context`)

By default every command runs through `adb shell run-as com.termux`, which has neither the app's seccomp filter nor its SELinux domain. Syscalls the app blocks pass there and crash on phones: Go's `faccessat2` (every `exec.LookPath`) on all Android versions, and its pidfd probe (every exec) on Android 8 to 11.

- `--app-context` runs the installer inside the Termux app process instead (`app-context.sh`): a small agent is started once per boot by typing one command into TermuxActivity (`adb shell input text`), checks that it runs under `Seccomp: 2`, and runs each test script. `app-context.txt` in the results records its context, e.g. `Seccomp: 2 context=u:r:untrusted_app_27 android=11`. On Android 12+ it also lifts the phantom-process limit, as users are told to.
- `--port N` boots the avd on console port N and only touches that emulator (`ANDROID_SERIAL=emulator-N`), so runs on different avds can share the machine.
- `app-context.sh` also works on its own: `tests/emulator/app-context.sh -s emulator-5560 <<<'andronix version'`.
- `syscall-probe/run.sh -s SERIAL` shows which Go stdlib paths survive the filter for a `GOOS=linux` and a `GOOS=android` build.
- AVDs for old Android: `andronix-a9` (API 28) and `andronix-a11` (API 30), both Google APIs arm64, 4 GB RAM, 16 GB data. Gate Android 8 to 11 there with `--app-context`.

## Results

Every run writes to `results/<timestamp>/`, which is kept out of git (as is any `.venv/`):

- `summary.md`: one row per avd × distro × desktop, with pass or fail.
- `<avd>/<distro>-<desktop>/`:
  - `install.log`: the plain installer output.
  - `os-release.txt`: what `andronix start <distro> -- cat /etc/os-release` printed.
  - `vnc.log`, `guest-logs.txt`: the VNC log, `/tmp/andronix-session-<uid>.log` and `/tmp/andronix-bwrap.log` from the guest.
  - `desktop.png`, `android.png`: the VNC screen and the phone's own screen.
  - `screen-check.txt`: how much of the screen was drawn. More than 50% pure black is a FAIL.

The runs that fixed Android-only bugs:

| Run | Bug |
|---|---|
| 20260926-004733 | Kali/Fedora XFCE with no session log |
| 20260926-010504 | glycin/bwrap black screens |
| 20260926-020853 | Fedora's glycin memory cap |

Those runs are the lead's local runs; they were never committed.
