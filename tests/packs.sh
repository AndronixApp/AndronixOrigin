#!/usr/bin/env bash
# The real dev packs through the Go command, in the Termux-like box.
#
#   tests/packs.sh [distro] [pack...]      default: debian python node db
#
# Installs <distro> (command line only; packs don't depend on the desktop)
# from dist/<distro>-*-<arch>.tar.xz when present, else the usual sources.
# Then, like a phone: 'andronix pack add <distro> <pack>' from Termux for
# each pack but the last, 'andronix pack add <last>' as root inside the
# distro, pack list, the db server started and queried when db is added,
# and 'pack remove' of the last pack. One TSV line per step.
set -u
cd "$(dirname "$0")/.." || exit 1
distro=${1:-debian}
[ $# -gt 0 ] && shift
packs=("$@")
[ ${#packs[@]} -gt 0 ] || packs=(python node db)
box="andronix-packs-$distro-$$"
mkdir -p dist/packs
log=dist/packs/$distro.log
docker build -q -t andronix-test -f tests/Dockerfile tests >/dev/null || { echo "docker build failed"; exit 1; }
docker run -d --init --name "$box" -v "$PWD":/src:ro andronix-test sleep infinity >/dev/null
trap 'docker rm -f "$box" >/dev/null 2>&1' EXIT
run() { docker exec -e COLUMNS=80 "$box" bash -c "$1"; }
A=/home/termux/.local/bin/andronix
t=$(command ls dist/"$distro"-*-aarch64.tar.xz 2>/dev/null | head -1)
env=""; [ -n "$t" ] && env="ANDRONIX_ROOTFS=/src/$t"
line() { printf '%s\t%s\t%s\t%s\n' "$distro" "$1" "$2" "$3"; }
step() { # step NAME COMMAND
    local t0=$SECONDS
    if run "$2" >>"$log" 2>&1; then line "$1" PASS "$((SECONDS - t0))s"; else line "$1" FAIL "$((SECONDS - t0))s"; fail=1; fi
}
fail=0
: >"$log"
step install "ANDRONIX_SRC=/src sh /src/get.sh >/dev/null && $env $A install $distro --de none --yes --no-start --plain" || exit 1
last=${packs[${#packs[@]}-1]}
for p in "${packs[@]}"; do
    if [ "$p" = "$last" ]; then
        step "add $p (inside)" "$A start $distro --root -- andronix pack add $p --plain"
    else
        step "add $p" "$A pack add $distro $p --plain"
    fi
done
S=/home/termux/.andronix/distros/$distro/rootfs/etc/andronix/packs
step list "$A pack list $distro --plain && [ \$(command ls $S | wc -l) = ${#packs[@]} ] && head -50 $S/*"
if printf '%s\n' "${packs[@]}" | grep -qx db; then
    step "postgres start+query" "$A start $distro --root -- sh -c 'andronix-postgres start && su -s /bin/sh postgres -c \"psql -tAc \\\"select 1\\\"\" | grep -qx 1 && andronix-postgres stop'"
    # Its System V segment lives in files (libandronix-shm), not proot's helper.
    step "postgres shm in files" "$A start $distro --root -- sh -c 'andronix-postgres start >/dev/null && ls /tmp/.andronix-shm | grep -q . && andronix-postgres stop >/dev/null && ! ls /tmp/.andronix-shm | grep -q .'"
    # An interrupted initdb (PG_VERSION, no config) is moved aside and redone.
    step "postgres recovers a half-made cluster" "$A start $distro --root -- sh -c 'andronix-postgres stop >/dev/null; mv /var/lib/andronix-postgres /var/lib/good && mkdir /var/lib/andronix-postgres && echo 17 >/var/lib/andronix-postgres/PG_VERSION && andronix-postgres check && ls -d /var/lib/andronix-postgres.broken-* && rm -rf /var/lib/andronix-postgres.broken-* /var/lib/andronix-postgres && mv /var/lib/good /var/lib/andronix-postgres'"
fi
step "remove $last" "$A pack remove $distro $last --plain && ! test -e $S/$last"
if [ "$last" = db ]; then
    step "db data kept, then purged" "$A start $distro --root -- test -d /var/lib/andronix-postgres && $A pack add $distro db --plain >/dev/null && $A start $distro --root -- mkdir /var/lib/andronix-postgres.broken-test && $A pack remove $distro db --purge --plain && ! $A start $distro --root -- sh -c 'ls -d /var/lib/andronix-postgres*'"
fi
exit $fail
