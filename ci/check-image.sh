#!/usr/bin/env bash
# shellcheck disable=SC2015,SC2016
# Image hygiene gate: fails (exit 1) if a rootfs we are about to publish
# carries anything personal or build-specific. Run on every free and
# modded image before upload (ci/build-rootfs.sh does).
#
#   ci/check-image.sh dist/debian-13-aarch64.tar.xz
#   ci/check-image.sh path/to/rootfs-dir
#
# Rules (DESIGN.md section 9):
#   1. no browser profiles, caches, history or cookies in /etc/skel, /root, /home
#   2. no preset passwords: root locked, no other hashed passwords, no VNC passwd
#   3. empty /etc/machine-id, no SSH host keys
#   4. VNC not set to listen on the network by default
#   6. portable: no symlinks to host paths, no proot .l2s.* leftovers
#   5. no build leftovers: shell history, package caches, /tmp, logs, human users,
#      and the installer's per-phone aid_* ids (scrub with ci/clean-rootfs.sh)
set -u
target=${1:?usage: check-image.sh <rootfs.tar.xz|rootfs-dir>}
fail=0
bad() { printf '  FAIL  %s\n' "$*"; fail=1; }
ok() { printf '  ok    %s\n' "$*"; }

# Work on a file list plus a few extracted files, so tarballs need no
# unpacking (and no root).
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
if [ -d "$target" ]; then
    (cd "$target" && find . \( -type f -o -type l -o -type d \) -printf '%y %s %p\n' 2>/dev/null) | sed 's| \./| |' >"$tmp/list"
    # Read files even when their mode is 0000 (Fedora's /etc/shadow): we own
    # them, so grant read briefly and put the mode back.
    get() {
        local f="$target/$1" m
        [ -e "$f" ] || return 0
        if [ -r "$f" ]; then cat "$f"; return; fi
        m=$(stat -c %a "$f" 2>/dev/null) || return 0
        chmod u+r "$f" 2>/dev/null && cat "$f" 2>/dev/null
        chmod "$m" "$f" 2>/dev/null
    }
else
    case "$target" in *.xz) z=J ;; *.gz) z=z ;; *.zst) z=--zstd ;; *) z= ;; esac
    tar -t"${z#--}"vf "$target" 2>/dev/null | awk '{ t = substr($1, 1, 1); if (t == "-") t = "f"; n = $6; for (i = 7; i <= NF; i++) n = n " " $i; sub(/^\.\//, "", n); print t, $3, n }' >"$tmp/list"
    mkdir -p "$tmp/x"
    # Straight to stdout: an extracted copy keeps the member's mode, and
    # Fedora ships /etc/shadow as 0000 (unreadable to a non-root checker).
    get() { tar -x"${z#--}"Of "$target" "./$1" 2>/dev/null || tar -x"${z#--}"Of "$target" "$1" 2>/dev/null; }
fi
[ -s "$tmp/list" ] || { echo "can't read $target"; exit 2; }
names() { awk '{ $1 = ""; $2 = ""; sub(/^  /, ""); print }' "$tmp/list"; }

echo "Image hygiene: $target"

# 1. Browser and personal data in home directories.
hits=$(names | grep -E '^(etc/skel|root|home/[^/]+)/(\.mozilla|\.cache|\.config/(google-chrome|chromium|BraveSoftware|vivaldi)|\.local/share/recently-used\.xbel|\.pki)(/|$)' | head -5)
[ -z "$hits" ] && ok "no browser profiles or caches in skel/root/home" || bad "browser/cache data: $hits"

# 2. Passwords.
shadow=$(get etc/shadow)
root_pw=$(printf '%s\n' "$shadow" | awk -F: '$1 == "root" { print $2 }')
if ! printf '%s\n' "$shadow" | grep -q '^root:'; then
    # No /etc/shadow, or one without a root line (Void, after pwconv on a
    # passwd with root 'x'): the password field is in /etc/passwd.
    root_pw=$(get etc/passwd | awk -F: '$1 == "root" { print $2 }')
    [ "$root_pw" = x ] && root_pw='*'   # shadow-managed but no shadow file: no login
fi
case "$root_pw" in
    '*'*|'!'*) ok "root is locked" ;;
    '') bad "root has an empty password (anyone can log in)" ;;
    *) bad "root has a password set" ;;
esac
others=$(printf '%s\n' "$shadow" | awk -F: '$1 != "root" && $2 ~ /^\$/ { print $1 }' | tr '\n' ' ')
[ -z "$others" ] && ok "no preset user passwords" || bad "preset passwords for: $others"
hits=$(names | grep -E '(^|/)(\.vnc|\.config/tigervnc)/passwd$' | head -3)
[ -z "$hits" ] && ok "no VNC password files" || bad "VNC password shipped: $hits"

