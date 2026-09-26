#!/usr/bin/env bash
# shellcheck disable=SC2088,SC2016,SC2015,SC1111  # commands run inside the container
# The Termux:X11 desktop (andronix desktop) in the Docker box, with Xvfb
# standing in for termux-x11 (tests/x11/fake-termux-x11).
#
#   tests/x11.sh                     errors and messages, then XFCE on :0
#   DESKTOPS="xfce lxqt mate kde" tests/x11.sh   each desktop in turn
#   DISTRO=ubuntu tests/x11.sh       another distro (dist/<tarball> if any)
#
# Screenshots go to dist/x11-<distro>-<desktop>.png. The Termux:X11 app
# itself and PulseAudio need a phone (tests/emulator).
set -u
cd "$(dirname "$0")/.." || exit 1
root=$PWD
distro=${DISTRO:-debian}
box=andronix-x11-$$  # unique: containers are shared across agents
pass=0 fail=0

ok() { pass=$((pass + 1)); printf '  \033[32m✓\033[0m %s\n' "$1"; }
bad() { fail=$((fail + 1)); printf '  \033[31m✗\033[0m %s\n' "$1"; }
run() { docker exec -e COLUMNS=60 "$box" bash -c "$1"; }
check() { # check NAME COMMAND
    if run "$2" >/tmp/andronix-x11.out 2>&1; then ok "$1"; else bad "$1"; sed 's/^/      /' /tmp/andronix-x11.out | tail -15; fi
}

docker build -q -t andronix-test-x11-base -f tests/Dockerfile tests >/dev/null &&
    docker build -q -t andronix-test-x11 tests/x11 >/dev/null || { echo "docker build failed"; exit 1; }
docker rm -f "$box" >/dev/null 2>&1
docker run -d --init --name "$box" -v "$root":/src:ro andronix-test-x11 sleep infinity >/dev/null
[ -n "${KEEP:-}" ] || trap 'docker rm -f "$box" >/dev/null 2>&1' EXIT  # KEEP=1 keeps the box

case $distro in
debian) tarball=$(ls dist/debian-13-aarch64.tar.xz 2>/dev/null) ;;
ubuntu) tarball=$(ls dist/ubuntu-26.04-aarch64.tar.xz 2>/dev/null) ;;
*) tarball=$(ls dist/"$distro"-*-aarch64.tar.xz 2>/dev/null | head -1) ;;
esac
src_env=""
[ -n "$tarball" ] && src_env="ANDRONIX_ROOTFS=/src/$tarball"
NO_NDK=1 ci/build-go.sh >/dev/null || { echo "build failed"; exit 1; }
A="\$HOME/.local/bin/andronix"
echo "andronix Termux:X11 test ($distro, ${tarball:-registry fallback})"

check "get.sh installs the binary" "ANDRONIX_SRC=/src sh /src/get.sh && $A version"
check "help lists andronix desktop" "COLUMNS=40 $A help | grep -q 'andronix desktop'"
check "desktop with nothing installed explains" "out=\$($A desktop 2>&1); [ \$? = 1 ] && echo \"\$out\" | grep -q 'No desktop installed yet'"
check "install $distro (command line)" "$src_env $A install $distro --de none --yes --no-start"
check "desktop on a command-line install explains" "out=\$($A desktop $distro 2>&1); [ \$? = 1 ] && echo \"\$out\" | grep -q 'has no desktop'"
check "start --x11 is the same command" "out=\$($A start $distro --x11 2>&1); [ \$? = 1 ] && echo \"\$out\" | grep -q 'has no desktop'"
check "desktop inside a distro says run it in Termux" "out=\$($A start $distro -- andronix desktop 2>&1); echo \"\$out\" | grep -q 'Run this in Termux'"
check "desktop stop with nothing running is fine" "$A desktop stop | grep -q 'Nothing was running'"

