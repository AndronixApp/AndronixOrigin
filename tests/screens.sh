#!/usr/bin/env bash
# Install one distro + desktop in a fresh Termux-like Docker box, check it
# really works, and save review screenshots to docs/screens/:
#   <distro>-<de>-desktop.png   desktop right after login (over VNC)
#   <distro>-<de>-menu.png      the app menu opened
#   <distro>-<de>-apps.png      a terminal and a file manager side by side
#   <distro>-<de>-install.txt   last 40 lines of the plain installer output
# and updates docs/screens/INDEX.md. Pass/fail comes from the checks,
# never from the screenshots.
#
#   tests/screens.sh debian xfce
#   ANDRONIX_ROOTFS=dist/debian-13-aarch64.tar.xz tests/screens.sh debian xfce
set -u
cd "$(dirname "$0")/.." || exit 1
distro=${1:?usage: screens.sh <distro> <desktop>}
de=${2:?usage: screens.sh <distro> <desktop>}
out=docs/screens
box="andronix-screens-$distro-$de"
mkdir -p "$out"
[ -x dist/andronix-linux-aarch64 ] || NO_NDK=1 ci/build-go.sh >/dev/null

family=$(sed -n 's/^DISTRO_FAMILY=//p' "distros/$distro.conf")
case "$family" in
    apt) tools="x11-apps netpbm xdotool" inst="apt-get install -y -qq --no-install-recommends" ;;
    dnf) tools="xwd xorg-x11-apps netpbm-progs xdotool" inst="dnf -y -q install --skip-unavailable" ;;
    pacman) tools="xorg-xwd netpbm xdotool" inst="pacman -S --noconfirm --needed" ;;
    apk) tools="xwd netpbm netpbm-extras xdotool" inst="apk add -q" ;;
    xbps) tools="xwd netpbm xdotool" inst="xbps-install -y" ;;
esac
# Per desktop: window-manager process, menu command, terminal, file manager.
case "$de" in
    xfce) wm=xfwm4 menu="xdotool mousemove 40 12 click 1" term=xfce4-terminal fm=thunar ;;
    lxqt) wm=openbox menu="xdotool mousemove 14 704 click 1" term=qterminal fm=pcmanfm-qt ;;
    lxde) wm=openbox menu="lxpanelctl menu" term=lxterminal fm=pcmanfm ;;
    mate) wm=marco menu="xdotool key alt+F1" term=mate-terminal fm=caja ;;
    kde) wm=kwin_x11 menu="xdotool mousemove 22 700 click 1" term=konsole fm=dolphin ;;
    *) echo "no screenshot recipe for $de"; exit 1 ;;
esac

docker build -q -t andronix-test -f tests/Dockerfile tests >/dev/null 2>&1
docker rm -f "$box" >/dev/null 2>&1
docker run -d --init --name "$box" -v "$PWD":/src:ro andronix-test sleep infinity >/dev/null
trap 'docker rm -f "$box" >/dev/null 2>&1' EXIT
run() { docker exec -e COLUMNS=60 "$box" bash -c "$1"; }

rootfs_env=""
[ -n "${ANDRONIX_ROOTFS:-}" ] && rootfs_env="ANDRONIX_ROOTFS=/src/$ANDRONIX_ROOTFS"
status=PASS note=""
run "ANDRONIX_SRC=/src sh /src/get.sh >/dev/null && $rootfs_env ~/.local/bin/andronix install $distro --de $de --yes --no-start" >/tmp/andronix-screens.log 2>&1
rc=$?
tail -n 40 /tmp/andronix-screens.log >"$out/$distro-$de-install.txt"
A='/home/termux/.local/bin/andronix'
if [ "$rc" != 0 ]; then
    status=FAIL note="install exited $rc: $(grep -m1 '^error:' /tmp/andronix-screens.log)"