# 3. Machine identity.
mid=$(get etc/machine-id | tr -d '[:space:]')
[ -z "$mid" ] && ok "/etc/machine-id is empty" || bad "/etc/machine-id is set ($mid)"
hits=$(names | grep -E '^etc/ssh/ssh_host_' | head -3)
[ -z "$hits" ] && ok "no SSH host keys" || bad "SSH host keys: $hits"

# 4. VNC network default.
if [ -d "$target" ]; then
    cfg=$(cat "$target"/etc/tigervnc/vncserver-config-* 2>/dev/null)
else
    cfg=$(tar -x"${z#--}"Of "$target" --wildcards --no-anchored 'etc/tigervnc/vncserver-config-*' 2>/dev/null)
fi
if printf '%s\n' "$cfg" | grep -Eq '^[[:space:]]*\$localhost[[:space:]]*=[[:space:]]*"no"'; then
    bad "TigerVNC config listens on the network by default"
else
    ok "VNC listens on localhost by default"
fi

# 6. Portability: no symlink may name a host path (Termux, a test box, or
#    any .../distros/<id>/rootfs), and no proot link2symlink leftovers.
if [ -d "$target" ]; then
    links=$(cd "$target" && find . -type l -printf '%p\t%l\n' 2>/dev/null | sed 's|^\./||')
else
    links=$(tar -t"${z#--}"vf "$target" 2>/dev/null | awk '$1 ~ /^l/ { s = $0; sub(/^.* [0-9][0-9]:[0-9][0-9] /, "", s); split(s, a, " -> "); sub(/^\.\//, "", a[1]); print a[1] "\t" a[2] }')
fi
hits=$(printf '%s\n' "$links" | awk -F'\t' '$2 ~ /^\/data\/data\// || $2 ~ /^\/home\/termux/ || $2 ~ /\/distros\/[^\/]+\/rootfs(\/|$)/ { print $1 " -> " $2 }' | head -3)
[ -z "$hits" ] && ok "no symlinks to host paths" || bad "host-path symlinks (run ci/clean-rootfs.sh): $hits"
hits=$(names | grep -E '(^|/)\.l2s\.' | head -3)
[ -z "$hits" ] && ok "no proot link2symlink leftovers" || bad ".l2s files (run ci/clean-rootfs.sh): $hits"

# 5. Build leftovers.
hits=$(names | grep -E '^(root|home/[^/]+|etc/skel)/\.(bash_history|zsh_history|ash_history|python_history|lesshst|viminfo|wget-hsts|sudo_as_admin_successful)$' | head -5)
[ -z "$hits" ] && ok "no shell history" || bad "history files: $hits"
hits=$(awk '$1 == "f" { $1 = ""; $2 = ""; sub(/^  /, ""); print }' "$tmp/list" |
    grep -E '^(var/cache/apt/archives/[^/]+\.deb|var/cache/apt/[^/]+\.bin|var/lib/apt/lists/[^/]+_(Packages|Sources|InRelease|Release)(\.[a-z0-9]+)?|var/cache/pacman/pkg/.+|var/cache/apk/.+|var/cache/(dnf|libdnf5)/.+|var/cache/xbps/.+)$' | head -3)
[ -z "$hits" ] && ok "no package caches" || bad "package caches: $hits"
hits=$(names | grep -E '^(tmp|var/tmp)/.+' | head -3)
[ -z "$hits" ] && ok "/tmp and /var/tmp are empty" || bad "temp files: $hits"
hits=$(awk '$1 == "f" && $2 > 0 { $1 = ""; $2 = ""; sub(/^  /, ""); print }' "$tmp/list" | grep -E '^var/log/' | head -3)
[ -z "$hits" ] && ok "logs are empty" || bad "non-empty logs: $hits"
users=$(get etc/passwd | awk -F: '$3 >= 1000 && $3 < 60000 && $1 !~ /^aid_/ { print $1 }' | tr '\n' ' ')
aids=$( { get etc/passwd; get etc/group; } | awk -F: '$1 ~ /^aid_/ { print $1 }' | sort -u | tr '\n' ' ')
[ -z "$aids" ] && ok "no per-phone Android ids (aid_*)" || bad "per-phone Android ids from an install: $aids(run ci/clean-rootfs.sh before export)"
homes=$(names | grep -E '^home/[^/]+/?$' | sed 's#/$##' | tr '\n' ' ')
[ -z "$users$homes" ] && ok "no human users or home directories" || bad "users/homes present: ${users}${homes}"

echo
[ "$fail" = 0 ] && echo "PASS: image is clean" || echo "FAIL: fix the items above before publishing"
exit "$fail"
