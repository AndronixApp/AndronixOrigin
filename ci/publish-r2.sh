#!/usr/bin/env bash
# Upload build output to the PUBLIC Andronix bucket (andronix-dl, served at
# https://dl.andronix.app). Only free artefacts may ever go there; paid
# Modded images never go there.
#
#   ci/publish-r2.sh rootfs <distro>     dist/<id>-<ver>-<arch>.tar.xz(.sha256, .meta)
#   ci/publish-r2.sh bin <version>       dist/andronix-{android-,linux-,}<arch> + SHA256SUMS -> bin/<version>/ and bin/latest/
#   ci/publish-r2.sh getsh [version]     get.sh -> get.sh (short cache; the app and docs fetch it)
#
# Env: R2_ACCOUNT_ID, AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY (a token
# scoped to this bucket). DRY_RUN=1 prints what would be uploaded.
#
# Guard: every object key must match the free allowlist below, and no key,
# file name or file content marker may look paid/modded. Anything else
# stops the whole upload before a single byte is sent.
set -euo pipefail
cd "$(dirname "$0")/.."

BUCKET=andronix-dl            # public; nothing else is ever uploaded
usage="usage: publish-r2.sh rootfs <distro> | bin <version> | getsh"
kind=${1:?$usage}
arg=${2:-}
[ -n "$arg" ] || [ "$kind" = getsh ] || { echo "$usage" >&2; exit 1; }

allowed() {
    local key=$1
    case "$key" in
        *modded*|*Modded*|*paid*|*premium*) return 1 ;;
    esac
    # Versions: numbers, or rolling/stable (Kali, Arch, Void, Manjaro).
    [[ $key =~ ^rootfs/[a-z0-9]+/([0-9.]+|rolling|stable)/[a-z0-9]+-([0-9.]+|rolling|stable)-(aarch64|arm|i686|x86_64)\.(tar\.xz|tar\.xz\.sha256|meta)$ ]] && return 0
    [ "$key" = get.sh ] && return 0
    [[ $key =~ ^bin/(latest|[0-9]+\.[0-9]+\.[0-9]+([-.][a-z0-9.]+)?)/(andronix-((android|linux)-)?(aarch64|arm|i686|x86_64)|SHA256SUMS)$ ]] && return 0
    return 1
}

# Build the upload plan: "local-file key cache-control", checksums last.
plan=()
case "$kind" in
    rootfs)
        conf="distros/$arg.conf"
        [ -f "$conf" ] || { echo "no $conf" >&2; exit 1; }
        grep -qiE '^(DISTRO_MODDED|DISTRO_PAID)=' "$conf" && { echo "refusing: $conf is marked paid/modded" >&2; exit 1; }
        # shellcheck source=/dev/null
        . "$conf"
        shopt -s nullglob
        for f in dist/"$DISTRO_ID-$DISTRO_VERSION"-*.tar.xz dist/"$DISTRO_ID-$DISTRO_VERSION"-*.meta; do
            plan+=("$f rootfs/$DISTRO_ID/$DISTRO_VERSION/$(basename "$f") public,max-age=3600")
        done
        for f in dist/"$DISTRO_ID-$DISTRO_VERSION"-*.tar.xz.sha256; do
            plan+=("$f rootfs/$DISTRO_ID/$DISTRO_VERSION/$(basename "$f") public,max-age=300")
        done
        ;;
    bin)
        for dest in "$arg" latest; do
            # andronix-android-<cpu> (Termux), andronix-linux-<cpu> (inside
            # distros, and off Android), andronix-<cpu> (the android build
            # under the old name, for older get.sh and self-updates).
            for cpu in aarch64 arm i686 x86_64; do
                for f in dist/andronix-android-$cpu dist/andronix-linux-$cpu dist/andronix-$cpu; do
                    [ -f "$f" ] && plan+=("$f bin/$dest/$(basename "$f") public,max-age=300")
                done
            done
            plan+=("dist/SHA256SUMS bin/$dest/SHA256SUMS public,max-age=300")
        done
        ;;
    getsh)
        # It downloads bin/latest; it must not point anywhere else.
        grep -q '/bin/latest}$' get.sh || { echo "refusing: get.sh doesn't default to bin/latest" >&2; exit 1; }
        sh -n get.sh || { echo "refusing: get.sh has a syntax error" >&2; exit 1; }
        plan+=("get.sh get.sh public,max-age=300")
        ;;
    *) echo "unknown kind $kind" >&2; exit 1 ;;
esac
[ ${#plan[@]} -gt 0 ] || { echo "nothing to upload" >&2; exit 1; }

# Guard pass over the whole plan first.
bad=0
for p in "${plan[@]}"; do
    read -r f key _ <<<"$p"
    if ! allowed "$key"; then echo "REFUSED (not a free artefact path): $key" >&2; bad=1; fi
    if [ ! -f "$f" ]; then echo "missing: $f" >&2; bad=1; fi
done
[ "$bad" = 0 ] || { echo "upload stopped; nothing was sent" >&2; exit 1; }

# Rootfs must pass the hygiene gate right before upload, too.
if [ "$kind" = rootfs ]; then
    for p in "${plan[@]}"; do
        read -r f _ _ <<<"$p"
        case "$f" in *.tar.xz) bash ci/check-image.sh "$f" >/dev/null || { echo "hygiene check failed: $f" >&2; exit 1; } ;; esac
    done
fi

ep="https://${R2_ACCOUNT_ID:?set R2_ACCOUNT_ID}.r2.cloudflarestorage.com"
# Rollback: rebuilds of a version (rolling distros, weekly refreshes)
# overwrite the same key, so keep the tarball being replaced as <key>.prev
# (a server-side copy; nothing if the key is new). See delivery.md.
if [ "$kind" = rootfs ] && [ -z "${DRY_RUN:-}" ]; then
    for p in "${plan[@]}"; do
        read -r _ key _ <<<"$p"
        case "$key" in *.tar.xz)
            aws s3 cp "s3://$BUCKET/$key" "s3://$BUCKET/$key.prev" --endpoint-url "$ep" --only-show-errors 2>/dev/null ||
                echo "no previous $key to keep (new file)" ;;
        esac
    done
fi
for p in "${plan[@]}"; do
    read -r f key cache <<<"$p"
    if [ -n "${DRY_RUN:-}" ]; then
        echo "would upload $f -> s3://$BUCKET/$key ($cache)"
    else
        aws s3 cp "$f" "s3://$BUCKET/$key" --endpoint-url "$ep" --cache-control "$cache" --only-show-errors
        echo "uploaded https://dl.andronix.app/$key"
    fi
done
