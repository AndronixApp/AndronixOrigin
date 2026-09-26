#!/usr/bin/env bash
# shellcheck disable=SC2088,SC2016,SC2015  # commands run inside the container
# End-to-end smoke test in a Termux-like Docker box (tests/Dockerfile).
#
#   tests/smoke.sh            CLI-only install, start, legacy compat, backup,
#                             restore, remove (Go binary from ci/build-go.sh)
#   FULL=1 tests/smoke.sh     also XFCE + VNC (a few minutes; saves a
#                             screenshot to dist/desktop.png)
#
# Uses dist/debian-13-<arch>.tar.xz when present (ci/build-rootfs.sh),
# otherwise the registry fallback.
set -u
cd "$(dirname "$0")/.." || exit 1
root=$PWD
img=andronix-test
box=andronix-smoke-$$
pass=0 fail=0

ok() { pass=$((pass + 1)); printf '  \033[32m✓\033[0m %s\n' "$1"; }
bad() { fail=$((fail + 1)); printf '  \033[31m✗\033[0m %s\n' "$1"; }
run() { docker exec -e COLUMNS=60 "$box" bash -c "$1"; }
check() { # check NAME COMMAND
    if run "$2" >/tmp/andronix-smoke.out 2>&1; then ok "$1"; else bad "$1"; sed 's/^/      /' /tmp/andronix-smoke.out | tail -15; fi
}

docker build -q -t "$img" -f tests/Dockerfile tests >/dev/null || { echo "docker build failed"; exit 1; }
docker rm -f "$box" >/dev/null 2>&1
docker run -d --init --name "$box" -v "$root":/src:ro "$img" sleep infinity >/dev/null
trap 'docker rm -f "$box" >/dev/null 2>&1' EXIT

tarball=$(ls dist/debian-13-aarch64.tar.xz 2>/dev/null | head -1)
src_env=""
[ -n "$tarball" ] && src_env="ANDRONIX_ROOTFS=/src/$tarball"

[ -x dist/andronix-linux-aarch64 ] || NO_NDK=1 ci/build-go.sh >/dev/null
A="\$HOME/.local/bin/andronix"
check "get.sh installs the binary" "ANDRONIX_SRC=/src sh /src/get.sh && $A version"
echo "andronix smoke test (${tarball:-registry fallback})"
check "help and list render at 40 columns" "COLUMNS=40 $A help >/dev/null && COLUMNS=40 $A list >/dev/null"
check "NO_COLOR has no escapes" "! NO_COLOR=1 ANDRONIX_COLOR=always $A help | grep -q \$'\\033'"
check "unknown distro gives a friendly error" "out=\$($A install nope 2>&1); [ \$? = 1 ] && echo \"\$out\" | grep -q 'Unknown distro'"

# A legacy install: old rootfs folder and launcher must survive.
run 'mkdir -p ~/debian-fs/root ~/debian-binds && printf "#!/bin/bash\ncd \$(dirname \$0)\n# old launcher\n" > ~/start-debian.sh'
run 'echo "command+=\" -b /src:/mnt/src\"" > ~/debian-binds/src.sh'

