package domain

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ---- The book of hosts this machine has played with -----------------------
//
// This is the half of decision 25 that a signature alone cannot buy. Verifying
// a card or a lobby answer proves the key that signed it is the key the
// registry pinned; it says nothing about WHO that key belongs to. Continuity
// does: a host you have played with five times, arriving with the same key, is
// the same installation. A known nickname arriving with a different key is the
// case the whole decision exists for.
//
// It is the mechanism SSH and Signal use, and it inherits their limit, which is
// stated rather than hidden: the FIRST time there is nothing to compare
// against.

// MaxKnownHosts caps the book.
//
// It is a group of friends, not an address book, and a cap keeps a file that is
// read at every join from growing without anybody deciding it should. When it
// is full the least recently seen entry makes room, which is the one whose
// absence costs least.
const MaxKnownHosts = 200

// FingerprintGroups and fingerprintGroupSize give the printed shape: five
// groups of four decimal digits.
//
// Signal prints 60 decimal digits because they are meant to be read out loud
// over a phone call. These are compared by pasting them into a chat, so they
// are short enough that somebody will actually look at both ends.
const (
	FingerprintGroups    = 5
	fingerprintGroupSize = 4
)

// KnownHost is one host, remembered by its key.
type KnownHost struct {
	// Key is the host's long-term public key, which is the identity here. The
	// nickname is a label and can be anything; this cannot.
	Key []byte
	// Nick is the last nickname this key introduced itself with.
	Nick string
	// FirstSeen and LastSeen bracket the relationship, and Rooms counts how
	// many times it was joined. They exist for the sentence the confirmation
	// dialog says, "you have played with this host five times", which is what
	// makes a fingerprint mean something to somebody who will never compare it
	// digit by digit.
	FirstSeen time.Time
	LastSeen  time.Time
	Rooms     int
}

// HostVerdict is what the book says about a host that is about to be joined.
type HostVerdict uint8

const (
	// HostUnverified is that there was nothing to judge: no key arrived, or the
	// signature did not verify, so nothing may be looked up. It is the zero on
	// purpose, because it is the honest answer for every path that returns
	// before asking anybody.
	HostUnverified HostVerdict = iota
	// HostNew is a key this machine has never seen. It is the normal state of
	// every first invitation, and it is not a warning.
	HostNew
	// HostKnown is the same key and the same nickname as last time.
	HostKnown
	// HostRenamed is the same key under a different nickname. Somebody changed
	// their name, which is allowed and cheap to do: the key is what carries
	// through, so this is information and not an alarm.
	HostRenamed
	// HostKeyChanged is a known nickname arriving with a DIFFERENT key. It is
	// the case decision 25 was written for.
	//
	// It does not block. Signal started by blocking on a key change and moved
	// to advising, because people do reinstall their phones and reinstalling
	// Windows regenerates `identity.key` the same way. A block in a game is a
	// button that gets clicked without reading; a warning next to two
	// fingerprints is something somebody can act on.
	HostKeyChanged
)

func (v HostVerdict) String() string {
	switch v {
	case HostNew:
		return "new"
	case HostKnown:
		return "known"
	case HostRenamed:
		return "renamed"
	case HostKeyChanged:
		return "key-changed"
	default:
		return "unverified"
	}
}

// Fingerprint renders a key as five groups of four decimal digits.
//
// It is SHA-256 of the key, and each group is two bytes of that hash reduced to
// four digits. Twenty digits carry about 66 bits, which is not the 256 of the
// key and does not need to be: this exists to be compared by two people who
// already have a channel to compare it on. The hash is what keeps it from being
// a slice of the key itself, and reducing modulo 10000 is what keeps it to
// digits, which travel through any chat without a font mangling them.
//
// An empty or malformed key returns the empty string. There is no fingerprint
// of nothing, and printing one would be inventing a fact.
func Fingerprint(key []byte) string {
	if len(key) != ed25519.PublicKeySize {
		return ""
	}
	sum := sha256.Sum256(key)
	var b strings.Builder
	for i := 0; i < FingerprintGroups; i++ {
		if i > 0 {
			b.WriteByte(' ')
		}
		n := binary.BigEndian.Uint16(sum[i*2 : i*2+2])
		fmt.Fprintf(&b, "%0*d", fingerprintGroupSize, n%10000)
	}
	return b.String()
}

// KnownHosts is the book itself.
type KnownHosts struct {
	Hosts []KnownHost
}

// Judge says what the book knows about a host that is about to be joined.
//
// It answers with the entry it matched, so that the screen can show the
// fingerprint of what it remembers next to the one that just arrived, which is
// the only form of this warning somebody can do something about.
//
// **The key is looked up first, and that ordering is the whole design.** A key
// that is already in the book is the same installation whatever it calls
// itself. Only when the key is unknown does the nickname get consulted, and a
// hit there is the alarm: a name that used to arrive with another key.
func (b KnownHosts) Judge(key []byte, nick string) (HostVerdict, KnownHost) {
	if len(key) != ed25519.PublicKeySize {
		return HostUnverified, KnownHost{}
	}
	if h, ok := b.byKey(key); ok {
		if nick != "" && !strings.EqualFold(h.Nick, nick) {
			return HostRenamed, h
		}
		return HostKnown, h
	}
	if nick != "" {
		if h, ok := b.byNick(nick); ok {
			return HostKeyChanged, h
		}
	}
	return HostNew, KnownHost{}
}

