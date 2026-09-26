#!/usr/bin/env bash
# get.sh on a half-upgraded Termux, in the Termux-like box: with a curl
# that can't start (libcurl newer than OpenSSL) wget is used, else a
# friendly error that says to upgrade Termux's packages. Downloads come from dist/ over
# file:// (fake wget reads the same path).
#
#   tests/getsh-test.sh          (needs dist/andronix-linux-<cpu>: ci/build-go.sh)
set -u
cd "$(dirname "$0")/.." || exit 1
box="andronix-getsh-$$"
pass=0 fail=0
docker build -q -t andronix-test -f tests/Dockerfile tests >/dev/null 2>&1 || { echo "docker build failed"; exit 1; }
docker run -d --init --name "$box" -v "$PWD":/src:ro andronix-test sleep infinity >/dev/null
trap 'docker rm -f "$box" >/dev/null 2>&1' EXIT
run() { docker exec "$box" bash -c "$1"; }
check() {
    if run "$2" >/tmp/andronix-getsh.out 2>&1; then pass=$((pass + 1)); echo "  ok   $1"; else fail=$((fail + 1)); echo "  FAIL $1"; sed 's/^/       /' /tmp/andronix-getsh.out | tail -12; fi
}
# A Termux-ish prefix with a broken curl and, per case, a wget.
setup='rm -rf ~/px && mkdir -p ~/px/bin ~/px/tmp && export PREFIX=~/px PATH=~/px/bin:$PATH ANDRONIX_DL=file:///src/dist
printf "#!/bin/sh\necho \"CANNOT LINK EXECUTABLE curl: cannot locate symbol SSL_set_quic_tls_early_data_enabled\" >&2\nexit 1\n" >~/px/bin/curl; chmod +x ~/px/bin/curl'
wget='printf "#!/bin/sh\n[ \"\$1\" = --version ] && exit 0\nwhile [ \$# -gt 1 ]; do [ \"\$1\" = -O ] && out=\$2; shift; done\ncp \"\${1#file://}\" \"\$out\"\n" >~/px/bin/wget; chmod +x ~/px/bin/wget'
echo "get.sh on a broken Termux curl"
check "wget when curl can't start" "$setup; $wget; sh /src/get.sh && ~/px/bin/andronix version"
check "friendly error with neither" "$setup; out=\$(sh /src/get.sh 2>&1); [ \$? = 1 ] && echo \"\$out\" | grep -q 'pkg upgrade -y'"
check "a working curl is used" "$setup; ln -sf /usr/bin/curl ~/px/bin/curl; sh /src/get.sh && ~/px/bin/andronix version"

# The resolver (products-api) first, then the built-in URL; offline: a fake
# curl answers the resolver and serves https://*/bin/.../<file> from dist/.
arch=$(run 'case $(uname -m) in aarch64) echo aarch64 ;; x86_64) echo x86_64 ;; esac')
sum=$(run "sha256sum /src/dist/andronix-linux-$arch | cut -d' ' -f1")
docker exec -i "$box" sh -c 'cat >/tmp/fakecurl' <<'FAKE'
#!/bin/sh
# Fake curl: logs each URL; the resolver answers $RESOLVE_JSON; https
# downloads come from /src/dist/<basename>.
[ "$1" = --version ] && exit 0
out="" url=""
while [ $# -gt 0 ]; do case $1 in -o) out=$2; shift ;; http*) url=$1 ;; esac; shift; done
echo "$url" >>~/curl.log
case $url in
    *installer/resolve*) [ -n "$RESOLVE_JSON" ] || exit 22; printf '%s' "$RESOLVE_JSON" ;;
    https://*/SHA256SUMS) cp /src/dist/SHA256SUMS "$out" ;;
    https://*) cp "/src/dist/${url##*/}" "$out" ;;
    *) exit 6 ;;
esac
FAKE
rsetup='rm -rf ~/px ~/curl.log && mkdir -p ~/px/bin && export ANDRONIX_NO_RESOLVE= PREFIX=~/px PATH=~/px/bin:$PATH && cp /tmp/fakecurl ~/px/bin/curl && chmod +x ~/px/bin/curl'
good="{\"url\":\"https://cdn.example/bin/9.9.9/andronix-linux-$arch\",\"sha256\":\"$sum\",\"size\":1,\"alt\":[],\"version\":\"9.9.9\"}"
bad="{\"url\":\"https://cdn.example/bin/9.9.9/andronix-linux-$arch\",\"sha256\":\"$(printf '0%.0s' $(seq 64))\"}"
echo "get.sh and the resolver"
check "resolved URL used, checksum verified" "$rsetup; RESOLVE_JSON='$good' sh /src/get.sh && grep -q 'resolve?item=bin&arch=$arch&flavor=linux&v=get.sh' ~/curl.log && grep -qx 'https://cdn.example/bin/9.9.9/andronix-linux-$arch' ~/curl.log && ! grep -q dl.andronix.app ~/curl.log"
check "wrong checksum falls back to dl.andronix.app" "$rsetup; RESOLVE_JSON='$bad' sh /src/get.sh && grep -q 'dl.andronix.app/bin/latest/andronix-linux-$arch' ~/curl.log && ~/px/bin/andronix version"
check "resolver down falls back" "$rsetup; RESOLVE_JSON= sh /src/get.sh && grep -q 'dl.andronix.app/bin/latest/andronix-linux-$arch' ~/curl.log"
check "t=1 only with telemetry on" "$rsetup; ANDRONIX_NO_TELEMETRY= RESOLVE_JSON='$good' sh /src/get.sh >/dev/null && grep -q 'v=get.sh&t=1' ~/curl.log && rm ~/curl.log && ANDRONIX_NO_TELEMETRY=1 RESOLVE_JSON='$good' sh /src/get.sh >/dev/null && ! grep -q 't=1' ~/curl.log"
echo "  $pass passed, $fail failed"
[ "$fail" = 0 ]
