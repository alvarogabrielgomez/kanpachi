//go:build !windows && !linux

package main

// Outside Windows and Linux there is nothing to diagnose, because there is no
// product to diagnose: the local channel is written for those two.
//
// It gets declared anyway so `go build ./...` keeps compiling, which is what
// keeps the rest of the binary buildable in CI on any system.

func systemChecks() []check { return []check{channelCheck()} }

func elevationHint() string {
	return "The local channel is written for Windows and for Linux."
}

// enginePath answers empty: with no product there is no engine to name.
func enginePath() string { return "" }
