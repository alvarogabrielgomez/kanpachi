package domain

import (
	"encoding/binary"
	"fmt"
	"time"
)

// Closing a room in the registry: what gets signed, and why it carries a clock.
//
// It lives in the domain for the same reason as [SeedAuthProof]: both ends need
// it, the client to sign and the registry to check, and neither can ask the
// other how it is built. A second version of these bytes at the far end is a
// signature that does not verify and a close that never happens.
//
// # Why closing is signed, when the registry already has a door
//
// Because the door belongs to the seed's OPERATOR and this belongs to the room's
// HOST. An open seed asks for nothing, so the password protects nothing there; a
// closed one asks it of everybody who hosts, which is anybody holding the shared
// credential. What says this room is yours is the key the registry pinned for
// that invite ID the first time, exactly as when publishing the card.
//
// # Why it carries a timestamp, when publishing does not
//
// Because closing is the one message whose recorded copy keeps working later.
// Publishing an old card leaves the room with an old card, which is what was
// already there. Replaying a close after the host REOPENS the same room under the
// same code kills a live room, and reopening under the same code is exactly what
// a headless host does on every boot. The stamp is what gives that copy an
// expiry date.

// roomCloseLabel keeps this signature apart from any other use of the same key.
// Versioned, like every other domain separator in this project.
const roomCloseLabel = "kanpachi/room-close/v1"

// RoomCloseSkew is how far off the signer's clock is allowed to be.
//
// Five minutes in both directions. There is nothing to synchronise between the
// two machines and a desktop clock drifts by seconds without anybody noticing,
// so a narrow margin would turn closing into something that fails depending on
// which PC asks. Wider buys nothing: what this bounds is how long a recorded
// copy is worth anything, and five minutes is far less than the six hours a card
// lives.
const RoomCloseSkew = 5 * time.Minute

// RoomCloseMessage builds the bytes that are signed to close a room.
//
// The zero byte separates the three fields and none of them can contain one: the
// label is a constant, the invite ID comes from the 32-symbol alphabet, and the
// time goes in eight fixed-width bytes. That makes the reading unambiguous with
// no length prefixes.
//
// The invite ID goes INSIDE, and without it a good signature for one room would
// close any other room of the same host.
func RoomCloseMessage(id InviteID, at time.Time) []byte {
	raw := id.Raw()
	msg := make([]byte, 0, len(roomCloseLabel)+1+len(raw)+1+8)
	msg = append(msg, roomCloseLabel...)
	msg = append(msg, 0)
	msg = append(msg, raw...)
	msg = append(msg, 0)
	return binary.BigEndian.AppendUint64(msg, uint64(at.Unix()))
}

// CheckRoomCloseTime says whether that stamp is still good.
//
// It rejects on both sides and not only on the past. A stamp in the future is a
// wrong clock or somebody extending a signature's life on purpose, and both end
// the same way: a message worth more than this code is willing to let anything
// be worth.
func CheckRoomCloseTime(at, now time.Time) error {
	d := now.Sub(at)
	if d < 0 {
		d = -d
	}
	if d > RoomCloseSkew {
		return fmt.Errorf("%w: la petición de cierre está fechada a %s de ahora, y el tope es %s",
			ErrInputShape, d.Round(time.Second), RoomCloseSkew)
	}
	return nil
}
