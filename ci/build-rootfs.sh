#!/usr/bin/env bash
# Build Andronix rootfs tarballs from a distro's OCI image with Docker.
#
#   ci/build-rootfs.sh debian                 all CPUs in DISTRO_ARCHES
#   ci/build-rootfs.sh debian aarch64 arm     just these
#
# Writes to dist/:
#   <id>-<version>-<arch>.tar.xz          flattened rootfs (docker export)
#   <id>-<version>-<arch>.tar.xz.sha256   checksum, read by the installer
#   <id>-<version>-<arch>.meta            size and file count, for progress
#
# Publish them with ci/publish-r2.sh (see .github/workflows/rootfs.yml). Needs Docker with binfmt/QEMU for
# foreign CPUs (docker/setup-qemu-action in CI).
#
# When the distro sets DISTRO_UPSTREAM_TARBALL_<arch> (Arch Linux ARM has
# no OCI image), that tarball is downloaded to dist/src/, checked against
# its .md5, imported into Docker, and DISTRO_UPSTREAM_REMOVE is uninstalled
# from it before the clean step and export.
set -euo pipefail
cd "$(dirname "$0")/.."

id=${1:?usage: build-rootfs.sh <distro> [arch...]}
shift
# shellcheck source=/dev/null
. "distros/$id.conf"
arches=("$@")
[ ${#arches[@]} -gt 0 ] || read -ra arches <<<"$DISTRO_ARCHES"
mkdir -p dist

# Runs inside the image before export: no identity, passwords, caches,
# history, temp files, logs or upstream default users.
# shellcheck disable=SC2016
CLEAN='
: >/etc/machine-id 2>/dev/null
[ -f /var/lib/dbus/machine-id ] && [ ! -L /var/lib/dbus/machine-id ] && rm -f /var/lib/dbus/machine-id
rm -f /etc/ssh/ssh_host_*
for u in $(awk -F: "\$3 >= 1000 && \$3 < 60000 { print \$1 }" /etc/passwd); do
    userdel -r "$u" 2>/dev/null || deluser --remove-home "$u" 2>/dev/null || true
done
rm -rf /home/*
passwd -l root >/dev/null 2>&1 || sed -i "s/^root:[^:]*:/root:*:/" /etc/shadow 2>/dev/null
# Images without /etc/shadow (Void) keep the password field in /etc/passwd.
[ -f /etc/shadow ] || sed -i "s/^root:[^:]*:/root:*:/" /etc/passwd
rm -rf /var/cache/apt/archives/*.deb /var/cache/apt/*.bin /var/lib/apt/lists/* \
    /var/cache/pacman/pkg/* /var/cache/apk/* /var/cache/dnf/* /var/cache/libdnf5/* /var/cache/xbps/*
rm -rf /tmp/* /tmp/.[!.]* /var/tmp/* /var/tmp/.[!.]*
find /var/log -type f | while read -r f; do : >"$f"; done
rm -f /root/.*_history /root/.lesshst /root/.viminfo /root/.wget-hsts
true
'

platform() {
    case "$1" in
        aarch64) echo linux/arm64 ;;
        arm) echo linux/arm/v7 ;;
        i686) echo linux/386 ;;
        x86_64) echo linux/amd64 ;;
        *) echo "unknown arch $1" >&2; return 1 ;;
    esac
}

# upstream_image ARCH URL → imports the upstream tarball as a local image,
# strips DISTRO_UPSTREAM_REMOVE, and prints the image name.
upstream_image() {
    local url=$2 file plat img="andronix-upstream-$id-$1"
    plat=$(platform "$1")
    file="dist/src/$(basename "$url")"
    mkdir -p dist/src
    if [ ! -s "$file" ]; then
        curl -fL --retry 3 -o "$file.part" "$url" >&2 && mv "$file.part" "$file" || return 1
    fi
    if curl -fsL --retry 3 -o "$file.md5" "$url.md5"; then
        local want have
        want=$(awk '{ print $1; exit }' "$file.md5")
        if command -v md5sum >/dev/null; then have=$(md5sum "$file" | awk '{ print $1 }'); else have=$(md5 -q "$file"); fi
        [ "$want" = "$have" ] || { echo "md5 mismatch for $file" >&2; rm -f "$file"; return 1; }
    fi
    docker import --platform "$plat" "$file" "$img:raw" >/dev/null || return 1
    local remove=""
    case "$DISTRO_FAMILY" in
        pacman)
            # Only what is installed, or pacman refuses the whole transaction.
            # shellcheck disable=SC2016  # runs in the container; $0 is the list
            remove='pkgs=$(pacman -Qq $0 2>/dev/null); [ -z "$pkgs" ] || pacman -Rns --noconfirm $pkgs
                rm -rf /boot/* /var/cache/pacman/pkg/* /var/lib/pacman/sync/*' ;;
        *) echo "no upstream strip step for family $DISTRO_FAMILY" >&2; return 1 ;;
    esac
    docker rm -f "$img" >/dev/null 2>&1 || true
    docker run --platform "$plat" --name "$img" "$img:raw" /bin/sh -c "$remove" "$DISTRO_UPSTREAM_REMOVE" >&2 || return 1
    docker commit "$img" "$img:latest" >/dev/null || return 1
    docker rm -f "$img" >/dev/null
    docker rmi -f "$img:raw" >/dev/null 2>&1 || true
    echo "$img:latest"
}

for arch in "${arches[@]}"; do
    plat=$(platform "$arch")
    name="$id-$DISTRO_VERSION-$arch.tar.xz"
    upstream_var="DISTRO_UPSTREAM_TARBALL_$arch"
    if [ -n "${!upstream_var:-}" ]; then
        echo "==> ${!upstream_var} ($plat) -> dist/$name"
        image=$(upstream_image "$arch" "${!upstream_var}")
    else
        echo "==> $DISTRO_IMAGE ($plat) -> dist/$name"
        image=$DISTRO_IMAGE
        docker pull -q --platform "$plat" "$image" >/dev/null
    fi
    # DISTRO_PRE_UPGRADE is baked in (with the package lists it needs;
    # CLEAN drops them again), then clean by construction (DESIGN.md
    # section 9), then export that container.
    pre=""
    if [ -n "${DISTRO_PRE_UPGRADE:-}" ]; then
        case "$DISTRO_FAMILY" in
            apt) pre="apt-get update -qq && { $DISTRO_PRE_UPGRADE; } && " ;;
            *) echo "DISTRO_PRE_UPGRADE: no refresh command for family $DISTRO_FAMILY" >&2; exit 1 ;;
        esac
    fi
    cid=$(docker create --platform "$plat" "$image" /bin/sh -c "$pre$CLEAN")
    trap 'docker rm -f "$cid" >/dev/null 2>&1 || true' EXIT
    docker start -a "$cid" >/dev/null
    # Flatten, drop Docker's own files, compress. GNU tar and xz run in a
    # helper container so this works the same on macOS and in CI.
    # Everything is written under a temporary name and renamed into place
    # only after the hygiene check: dist/ files are never rewritten in
    # place (other checkouts may hard-link them, and a failed build must
    # not replace a good one).
    tmp="dist/.build-$$-$name"
    docker export "$cid" | docker run -i --rm debian:13 bash -c '
        command -v xz >/dev/null || { apt-get update -qq && apt-get install -y -qq xz-utils; } >/dev/null 2>&1
        tar --delete -f - .dockerenv 2>/dev/null | xz -T0 -6' >"$tmp"
    docker rm -f "$cid" >/dev/null
    trap - EXIT
    files=$(xz -dc "$tmp" | tar -tf - | wc -l | tr -d ' ')
    if command -v sha256sum >/dev/null; then sum=(sha256sum); else sum=(shasum -a 256); fi
    printf '%s  %s\n' "$("${sum[@]}" "$tmp" | awk '{ print $1 }')" "$name" >"$tmp.sha256"
    printf 'size=%s\nfiles=%s\nimage=%s\nbuilt=%s\n' "$(wc -c <"$tmp" | tr -d ' ')" "$files" \
        "$DISTRO_IMAGE" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >"$tmp.meta"
    echo "    $(cat "$tmp.sha256")"
    # Refuse a dirty image.
    docker run --rm -v "$PWD":/src:ro -w /src andronix-test bash ci/check-image.sh "$tmp" ||
        { rm -f "$tmp" "$tmp.sha256" "$tmp.meta"; echo "dist/$name failed the hygiene check" >&2; exit 1; }
    mv -f "$tmp.sha256" "dist/$name.sha256"
    mv -f "$tmp.meta" "dist/${name%.tar.xz}.meta"
    mv -f "$tmp" "dist/$name"
done
