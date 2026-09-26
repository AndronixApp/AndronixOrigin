#!/usr/bin/env bash
# The public-bucket guard in ci/publish-r2.sh must allow only free
# artefacts and refuse anything that looks paid or modded.
set -u
cd "$(dirname "$0")/.." || exit 1
eval "$(sed -n '/^allowed()/,/^}/p' ci/publish-r2.sh)"
fail=0
for k in rootfs/debian/13/debian-13-aarch64.tar.xz rootfs/ubuntu/26.04/ubuntu-26.04-arm.tar.xz.sha256 \
    rootfs/ubuntu24/24.04/ubuntu24-24.04-x86_64.meta rootfs/kali/rolling/kali-rolling-arm.tar.xz rootfs/manjaro/stable/manjaro-stable-aarch64.tar.xz.sha256 bin/latest/andronix-arm bin/latest/andronix-android-arm bin/0.3.0/andronix-linux-x86_64 bin/0.2.0/SHA256SUMS get.sh; do
    allowed "$k" || { echo "wrongly refused: $k"; fail=1; }
done
for k in modded/ubuntu-xfce.tar.xz rootfs/ubuntu-modded/26.04/x.tar.xz rootfs/debian/13/secret.txt \
    bin/latest/andronix-aarch64.exe bin/latest/andronix-windows-arm paid/debian.tar.xz rootfs/debian/13/debian-13-aarch64-modded.tar.xz \
    ../rootfs/debian/13/debian-13-aarch64.tar.xz rootfs/kali/latest/kali-latest-arm.tar.xz rootfs/kali/rolling-modded/kali-rolling-arm.tar.xz get.sh.bak bin/get.sh get-modded.sh; do
    allowed "$k" && { echo "wrongly allowed: $k"; fail=1; }
done
[ "$fail" = 0 ] && echo "publish guard: ok"
exit "$fail"
