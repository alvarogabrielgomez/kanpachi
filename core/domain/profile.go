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

	// Verbose is whether the faces narrate, step by step, what the daemon is
	// doing.
	//
	// It lives here and not in the window because the diary it turns on is the
	// DAEMON's, published over `progress` for whoever asks. The window kept it
	// in a file of its own, which made it read as a setting about a pane of
	// glass; it is a setting about how much this machine explains itself.
	Verbose bool

	// WindowW and WindowH are the size the window had when it was last closed.
	// Zero on both means nobody has resized it.
	//
	// The POSITION is deliberately not here, and that is not an oversight:
	// somebody who made the window taller wants it taller next time, while
	// where it happened to sit is a fact about which monitor was plugged in.
	//
	// They arrive already clamped to what the layout can draw, by the window,
	// because the minimum is a UI token and the daemon has no business knowing
	// what a screen is drawn with.
	WindowW int
	WindowH int

	// PendingUpdate is a published version newer than the one running, found by
	// whichever face asked last, or empty when none is known.
	//
	// **A shared answer and not a cache with a lifetime.** A published version
	// does not get unpublished, so it never goes stale; what it saves is the
	// other face asking the same question of a channel that allows sixty an
	// hour per IP, shared by everybody behind one router. It clears itself the
	// day the running version catches up, which is the day somebody installed
	// it.
	PendingUpdate string
}

// SettingsPatch is a change to [Profile] where nil means "leave it alone".
//
// Pointers and not values, and that is the whole point: without them, a window
// that only turns narration on would send a zero size along with it and wipe
// the one that was there.
//
// The name is not in it. It has validation and a derived suggestion behind it,
// both of them in `SetNickname`, so it keeps the writer it already had: one
// writer per fact.
type SettingsPatch struct {
	Verbose       *bool
	WindowW       *int
	WindowH       *int
	PendingUpdate *string
}

// machineProfileJSON is the shape on disk. Named fields, so a second one can be added
// without a version number.
type machineProfileJSON struct {
	Nickname      string `json:"nickname"`
	Verbose       bool   `json:"verbose,omitempty"`
	WindowW       int    `json:"window_width,omitempty"`
	WindowH       int    `json:"window_height,omitempty"`
	PendingUpdate string `json:"pending_update,omitempty"`
}

// EncodeProfile serialises the profile. Indented, because this file is in the
// clear and somebody opening it should be able to read it.
func EncodeProfile(p Profile) ([]byte, error) {
	return json.MarshalIndent(machineProfileJSON{
		Nickname:      p.Nick.String(),
		Verbose:       p.Verbose,
		WindowW:       p.WindowW,
		WindowH:       p.WindowH,
		PendingUpdate: p.PendingUpdate,
	}, "", "  ")
}

// ParseProfile reads the profile, and is DELIBERATELY tolerant where the rest
// of the persisted shapes are strict.
//
// [DecodeHostedRoom] fails the whole file on a bad field because a room half
// read is a room that cannot be reopened correctly. Here the opposite is true:
// a name that no longer validates — the rule tightened, somebody edited the
// file, a future version wrote a longer one — must not leave a person unable
// to open a room. The name comes back zero, which reads as "nobody chose a
// name", and the first thing any face does with that is offer to pick one.
//
// **The tolerance is per FIELD and not per file**, and that changed when the
// file grew. Returning the zero profile on a bad name cost nothing while the
// name was all there was; with four fields it would throw away the window size
// and the update notice because somebody typed a name too long by hand.
//
// Unknown FIELDS are ignored for the same reason and one more: a newer daemon
// that added a fifth field must not brick an older one that rolls back.
func ParseProfile(raw []byte) Profile {
	var j machineProfileJSON
	if err := json.Unmarshal(raw, &j); err != nil {
		return Profile{}
	}
	p := Profile{
		Verbose:       j.Verbose,
		WindowW:       j.WindowW,
		WindowH:       j.WindowH,
		PendingUpdate: j.PendingUpdate,
	}
	if nick, err := ParseNickname(j.Nickname); err == nil {
		p.Nick = nick
	}
	return p
}