else
    note="$(grep -Eo 'Installing [A-Za-z]+ desktop \([^)]*\) [0-9]+ packages' /tmp/andronix-screens.log | head -1)"
    # Scripted first-boot user, then the checks that decide pass/fail.
    run "$A start $distro --root -- env ANDRONIX_USER_PASSWORD=andronix ANDRONIX_VNC_PASSWORD=andronix andronix setup-user --user alex" >/dev/null 2>&1 ||
        { status=FAIL; note="$note; setup-user failed"; }
    [ "$(run "$A start $distro -- id -un" 2>/dev/null | tail -1)" = alex ] || { status=FAIL; note="$note; login user is not alex"; }
    case "$(sed -n 's/^DISTRO_SUDO=//p' "distros/$distro.conf")" in
        none) note="$note; no sudo (DISTRO_SUDO=none)" ;;
        nopasswd) run "$A start $distro -- sudo -n true" >/dev/null 2>&1 || { status=FAIL; note="$note; sudo failed"; } ;;
        *) run "$A start $distro -- sh -c 'echo andronix | sudo -S -p \"\" true'" >/dev/null 2>&1 || { status=FAIL; note="$note; sudo failed"; } ;;
    esac
    # Capture tools (test only).
    # One at a time: a missing name must not block the others.
    run "$A start $distro --root -- sh -c 'for p in $tools; do $inst \$p >/dev/null 2>&1; done; command -v xwd >/dev/null && command -v pnmtopng >/dev/null'" || note="$note; capture tools missing"
    shot='xwd -root -silent | xwdtopnm 2>/dev/null | pnmtopng'
    run "$A start $distro -- sh -c '
        vncserver-start 1280x720 >/tmp/vnc.txt 2>&1 || { cat /tmp/vnc.txt; exit 3; }
        export DISPLAY=:1
        # Wait until the desktop is drawn (an all-black screen compresses
        # to a few hundred bytes), up to 90 s.
        for i in \$(seq 1 45); do
            sleep 2; $shot >/tmp/s-desktop.png
            [ \$(wc -c </tmp/s-desktop.png) -gt 4000 ] && break
        done
        sleep 3; $shot >/tmp/s-desktop.png
        pgrep -x $wm >/dev/null || { echo no-wm; exit 4; }
        ($menu) >/dev/null 2>&1; sleep 3; $shot >/tmp/s-menu.png; xdotool key Escape; sleep 1
        $term >/dev/null 2>&1 & sleep 4; $fm >/dev/null 2>&1 & sleep 6
        t=\$(xdotool search --onlyvisible --class $term 2>/dev/null | tail -1)
        f=\$(xdotool search --onlyvisible --class $fm 2>/dev/null | tail -1)
        [ -n \"\$t\" ] && xdotool windowsize \$t 620 600 windowmove \$t 10 40
        [ -n \"\$f\" ] && xdotool windowsize \$f 620 600 windowmove \$f 650 40
        xdotool mousemove 640 700; sleep 2; $shot >/tmp/s-apps.png
        [ -n \"\$t\" ] && [ -n \"\$f\" ] || echo apps-missing
        vncserver-stop 1 >/dev/null'" >/tmp/andronix-screens-vnc.log 2>&1
    vrc=$?
    case "$vrc" in
        0) ;;
        3) status=FAIL note="$note; vncserver-start failed" ;;
        4) status=FAIL note="$note; $wm not running" ;;
        *) status=FAIL note="$note; desktop session exit $vrc" ;;
    esac
    grep -q apps-missing /tmp/andronix-screens-vnc.log && note="$note; terminal or file manager didn't open"
    # Plasma sets the wallpaper from an update script at first login.
    if [ "$de" = kde ] && ! run "grep -q andronix.png /home/termux/.andronix/distros/$distro/rootfs/home/alex/.config/plasma-org.kde.plasma.desktop-appletsrc"; then
        status=FAIL note="$note; Andronix wallpaper not set"
    fi
    tmp="/home/termux/.andronix/distros/$distro/rootfs/tmp"
    for k in desktop menu apps; do
        docker exec "$box" cat "$tmp/s-$k.png" >"$out/$distro-$de-$k.png" 2>/dev/null
        [ -s "$out/$distro-$de-$k.png" ] || rm -f "$out/$distro-$de-$k.png"
    done
fi
note=${note#; }

# INDEX.md: one row per combo (the header is kept as written).
index="$out/INDEX.md"
pt=$(grep -Eo 'Installing [A-Za-z]+ desktop \(([^)]*)\) ([0-9]+) packages' /tmp/andronix-screens.log | sed -E 's/.*\(([^)]*)\) ([0-9]+) packages/\2 · \1/' | head -1)
note=$(printf '%s' "$note" | sed -E 's/Installing [A-Za-z]+ desktop \([^)]*\) [0-9]+ packages;? ?//')
row="| $distro-$de | $status | $(date +%F) | Go binary | ${pt:-} | ${note:-ok} |"
grep -v "^| $distro-$de |" "$index" >"$index.tmp"
printf '%s\n' "$row" >>"$index.tmp"
mv "$index.tmp" "$index"
echo "$row"
[ "$status" = PASS ]
