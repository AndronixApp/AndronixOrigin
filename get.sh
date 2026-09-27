#!/data/data/com.termux/files/usr/bin/sh
# Andronix bootstrap: downloads the andronix binary for this CPU, then runs
# it with the arguments you pass, e.g.
#
#   curl -fsSL https://dl.andronix.app/get.sh -o get.sh
#   sh get.sh install debian --de xfce
#
# Needs only sh and curl (or wget). Installs to $PREFIX/bin in Termux, else
# ~/.local/bin. ANDRONIX_SRC=<checkout> uses <checkout>/dist/andronix-<os>-<cpu>
# (from ci/build-go.sh) instead of downloading; ANDRONIX_OS=android|linux
# overrides the detection.

# R2 (decided default); ANDRONIX_MIRROR matches the installer's setting.
BASE=${ANDRONIX_DL:-${ANDRONIX_MIRROR:-https://dl.andronix.app}/bin/latest}

if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then O=$(printf '\033[38;5;208m') R=$(printf '\033[38;5;203m') Z=$(printf '\033[0m'); else O='' R='' Z=''; fi
say() { printf '  %s●%s %s\n' "$O" "$Z" "$1"; }
fail() {
    printf '\n  %s✗ %s%s\n    %s\n\n  Need a hand?  https://chat.andronix.app  ·  https://docs.andronix.app  ·  support@andronix.app\n\n' "$R" "$1" "$Z" "$2"
    exit 1
}

# CPU of the Termux userland (a 64-bit phone can run 32-bit Termux).
arch=$(dpkg --print-architecture 2>/dev/null || uname -m)
case "$arch" in
    aarch64|arm64) arch=aarch64 ;;
    arm|armhf|armv7*|armv8l) arch=arm ;;
    i*86|x86) arch=i686 ;;
    x86_64|amd64) arch=x86_64 ;;
    *) fail "Unsupported CPU ($arch)" "Please tell us on Discord which phone you have." ;;
esac

# Termux gets the GOOS=android build (bionic-aware Go runtime; the linux
# one can die of SIGSYS on Android 8-11). Elsewhere, the static linux one.
os=${ANDRONIX_OS:-}
if [ -z "$os" ]; then
    if [ "$(uname -o 2>/dev/null)" = Android ] || [ -e /system/bin/linker ] || [ -e /system/bin/linker64 ]; then os=android; else os=linux; fi
fi
name="andronix-$os-$arch"

curl_works() { command -v curl >/dev/null 2>&1 && curl --version >/dev/null 2>&1; }
wget_works() { command -v wget >/dev/null 2>&1 && wget --version >/dev/null 2>&1; }
fetch() { # fetch URL FILE: curl, else wget
    # --retry alone skips a connection cut mid-TLS ("unexpected eof",
    # curl 55/56) on a lossy network; --retry-all-errors (curl 7.71+)
    # retries that too.
    ra=""
    curl --help all 2>/dev/null | grep -q -- --retry-all-errors && ra=--retry-all-errors
    if curl_works && curl -fsSL --retry 2 $ra --retry-delay 2 --connect-timeout 10 -o "$2" "$1"; then return 0; fi
    # A network with IPv6 that goes nowhere (seen on an Android 9
    # emulator) hangs every connect over it: try IPv4 only.
    if curl_works && curl -4 -fsSL --retry 3 $ra --retry-delay 3 --connect-timeout 20 -o "$2" "$1"; then return 0; fi
    wget_works && wget -q -T 20 -t 4 -O "$2" "$1"
}

if [ -n "${PREFIX:-}" ] && [ -d "$PREFIX/bin" ]; then bin="$PREFIX/bin"; else bin="$HOME/.local/bin"; fi
mkdir -p "$bin" || fail "Can't write to $bin" "Check that Termux has storage space left."
dst="$bin/andronix"

if [ -n "${ANDRONIX_SRC:-}" ]; then
    say "Installing andronix ($arch) from $ANDRONIX_SRC"
    cp "$ANDRONIX_SRC/dist/$name" "$dst.new" 2>/dev/null ||
        fail "No build found" "Run ci/build-go.sh in $ANDRONIX_SRC first."
