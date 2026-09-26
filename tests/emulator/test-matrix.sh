#!/usr/bin/env bash
# End-to-end test of the Andronix installer in real Termux on Android emulators.
#
#   tests/emulator/test-matrix.sh [options]
#
#   --avd NAME[,NAME…]     emulators to run on (default: andronix-a16)
#   --distros "a b"        distros to test (default: every distros/*.conf)
#   --des "a b"            desktops to test (default: "xfce none")
#   --fresh-termux         wipe Termux data before each emulator (first-run bootstrap)
#   --keep                 don't remove the distro after a passing test
#   --src DIR              installer checkout to test (default: this repo, HEAD)
#   --modded DIR           test Modded images: installs DIR/<distro>-*-<desktop>-modded-aarch64.tar.xz
#   --app-context          run andronix inside the Termux app process (its seccomp filter and
#                          SELinux domain, like a user), not via run-as; see app-context.sh
#   --port N               emulator console port (5554, 5556, ...): boots the avd there and
#                          only ever touches that emulator, so other emulators keep running
#   --reinstall-termux     uninstall Termux first and install $TERMUX_APK (to switch between
#                          the GitHub and the Play Store build, which share com.termux)
#   --x11                  for desktops, also run `andronix desktop` on Termux:X11 (installs
#                          $X11_APK, the termux-x11 app) and check the phone's screen
#
# For every avd × distro × desktop it:
#   1. copies the installer (git HEAD of --src) and any dist/ tarballs to the device
#   2. runs `andronix install <distro> --de <desktop> --yes --no-start` inside Termux
#   3. checks the distro boots (`andronix start <distro> -- cat /etc/os-release`)
#   4. for desktops: starts VNC at 1280x720, screenshots it, checks something was drawn
#   5. removes it again and checks nothing is left behind
# and writes tests/emulator/results/<timestamp>/summary.md with logs and screenshots.
#
# Needs: the Android command-line SDK (emulator, adb), an AVD with a large data
# partition, and a *debuggable* Termux APK (GitHub "debug" build) so `run-as` works.
set -uo pipefail

here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../.." && pwd)

# First SDK that actually has adb: $ANDROID_HOME, Android Studio's default, Homebrew's.
for d in "${ANDROID_HOME:-}" "$HOME/Library/Android/sdk" /opt/homebrew/share/android-commandlinetools; do
    [ -n "$d" ] && [ -x "$d/platform-tools/adb" ] && [ -x "$d/emulator/emulator" ] && { ANDROID_HOME=$d; break; }
done
ADB="$ANDROID_HOME/platform-tools/adb"
EMULATOR="$ANDROID_HOME/emulator/emulator"
TERMUX_APK=${TERMUX_APK:-}
X11_APK=${X11_APK:-}
TERMUX_PKG=com.termux
T_HOME=/data/data/com.termux/files/home
T_PREFIX=/data/data/com.termux/files/usr
VNC_PASS=andro123
INSTALL_TIMEOUT=${INSTALL_TIMEOUT:-2400}   # seconds per install

avds="andronix-a16"; distros=""; des="xfce none"; fresh=0; keep=0; src="$repo"; modded_dir=""; app=0; port=""; reinstall=0; x11=0
while [ $# -gt 0 ]; do
    case $1 in
        --avd) avds=$2; shift ;;
        --distros) distros=$2; shift ;;
        --des) des=$2; shift ;;
        --fresh-termux) fresh=1 ;;
        --keep) keep=1 ;;
        --modded) modded_dir=$(cd "$2" && pwd); shift ;;
        --app-context) app=1 ;;
        --port) port=$2; shift ;;
        --reinstall-termux) reinstall=1 ;;
        --x11) x11=1 ;;
        --src) src=$(cd "$2" && pwd); shift ;;
        -h|--help) sed -n '2,30p' "$0"; exit 0 ;;
        *) echo "unknown option: $1" >&2; exit 2 ;;
    esac
    shift
