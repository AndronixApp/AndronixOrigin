# A Termux-like test box: a non-root user with bash, curl, tar, xz and
# Termux's own proot fork (built from source, since Debian's proot 5.1.0
# segfaults on arm64 hosts). Used by tests/smoke.sh. Not shipped.
FROM debian:13 AS build
RUN apt-get update -qq && apt-get install -y -qq --no-install-recommends \
      git ca-certificates make gcc gawk libc6-dev libtalloc-dev >/dev/null
RUN git clone --depth 1 https://github.com/termux/proot /proot && \
    make -s -C /proot/src proot HAS_LOADER_32BIT=

FROM debian:13
RUN apt-get update -qq && apt-get install -y -qq --no-install-recommends \
      libtalloc2 curl ca-certificates xz-utils procps >/dev/null && rm -rf /var/lib/apt/lists/*
COPY --from=build /proot/src/proot /usr/local/bin/proot
RUN useradd -m -s /bin/bash termux
USER termux
WORKDIR /home/termux
ENV TERM=xterm-256color LANG=C.UTF-8
# Tests never send telemetry (a smoke check turns it on against a dead URL).
ENV ANDRONIX_NO_TELEMETRY=1
# ...and never ask the production resolver (unit tests and getsh-test cover it).
ENV ANDRONIX_NO_RESOLVE=1