// Record writes down a host that was JOINED, and only after its signature
// verified.
//
// Recording an unverified key would turn the book into a log of whatever
// anybody claimed, and the next join would compare against it as if it meant
// something. That is worse than having no book: it is a book that lies with the
// authority of one.
//
// **A changed key REPLACES the old entry**, and that is deliberate. Keeping
// both would mean the next join finds two keys for one nickname and cannot say
// which is the impostor. The warning is shown before this runs; joining anyway
// is somebody deciding the new key is fine, and the book records the decision
// rather than second-guessing it.
func (b *KnownHosts) Record(key []byte, nick string, now time.Time) {
	if len(key) != ed25519.PublicKeySize {
		return
	}
	now = now.UTC()
	for i := range b.Hosts {
		if keyEqual(b.Hosts[i].Key, key) {
			b.Hosts[i].LastSeen = now
			b.Hosts[i].Rooms++
			if nick != "" {
				b.Hosts[i].Nick = nick
			}
			return
		}
	}
	// A key change takes the nickname's old entry with it. See above.
	if nick != "" {
		if h, ok := b.byNick(nick); ok {
			b.remove(h.Key)
		}
	}
	b.Hosts = append(b.Hosts, KnownHost{
		Key:       append([]byte(nil), key...),
		Nick:      nick,
		FirstSeen: now,
		LastSeen:  now,
		Rooms:     1,
	})
	b.evictOldest()
}

func (b KnownHosts) byKey(key []byte) (KnownHost, bool) {
	for _, h := range b.Hosts {
		if keyEqual(h.Key, key) {
			return h, true
		}
	}
	return KnownHost{}, false
}

func (b KnownHosts) byNick(nick string) (KnownHost, bool) {
	for _, h := range b.Hosts {
		if h.Nick != "" && strings.EqualFold(h.Nick, nick) {
			return h, true
		}
	}
	return KnownHost{}, false
}

func (b *KnownHosts) remove(key []byte) {
	out := b.Hosts[:0]
	for _, h := range b.Hosts {
		if !keyEqual(h.Key, key) {
			out = append(out, h)
		}
	}
	b.Hosts = out
}

// evictOldest keeps the book under [MaxKnownHosts], dropping least recently
// seen first.
func (b *KnownHosts) evictOldest() {
	for len(b.Hosts) > MaxKnownHosts {
		oldest := 0
		for i, h := range b.Hosts {
			if h.LastSeen.Before(b.Hosts[oldest].LastSeen) {
				oldest = i
			}
		}
		b.Hosts = append(b.Hosts[:oldest], b.Hosts[oldest+1:]...)
	}
}

func keyEqual(a, b []byte) bool {
	// Not constant time on purpose: these are public keys, both sides of the
	// comparison are already on this disk, and there is no secret to leak by
	// how long it takes.
	if len(a) != len(b) || len(a) == 0 {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// knownHostsJSON is the on-disk schema. Closed, like every other file the
// daemon reads back.
type knownHostsJSON struct {
	Version int             `json:"version"`
	Hosts   []knownHostJSON `json:"hosts"`
}

type knownHostJSON struct {
	Key       string `json:"key"`
	Nick      string `json:"nick"`
	FirstSeen string `json:"first_seen"`
	LastSeen  string `json:"last_seen"`
	Rooms     int    `json:"rooms"`
}

// KnownHostsVersion is the schema version written to disk.
const KnownHostsVersion = 1

// Encode serializes the book.
func (b KnownHosts) Encode() ([]byte, error) {
	out := knownHostsJSON{Version: KnownHostsVersion, Hosts: make([]knownHostJSON, 0, len(b.Hosts))}
	for _, h := range b.Hosts {
		if len(h.Key) != ed25519.PublicKeySize {
			continue
		}
		out.Hosts = append(out.Hosts, knownHostJSON{
			Key:       base64.StdEncoding.EncodeToString(h.Key),
			Nick:      h.Nick,
			FirstSeen: h.FirstSeen.UTC().Format(time.RFC3339),
			LastSeen:  h.LastSeen.UTC().Format(time.RFC3339),
			Rooms:     h.Rooms,
		})
	}
	return json.MarshalIndent(out, "", "  ")
}

// DecodeKnownHosts is the only decoder of the book.
//
// Strict both ways, like the hosted room: an unknown field rejects the file,
// and so does anything trailing the object.
//
// **A single bad entry is dropped, not the whole file.** The book is advice,
// not access: refusing to load it because one line rotted would silently turn
// every host into a stranger, which is exactly the warning nobody should see
// for the wrong reason.
func DecodeKnownHosts(raw []byte) (KnownHosts, error) {
	j, err := decodeStrict[knownHostsJSON](raw)
	if err != nil {
		return KnownHosts{}, err
	}
	if j.Version != KnownHostsVersion {
		return KnownHosts{}, fmt.Errorf("%w: la libreta es de la versión %d y esta versión lee la %d",
			ErrPersistedShape, j.Version, KnownHostsVersion)
	}
	out := KnownHosts{Hosts: make([]KnownHost, 0, len(j.Hosts))}
	for _, h := range j.Hosts {
		key, err := base64.StdEncoding.DecodeString(h.Key)
		if err != nil || len(key) != ed25519.PublicKeySize {
			continue
		}
		first, err := time.Parse(time.RFC3339, h.FirstSeen)
		if err != nil {
			continue
		}
		last, err := time.Parse(time.RFC3339, h.LastSeen)
		if err != nil {
			last = first
		}
		out.Hosts = append(out.Hosts, KnownHost{
			Key: key, Nick: h.Nick, FirstSeen: first, LastSeen: last, Rooms: h.Rooms,
		})
		if len(out.Hosts) == MaxKnownHosts {
			break
		}
	}
	return out, nil
}