else
    # On an old Termux, installing one package can upgrade libcurl without
    # OpenSSL, and curl can't start ("cannot locate symbol SSL_..."); the
    # app's command runs 'pkg upgrade' first. wget, if there, still works.
    curl_works || wget_works ||
        fail "Can't download: curl doesn't work" "Update Termux's packages, then run the same command again: pkg upgrade -y"
    say "Downloading andronix for your phone ($arch)"
    # products-api's resolver first (it can pin or move the download), with
    # a 3 s limit; the answer's sha256 must match. Anything else falls back
    # to the built-in URL below. t=1 only with telemetry on.
    if [ -z "${ANDRONIX_DL:-}${ANDRONIX_MIRROR:-}${ANDRONIX_NO_RESOLVE:-}" ] && command -v sha256sum >/dev/null 2>&1; then
        t="&t=1"
        { [ -n "${ANDRONIX_NO_TELEMETRY:-}" ] || [ "${DO_NOT_TRACK:-}" = 1 ] ||
            grep -qx ENABLED=no "${ANDRONIX_HOME:-$HOME/.andronix}/telemetry" 2>/dev/null; } && t=""
        q="item=bin&arch=$arch&flavor=$os&v=get.sh$t"
        j=$( { curl_works && curl -fsS -m 3 "${ANDRONIX_API:-https://products.andronix.xyz}/v1/installer/resolve?$q"; } 2>/dev/null ||
            { wget_works && wget -q -T 3 -t 1 -O - "${ANDRONIX_API:-https://products.andronix.xyz}/v1/installer/resolve?$q"; } 2>/dev/null) || j=""
        rurl=$(printf '%s' "$j" | sed -n 's/.*"url"[[:space:]]*:[[:space:]]*"\(https:[^"]*\)".*/\1/p' | head -1)
        rsha=$(printf '%s' "$j" | sed -n 's/.*"sha256"[[:space:]]*:[[:space:]]*"\([0-9a-f]\{64\}\)".*/\1/p' | head -1)
        if [ -n "$rurl" ] && [ -n "$rsha" ] && fetch "$rurl" "$dst.new" &&
            [ "$(sha256sum "$dst.new" | awk '{ print $1 }')" = "$rsha" ]; then
            resolved=1
        else
            rm -f "$dst.new"
        fi
    fi
    # andronix-<cpu> is the old name (the android build), for mirrors that
    # only have that.
    [ -n "${resolved:-}" ] || fetch "$BASE/$name" "$dst.new" ||
        { [ "$os" = android ] && fetch "$BASE/andronix-$arch" "$dst.new" && name="andronix-$arch"; } ||
        fail "Download failed" "Check your internet connection, then run the same command again."
    if [ -z "${resolved:-}" ] && fetch "$BASE/SHA256SUMS" "$dst.sums" 2>/dev/null && command -v sha256sum >/dev/null 2>&1; then
        want=$(awk -v f="$name" '$2 == f || $2 == "*" f { print $1 }' "$dst.sums")
        got=$(sha256sum "$dst.new" | awk '{ print $1 }')
        [ -z "$want" ] || [ "$want" = "$got" ] || { rm -f "$dst.new" "$dst.sums"; fail "Download is damaged" "Run the same command again."; }
    fi
    rm -f "$dst.sums"
fi
chmod 755 "$dst.new" && mv -f "$dst.new" "$dst"
# The phase-1 bash prototype lived in $PREFIX/share/andronix; the binary
# doesn't use it, so drop it to avoid confusion.
[ -n "${PREFIX:-}" ] && [ -f "$PREFIX/share/andronix/lib/ui.sh" ] && rm -rf "$PREFIX/share/andronix"

# The old installers ran from the app's copied command; keep that flow.
command -v proot >/dev/null 2>&1 || say "Tip: andronix needs proot. If it asks, run: pkg install proot -y"
if [ $# -gt 0 ]; then
    # "curl ... | sh -s -- install ..." leaves stdin on the pipe, so the
    # installer's questions would get no answers; give it the terminal back.
    if [ ! -t 0 ] && (: </dev/tty) 2>/dev/null; then
        exec "$dst" "$@" </dev/tty
    fi
    exec "$dst" "$@"
fi
say "Done. Run: andronix help"
