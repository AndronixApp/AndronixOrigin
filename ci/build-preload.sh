#!/usr/bin/env bash
# Build the guest preload libraries (fchmodat.c, shm.c, mntid.c) for every CPU
# Andronix ships, in a Debian container with cross compilers. Output:
# guest/preload/<arch>/libandronix-{fchmodat,shm}.so
#
#   ci/build-preload.sh
set -euo pipefail
cd "$(dirname "$0")/.."
docker run --rm -v "$PWD/guest/preload":/w -w /w debian:13 bash -c '
    set -e
    apt-get update -qq >/dev/null
    apt-get install -y -qq --no-install-recommends gcc gcc-aarch64-linux-gnu gcc-arm-linux-gnueabihf \
        gcc-i686-linux-gnu gcc-x86-64-linux-gnu libc6-dev libc6-dev-arm64-cross libc6-dev-armhf-cross \
        libc6-dev-i386-cross libc6-dev-amd64-cross >/dev/null 2>&1
    for t in aarch64:aarch64-linux-gnu arm:arm-linux-gnueabihf i686:i686-linux-gnu x86_64:x86_64-linux-gnu; do
        a=${t%%:*} cc=${t#*:}-gcc
        mkdir -p "$a"
        # 16 KB pages on arm64 (newer Android kernels use them), 4 KB elsewhere.
        page="-Wl,-z,max-page-size=4096"; [ "$a" = aarch64 ] && page="-Wl,-z,max-page-size=16384"
        $cc -shared -fPIC $page -O2 -Wall -Wextra -D_FILE_OFFSET_BITS=64 -nostartfiles \
            -Wl,-soname,libandronix-fchmodat.so -o "$a/libandronix-fchmodat.so" fchmodat.c
        # shm.c: no _FILE_OFFSET_BITS (plain symbol names; segments are tiny).
        $cc -shared -fPIC $page -O2 -Wall -Wextra -nostartfiles \
            -Wl,-soname,libandronix-shm.so -o "$a/libandronix-shm.so" shm.c
        $cc -shared -fPIC $page -O2 -Wall -Wextra -nostartfiles \
            -Wl,-soname,libandronix-mntid.so -o "$a/libandronix-mntid.so" mntid.c -ldl
        ${t#*:}-strip "$a/libandronix-fchmodat.so" "$a/libandronix-shm.so" "$a/libandronix-mntid.so"
        echo "$a: $(wc -c <"$a/libandronix-fchmodat.so") + $(wc -c <"$a/libandronix-shm.so") + $(wc -c <"$a/libandronix-mntid.so") bytes"
    done'