# Each desktop and the process that proves it's up (bash 3: no maps).
for de in ${DESKTOPS:-xfce}; do
    case $de in xfce) wm=xfwm4 ;; lxqt) wm=lxqt-panel ;; mate) wm=mate-panel ;; kde) wm=plasmashell ;; *) wm=$de ;; esac
    echo "  -- $de"
    check "install $distro --de $de" "$src_env $A install $distro --de $de --reinstall --yes --no-start --no-browser >/tmp/install-$de.log 2>&1 || { tail -20 /tmp/install-$de.log; false; }"
    check "first-boot user alex" "$A start $distro --root -- env ANDRONIX_USER_PASSWORD=pw ANDRONIX_VNC_PASSWORD=andronix andronix setup-user --user alex >/dev/null"
    check "xstartup runs session-prep (same flow as VNC)" "grep -q 'andronix session-prep' ~/.andronix/distros/$distro/rootfs/home/alex/.config/tigervnc/xstartup"

    run "echo no-app > ~/.fake-x11"
    check "missing Termux:X11 app: download link, exit 1" "out=\$($A desktop $distro --plain 2>&1); [ \$? = 1 ] && echo \"\$out\" | grep -q 'termux-x11-universal-debug.apk' && echo \"\$out\" | grep -q 'releases/tag/nightly' && ! test -e ~/.andronix/x11.state"
    run "echo signature > ~/.fake-x11"
    check "app/package mismatch explained" "out=\$($A desktop $distro --plain 2>&1); [ \$? = 1 ] && echo \"\$out\" | grep -q \"doesn't match\""
    run "rm -f ~/.fake-x11"

    run "setsid $A desktop $distro --plain >/tmp/desktop.log 2>&1 < /dev/null &"
    check "desktop comes up on :0 ($de: $wm)" "for i in \$(seq 90); do pgrep -x $wm >/dev/null && break; sleep 2; done; pgrep -x $wm >/dev/null && grep -q 'Desktop is running' /tmp/desktop.log"
    check "session has DISPLAY=:0 and PULSE_SERVER" "pid=\$(pgrep -x $wm | head -1); tr '\\0' '\\n' </proc/\$pid/environ | grep -qx DISPLAY=:0 && tr '\\0' '\\n' </proc/\$pid/environ | grep -qx PULSE_SERVER=127.0.0.1"
    check "session has LIBGL_DRI3_DISABLE=1 (Mesa's DRI3 path hangs on Termux:X11)" "pid=\$(pgrep -x $wm | head -1); tr '\\0' '\\n' </proc/\$pid/environ | grep -qx LIBGL_DRI3_DISABLE=1"
    [ "$de" = xfce ] && check "XFCE wallpaper default covers Termux:X11's monitor (builtin)" "grep -q monitorbuiltin ~/.andronix/distros/$distro/rootfs/etc/xdg/xfce4/xfconf/xfce-perchannel-xml/xfce4-desktop.xml"
    check "session runs as the first-boot user" "grep -q 'as alex on :0' ~/.andronix/distros/$distro/rootfs/tmp/andronix-session-*.log"
    check "a second session sees :0 through the bound socket" "~/start-$distro.sh 'test -S /tmp/.X11-unix/X0'"
    check "desktop again while running: says so, exit 0" "$A desktop $distro --plain | grep -q 'already running'"
    # A desktop that drew nothing (black root window) fails; the Andronix
    # wallpaper alone averages well above this. KDE's splash is black too,
    # so wait for the desktop (up to 90 s).
    check "the screen isn't black (after a second session started)" "for i in \$(seq 45); do m=\$(DISPLAY=:0 xwd -root -silent | xwdtopnm 2>/dev/null | ppmtopgm | pamsumm -mean -brief); [ \"\${m%%.*}\" -ge 8 ] && break; sleep 2; done; echo mean=\$m; [ \"\${m%%.*}\" -ge 8 ]"
    sleep 5
    run "DISPLAY=:0 xwd -root -silent | xwdtopnm 2>/dev/null | pnmtopng > /tmp/x11.png"
    mkdir -p dist && docker exec "$box" cat /tmp/x11.png >"dist/x11-$distro-$de.png" 2>/dev/null &&
        [ -s "dist/x11-$distro-$de.png" ] && ok "screenshot dist/x11-$distro-$de.png" || bad "screenshot"
    # Programs that exist but couldn't be run (optional ones that aren't
    # installed, like gnome-keyring-daemon, don't count).
    check "no failed launches in the session log" "R=~/.andronix/distros/$distro/rootfs; bad=; for n in \$(grep -ho 'Failed to execute child process “[^”]*”' \$R/tmp/andronix-session-*.log | sed 's/.*“//; s/”//' | sort -u); do case \$n in /*) p=\$R\$n ;; *) p=\$R/usr/bin/\$n ;; esac; [ -e \"\$p\" ] && bad=\"\$bad \$n\"; done; echo \"exists but failed:\$bad\"; [ -z \"\$bad\" ]"
    check "desktop stop ends the session and the server" "$A desktop stop | grep -q 'Stopped' && for i in \$(seq 20); do pgrep -x Xvfb >/dev/null || break; sleep 0.5; done; for i in \$(seq 20); do pgrep -x $wm >/dev/null || break; sleep 0.5; done; ! pgrep -x Xvfb && ! pgrep -x $wm && ! pgrep -x dbus-daemon && ! test -e ~/.andronix/x11.state && ! test -e /tmp/.X11-unix/X0"
    check "foreground run finished cleanly" "for i in \$(seq 20); do grep -q 'has stopped' /tmp/desktop.log && break; sleep 0.5; done; grep -q 'has stopped' /tmp/desktop.log"
    check "VNC still works after Termux:X11" "~/start-$distro.sh 'vncserver-start 1280x720 >/dev/null && sleep 12 && pgrep -x $wm >/dev/null && vncserver-stop 1 >/dev/null'"
done

echo
echo "  $pass passed, $fail failed"
[ "$fail" = 0 ]
