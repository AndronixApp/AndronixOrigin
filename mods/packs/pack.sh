#!/bin/sh
# Dev packs, reference implementation (POSIX sh, run as root inside a distro).
# The Go command `andronix pack` should behave the same; see docs/packs.md.
#
#   sh mods/packs/pack.sh list
#   sh mods/packs/pack.sh add <pack>...      e.g. add python node
#   sh mods/packs/pack.sh remove <pack>...
#
# Packs are data: mods/packs/<id>.conf (PACK_PKGS_<family>, PACK_FILES,
# PACK_POST, PACK_CHECK, PACK_HINT). Installed packs are recorded in
# /etc/andronix/packs/<id> (the packages that were installed), so remove
# takes out exactly those.
set -eu
here=$(cd "$(dirname "$0")" && pwd)
state=/etc/andronix/packs

for f in apt-get:apt dnf:dnf pacman:pacman apk:apk xbps-install:xbps; do
    command -v "${f%%:*}" >/dev/null 2>&1 && { family=${f#*:}; break; }
done
[ -n "${family:-}" ] || { echo "pack: no known package manager" >&2; exit 1; }

get() { # get FILE KEY
    sed -n "s/^$2=//p" "$1" | head -1 | sed 's/^"\(.*\)"$/\1/'
}
available() {
    case $family in
        apt) apt-cache policy "$1" 2>/dev/null | grep -q 'Candidate: [^(]' ;;
        dnf) dnf -y -q repoquery --available "$1" 2>/dev/null | grep -q . ;;
        pacman) pacman -Si "$1" >/dev/null 2>&1 || pacman -Sg "$1" >/dev/null 2>&1 ;;
        apk) apk search -e -q "$1" 2>/dev/null | grep -qx "$1" ;;
        xbps) xbps-query -R "$1" >/dev/null 2>&1 ;;
    esac
}
refresh() {
    case $family in
        apt) apt-get update -qq ;;
        dnf) dnf -y -q makecache ;;
        pacman) pacman -Sy --noconfirm >/dev/null ;;
        apk) apk update -q ;;
        xbps) xbps-install -S >/dev/null ;;
    esac
}
installed() {
    case $family in
        apt) dpkg-query -W -f='${Status}' "$1" 2>/dev/null | grep -q 'ok installed' ;;
        dnf) rpm -q "$1" >/dev/null 2>&1 ;;
        pacman) pacman -Qq "$1" >/dev/null 2>&1 ;;
        apk) apk info -e "$1" >/dev/null 2>&1 ;;
        xbps) xbps-query "$1" >/dev/null 2>&1 ;;
    esac
}
install_pkgs() {
    case $family in
        apt) DEBIAN_FRONTEND=noninteractive apt-get install -y -q --no-install-recommends "$@" ;;
        dnf) dnf install -y -q --setopt=install_weak_deps=False "$@" ;;
        pacman) pacman -S --noconfirm --needed "$@" ;;
        apk) apk add -q "$@" ;;
        xbps) xbps-install -y "$@" ;;
    esac
}
remove_pkgs() {
    case $family in
        apt) DEBIAN_FRONTEND=noninteractive apt-get remove -y -q --autoremove "$@" ;;
        dnf) dnf remove -y -q "$@" ;;
        pacman) pacman -Rns --noconfirm "$@" ;;
        apk) apk del -q "$@" ;;
        xbps) xbps-remove -Ry "$@" ;;
    esac
}
resolve() { # resolve "tokens" -> package names this distro has
    out=""
    for tok in $1; do
        opt=""
        case $tok in *\?) opt=1; tok=${tok%?} ;; esac
        pick=""
        for alt in $(echo "$tok" | tr '|' ' '); do
            available "$alt" && { pick=$alt; break; }
        done
        if [ -n "$pick" ]; then out="$out $pick"
        elif [ -n "$opt" ]; then echo "pack: optional $tok not available, skipped" >&2
        else echo "pack: $tok is not available on this distro" >&2; return 1; fi
    done
    echo "$out"
}

cmd=${1:-list}
[ $# -gt 0 ] && shift
case $cmd in
    list)
        for f in "$here"/*.conf; do
            id=$(get "$f" PACK_ID)
            mark="  "; [ -f "$state/$id" ] && mark="✓ "
            printf '%s%-7s %s\n' "$mark" "$id" "$(get "$f" PACK_DESC)"
        done
        ;;
    add)
        [ "$(id -u)" = 0 ] || { echo "pack: run as root" >&2; exit 1; }
        refresh
        for id in "$@"; do
            f=$here/$id.conf
            [ -f "$f" ] || { echo "pack: no pack called $id" >&2; exit 2; }
            pkgs=$(resolve "$(get "$f" "PACK_PKGS_$family")") || exit 1
            echo "==> $(get "$f" PACK_NAME):$pkgs"
            # Record only what this pack brings in: remove must not take out
            # packages the system (or another pack) had already.
            new=""
            for p in $pkgs; do installed "$p" || new="$new $p"; done
            # shellcheck disable=SC2086
            install_pkgs $pkgs
            for spec in $(get "$f" PACK_FILES); do
                src=${spec%%:*} rest=${spec#*:}; dst=${rest%%:*} mode=${rest#*:}
                install -D -m "$mode" "$here/files/$src" "$dst"
            done
            post=$(get "$f" PACK_POST); [ -z "$post" ] || sh -c "$post"
            mkdir -p "$state"
            printf 'PACK_ID=%s\nPACKAGES="%s"\nADDED=%s\n' "$id" "${new# }" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >"$state/$id"
            if sh -c "$(get "$f" PACK_CHECK)"; then
                echo "ok: $(get "$f" PACK_NAME) is ready. $(get "$f" PACK_HINT)"
            else
                echo "fail: $(get "$f" PACK_NAME) installed but its check failed" >&2; exit 3
            fi
        done
        ;;
    remove)
        [ "$(id -u)" = 0 ] || { echo "pack: run as root" >&2; exit 1; }
        for id in "$@"; do
            [ -f "$state/$id" ] || { echo "pack: $id isn't installed"; continue; }
            pkgs=$(get "$state/$id" PACKAGES)
            # Keep packages another installed pack also brought in.
            for o in "$state"/*; do
                [ "$o" = "$state/$id" ] || [ ! -f "$o" ] && continue
                for q in $(get "$o" PACKAGES); do pkgs=$(echo " $pkgs " | sed "s/ $q / /g"); done
            done
            f=$here/$id.conf
            [ "$id" = db ] && command -v andronix-postgres >/dev/null && andronix-postgres stop >/dev/null 2>&1 || true
            # shellcheck disable=SC2086
            [ -z "$(echo $pkgs)" ] || remove_pkgs $pkgs
            for spec in $(get "$f" PACK_FILES); do rest=${spec#*:}; rm -f "${rest%%:*}"; done
            rm -f "$state/$id"
            echo "ok: removed $id"
        done
        ;;
    *) echo "usage: pack.sh list | add <pack>... | remove <pack>..." >&2; exit 2 ;;
esac
