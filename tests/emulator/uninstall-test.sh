#!/usr/bin/env bash
# The app's uninstall command (android d21e5b1), typed into the Termux
# terminal like a user pastes it, so the installer's "Remove it?" question
# appears and is answered with a key press.
#
#   ALLOW_WIPE=1 TERMUX_APK=<apk> tests/emulator/uninstall-test.sh -s emulator-5564 --case a|b [--fallback pkg|apt]
#   (emulators only: it uninstalls Termux; ALLOW_WIPE=1 says that's intended)
#
#   a  andronix installed, a distro from the new installer: the command must
#      take the 'andronix remove <id> --legacy' branch (no pkg upgrade) and
#      the distro must be gone.
#   b  a fresh Termux with NO andronix and only a distro from the OLD
#      installer (made with the old Firestore UnModdedOS.Debian command):
#      the fallback branch (fetch get.sh, upgrading Termux if curl fails,
#      then 'get.sh remove debian --legacy') must clean it up.
#
# --fallback: how the command repairs a Termux whose curl can't run. apt
# (default, the app's template since android c579e30: apt-get only) or pkg
# (the template before: pkg upgrade, which itself needs curl to pick a
# mirror, so it can't repair it).
#
# get.sh and the binary come from this checkout's dist/, served from the
# host (adb reverse) in place of https://dl.andronix.app.
set -uo pipefail
here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../.." && pwd)
case_="" fallback=apt
while [ $# -gt 0 ]; do
    case $1 in
        -s) export ANDROID_SERIAL=$2; shift ;;
        --case) case_=$2; shift ;;
        --fallback) fallback=$2; shift ;;
        -h|--help) sed -n '2,23p' "$0"; exit 0 ;;
        *) echo "unknown option: $1" >&2; exit 2 ;;
    esac
    shift
done
case $case_ in a|b) ;; *) echo "--case a or b" >&2; exit 2 ;; esac
for d in "${ANDROID_HOME:-}" "$HOME/Library/Android/sdk" /opt/homebrew/share/android-commandlinetools; do
    [ -n "$d" ] && [ -x "$d/platform-tools/adb" ] && { ADB=$d/platform-tools/adb; break; }
done
export ADB=${ADB:-adb}
[ -n "${TERMUX_APK:-}" ] || { echo "TERMUX_APK is required" >&2; exit 2; }
out="$here/results/uninstall-$case_-$fallback-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$out"
log() { printf '%s  %s\n' "$(date +%H:%M:%S)" "$*" | tee -a "$out/run.log"; }
# shellcheck source=app-context.sh
. "$here/app-context.sh"
ac_guard_serial
ac_require_wipe_ok   # uninstalls Termux below
P=/data/data/com.termux/files/usr H=/data/data/com.termux/files/home

port=${HOST_PORT:-$(ac_port 8700)}
# A server left by an interrupted run would answer 404 from a deleted folder.
while lsof -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1; do port=$((port + 100)); done
www=$(mktemp -d)
mkdir -p "$www/bin/latest"
cp "$repo"/dist/andronix-android-aarch64 "$repo"/dist/andronix-linux-aarch64 "$repo"/dist/SHA256SUMS "$www/bin/latest/"
cp "$repo/get.sh" "$www/get.sh"
(cd "$www" && exec python3 -m http.server "$port" --bind 127.0.0.1 >"$out/http.log" 2>&1) & http=$!
trap 'kill $http 2>/dev/null; rm -rf "$www"' EXIT
"$ADB" reverse "tcp:$port" "tcp:$port" >/dev/null
mirror="http://127.0.0.1:$port"

# A fresh Termux (its own old bootstrap).
"$ADB" uninstall com.termux >/dev/null 2>&1
"$ADB" install -r "$TERMUX_APK" >/dev/null || { log "couldn't install $TERMUX_APK"; exit 1; }
"$ADB" shell dumpsys deviceidle whitelist +com.termux >/dev/null 2>&1
"$ADB" shell am start -n com.termux/.app.TermuxActivity >/dev/null 2>&1
for _ in $(seq 1 100); do "$ADB" shell "run-as com.termux ls $P/bin/bash" >/dev/null 2>&1 && break; sleep 3; done
sleep 10
ac_ensure || { log "no agent in the Termux app"; exit 1; }
log "Termux $("$ADB" shell dumpsys package com.termux | grep -m1 versionName | tr -d '\r ')"

# The distro to remove.
if [ "$case_" = a ]; then
    tarball=$(ls "$repo"/dist/debian-13-aarch64.tar.xz)
    "$ADB" push "$tarball" /data/local/tmp/uninstall-rootfs.tar.xz >/dev/null 2>&1
    "$ADB" shell chmod 644 /data/local/tmp/uninstall-rootfs.tar.xz
    tsh_app >"$out/setup.log" 2>&1 <<EOF
