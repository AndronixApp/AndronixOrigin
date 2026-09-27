#!/bin/sh
# Inside a distro (andronix start <distro> -- sh /tmp/ff-check.sh PORT):
# Firefox loads a real http page (web content processes, not file://) that
# plays H.264, VP9, AAC, Opus and MP3. It draws into a real X server (a
# private Xvnc), not headless: Ubuntu's Firefox 156 only crashed its
# content processes when drawing ("Shared memory PlatformHandle is not safe
# to map"). Prints RESULT lines; "RESULT done" only if the page ran to the end.
port=$1
. /etc/profile.d/andronix.sh 2>/dev/null   # the installer's Firefox switches
for v in ${FF_UNSET:-}; do unset "$v"; done   # e.g. FF_UNSET=MOZ_SHM_NO_SEALS: the check without one
FF=$(command -v firefox-esr || command -v firefox)
[ -n "$FF" ] || { echo "RESULT no-browser"; exit 0; }
P=$(mktemp -d)
echo 'user_pref("browser.dom.window.dump.enabled", true);' >"$P/user.js"
echo "RESULT browser=$($FF --version 2>/dev/null | tr ' ' '_')"
mode=--headless
if [ -n "${FF_DISPLAY:-}" ]; then
    # A running desktop's display, e.g. Termux:X11's :0 (andronix desktop)
    export DISPLAY=$FF_DISPLAY
    mode=
    echo "RESULT display=$FF_DISPLAY"
elif command -v Xvnc >/dev/null 2>&1; then
    Xvnc :17 -geometry 1024x768 -depth 24 -SecurityTypes None -localhost >/tmp/ff-check-x.log 2>&1 &
    x=$!
    sleep 3
    export DISPLAY=:17
    mode=
    echo "RESULT display=xvnc"
fi
timeout 100 "$FF" $mode --no-remote --profile "$P" "http://127.0.0.1:$port/media-each.html" >/tmp/ff-check.log 2>&1
grep -a '^RESULT' /tmp/ff-check.log
echo "CRASHES $(grep -a -c 'not safe to map' /tmp/ff-check.log)"
[ -n "${x:-}" ] && kill "$x" 2>/dev/null
rm -rf "$P"
