#!/usr/bin/env bash
# Test the dev packs on one edition, in a fresh Termux-like box.
#
#   mods/packs/test-packs.sh <distro> <desktop> [pack...]     default: all packs
#
# Installs dist/modded/<distro>-*-<desktop>-modded-aarch64.tar.xz (or the free
# distro if there is none) with the andronix binary from ANDRONIX_SRC, then,
# as root under proot like a phone: pack.sh add <pack> for each pack (install
# + its check), pack.sh list, and pack.sh remove for the last pack. Prints
# one TSV line per pack: distro, desktop, pack, result, seconds, disk MB.
set -u
cd "$(dirname "$0")/../.." || exit 1
repo=$PWD
distro=${1:?usage: test-packs.sh <distro> <desktop> [pack...]}
de=${2:?usage: test-packs.sh <distro> <desktop> [pack...]}
shift 2
packs=("$@")
[ ${#packs[@]} -gt 0 ] || packs=(python node java go db)
src=$(cd "${ANDRONIX_SRC:-$repo}" && pwd)
box="modder-packs-$distro-$de-$$"
out=cache/packs; mkdir -p "$out"
log=$out/$distro-$de.log
docker rm -f "$box" >/dev/null 2>&1
docker run -d --init --name "$box" -v "$src":/src:ro -v "$repo/dist":/dist:ro -v "$repo/mods/packs":/packs:ro andronix-test sleep infinity >/dev/null
[ -n "${KEEP:-}" ] || trap 'docker rm -f "$box" >/dev/null 2>&1' EXIT
run() { docker exec -e COLUMNS=60 "$box" bash -c "$1"; }
A=/home/termux/.local/bin/andronix
R=/home/termux/.andronix/distros/$distro/rootfs
t=$(ls dist/modded/"$distro"-*-"$de"-modded-aarch64.tar.xz 2>/dev/null | head -1)
env=""; [ -n "$t" ] && env="ANDRONIX_ROOTFS=/dist/modded/$(basename "$t")"
run "ANDRONIX_SRC=/src sh /src/get.sh >/dev/null && $env $A install $distro --de $de --yes --no-start --plain" >"$log" 2>&1 ||
    { echo -e "$distro\t$de\t(install)\tFAIL\t-\t-"; exit 1; }
P="ANDRONIX_BINDS=/packs:/mnt/packs $A start $distro --root -- sh /mnt/packs/pack.sh"
used() { run "du -sm $R 2>/dev/null | cut -f1"; }
for p in "${packs[@]}"; do
    before=$(used); t0=$(date +%s)
    if run "$P add $p" >>"$log" 2>&1; then res=PASS; else res=FAIL; fi
    printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$distro" "$de" "$p" "$res" "$(($(date +%s) - t0))" "$(($(used) - before))"
done
run "$P list" >>"$log" 2>&1
last=${packs[${#packs[@]}-1]}
if run "$P remove $last" >>"$log" 2>&1 && ! run "$P list" | grep -q "^✓ $last "; then
    printf '%s\t%s\t%s\t%s\t-\t-\n' "$distro" "$de" "remove $last" PASS
else
    printf '%s\t%s\t%s\t%s\t-\t-\n' "$distro" "$de" "remove $last" FAIL
fi
