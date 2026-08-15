package domain

// Displacement is what entering a room would cost RIGHT NOW.
//
// # Why this is a value and not a rule each screen applies
//
// Because there are three faces and there would be three copies of the same
// rule, drifting. The law of this project is that the decisions live in the
// daemon and the faces only paint, and the shape of that here is: the daemon
// works out what entering displaces, and each face renders it and asks in
// whatever way it can — a modal in the window, a `survey.Confirm` in the
// wizard, `--yes` with a refusal when there is no terminal in the CLI.
//
// The daemon cannot ask. It runs as SYSTEM or as root, with no window and no
// terminal, so the half that decides and the half that asks have to be
// separate. Only the deciding half is shared, and it is this one.
//
// # Its zero is the ordinary case
//
// Most of the time entering costs nothing: there is no room and nothing to go
// back to. [Displacement.Any] is the question worth asking.
type Displacement struct {
	Kind DisplacementKind

	// Room and Name are what would be displaced, so a face can name it instead
	// of saying "the previous one".
	Room Room

	// Name is the room's display name, empty when it never had one.
	Name string

	// Members is how many OTHER people are in the room, and it is only filled
	// for [DisplaceCloseRoom]. It is the number that makes closing a room read
	// as what it is.
	Members int
}

// DisplacementKind says what kind of thing is in the way.
type DisplacementKind uint8

const (
	// DisplaceNothing is the ordinary case: no room, and nothing to go back to.
	DisplaceNothing DisplacementKind = iota

	// DisplaceLeaveRoom is a GUEST inside a room. Entering somewhere else means
	// leaving this one, which costs this machine its place and costs nobody
	// else anything.
	DisplaceLeaveRoom

	// DisplaceCloseRoom is a HOST inside their own room. Entering somewhere else
	// means CLOSING it: the ports come down and everybody inside drops.
	//
	// It is the same asymmetry `leave` has always had, where leaving is closing
	// if the room is yours, and which nothing used to say out loud.
	DisplaceCloseRoom

	// DisplaceStopReturning is a machine on its way back into a room it was in.
	// It is in no room, so nothing comes down and nobody drops; what it costs is
	// giving up on getting back.
	DisplaceStopReturning
)

// Any says whether entering would displace anything at all.
func (d Displacement) Any() bool { return d.Kind != DisplaceNothing }

// String names the kind for the wire and for a log. The zero is the empty
// string, so an omitted field and "nothing to displace" are the same thing.
func (k DisplacementKind) String() string {
	switch k {
	case DisplaceLeaveRoom:
		return "leave_room"
	case DisplaceCloseRoom:
		return "close_room"
	case DisplaceStopReturning:
		return "stop_returning"
	default:
		return ""
	}
}