# A fake am records the app's install-result broadcast (ANDRONIX_INSTALL_ID).
run 'mkdir -p ~/fakebin && printf "#!/bin/sh\necho \"\$*\" >>~/am.log\n" >~/fakebin/am && chmod +x ~/fakebin/am'
AM="PATH=~/fakebin:\$PATH ANDRONIX_INSTALL_ID=0123456789abcdef"
check "install debian (command line)" "$AM $src_env $A install debian --de none --yes --no-start"
check "install reports ok to the app" "sleep 1; grep -q -- '-n studio.com.techriz.andronix/.receivers.InstallResultReceiver -a studio.com.techriz.andronix.INSTALL_RESULT --es id 0123456789abcdef --es status ok --es distro debian --es de none --es version' ~/am.log"
check "failed install reports the step" "rm -f ~/am.log; ! $AM ANDRONIX_ROOTFS=/nope.tar.xz $A install alpine --de none --yes --no-start >/dev/null 2>&1; sleep 1; grep -q -- '--es status fail --es distro alpine --es de none --es step ' ~/am.log && rm -rf ~/.andronix/distros/alpine"
check "no broadcast without a valid install id" "rm -f ~/am.log; PATH=~/fakebin:\$PATH ANDRONIX_INSTALL_ID='0123456789abcdef;x' $A install nope >/dev/null 2>&1; $A install debian --de none --yes --no-start >/dev/null; sleep 1; ! test -e ~/am.log"
check "legacy launcher kept as start-debian-old.sh" "grep -q 'old launcher' ~/start-debian-old.sh && [ -d ~/debian-fs/root ]"
check "start-debian.sh runs a command" "~/start-debian.sh 'grep -q trixie /etc/os-release'"
check "start passes PULSE_SERVER and DISPLAY" "~/start-debian.sh 'echo \$PULSE_SERVER \$DISPLAY' | grep -q '127.0.0.1 :1'"
check "start keeps argv unchanged" "[ \"\$(~/start-debian.sh printf '%s|' 'a b' c)\" = 'a b|c|' ] && ~/start-debian.sh sh -c 'mkdir -p /tmp/q && test -d /tmp/q'"
check "one argument is a shell command line" "~/start-debian.sh 'true && echo ok' | grep -qx ok"
check "legacy debian-binds are honoured" "~/start-debian.sh 'test -f /mnt/src/DESIGN.md'"
check "apt works inside (sandbox user fix)" "~/start-debian.sh 'apt-get -o Dpkg::Use-Pty=0 install -y -qq less >/dev/null && command -v less'"
check "desktop (Termux:X11) on a command-line install explains" "out=\$($A desktop debian 2>&1); [ \$? = 1 ] && echo \"\$out\" | grep -q 'has no desktop' && out=\$($A start debian --x11 2>&1); echo \"\$out\" | grep -q 'has no desktop'"
check "desktop stop with nothing running is fine" "$A desktop stop | grep -q 'Nothing was running'"
check "second install is a no-op" "$A install debian --de none --yes --no-start | grep -q 'already installed'"
# Telemetry: the notice once, an id, never a delay or failure (the URL is
# a closed port), and off for good with 'telemetry off'.
T="ANDRONIX_NO_TELEMETRY= ANDRONIX_TELEMETRY_URL=http://127.0.0.1:9/v1/events"
check "telemetry: one-line notice once, anonymous id, no failure" "out=\$($T $A install debian --de none --yes --no-start 2>&1) && echo \"\$out\" | grep -q 'telemetry off' && grep -qE '^ID=[0-9a-f]{32}$' ~/.andronix/telemetry && ! $T $A install debian --de none --yes --no-start 2>&1 | grep -q 'telemetry off'"
check "telemetry off is remembered" "$T $A telemetry off >/dev/null && $T $A telemetry status | grep -q 'off' && grep -qx 'ENABLED=no' ~/.andronix/telemetry"
# Dev packs, with small test packs (ANDRONIX_DATA overlay) so this stays
# quick: alternatives, optionals, files, post, check, shared packages.
run 'd=~/packdata/mods/packs; mkdir -p $d/files
printf "#!/bin/sh\necho hello from a pack\n" >$d/files/hello
printf "%s\n" "PACK_ID=tinya" "PACK_NAME=\"Tiny A\"" "PACK_DESC=\"test pack\"" "PACK_PKGS_apt=\"not-a-package|jq tree not-there?\"" \
  "PACK_FILES=\"hello:/usr/local/bin/andronix-hello:755\"" "PACK_POST=\"touch /etc/andronix-tinya-post\"" \
  "PACK_CHECK=\"jq --version && tree --version && andronix-hello\"" "PACK_HINT=\"hint for tiny a\"" "PACK_DISK_MB=5" >$d/tinya.conf
