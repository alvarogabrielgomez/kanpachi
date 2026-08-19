package main

// The MACHINE's name, which is what an unnamed room gets called after.
//
// # What this file no longer does
//
// Save the nickname. It had a `nickname.txt` of its own in the data directory,
// with a comment saying that sharing it with `roomprobe` kept anybody from
// ending up «with two different names depending on which face they entered
// through». It unified two of the three faces and forgot the window, which kept
// its own next door, so it produced exactly what it existed to prevent: measured
// on 2026-08-18, a window saying «Alvaro» and a room showing «AlvaroGDeskt».
//
// Today the daemon keeps the nickname and this face asks it for one. See
// `nickname` in `comandos.go` and [protocol.MethodNickname]. Deriving one from
// the machine's name went the same way, to [domain.NicknameFromHost], for the
// same reason: it was a copy, and the one here wrote its result down.

import (
	"os"
	"strings"
)

// machineName is this machine's name, for giving to the ROOM when whoever opens
// it gives it none. It is nobody's nickname.
//
// It travels INSIDE the encrypted card and the registry never learns it, so this
// leaks the server's name to no third party: it is seen by whoever receives the
// link, which is exactly who it was meant for.
func machineName() string {
	h, err := os.Hostname()
	if err != nil || strings.TrimSpace(h) == "" {
		return "Kanpachi"
	}
	return h
}
