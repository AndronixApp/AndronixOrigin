#!/usr/bin/env bash
# Clean an unpacked rootfs directory before it is exported as an image:
# the same rules as the clean step in ci/build-rootfs.sh (DESIGN.md
# section 9), done host-side so it also works on a rootfs the installer
# made (the modded builds export one). On top of the image rules it drops
# what an install adds per phone: the aid_* Android ids, the first-boot
# user and its home, and the first-boot user pointer.
#
#   ci/clean-rootfs.sh path/to/rootfs && ci/check-image.sh path/to/rootfs
# shellcheck disable=SC2016  # awk programs
set -euo pipefail
r=${1:?usage: clean-rootfs.sh <rootfs-dir>}
[ -f "$r/etc/passwd" ] || { echo "$r doesn't look like a rootfs" >&2; exit 1; }

# Edit a file even if its mode is 0000 (Fedora's shadow), keeping the mode.
edit() { # edit FILE [AWK-ARGS...] AWK-PROGRAM
    local f=$1 m
    shift
    [ -f "$f" ] || return 0
    m=$(stat -c %a "$f")
    chmod u+rw "$f"
    awk -F: -v OFS=: "$@" "$f" >"$f.andronix-tmp" && cat "$f.andronix-tmp" >"$f"
    rm -f "$f.andronix-tmp"
    chmod "$m" "$f"
}

# Human users (uid 1000-59999) and the aid_* ids, from every account file.
drop=$(awk -F: '($3 >= 1000 && $3 < 60000) || $1 ~ /^aid_/ { print $1 }' "$r/etc/passwd" | tr '\n' ' ')
for u in $drop; do
    case "$u" in aid_*) ;; *) home=$(awk -F: -v u="$u" '$1 == u { print $6 }' "$r/etc/passwd")
        [ -n "$home" ] && [ "$home" != / ] && rm -rf "${r:?}/${home#/}" ;;
    esac
done
for f in passwd shadow group gshadow; do
    edit "$r/etc/$f" -v drop=" $drop " 'index(drop, " " $1 " ") == 0 && $1 !~ /^aid_/'
done
# Also drop those names from group member lists.
for f in group gshadow; do
    edit "$r/etc/$f" -v drop=" $drop " '{ n = split($NF, m, ","); out = ""; for (i = 1; i <= n; i++) if (index(drop, " " m[i] " ") == 0 && m[i] !~ /^aid_/) out = out (out == "" ? "" : ",") m[i]; $NF = out; print }'
done
rm -f "$r/etc/andronix/user" "$r/etc/andronix/keyboard" "$r/etc/sudoers.d/andronix"

# Root locked; without /etc/shadow (Void) the field lives in passwd.
# Both files: a shadow without a root line leaves the field in passwd.
edit "$r/etc/shadow" '$1 == "root" && $2 !~ /^[!*]/ { $2 = "*" } { print }'
edit "$r/etc/passwd" '$1 == "root" && $2 != "x" && $2 !~ /^[!*]/ { $2 = "*" } { print }'

# Undo proot --link2symlink: under
# proot a hard link becomes a .l2s.<name>NNNN data file plus symlinks that
# hold the HOST path of the rootfs, which dangle anywhere else. Turn each
# such symlink back into a hard link, make other host-path symlinks guest
# paths, drop the .l2s leftovers. Host paths are recognised by their
# .../distros/<id>/rootfs/ part, so a copied rootfs works too.
guest_of() { # guest_of TARGET -> guest path, or nothing
    case $1 in
        "$r"/*) printf '/%s\n' "${1#"$r"/}" ;;
        /*/distros/*/rootfs/*) printf '/%s\n' "$(printf '%s' "$1" | sed 's|^/.*/distros/[^/]*/rootfs/||')" ;;
    esac
}
host_of() { # host_of LINK TARGET -> where TARGET is on this machine
    local g
    g=$(guest_of "$2")
    if [ -n "$g" ]; then printf '%s%s\n' "$r" "$g"
    elif [ "${2#/}" != "$2" ]; then printf '%s%s\n' "$r" "$2"
    else printf '%s/%s\n' "$(dirname "$1")" "$2"; fi
}
l2s_fixed=0 l2s_rel=0
while IFS= read -r l; do
    [ -L "$l" ] || continue
    case ${l##*/} in .l2s.*) continue ;; esac   # counters go with the data
    t=$(readlink "$l")
    case ${t##*/} in
        .l2s.*)
            d=$(host_of "$l" "$t") n=0
            while [ -L "$d" ] && [ $n -lt 16 ]; do d=$(host_of "$d" "$(readlink "$d")"); n=$((n + 1)); done
            [ -f "$d" ] || { echo "clean-rootfs: dangling $l -> $t" >&2; continue; }
            ln -f "$d" "$l.unl2s" && mv -f "$l.unl2s" "$l"
            l2s_fixed=$((l2s_fixed + 1))
            continue ;;
    esac
    g=$(guest_of "$t")
    if [ -n "$g" ]; then ln -sfn "$g" "$l"; l2s_rel=$((l2s_rel + 1)); fi
done < <(find "$r" -xdev -type l)
find "$r" -xdev -name '.l2s.*' -exec rm -f {} +
[ "$l2s_fixed$l2s_rel" = 00 ] || echo "link2symlink: $l2s_fixed hard links restored, $l2s_rel host-path links made guest paths"

# Identity.
# The installer writes it read-only (0444).
chmod u+w "$r/etc/machine-id" 2>/dev/null || true
: >"$r/etc/machine-id"
[ -f "$r/var/lib/dbus/machine-id" ] && [ ! -L "$r/var/lib/dbus/machine-id" ] && rm -f "$r/var/lib/dbus/machine-id"
rm -f "$r"/etc/ssh/ssh_host_*

# Leftovers.
rm -rf "$r"/var/cache/apt/archives/*.deb "$r"/var/cache/apt/*.bin "$r"/var/lib/apt/lists/* \
    "$r"/var/cache/pacman/pkg/* "$r"/var/cache/apk/* "$r"/var/cache/dnf/* "$r"/var/cache/libdnf5/* "$r"/var/cache/xbps/*
find "$r/tmp" "$r/var/tmp" -mindepth 1 -maxdepth 1 -exec rm -rf {} + 2>/dev/null || true
find "$r/var/log" -type f -exec sh -c ': >"$1"' _ {} \; 2>/dev/null || true
for h in "$r/root" "$r/etc/skel"; do
    rm -f "$h"/.*_history "$h/.lesshst" "$h/.viminfo" "$h/.wget-hsts" "$h/.sudo_as_admin_successful"
    rm -rf "$h/.cache" "$h/.mozilla" "$h/.vnc/passwd" "$h/.config/tigervnc/passwd"
done
echo "cleaned $r"