printf "%s\n" "PACK_ID=tinyb" "PACK_NAME=\"Tiny B\"" "PACK_PKGS_apt=\"jq\"" "PACK_CHECK=\"jq --version\"" "PACK_DISK_MB=5" >$d/tinyb.conf
printf "%s\n" "PACK_ID=tinyc" "PACK_NAME=\"Tiny C\"" "PACK_PKGS_apt=\"not-a-package\"" "PACK_CHECK=true" "PACK_DISK_MB=5" >$d/tinyc.conf
cp -r ~/packdata ~/.andronix/distros/debian/rootfs/tmp/'
P="ANDRONIX_DATA=~/packdata $A pack"
S='~/.andronix/distros/debian/rootfs/etc/andronix/packs'
check "pack add from Termux (alternatives, optional, files, post, check)" "$P add debian tinya | grep -q 'hint for tiny a' && grep -qx 'PACKAGES=\"jq tree\"' $S/tinya && ~/start-debian.sh 'andronix-hello && test -f /etc/andronix-tinya-post'"
check "pack add inside the distro records only new packages" "$A start debian --root -- env ANDRONIX_DATA=/tmp/packdata andronix pack add tinyb >/dev/null && grep -qx 'PACKAGES=\"\"' $S/tinyb && grep -qx 'NEEDS=\"jq\"' $S/tinyb"
check "pack list marks added packs" "$P list debian | grep -q '✓ tinya'"
check "pack remove keeps packages another pack uses" "$P remove debian tinya >/dev/null && ! test -e $S/tinya && ~/start-debian.sh '! command -v tree && ! test -e /usr/local/bin/andronix-hello && jq --version'"
check "pack remove takes out what it installed" "$P remove debian tinyb >/dev/null && ~/start-debian.sh 'jq --version' 2>&1 | grep -q 'not found'"
check "missing package and unknown pack give friendly errors" "out=\$($P add debian tinyc 2>&1); [ \$? = 1 ] && echo \"\$out\" | grep -q \"isn't available\" && ! test -e $S/tinyc && $P add debian nope 2>&1 | grep -q 'Unknown pack'"
check "backup" "$A backup debian ~/b.tar.gz && test -s ~/b.tar.gz"
check "remove (keeps legacy)" "$A remove debian --yes && ! test -e ~/.andronix/distros/debian && ! test -e ~/start-debian.sh && test -d ~/debian-fs"
check "restore" "$A restore ~/b.tar.gz --yes && ~/start-debian.sh 'perl -e 1 && command -v less'"
check "backup is portable: no .l2s files or host-path links" "$A start debian --root -- sh -c 'apt-get install -y -qq perl >/dev/null 2>&1' && find ~/.andronix/distros/debian/rootfs -name '.l2s.*' | grep -q . && $A backup debian ~/p.tar.gz >/dev/null && ! tar -tvzf ~/p.tar.gz | grep -qE '\.l2s\.|-> /home/termux'"
check "restore to another ANDRONIX_HOME: perl and dpkg work" "ANDRONIX_HOME=~/moved $A restore ~/p.tar.gz --yes >/dev/null && ANDRONIX_HOME=~/moved $A start debian --root -- sh -c 'perlbug --version >/dev/null && dpkg --verify perl && dpkg -l | grep -q ^ii' && ! find ~/moved -type l -lname '/home/termux/*' | grep -q . && rm -rf ~/moved"

if [ -n "${FULL:-}" ]; then
    check "add XFCE to the existing install" "$A install debian --de xfce --yes --no-start"
    check "vncserver-start brings up XFCE on :1" "~/start-debian.sh 'echo andronix | vncpasswd -f > ~/.vnc/passwd && chmod 600 ~/.vnc/passwd && vncserver-start 1280x720 >/dev/null && sleep 12 && pgrep -x xfwm4 >/dev/null && pgrep -f Xtigervnc >/dev/null && vncserver-stop 1 >/dev/null'"
    run "~/start-debian.sh 'apt-get install -y -qq --no-install-recommends x11-apps netpbm >/dev/null 2>&1; vncserver-start 1280x720 >/dev/null; sleep 12; DISPLAY=:1 xwd -root -silent | xwdtopnm 2>/dev/null | pnmtopng > /tmp/desktop.png; vncserver-stop 1 >/dev/null'"
    mkdir -p dist && docker exec "$box" cat /home/termux/.andronix/distros/debian/rootfs/tmp/desktop.png >dist/desktop.png 2>/dev/null &&
        [ -s dist/desktop.png ] && ok "screenshot saved to dist/desktop.png" || bad "screenshot"
fi

check "remove --legacy removes the old install too" "$A remove debian --legacy --yes && ! test -e ~/debian-fs && ! test -e ~/start-debian-old.sh"

echo
echo "  $pass passed, $fail failed"
[ "$fail" = 0 ]
