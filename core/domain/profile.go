package domain

import "encoding/json"

// The machine's own profile: what this installation of Kanpachi remembers
// about the person using it, independent of any room.
//
// # Why this is daemon state and not each face's business
//
// Because there are three faces on one machine — the window, the terminal and
// the wizard — and the name is ONE fact about the machine, not three. When
// nobody owned it, each face invented its own store in the same data folder
// and the machine ended up with two names: measured on 2026-08-18, a window
// that said "Alvaro" and a terminal that entered the room as "AlvaroGDeskt".
// The daemon owns it for the same reason it owns the seed: it is the only
// piece all three faces already share, and in an installed product it is the
// only one that can write in the data directory at all.
//
// # Why it grows here instead of one file per fact
//
// Because the next thing a face wants to remember about the person belongs
// next to this one. One file with named fields extends by adding a field; a
// file per fact extends by adding a file, a store method, a protocol method
// and a migration.
type Profile struct {
	// Nick is the name other members see. Zero means nobody chose one yet,
	// which is a legitimate answer and never an error: what answers then is
	// [NicknameFromHost], as a SUGGESTION that is not written down.
	Nick Nickname
}

// machineProfileJSON is the shape on disk. Named fields, so a second one can be added
// without a version number.
type machineProfileJSON struct {
	Nickname string `json:"nickname"`
}

// EncodeProfile serialises the profile. Indented, because this file is in the
// clear and somebody opening it should be able to read it.
func EncodeProfile(p Profile) ([]byte, error) {
	return json.MarshalIndent(machineProfileJSON{Nickname: p.Nick.String()}, "", "  ")
}

// ParseProfile reads the profile, and is DELIBERATELY tolerant where the rest
// of the persisted shapes are strict.
//
// [DecodeHostedRoom] fails the whole file on a bad field because a room half
// read is a room that cannot be reopened correctly. Here the opposite is true:
// a name that no longer validates — the rule tightened, somebody edited the
// file, a future version wrote a longer one — must not leave a person unable
// to open a room. It comes back as the zero profile, which reads as "nobody
// chose a name", and the first thing any face does with that is offer to pick
// one.
//
// Unknown FIELDS are ignored for the same reason and one more: a newer daemon
// that added a second field must not brick an older one that rolls back.
func ParseProfile(raw []byte) Profile {
	var j machineProfileJSON
	if err := json.Unmarshal(raw, &j); err != nil {
		return Profile{}
	}
	nick, err := ParseNickname(j.Nickname)
	if err != nil {
		return Profile{}
	}
	return Profile{Nick: nick}
}
