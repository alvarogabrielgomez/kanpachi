package main

import (
	"context"
	"fmt"
	"runtime"
	"runtime/debug"

	"github.com/accentiostudios/kanpachi/internal/engineid"
)

// Version gets stamped by whoever builds, with
// `-ldflags "-X main.Version=$VERSION"`.
//
// It is the same mechanism the seed already uses (`registry/cli.Version`), and
// what it leaves in a hand-built binary is `dev`, which is the truth: an
// unstamped binary did not come out of a release, and saying otherwise would
// make a bug report point at a published version that is not the one that
// failed.
var Version = "dev"

// cmdVersion says which version this is, and which batch it came from.
//
// # Why the commit as well
//
// Because the version number only changes on a release, and a good part of what
// gets tested sits between two. `go build` stamps the repository's state into the
// binary, so it comes for free and answers the question that actually gets asked
// when something goes wrong: exactly which code is this.
func cmdVersion(_ context.Context, op options, _ []string) error {
	rev, dirty := revision()

	// The engine gets read from the FILE, without running it, thanks to the
	// sentinels its build stamps inside. Empty means it exists and predates the
	// sentinel; missing or unreadable stays quiet, because this order describes
	// this binary and the engine is an extra, not a condition.
	engine := ""
	if path := enginePath(); path != "" {
		if id, err := engineid.Scan(path); err == nil {
			engine = id.String()
		}
	}

	if op.json {
		fmt.Printf("{\"version\":%q,\"revision\":%q,\"dirty\":%v,\"engine\":%q,\"go\":%q,\"os\":%q}\n",
			Version, rev, dirty, engine, runtime.Version(), runtime.GOOS)
		return nil
	}

	// One version for the three faces, and saying so is part of the answer: the
	// daemon, the terminal and the window come out of the same cut and travel
	// together.
	fmt.Printf("kanpachi %s (daemon, cli, ui)\n", Version)
	if rev != "" {
		mark := ""
		if dirty {
			mark = " (with uncommitted changes)"
		}
		fmt.Printf("  commit  %s%s\n", rev, mark)
	}
	if engine != "" {
		fmt.Printf("  engine  %s\n", engine)
	}
	fmt.Printf("  go      %s on %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
	return nil
}

// revision reads the git stamp `go build` leaves in the binary.
//
// It returns empty when the build used `-buildvcs=false`, which is what
// `scripts/build-deb.sh` does when the checkout belongs to another user. There
// is nothing to say there, so it says nothing instead of inventing a value.
func revision() (string, bool) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "", false
	}
	var rev string
	var dirty bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	return rev, dirty
}
