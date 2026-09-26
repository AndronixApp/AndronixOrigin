# The smoke-test box (tests/Dockerfile, built as $BASE) plus Xvfb standing
# in for Termux:X11's server. Used by tests/x11.sh. Not shipped.
ARG BASE=andronix-test-x11-base
FROM ${BASE}
USER root
RUN apt-get update -qq && apt-get install -y -qq --no-install-recommends \
      xvfb x11-utils x11-apps netpbm >/dev/null && rm -rf /var/lib/apt/lists/*
COPY fake-termux-x11 /usr/local/bin/termux-x11
USER termux