pkg install -y proot >/dev/null 2>&1
curl -fsSL $mirror/get.sh -o \$PREFIX/tmp/get.sh && ANDRONIX_MIRROR=$mirror ANDRONIX_ROOTFS=/data/local/tmp/uninstall-rootfs.tar.xz sh \$PREFIX/tmp/get.sh install debian --de none --yes --no-start --plain | tail -3
ls -d ~/.andronix/distros/debian ~/start-debian.sh
EOF
else
    # The old Firestore command, UnModdedOS.Debian.Install.DE.Node.
    tsh_app >"$out/setup.log" 2>&1 <<'EOF'
pkg update -y && pkg install wget curl proot tar -y && wget https://raw.githubusercontent.com/AndronixApp/AndronixOrigin/master/Installer/Debian/debian.sh -O debian.sh && chmod +x debian.sh && bash debian.sh
echo "EXIT=$?"
ls -d ~/debian-fs ~/start-debian.sh; command -v andronix || echo "no andronix"
EOF
fi
tail -4 "$out/setup.log" | tee -a "$out/run.log"
before=$(ac_runas "grep -c '^Commandline:' $P/var/log/apt/history.log 2>/dev/null || echo 0")

# The app's command (InstallCatalog.command(), d21e5b1 + d58e439 template),
# get.sh from the host.
g="curl -fsSL $mirror/get.sh -o \$PREFIX/tmp/get.sh"
o="-o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold"
case $fallback in
apt) ;;  # the app's template (android c579e30)
*) echo "--fallback pkg: the template before c579e30" >&2 ;;
esac
if [ "$fallback" = apt ]; then
    full="(curl -fsSL $mirror/get.sh -o \$PREFIX/tmp/get.sh || (DEBIAN_FRONTEND=noninteractive apt-get update && DEBIAN_FRONTEND=noninteractive apt-get -y -o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold full-upgrade && DEBIAN_FRONTEND=noninteractive apt-get -y -o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold install curl && curl -fsSL $mirror/get.sh -o \$PREFIX/tmp/get.sh)) && (command -v proot >/dev/null 2>&1 || (DEBIAN_FRONTEND=noninteractive apt-get update && DEBIAN_FRONTEND=noninteractive apt-get -y -o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold install proot)) && sh \$PREFIX/tmp/get.sh remove debian --legacy"
else
    full="($g || (DEBIAN_FRONTEND=noninteractive pkg upgrade -y $o && pkg install -y curl && $g)) && (command -v proot >/dev/null 2>&1 || pkg install -y proot) && sh \$PREFIX/tmp/get.sh remove debian --legacy"
fi
cmd="if command -v andronix >/dev/null 2>&1; then andronix remove debian --legacy; else $full; fi"
echo "$cmd" >"$out/command.txt"
printf 'export ANDRONIX_MIRROR=%s\n%s\necho "UNINSTALL-EXIT=$?" >%s/.ac/uninstall.rc\n' "$mirror" "$cmd" "$H" | ac_put uninstall.sh 700
ac_runas "rm -f $H/.ac/uninstall.rc"

log "typing the uninstall command into Termux"
ac_front || { log "TermuxActivity not in front"; exit 1; }
sleep 2
"$ADB" shell input text "bash%s$H/.ac/uninstall.sh" </dev/null
"$ADB" shell input keyevent KEYCODE_ENTER </dev/null
# Wait for the question (andronix remove running), then answer yes.
asked=0
for _ in $(seq 1 600); do
    "$ADB" shell ps -A -o ARGS 2>/dev/null | grep -q '^andronix remove debian' && { asked=1; break; }
    [ -n "$(ac_runas "cat $H/.ac/uninstall.rc 2>/dev/null")" ] && break
    sleep 2
done
sleep 4
"$ADB" exec-out screencap -p >"$out/question.png"
[ $asked = 1 ] && { "$ADB" shell input text y </dev/null; sleep 1; "$ADB" shell input keyevent KEYCODE_ENTER </dev/null; }
for _ in $(seq 1 300); do
    [ -n "$(ac_runas "cat $H/.ac/uninstall.rc 2>/dev/null")" ] && break
    sleep 2
done
"$ADB" exec-out screencap -p >"$out/after.png"
rc=$(ac_runas "cat $H/.ac/uninstall.rc")
after=$(ac_runas "grep -c '^Commandline:' $P/var/log/apt/history.log 2>/dev/null || echo 0")
ac_runas "grep -A1 '^Commandline:' $P/var/log/apt/history.log" >"$out/apt-history.txt"
left=$(ac_runas "ls -d $H/.andronix/distros/debian $H/start-debian.sh $H/debian-fs $H/start-debian-old.sh 2>/dev/null")

res=PASS notes="asked=$asked $rc apt-runs=$before->$after"
[ "$rc" = UNINSTALL-EXIT=0 ] || { res=FAIL; notes="$notes; command failed"; }
[ -z "$left" ] || { res=FAIL; notes="$notes; left behind: $(echo "$left" | tr '\n' ' ')"; }
if [ "$case_" = a ] && [ "$after" != "$before" ]; then res=FAIL; notes="$notes; ran apt/pkg"; fi
log "case $case_: $res  $notes"
echo "$res $notes" >"$out/result.txt"
[ $res = PASS ]
