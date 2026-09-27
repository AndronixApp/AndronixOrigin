#!/usr/bin/env bash
# Firefox with the installer's phone prefs (firefoxPrefs in
# internal/app/desktop.go, written to defaults/pref/andronix-phone.js) must
# still play media: H.264 and VP9 video, AAC, Opus and MP3 audio, and draw
# a canvas. Prefs that move media into the parent process broke this
# (2.0.1-rc1: every video NotSupportedError, AAC never played).
#
#   tests/firefox-media.sh            Debian 13's firefox-esr (headless, Docker)
set -euo pipefail
cd "$(dirname "$0")/.."
work=$(mktemp -d "$HOME/.andronix-ffmedia.XXXXXX")   # under $HOME: colima mounts only that
trap 'rm -rf "$work"' EXIT
cp tests/firefox-media/media.html "$work/"
# The prefs exactly as the installer writes them.
sed -n '/^const firefoxPrefs = `/,/^`/p' internal/app/desktop.go | sed '1s/^const firefoxPrefs = `//; $d' >"$work/andronix-phone.js"
grep -q '^pref(' "$work/andronix-phone.js" || { echo "couldn't read firefoxPrefs"; exit 1; }
cat >"$work/run.sh" <<'IN'
set -e
apt-get update -qq >/dev/null 2>&1
apt-get install -y -qq --no-install-recommends firefox-esr libavcodec-extra ffmpeg python3 >/dev/null 2>&1
cd /w
ffmpeg -loglevel error -y -f lavfi -i testsrc=duration=5:size=320x240:rate=25 -c:v libx264 -pix_fmt yuv420p t.mp4
ffmpeg -loglevel error -y -f lavfi -i testsrc=duration=5:size=320x240:rate=25 -c:v libvpx-vp9 t.webm
ffmpeg -loglevel error -y -f lavfi -i sine=duration=5 -c:a aac a.m4a
ffmpeg -loglevel error -y -f lavfi -i sine=duration=5 -c:a libopus a.opus.webm
ffmpeg -loglevel error -y -f lavfi -i sine=duration=5 a.mp3
cp /w/andronix-phone.js /usr/lib/firefox-esr/defaults/pref/andronix-phone.js
python3 -m http.server 8000 >/tmp/http.log 2>&1 &
sleep 1
HOME=/tmp timeout 60 firefox-esr --headless --no-remote --profile "$(mktemp -d)" http://127.0.0.1:8000/media.html >/dev/null 2>&1 || true
grep -a -o 'result?[^ ]*' /tmp/http.log | tail -1 | sed 's/^result?//' | tr '&' '\n'
IN
out=$(docker run --rm --name "andronix-ffmedia-$$" -v "$work":/w debian:trixie bash /w/run.sh 2>&1 | tail -8)
echo "$out" | sed 's/^/  /'
fail=0
for k in t.mp4 t.webm a.m4a a.opus.webm a.mp3 canvas2d; do
    echo "$out" | grep -q "^$k=ok" || { echo "  FAIL: $k"; fail=1; }
done
[ $fail = 0 ] && echo "firefox media: all play" || exit 1
