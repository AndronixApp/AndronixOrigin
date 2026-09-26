#!/usr/bin/env bash
# Which Go stdlib paths survive Android's app seccomp filter, per build
# target: runs main.go (built for GOOS=linux and GOOS=android, arm64) inside
# the Termux app process on the emulator ANDROID_SERIAL (or -s SERIAL).
#
#   tests/emulator/syscall-probe/run.sh -s emulator-5560
#
# Cases: lookpath (exec.LookPath: faccessat2), exec (absolute path, so only
# os.StartProcess: the pidfd probe), copy (io.Copy file to file:
# copy_file_range). "SIGSYS: bad system call" means the app filter killed it.
set -uo pipefail
here=$(cd "$(dirname "$0")" && pwd)
[ "${1:-}" = -s ] && export ANDROID_SERIAL=$2
for d in "${ANDROID_HOME:-}" "$HOME/Library/Android/sdk" /opt/homebrew/share/android-commandlinetools; do
    [ -n "$d" ] && [ -x "$d/platform-tools/adb" ] && { ADB=$d/platform-tools/adb; break; }
done
export ADB=${ADB:-adb}
H=/data/data/com.termux/files/home
tmp=$(mktemp -d)
for b in linux android; do
    (cd "$here" && CGO_ENABLED=0 GOOS=$b GOARCH=arm64 go build -o "$tmp/probe-$b" .) || exit 1
    "$ADB" push "$tmp/probe-$b" "/data/local/tmp/probe-$b" >/dev/null 2>&1
    "$ADB" shell "chmod 644 /data/local/tmp/probe-$b && run-as com.termux sh -c 'cat /data/local/tmp/probe-$b > $H/probe-$b && chmod 700 $H/probe-$b'"
done
rm -rf "$tmp"
echo "Android $("$ADB" shell getprop ro.build.version.release | tr -d '\r'), inside the Termux app:"
bash "$here/../app-context.sh" <<'J'
for b in linux android; do for t in lookpath exec copy; do
    printf '  %-8s %-9s %s\n' "$b" "$t" "$(./probe-$b $t 2>&1 | head -1)"
done; done
J
