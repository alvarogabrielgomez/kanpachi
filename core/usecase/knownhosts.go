package usecase

import (
	"github.com/accentiostudios/kanpachi/core/domain"
)

// The fingerprint book, read before entering a room and written after.
//
// This is the half of decision 25 a signature cannot buy on its own: the
// signature says the key is the one the registry pinned, and the book says the
// key is the one this machine has seen before under that name. See
// [domain.KnownHosts].

// hostNickOf opens the card only to read the nickname the host introduced
// itself with.
//
// It returns the empty string for every problem, and none of them is an error:
// a code dictated over the phone carries no fragment, and a card that does not
// open is one that must not be believed. What is lost is the name next to the
// key in the book, never the join.
func hostNickOf(link string, lookup domain.InviteLookup) string {
	key, ok := cardKeyOf(link)
	if !ok {
		return ""
	}
	card, err := domain.OpenRoomCard(lookup.Sealed, key)
	if err != nil {
		return ""
	}
	return card.Host.String()
}

// knownHosts reads the book, and NEVER fails.
//
// An empty book is the honest answer to every problem here: a machine that has
// joined nobody, a file that is not there, a file that no longer decodes. The
// consequence of an empty book is that hosts read as new, which is the same
// thing this machine would say the first time anyway. Refusing to continue
// would turn a rotten file into a room nobody can enter, over a mechanism whose
// whole point is that it advises rather than blocks.
func (s *Session) knownHosts() domain.KnownHosts {
	raw, err := s.deps.State.LoadKnownHosts()
	if err != nil {
		return domain.KnownHosts{}
	}
	book, err := domain.DecodeKnownHosts(raw)
	if err != nil {
		s.deps.Log.Warn("la libreta de huellas no se pudo leer, así que esta vez no se compara con nada",
			"error", err)
		return domain.KnownHosts{}
	}
	return book
}

// KnownHosts is the book, for whoever wants to show it.
//
// Read-only and never fails, like [Session.knownHosts]. It takes no lock: the
// book lives on disk and is not session state, so reading it while a join is
// in flight shows the book as it was a moment ago, which is what any view of it
// can ever promise.
func (s *Session) KnownHosts() domain.KnownHosts { return s.knownHosts() }

// judgeHost asks the book about a host that is about to be joined.
//
// The key must be one whose signature ALREADY verified. Judging an unverified
// key would compare the book against whatever the last person to answer
// claimed.
func (s *Session) judgeHost(key []byte, nick string) (domain.HostVerdict, domain.KnownHost) {
	return s.knownHosts().Judge(key, nick)
}

// rememberHost writes down a host that was joined with its signature verified.
//
// It never fails the join. Somebody is already inside a room by the time this
// runs, and the cost of not writing the entry is that the next invitation from
// the same host reads as new. That is a worse warning, not a broken room, so it
// goes to the log and the join stands.
func (s *Session) rememberHost(key []byte, nick string) {
	if len(key) == 0 {
		return
	}
	book := s.knownHosts()
	book.Record(key, nick, s.deps.Clock.Now())
	raw, err := book.Encode()
	if err != nil {
		s.deps.Log.Warn("no se pudo serializar la libreta de huellas", "error", err)
		return
	}
	if err := s.deps.State.SaveKnownHosts(raw); err != nil {
		s.deps.Log.Warn("no se pudo guardar la libreta de huellas, así que este host se verá como nuevo la próxima vez",
			"error", err)
	}
}
