//go:build !linux

package compat

// RunOne needs Linux; elsewhere (tests on a Mac) nothing is probed.
func RunOne(name string) string { return "unsupported" }
