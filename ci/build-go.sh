#!/usr/bin/env bash
# Build andronix for every CPU Termux runs on, two builds per CPU (each
# written under a temporary name and renamed: dist/ files are never
# rewritten in place, other checkouts may hard-link them):
#
#   dist/andronix-android-<cpu>   runs in Termux. GOOS=android: Go's runtime
#                                 and standard library avoid the syscalls
#                                 Android 8-11's seccomp kills (SIGSYS).
#   dist/andronix-linux-<cpu>     static GOOS=linux, no cgo: the copy inside
#                                 every distro (glibc or musl), for vnc,
#                                 setup-user, the bwrap stand-in, packs.
#   dist/andronix-<cpu>           the android build under its old name.
#
# Each android build embeds its CPU's linux build (selfbin/, gzip) so the
# installer can put it into distros offline. aarch64 builds without cgo;
# arm, i686 and x86_64 need cgo through the NDK: ANDROID_NDK_HOME, or else
# the build container (ci/build.Dockerfile, run with docker).
#
#   ci/build-go.sh                  everything
#   ci/build-go.sh android-cgo      only the NDK builds (the container runs this)
set -euo pipefail
cd "$(dirname "$0")/.."
version=${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}
ldflags="-s -w -X github.com/AndronixApp/andronix-distros/internal/app.Version=$version"
api=24 # Android 7, the oldest Termux supports
cpus="aarch64:arm64: arm:arm:7 i686:386: x86_64:amd64:"
mkdir -p dist selfbin

size() { printf '%-32s %s\n' "$1" "$(du -h "$1" | cut -f1)"; }

# android <cpu> <goarch> <goarm> [CC]: embeds dist/andronix-linux-<cpu>.
android() {
    local name=$1 goarch=$2 goarm=$3 cc=${4:-}
    gzip -9c "dist/andronix-linux-$name" >selfbin/andronix-linux.gz
    if [ -n "$cc" ]; then
        CGO_ENABLED=1 CC=$cc GOOS=android GOARCH=$goarch GOARM=$goarm \
            go build -trimpath -buildvcs=false -ldflags "$ldflags" -o "dist/.new-andronix-android-$name" ./cmd/andronix
    else
        CGO_ENABLED=0 GOOS=android GOARCH=$goarch \
            go build -trimpath -buildvcs=false -ldflags "$ldflags" -o "dist/.new-andronix-android-$name" ./cmd/andronix
    fi
    rm -f selfbin/andronix-linux.gz
    mv -f "dist/.new-andronix-android-$name" "dist/andronix-android-$name"
    cp "dist/andronix-android-$name" "dist/.new-andronix-$name" && mv -f "dist/.new-andronix-$name" "dist/andronix-$name"
    size "dist/andronix-android-$name"
}

ndk_builds() {
    local tc=$ANDROID_NDK_HOME/toolchains/llvm/prebuilt
    tc=$(echo "$tc"/*/bin)
    android arm arm 7 "$tc/armv7a-linux-androideabi$api-clang"
    android i686 386 "" "$tc/i686-linux-android$api-clang"
    android x86_64 amd64 "" "$tc/x86_64-linux-android$api-clang"
}

if [ "${1:-}" = android-cgo ]; then
    ndk_builds
    exit 0
fi

rm -f selfbin/andronix-linux.gz dist/andronix-android-* dist/andronix-aarch64 dist/andronix-arm dist/andronix-i686 dist/andronix-x86_64
for t in $cpus; do
    IFS=: read -r name goarch goarm <<<"$t"
    CGO_ENABLED=0 GOOS=linux GOARCH=$goarch GOARM=$goarm \
        go build -trimpath -buildvcs=false -ldflags "$ldflags" -o "dist/.new-andronix-linux-$name" ./cmd/andronix
    mv -f "dist/.new-andronix-linux-$name" "dist/andronix-linux-$name"
    size "dist/andronix-linux-$name"
done
android aarch64 arm64 ""
if [ -n "${ANDROID_NDK_HOME:-}" ]; then
    ndk_builds
elif [ -n "${NO_NDK:-}" ]; then
    echo "NO_NDK: skipping the arm, i686 and x86_64 android builds" >&2
else
    docker build -q -t andronix-build -f ci/build.Dockerfile ci >/dev/null ||
        docker build -t andronix-build -f ci/build.Dockerfile ci # again, to show why
    # The source goes in and the binaries come out as tar streams, not a
    # bind mount: Docker VMs (colima, Docker Desktop) share only some host
    # folders, and a checkout elsewhere would show up empty.
    { if git rev-parse --git-dir >/dev/null 2>&1; then git ls-files -z --cached --others --exclude-standard
      else find . \( -path ./dist -o -path ./cache -o -path ./.git \) -prune -o -type f -print0; fi; } |
        COPYFILE_DISABLE=1 tar --null -T - -cf - dist/andronix-linux-* |
        docker run --rm -i -e VERSION="$version" \
            -v andronix-gocache:/root/.cache/go-build -v andronix-gomod:/go/pkg/mod \
            andronix-build sh -c 'mkdir -p /src && cd /src && tar --warning=no-unknown-keyword -xf - && ci/build-go.sh android-cgo >&2 &&
                tar -cf - dist/andronix-android-arm dist/andronix-android-i686 dist/andronix-android-x86_64' |
        tar -xf -
    for name in arm i686 x86_64; do
        [ -s "dist/andronix-android-$name" ] || { echo "the NDK build of andronix-android-$name failed" >&2; exit 1; }
        cp "dist/andronix-android-$name" "dist/.new-andronix-$name" && mv -f "dist/.new-andronix-$name" "dist/andronix-$name"
    done
fi
(cd dist && { command -v sha256sum >/dev/null && sha256sum andronix-* || shasum -a 256 andronix-*; } | grep -v '\.sha256' >.SHA256SUMS.new && mv -f .SHA256SUMS.new SHA256SUMS)