done
# With --port, every adb call goes to that one emulator.
[ -n "$port" ] && export ANDROID_SERIAL="emulator-$port"
vnc_local=$((5901 + ${port:-5554} - 5554))   # host port for VNC, per emulator
[ -n "$distros" ] || distros=$(cd "$src/distros" && ls *.conf | sed 's/\.conf$//' | tr '\n' ' ')

out="$here/results/$(date +%Y%m%d-%H%M%S)"
mkdir -p "$out"
summary="$out/summary.md"
log() { printf '%s  %s\n' "$(date +%H:%M:%S)" "$*" | tee -a "$out/run.log"; }

# Run a command or shell function with a timeout (macOS has no coreutils `timeout`).
# Stdin is read up front because background jobs don't inherit it.
with_timeout() {
    local s=$1 input pid watcher rc; shift
    input=$(cat)
    ( printf '%s\n' "$input" | "$@" ) & pid=$!
    ( sleep "$s"; kill -TERM "$pid" 2>/dev/null ) & watcher=$!
    wait "$pid"; rc=$?
    kill "$watcher" 2>/dev/null; wait "$watcher" 2>/dev/null
    return $rc
}

# Run a shell script inside Termux's environment as the Termux app user.
# The script body comes on stdin; output goes to stdout.
tsh() {
    local tmp; tmp=$(mktemp)
    {
        echo "export PREFIX=$T_PREFIX HOME=$T_HOME TMPDIR=$T_PREFIX/tmp LANG=en_US.UTF-8"
        echo "export PATH=$T_HOME/.local/bin:$T_PREFIX/bin LD_PRELOAD=$T_PREFIX/lib/libtermux-exec.so"
        echo "cd \$HOME"
        cat
    } >"$tmp"
    "$ADB" push "$tmp" /data/local/tmp/at.sh >/dev/null && rm -f "$tmp"
    "$ADB" shell "run-as $TERMUX_PKG sh -c 'cat /data/local/tmp/at.sh > $T_HOME/.at.sh' && run-as $TERMUX_PKG $T_PREFIX/bin/bash $T_HOME/.at.sh"
}

# --app-context: the same, inside the Termux app process (tsh_app).
# shellcheck source=app-context.sh
. "$here/app-context.sh"
TSH=tsh
[ "$app" = 1 ] && TSH=tsh_app

# Tap "Allow"-style buttons on Android permission dialogs while a step runs.
dialog_watcher() {
    while :; do
        "$ADB" shell uiautomator dump /sdcard/.ui.xml >/dev/null 2>&1
        local b
        b=$("$ADB" shell cat /sdcard/.ui.xml 2>/dev/null |
            grep -oE 'text="(Allow|ALLOW|Wait)"[^>]*bounds="\[[0-9]+,[0-9]+\]\[[0-9]+,[0-9]+\]"' |
            head -1 | grep -oE '[0-9]+' | tail -4 | tr '\n' ' ')
        if [ -n "$b" ]; then
            set -- $b
            "$ADB" shell input tap $(( ($1 + $3) / 2 )) $(( ($2 + $4) / 2 ))
        fi
        sleep 5
    done
}

boot_avd() {
    local avd=$1 gpu try
    if [ -n "$port" ]; then
        "$ADB" devices | grep -q "^$ANDROID_SERIAL" && "$ADB" emu kill >/dev/null 2>&1
        for _ in $(seq 1 30); do pgrep -f "qemu-system.*-port $port" >/dev/null || break; sleep 1; done
    else
        "$ADB" devices | grep -q emulator && "$ADB" emu kill >/dev/null 2>&1
        for _ in $(seq 1 30); do pgrep -f qemu-system >/dev/null || break; sleep 1; done
    fi
    for try in 1 2; do
        # second try: software rendering, which avoids GPU-related boot hangs
        gpu=auto; [ $try = 2 ] && gpu=swiftshader_indirect
        log "booting $avd (gpu=$gpu)"
        "$EMULATOR" -avd "$avd" ${port:+-port $port} -no-snapshot-save -no-boot-anim -gpu $gpu -dns-server 8.8.8.8,1.1.1.1 \
            >"$out/emulator-$avd.log" 2>&1 &
        local booted=0
        for _ in $(seq 1 100); do
            [ "$("$ADB" shell getprop sys.boot_completed 2>/dev/null | tr -d '\r')" = 1 ] && { booted=1; break; }
            sleep 3
        done
        if [ $booted = 1 ]; then
            log "$avd up: Android $("$ADB" shell getprop ro.build.version.release | tr -d '\r')"
            return 0
        fi
        log "$avd didn't boot in 5 min (try $try); killing it"
        "$ADB" emu kill >/dev/null 2>&1; pkill -f "qemu-system.*-avd $avd" 2>/dev/null; sleep 5
    done
    return 1
}

