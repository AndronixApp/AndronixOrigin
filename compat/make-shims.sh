#!/usr/bin/env bash
# Generate drop-in replacements for AndronixOrigin's installer scripts.
# The Firestore commands download e.g. Installer/Debian/debian-xfce.sh and
# run it; each shim fetches get.sh and runs `andronix install ...`, so the
# app keeps working with no release and no Firestore change.
#
#   compat/make-shims.sh OUT_DIR
set -eu
cd "$(dirname "$0")"
out=${1:?usage: make-shims.sh OUT_DIR}
GET_URL=${GET_URL:-https://dl.andronix.app/get.sh}
grep -v '^#' keys.conf | while read -r path distro de; do
    case "${path:-}" in */*) ;; *) continue ;; esac   # ModdedOS.* keys: no shim
    mkdir -p "$out/$(dirname "$path")"
    sed -e "s|@GET_URL@|$GET_URL|" -e "s|@DISTRO@|$distro|" -e "s|@DE@|$de|" shim.sh.in >"$out/$path"
    chmod 755 "$out/$path"
    echo "wrote $out/$path"
done
