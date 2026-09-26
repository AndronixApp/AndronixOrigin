// Package andronixdistros holds the data files (distros/*.conf,
// desktops/*.conf and the dev packs in mods/packs) that the Go CLI reads.
package andronixdistros

import "embed"

// Data is embedded into the binary so a single file is all a phone needs.
//
//go:embed distros/*.conf desktops/*.conf editions.conf mods/packs/*.conf mods/packs/files/*
var Data embed.FS

// Preload holds guest/preload/<arch>/libandronix-fchmodat.so (built by
// ci/build-preload.sh): an LD_PRELOAD shim for glibc 2.39+'s fchmodat2,
// which proot doesn't translate on Linux 6.6+ (docs/ports/void.md).
//
//go:embed guest/preload/*/*.so
var Preload embed.FS

// SelfBin holds selfbin/andronix-linux.gz in the GOOS=android builds: the
// static GOOS=linux build for the same CPU, which the installer copies
// into distros (ci/build-go.sh puts it there; other builds have none).
//
//go:embed all:selfbin
var SelfBin embed.FS
