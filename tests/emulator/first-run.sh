#!/usr/bin/env bash
# The app's first command on a FRESH Termux, inside the Termux app process
# (app-context.sh), exactly as a user pastes it. --command fast (default,
# the app's template from android d58e439): upgrade Termux only if curl
# can't fetch get.sh, and install proot only if it's missing:
#
#   (curl -fsSL <get.sh> -o $PREFIX/tmp/get.sh || (DEBIAN_FRONTEND=noninteractive
#     pkg upgrade -y -o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold &&
#     pkg install -y curl && curl -fsSL <get.sh> -o $PREFIX/tmp/get.sh)) &&
#   (command -v proot >/dev/null 2>&1 || pkg install -y proot) &&
#   sh $PREFIX/tmp/get.sh install debian --de none --yes --no-start
#
# --command upgrade: the earlier template, pkg upgrade first, always.
#
#   TERMUX_APK=<apk> tests/emulator/first-run.sh -s emulator-5564 [--distro debian] [--edited]
#       [--command fast|upgrade]
#
# --edited: before the command, edit $PREFIX/etc/bash.bashrc and choose a
# mirror the way termux-change-repo does ($PREFIX/etc/termux/chosen_mirrors;
# pkg rewrites sources.list from it on every run), or mark the apt source
# on builds without mirror lists (Play: termux.sources), then check both
# survive.
#
# Termux is uninstalled and installed again first, so its bootstrap is the
# APK's own (old) one. The mirror is this checkout's dist/ served from the
# host (adb reverse), unless ANDRONIX_MIRROR is set; the rootfs comes from
# dist/<distro>-*-aarch64.tar.xz when present. Results go to
# tests/emulator/results/first-run-<time>/.
set -uo pipefail
here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../.." && pwd)
distro=debian edited=0 command=fast
while [ $# -gt 0 ]; do
    case $1 in
        -s) export ANDROID_SERIAL=$2; shift ;;
        --distro) distro=$2; shift ;;
        --edited) edited=1 ;;
        --command) command=$2; shift ;;
        -h|--help) sed -n '2,31p' "$0"; exit 0 ;;
        *) echo "unknown option: $1" >&2; exit 2 ;;
    esac
    shift
done
for d in "${ANDROID_HOME:-}" "$HOME/Library/Android/sdk" /opt/homebrew/share/android-commandlinetools; do
    [ -n "$d" ] && [ -x "$d/platform-tools/adb" ] && { ADB=$d/platform-tools/adb; break; }
done
export ADB=${ADB:-adb}
[ -n "${TERMUX_APK:-}" ] || { echo "TERMUX_APK is required" >&2; exit 2; }
out="$here/results/first-run-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$out"
log() { printf '%s  %s\n' "$(date +%H:%M:%S)" "$*" | tee -a "$out/run.log"; }
# shellcheck source=app-context.sh
. "$here/app-context.sh"
P=/data/data/com.termux/files/usr

