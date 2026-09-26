# Build container for the GOOS=android binaries that need cgo (arm, i686,
# x86_64 link through the NDK's clang, as Termux's own Go packages do).
# Go runs natively; the NDK ships for linux-x86_64 only, so on arm64 hosts
# its clang runs through binfmt (qemu), with amd64 glibc from multiarch.
# (Go itself under qemu crashes, hence not a whole amd64 image.)
# ci/build-go.sh uses this when ANDROID_NDK_HOME isn't set.
FROM golang:1.26.5-bookworm
ARG NDK=r30
ARG NDK_SHA1=5107f898313790e449e87eee2183d9a20602dee9
RUN if [ "$(dpkg --print-architecture)" != amd64 ]; then dpkg --add-architecture amd64; x=":amd64"; fi && \
    apt-get update -qq && apt-get install -y -qq --no-install-recommends unzip "libc6${x:-}" "zlib1g${x:-}" >/dev/null && \
    rm -rf /var/lib/apt/lists/* && \
    curl -fsSLo /tmp/ndk.zip "https://dl.google.com/android/repository/android-ndk-$NDK-linux.zip" && \
    echo "$NDK_SHA1  /tmp/ndk.zip" | sha1sum -c - && \
    unzip -q /tmp/ndk.zip -d /opt && rm /tmp/ndk.zip && mv /opt/android-ndk-$NDK /opt/ndk
ENV ANDROID_NDK_HOME=/opt/ndk
