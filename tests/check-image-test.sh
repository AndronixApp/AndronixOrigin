#!/usr/bin/env bash
# Cases for ci/check-image.sh and ci/clean-rootfs.sh. Needs GNU tar and a
# non-root user (run in the test box: tests/Dockerfile), because the point
# of some cases is a file the checker can't read directly.
#
#   docker run --rm -v "$PWD":/src:ro -w /src andronix-test bash tests/check-image-test.sh
# shellcheck disable=SC2015
set -u
cd "$(dirname "$0")/.." || exit 1
w=$(mktemp -d)
trap 'chmod -R u+rwX "$w"; rm -rf "$w"' EXIT
fail=0

mk() { # mk NAME ROOT-SHADOW-FIELD|none ROOT-PASSWD-FIELD
    local r="$w/$1"
    mkdir -p "$r/etc" "$r/bin" "$r/tmp" "$r/root"
    printf 'root:%s:0:0:root:/root:/bin/sh\nnobody:x:65534:65534::/:/bin/false\n' "$3" >"$r/etc/passwd"
    printf 'root:x:0:\n' >"$r/etc/group"
    : >"$r/etc/machine-id"
    tar -C "$r" -cf "$w/$1.tar" .
    if [ "$2" != none ]; then
        if [ "$2" = NOROOT ]; then
            printf 'nobody:*:19000:0:99999:7:::\n' >"$r/etc/shadow"
        else
            printf 'root:%s:19000:0:99999:7:::\n' "$2" >"$r/etc/shadow"
        fi
        # Recorded as mode 0000 in the tarball, like Fedora's image.
        tar -C "$r" -rf "$w/$1.tar" --mode=0000 ./etc/shadow
        chmod 000 "$r/etc/shadow"
    fi
    xz -f "$w/$1.tar"
}
expect() { # expect pass|fail NAME TARGET
    if bash ci/check-image.sh "$3" >"$w/out" 2>&1; then got=pass; else got=fail; fi
    if [ "$got" = "$1" ]; then echo "  ok    $2 -> $got"; else echo "  WRONG $2 -> $got (want $1)"; sed 's/^/        /' "$w/out"; fail=1; fi
}

mk fedora '!unprovisioned' x        # shadow mode 0000, root locked
mk void none x                       # no shadow, 'x' in passwd
mk void-noroot 'NOROOT' x            # shadow exists without a root line
mk void-empty none ''                # no shadow, empty password in passwd
mk hashed '$6$salt$hash' x           # a real root password
mk empty '' x                        # empty root password in shadow

for t in dir tar; do
    s=""; [ "$t" = tar ] && s=.tar.xz
    expect pass "fedora shadow 0000 ($t)" "$w/fedora$s"
    expect pass "void no shadow, x ($t)" "$w/void$s"
    expect fail "void no shadow, empty ($t)" "$w/void-empty$s"
    expect pass "shadow without root, x in passwd ($t)" "$w/void-noroot$s"
    expect fail "root hash ($t)" "$w/hashed$s"
    expect fail "root empty ($t)" "$w/empty$s"
done
# The shadow file must keep its 0000 mode after a directory check.
[ "$(stat -c %a "$w/fedora/etc/shadow")" = 0 ] && echo "  ok    shadow mode kept" || { echo "  WRONG shadow mode changed"; fail=1; }

# An installed rootfs: aid_* ids and a first-boot user fail; clean-rootfs.sh fixes it.
mk installed '!locked' x
r="$w/installed"
chmod 600 "$r/etc/shadow"
printf 'aid_u0_a123:x:10123:10123:Termux:/:/sbin/nologin\nalex:x:1001:1001::/home/alex:/bin/sh\n' >>"$r/etc/passwd"
printf 'aid_inet:x:3003:root,aid_u0_a123\nalex:x:1001:\nwheel:x:10:alex,aid_u0_a123\n' >>"$r/etc/group"
printf 'aid_u0_a123:*:18446:0:99999:7:::\nalex:$6$x$y:19000:0:99999:7:::\n' >>"$r/etc/shadow"
chmod 000 "$r/etc/shadow"
mkdir -p "$r/home/alex" "$r/etc/andronix" && echo alex >"$r/etc/andronix/user"
echo 0123456789abcdef0123456789abcdef >"$r/etc/machine-id"
chmod 444 "$r/etc/machine-id"   # as the installer writes it
expect fail "installed rootfs" "$r"
bash ci/clean-rootfs.sh "$r" >/dev/null
expect pass "installed rootfs after clean-rootfs.sh" "$r"
grep -q 'wheel:x:10:$' "$r/etc/group" && echo "  ok    group members scrubbed" || { echo "  WRONG group members: $(grep wheel "$r/etc/group")"; fail=1; }

# proot link2symlink leftovers and host-path links fail; clean-rootfs.sh
# turns them back into hard links and guest paths.
mk l2s '!locked' x
r="$w/l2s"
mkdir -p "$r/usr/bin" "$r/etc/alternatives"
printf '#!/bin/sh\necho data\n' >"$r/usr/bin/.l2s.tool0001.0002"
ln -s "/data/data/com.termux/files/home/.andronix/distros/x/rootfs/usr/bin/.l2s.tool0001.0002" "$r/usr/bin/.l2s.tool0001"
ln -s "/data/data/com.termux/files/home/.andronix/distros/x/rootfs/usr/bin/.l2s.tool0001" "$r/usr/bin/tool"
ln -s "/data/data/com.termux/files/home/.andronix/distros/x/rootfs/usr/bin/.l2s.tool0001" "$r/usr/bin/tool2"
ln -s "/data/data/com.termux/files/home/.andronix/distros/x/rootfs/etc/alternatives/editor" "$r/usr/bin/editor"
expect fail "link2symlink leftovers" "$r"
bash ci/clean-rootfs.sh "$r" >/dev/null
expect pass "link2symlink after clean-rootfs.sh" "$r"
[ "$(stat -c %h "$r/usr/bin/tool")" = 2 ] && [ "$(cat "$r/usr/bin/tool2" | tail -1)" = "echo data" ] && [ "$(readlink "$r/usr/bin/editor")" = /etc/alternatives/editor ] &&
    echo "  ok    hard links and guest paths restored" || { echo "  WRONG link2symlink repair"; ls -la "$r/usr/bin"; fail=1; }

[ "$fail" = 0 ] && echo "check-image tests: ok"
exit "$fail"