ensure_termux() {
    if [ "$reinstall" = 1 ]; then
        [ -n "$TERMUX_APK" ] || { log "--reinstall-termux needs TERMUX_APK"; return 1; }
        "$ADB" uninstall "$TERMUX_PKG" >/dev/null 2>&1
        "$ADB" install -r "$TERMUX_APK" >/dev/null || { log "couldn't install $TERMUX_APK"; return 1; }
        log "installed Termux $("$ADB" shell dumpsys package $TERMUX_PKG | grep -m1 versionName | tr -d '\r ')"
    fi
    if [ "$x11" = 1 ]; then
        [ -n "$X11_APK" ] || { log "--x11 needs X11_APK (termux-x11-universal-debug.apk)"; return 1; }
        "$ADB" install -r "$X11_APK" >/dev/null || { log "couldn't install $X11_APK"; return 1; }
    fi
    if ! "$ADB" shell pm list packages | grep -q "package:$TERMUX_PKG"; then
        [ -n "$TERMUX_APK" ] || { log "Termux missing and TERMUX_APK not set"; return 1; }
        "$ADB" install -r "$TERMUX_APK" >/dev/null
    elif [ "$fresh" = 1 ]; then
        "$ADB" shell am force-stop "$TERMUX_PKG"
        "$ADB" shell pm clear "$TERMUX_PKG" >/dev/null
    fi
    # exempt Termux from battery optimisation so termux-wake-lock never pops a dialog
    "$ADB" shell dumpsys deviceidle whitelist +$TERMUX_PKG >/dev/null 2>&1

    # Launch Termux exactly once and wait for its first-run bootstrap to finish.
    # Two launches in a row start two bootstraps that break each other.
    local try ok
    for try in 1 2 3; do
        "$ADB" logcat -c
        # right after boot the package manager may not know the activity yet
        for _ in $(seq 1 20); do
            "$ADB" shell cmd package resolve-activity --brief "$TERMUX_PKG" | grep -q TermuxActivity && break
            sleep 3
        done
        "$ADB" shell am start -n "$TERMUX_PKG/.app.TermuxActivity" >/dev/null 2>&1
        ok=0
        for _ in $(seq 1 60); do
            if "$ADB" shell "run-as $TERMUX_PKG ls $T_PREFIX/bin/bash" >/dev/null 2>&1 &&
               ! "$ADB" logcat -d 2>/dev/null | grep -q "Installing Termux bootstrap packages" ||
               "$ADB" logcat -d 2>/dev/null | grep -q "Bootstrap packages installed successfully"; then
                ok=1; break
            fi
            sleep 3
        done
        [ $ok = 1 ] && "$ADB" shell "run-as $TERMUX_PKG ls $T_PREFIX/bin/bash" >/dev/null 2>&1 && break
        log "Termux bootstrap didn't finish (try $try); restarting it"
        "$ADB" shell am force-stop "$TERMUX_PKG"
        "$ADB" shell pm clear "$TERMUX_PKG" >/dev/null
    done
    [ $ok = 1 ] || return 1
    sleep 5
    if [ "$app" = 1 ]; then
        # The Play Store build (targetSdk 29+) can't run its own files
        # outside the app, so set up through the agent too.
        "$ADB" shell device_config set_sync_disabled_for_tests persistent >/dev/null 2>&1
        "$ADB" shell device_config put activity_manager max_phantom_processes 2147483647 >/dev/null 2>&1
        "$ADB" shell settings put global settings_enable_monitor_phantom_procs false >/dev/null 2>&1
        ac_ensure || { log "app-context: no agent in the Termux app"; return 1; }
        "$ADB" shell "run-as $TERMUX_PKG cat $T_HOME/.ac/agent.ctx" >"$out/app-context-$1.txt" 2>&1
    fi
    printf 'pkg install -y proot curl tar xz-utils || { apt-get update && apt-get install -y proot curl tar xz-utils; }\ncommand -v proot curl tar xz\n' |
        $TSH >"$out/termux-setup.log" 2>&1
    grep -q '/proot$' "$out/termux-setup.log" || { log "couldn't install proot in Termux (see termux-setup.log)"; return 1; }
    log "Termux ready"
    return 0
}