# The mirror: bin/latest/ from this checkout's dist/, on a host port.
mirror=${ANDRONIX_MIRROR:-}
if [ -z "$mirror" ]; then
    port=$((8700 + ${ANDROID_SERIAL##*-} % 100))
    www=$(mktemp -d)
    mkdir -p "$www/bin/latest"
    cp "$repo"/dist/andronix-android-aarch64 "$repo"/dist/andronix-linux-aarch64 "$repo"/dist/SHA256SUMS "$repo/get.sh" "$www/bin/latest/"
    (cd "$www" && exec python3 -m http.server "$port" --bind 127.0.0.1 >"$out/http.log" 2>&1) & http=$!
    trap 'kill $http 2>/dev/null; rm -rf "$www"' EXIT
    "$ADB" reverse "tcp:$port" "tcp:$port" >/dev/null
    mirror="http://127.0.0.1:$port"
fi
rootfs=""
tarball=""; for f in "$repo"/dist/"$distro"-*-aarch64.tar.xz; do case $f in *modded*) ;; *) [ -f "$f" ] && { tarball=$f; break; } ;; esac; done
if [ -n "$tarball" ]; then
    "$ADB" push "$tarball" /data/local/tmp/first-run-rootfs.tar.xz >/dev/null 2>&1
    "$ADB" shell chmod 644 /data/local/tmp/first-run-rootfs.tar.xz
    rootfs="ANDRONIX_ROOTFS=/data/local/tmp/first-run-rootfs.tar.xz"
fi

log "Android $("$ADB" shell getprop ro.build.version.release | tr -d '\r'), mirror $mirror"
"$ADB" uninstall com.termux >/dev/null 2>&1
"$ADB" install -r "$TERMUX_APK" >/dev/null || { log "couldn't install $TERMUX_APK"; exit 1; }
log "Termux $("$ADB" shell dumpsys package com.termux | grep -m1 versionName | tr -d '\r ')"
"$ADB" shell dumpsys deviceidle whitelist +com.termux >/dev/null 2>&1
"$ADB" shell device_config put activity_manager max_phantom_processes 2147483647 >/dev/null 2>&1
"$ADB" shell settings put global settings_enable_monitor_phantom_procs false >/dev/null 2>&1
"$ADB" logcat -c
"$ADB" shell am start -n com.termux/.app.TermuxActivity >/dev/null 2>&1
for _ in $(seq 1 100); do
    "$ADB" shell "run-as com.termux ls $P/bin/bash" >/dev/null 2>&1 && break
    sleep 3
done
sleep 10
ac_ensure || { log "no agent in the Termux app"; exit 1; }

# 0. --edited: a user who customised Termux before.
if [ $edited = 1 ]; then
    tsh_app >"$out/edit.log" 2>&1 <<'EDIT'
echo '# andronix-test: user edit' >>$PREFIX/etc/bash.bashrc
m=$(find $PREFIX/etc/termux/mirrors/europe -type f 2>/dev/null | sort | head -1)
if [ -n "$m" ]; then
    ln -sfn "$m" $PREFIX/etc/termux/chosen_mirrors
    echo "chosen: $(grep -m1 '^MAIN=' "$m" | sed 's/^MAIN=//; s/"//g')"
else
    # Play builds: one deb822 file (termux.net), no mirror rotation.
    f=$(ls $PREFIX/etc/apt/sources.list.d/*.sources $PREFIX/etc/apt/sources.list 2>/dev/null | head -1)
    echo '# andronix-test mirror' >>"$f"
    echo "chosen: # andronix-test mirror"
fi
EDIT
    chosen=$(sed -n 's/^chosen: //p' "$out/edit.log" | tr -d '\r' | head -1)
    log "edited bash.bashrc; mirror: $chosen"
fi

# 1. The app's command, first thing, nothing else run before it.
log "running the app's first command ($command)"
g=$mirror/bin/latest/get.sh
case $command in
fast) line="(curl -fsSL $g -o \$PREFIX/tmp/get.sh || (DEBIAN_FRONTEND=noninteractive pkg upgrade -y -o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold && pkg install -y curl && curl -fsSL $g -o \$PREFIX/tmp/get.sh)) && (command -v proot >/dev/null 2>&1 || pkg install -y proot) && sh \$PREFIX/tmp/get.sh install $distro --de none --yes --no-start" ;;
*) line="DEBIAN_FRONTEND=noninteractive pkg upgrade -y -o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold && pkg install -y curl proot && curl -fsSL $g -o \$PREFIX/tmp/get.sh && sh \$PREFIX/tmp/get.sh install $distro --de none --yes --no-start" ;;
esac
echo "$line" >"$out/command.txt"
tsh_app >"$out/first-command.log" 2>&1 <<EOF
export $rootfs ANDRONIX_MIRROR=$mirror
$line
echo "EXIT=\$?"
EOF
res=PASS notes=""
grep -q '^EXIT=0' "$out/first-command.log" || { res=FAIL; notes="command failed"; }
# A dpkg conffile question would have hung (stdin is /dev/null); make sure
# none was even printed.
grep -qE '\(Y/I/N/O/D/Z\)|Configuration file .* modified' "$out/first-command.log" && notes="${notes:+$notes; }conffile prompt printed"

# 2. What a user does next.
tsh_app >"$out/after.log" 2>&1 <<EOF
echo "curl: \$(curl --version | head -1)"
curl -fsS -o /dev/null $mirror/bin/latest/SHA256SUMS && echo "curl fetch ok"
andronix version
andronix start $distro -- cat /etc/os-release | grep PRETTY_NAME
pkg install -y htop >/dev/null 2>&1 && echo "pkg still works"
openssl version
echo "EXIT=\$?"
EOF
if [ $edited = 1 ]; then
    tsh_app >"$out/kept.log" 2>&1 <<KEPT
grep -q 'andronix-test: user edit' \$PREFIX/etc/bash.bashrc && echo 1 || echo 0
cat \$PREFIX/etc/apt/sources.list \$PREFIX/etc/apt/sources.list.d/* 2>/dev/null | grep -qF '${chosen%/}' && echo 1 || echo 0
KEPT
    [ "$(tr -d '\r' <"$out/kept.log" | grep -cx 1)" = 2 ] || { res=FAIL; notes="${notes:+$notes; }user edits lost ($(tr '\n' ' ' <"$out/kept.log"))"; }
fi
grep -q 'curl fetch ok' "$out/after.log" || { res=FAIL; notes="${notes:+$notes; }curl broken after"; }
grep -q '^PRETTY_NAME=' "$out/after.log" || { res=FAIL; notes="${notes:+$notes; }distro doesn't start"; }
grep -q 'pkg still works' "$out/after.log" || { res=FAIL; notes="${notes:+$notes; }pkg broken after"; }
"$ADB" shell "run-as com.termux cat $P/var/log/apt/term.log" >"$out/apt-term.log" 2>/dev/null
"$ADB" exec-out screencap -p >"$out/android.png"
log "$res  $notes"
echo "$res $notes" >"$out/result.txt"
[ $res = PASS ]