push_installer() {
    # Staged via /data/local/tmp as one tarball: a fresh Termux can't read /sdcard yet.
    local stage tarball; stage=$(mktemp -d); tarball=$(mktemp).tar
    (cd "$src" && git archive HEAD | tar -x -C "$stage")
    if [ -n "$modded_dir" ]; then
        # modded runs push their image per combo; only the binaries go here
        mkdir -p "$stage/dist" && cp "$src"/dist/andronix-* "$src"/dist/SHA256SUMS "$stage/dist/" 2>/dev/null
    else
        [ -d "$src/dist" ] && mkdir -p "$stage/dist" && cp "$src"/dist/*aarch64* "$stage/dist/" 2>/dev/null
    fi
    tar -cf "$tarball" -C "$stage" .
    "$ADB" push "$tarball" /data/local/tmp/andronix-src.tar >/dev/null
    "$ADB" shell chmod 644 /data/local/tmp/andronix-src.tar
    rm -rf "$stage" "$tarball"
    $TSH <<'EOF' >"$out/get.log" 2>&1
rm -rf ~/ad && mkdir -p ~/ad && tar -xf /data/local/tmp/andronix-src.tar -C ~/ad && ANDRONIX_SRC=~/ad sh ~/ad/get.sh
andronix version
EOF
    log "installer: $(tail -1 "$out/get.log")"
}

# PNG looks like a drawn desktop: enough distinct colours and not mostly black.
screen_ok() {
    "$venv/bin/python" - "$1" <<'EOF'
import sys
from PIL import Image
im = Image.open(sys.argv[1]).convert("RGB").resize((160, 90))
px = list(im.getdata())
colors = len(set(px))
dark = sum(1 for r, g, b in px if r + g + b < 40) / len(px)
# pure black = nothing drew there (no wallpaper or desktop layer); the Andronix wallpaper is dark but never #000
black = sum(1 for r, g, b in px if max(r, g, b) < 10) / len(px)
print(f"colors={colors} dark={dark:.2f} black={black:.2f}")
sys.exit(0 if colors > 40 and dark < 0.97 and black < 0.5 else 1)
EOF
}

venv="$here/.venv"
if [ ! -x "$venv/bin/vncdo" ]; then
    python3 -m venv "$venv" && "$venv/bin/pip" -q install vncdotool pillow >/dev/null
fi

run_combo() {
    local avd=$1 d=$2 de=$3 dir="$out/$avd/$d-$de" res=PASS note="" t0=$SECONDS
    mkdir -p "$dir"
    log "── $avd  $d  --de $de"

    local rootfs=""
    if [ -n "$modded_dir" ]; then
        local img; img=$(ls "$modded_dir"/"$d"-*-"$de"-modded-aarch64.tar.xz 2>/dev/null | head -1)
        [ -n "$img" ] || { log "   SKIP  no modded image for $d $de"; return; }
        "$ADB" push "$img" "/data/local/tmp/$(basename "$img")" >/dev/null
        [ -f "$img.sha256" ] && "$ADB" push "$img.sha256" "/data/local/tmp/$(basename "$img").sha256" >/dev/null
        "$ADB" shell chmod 644 "/data/local/tmp/$(basename "$img")*"
        rootfs="ANDRONIX_ROOTFS=/data/local/tmp/$(basename "$img")"
    elif ls "$src"/dist/"$d"-*-aarch64.tar.xz >/dev/null 2>&1; then
        rootfs="ANDRONIX_ROOTFS=\$(ls ~/ad/dist/$d-*-aarch64.tar.xz | grep -v modded | head -1)"
    fi

    # 0. start clean
    printf 'andronix remove %s --yes >/dev/null 2>&1; true\n' "$d" | with_timeout 300 $TSH >/dev/null 2>&1

    # 1. install
    if ! printf '%s andronix install %s --de %s --yes --no-start; echo "EXIT=$?"\n' "$rootfs" "$d" "$de" |
         with_timeout "$INSTALL_TIMEOUT" $TSH >"$dir/install.log" 2>&1 ||
       ! grep -q '^EXIT=0' "$dir/install.log"; then
        res=FAIL; note="install failed (see install.log)"
    fi
    local t_install=$((SECONDS - t0))

    # 2. boots
    if [ $res = PASS ]; then
        printf 'andronix start %s -- cat /etc/os-release\n' "$d" | with_timeout 120 $TSH >"$dir/os-release.txt" 2>&1
        grep -q '^PRETTY_NAME=' "$dir/os-release.txt" || { res=FAIL; note="does not boot"; }
    fi

    # 2b. first-boot user, like a real install (the desktop runs as this user, not root)
    if [ $res = PASS ]; then
        printf 'andronix start %s --root -- env ANDRONIX_USER_PASSWORD=%s ANDRONIX_VNC_PASSWORD=%s andronix setup-user --user tester; echo "EXIT=$?"\n' "$d" "$VNC_PASS" "$VNC_PASS" |
            with_timeout 300 $TSH >"$dir/setup-user.log" 2>&1
        grep -q '^EXIT=0' "$dir/setup-user.log" || note="${note:+$note; }setup-user failed (desktop runs as root)"
    fi

    # 3. desktop over VNC
    if [ $res = PASS ] && [ "$de" != none ]; then
        # Keep the session open while we look: proot stops everything it started
        # when the session exits, VNC included.
        printf '%s\n' "ANDRONIX_VNC_PASSWORD=$VNC_PASS andronix start $d -- sh -c 'mkdir -p ~/.vnc && { [ -s ~/.vnc/passwd ] || echo $VNC_PASS | vncpasswd -f > ~/.vnc/passwd; } && chmod 600 ~/.vnc/passwd && ANDRONIX_VNC_PASSWORD=$VNC_PASS vncserver-start 1280x720 && sleep 900'" |
            with_timeout 960 $TSH >"$dir/vnc.log" 2>&1 &
        local vnc_session=$!
        "$ADB" forward tcp:$vnc_local tcp:5901 >/dev/null
        local shot=0
        for _ in $(seq 1 12); do
            sleep 10
            with_timeout 60 "$venv/bin/vncdo" -s localhost::$vnc_local -p "$VNC_PASS" capture "$dir/desktop.png" </dev/null \
                >/dev/null 2>&1 && screen_ok "$dir/desktop.png" >"$dir/screen-check.txt" && { shot=1; break; }
        done
        [ $shot = 1 ] || { res=FAIL; note="desktop not drawn over VNC ($(cat "$dir/screen-check.txt" 2>/dev/null))"; }
        printf 'andronix start %s -- vncserver-stop\n' "$d" | with_timeout 60 $TSH >>"$dir/vnc.log" 2>&1
        kill "$vnc_session" 2>/dev/null; wait "$vnc_session" 2>/dev/null
        printf 'pkill -f "sleep 900" 2>/dev/null; true\n' | with_timeout 30 $TSH >/dev/null 2>&1
    fi
    # 3b. the same desktop on Termux:X11
    if [ $res = PASS ] && [ "$de" != none ] && [ "$x11" = 1 ]; then
        printf 'andronix desktop %s --plain; echo "EXIT=$?"\n' "$d" | with_timeout 600 $TSH >"$dir/x11.log" 2>&1 &
        local x11_session=$! xshot=0
        for _ in $(seq 1 18); do
            sleep 10
            "$ADB" shell dumpsys window 2>/dev/null | grep -E 'mCurrentFocus|mFocusedWindow' | grep -q com.termux.x11 || continue
            "$ADB" exec-out screencap -p >"$dir/x11.png" &&
                screen_ok "$dir/x11.png" >"$dir/x11-screen-check.txt" && { xshot=1; break; }
        done
        sleep 5; "$ADB" exec-out screencap -p >"$dir/x11.png"
        printf 'andronix desktop stop\n' | with_timeout 120 $TSH >"$dir/x11-stop.log" 2>&1
        for _ in $(seq 1 30); do kill -0 "$x11_session" 2>/dev/null || break; sleep 2; done
        kill "$x11_session" 2>/dev/null; wait "$x11_session" 2>/dev/null
        [ $xshot = 1 ] || { res=FAIL; note="${note:+$note; }Termux:X11 desktop not drawn ($(cat "$dir/x11-screen-check.txt" 2>/dev/null))"; }
        grep -q 'has stopped' "$dir/x11.log" || note="${note:+$note; }desktop stop didn't end the session cleanly"
        "$ADB" shell am start -n "$TERMUX_PKG/.app.TermuxActivity" >/dev/null 2>&1
    fi
    "$ADB" exec-out screencap -p >"$dir/android.png"

    # Guest-side desktop logs, before anything is removed
    if [ "$de" != none ]; then
        $TSH >"$dir/guest-logs.txt" 2>&1 <<EOF
R=\$HOME/.andronix/distros/$d/rootfs
for f in \$R/root/.vnc/*.log \$R/home/*/.vnc/*.log \$R/root/.xsession-errors \$R/home/*/.xsession-errors \$R/root/.local/share/xorg/*.log \$R/tmp/*.log; do
  [ -f "\$f" ] && { echo "===== \${f#\$R}"; tail -120 "\$f"; }
done
EOF
    fi

    # 4. remove
    if [ $keep = 0 ]; then
        printf 'andronix remove %s --yes; ls -d ~/.andronix/distros/%s ~/start-%s.sh 2>/dev/null | wc -l\n' "$d" "$d" "$d" |
            with_timeout 300 $TSH >"$dir/remove.log" 2>&1
        [ "$(tail -1 "$dir/remove.log" | tr -d '\r ')" = 0 ] || { res=FAIL; note="${note:+$note; }remove left files behind"; }
    fi

    local secs=$((SECONDS - t0))
    log "   $res  ${secs}s  $note"
    printf '| %s | %s | %s | %s | %ss | %ss | %s |\n' "$avd" "$d" "$de" "$res" "$t_install" "$secs" "$note" >>"$summary"
}

{
    echo "# Emulator test run $(basename "$out")"
    echo
    echo "Installer: \`$(cd "$src" && git rev-parse --short HEAD) $(cd "$src" && git log -1 --format=%s)\`"
    [ "$app" = 1 ] && echo && echo "Ran inside the Termux app process (--app-context); each emulator's agent context is in app-context-<avd>.txt."
    echo
    echo "| Emulator | Distro | Desktop | Result | Install | Total | Notes |"
    echo "|---|---|---|---|---|---|---|"
} >"$summary"

for avd in ${avds//,/ }; do
    boot_avd "$avd" || { log "skipping $avd (no boot)"; continue; }
    ensure_termux "$avd" || { log "skipping $avd"; continue; }
    push_installer
    dialog_watcher & watcher=$!
    for d in $distros; do
        for de in $des; do
            run_combo "$avd" "$d" "$de"
        done
    done
    kill "$watcher" 2>/dev/null
done

log "summary: $summary"
cat "$summary"
ran=$(grep -cE '\| (PASS|FAIL) \|' "$summary")
[ "$ran" -gt 0 ] || { log "no tests ran"; exit 1; }
grep -q '| FAIL |' "$summary" && exit 1 || exit 0
