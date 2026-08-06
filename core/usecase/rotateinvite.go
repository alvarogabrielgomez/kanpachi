package usecase

import (
	"context"
	"fmt"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// RotateInviteCode cierra la puerta a quien está fuera.
//
// SOLO el host. Es la otra mitad de la decisión 22 y es INDEPENDIENTE de
// expulsar: los dos controles resuelven problemas distintos y por eso están
// separados también en el código.
//
//	Renovar el código   → nadie nuevo entra con el viejo. No toca a los presentes.
//	Revocar credencial  → esa persona sale ya. No toca al código.
//
// No toca a los presentes porque ellos tienen credencial, no código. Esa es
// exactamente la propiedad que la derivación local pura no podía dar: con el
// código como secreto de la red, renovar exigía mudar a todos a otra red, o
// sea cortar el túnel con la partida viva.
//
// La red REAL no cambia. Lo único que cambia es la llave de búsqueda del
// vestíbulo, así que la partida no se entera.
func (s *Session) RotateInviteCode(ctx context.Context) (domain.RoomState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.requireHost(); err != nil {
		return domain.RoomState{}, err
	}

	old := s.state.Room

	card := domain.RoomCard{Host: s.nick, Room: s.state.Name}
	sealed, key, err := domain.SealRoomCard(card, s.deps.Rand)
	if err != nil {
		return domain.RoomState{}, err
	}

	// Lo publicado, o nada. El respaldo genera el ID acá, así que el registro no
	// lo emitió nunca y no tiene ninguna tarjeta de esa sala que restaurar.
	publicada := sealed

	room, err := s.deps.Directory.Open(ctx, sealed)
	if err != nil {
		s.deps.Log.Warn("el registro del seed no respondió al renovar, el código nuevo va sin tarjeta", "error", err)
		id, err := domain.NewInviteID(s.deps.Rand)
		if err != nil {
			return domain.RoomState{}, fmt.Errorf("generando un código nuevo sin registro: %w", err)
		}
		// El seed se conserva: renovar cambia la llave de búsqueda, no el
		// registro donde vive la sala.
		room = domain.Room{InviteID: id, Seed: old.Seed}
		publicada = nil
	}

	// El vestíbulo se rehospeda con el nombre nuevo, y esto NO es opcional: el
	// nombre del vestíbulo deriva del invite ID, así que un código nuevo es un
	// vestíbulo nuevo. Quedarse en el viejo produciría exactamente el peor
	// resultado posible de esta operación, un código que la UI muestra y por el
	// que nadie puede entrar.
	//
	// La red REAL no se toca. Los que están dentro viven ahí y no en el
	// vestíbulo, así que la partida no se entera.
	if err := s.deps.Engine.JoinRendezvous(ctx, domain.RendezvousSpec{
		Rendezvous: domain.DeriveRendezvous(room.InviteID),
		Address:    domain.RendezvousHostAddress,
		Name:       s.nick,
		Seeds:      seedsFor(room),
	}); err != nil {
		// El código viejo sigue siendo el bueno: el vestíbulo que está
		// levantado es el suyo. Publicar el nuevo dejaría a la UI mostrando una
		// puerta que no existe.
		return domain.RoomState{}, fmt.Errorf("abriendo la puerta con el código nuevo: %w", err)
	}

	s.state.Room = room
	s.cardKey = key
	s.sealedCard = publicada
	s.saveRoomLocked()

	// El código nuevo se le reparte a los que están DENTRO.
	//
	// Arregla un efecto secundario que nadie había nombrado: renovar dejaba a
	// los presentes con el código viejo guardado. Siguen en la sala y la
	// partida no se entera, y el día que quieran volver tienen un código
	// muerto. La confianza ya está dada, están dentro porque el host los dejó
	// entrar, y renovar con ellos dentro es la señal de que se quedan.
	//
	// Va cifrado contra la llave de cada uno, que es cosa del adaptador. Que
	// falle no invalida nada: el código nuevo ya es el bueno y el vestíbulo ya
	// está levantado con él. Lo que se pierde es que el otro lo tenga guardado.
	if err := s.deps.Control.AnnounceCode(ctx, room); err != nil {
		s.deps.Log.Warn("no se les pudo repartir el código nuevo a los presentes", "error", err)
	}

	s.deps.Log.Info("código renovado",
		"antes", old.InviteID.String(), "ahora", room.InviteID.String(), "presentes", len(s.state.Peers))
	return s.snapshot(), nil
}

// OnCodeRotated aplica el código nuevo que repartió el host.
//
// Lo llama el supervisor drenando [port.ControlChannel.Codes]. Lo único que
// cambia es la llave de búsqueda de la sala: la red real es la misma, la
// credencial sigue valiendo, y nadie se reconecta ni migra a ningún lado.
//
// Un host no toma códigos de nadie. El suyo es el original, y aceptarlos le
// permitiría a un miembro modificado cambiarle el código a la sala que hospeda,
// o sea dejarlo publicando una puerta que no existe.
func (s *Session) OnCodeRotated(ctx context.Context, r domain.Room) domain.RoomState {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.state.Conn.InRoom() || s.state.IsHost() {
		return s.snapshot()
	}
	// Que llegue un código es prueba de vida del host, como cualquier otra cosa
	// que venga por ese canal.
	s.state.NoteHostAlive(s.deps.Clock.Now())

	if r.InviteID.IsZero() || r.Seed == "" {
		// Llega de otra máquina, así que se comprueba acá y no se confía en que
		// el otro lado haya mandado algo coherente. Un código a medias
		// guardado en disco haría fallar el "volver a la última sala" sin
		// ninguna pista de por qué.
		s.deps.Log.Warn("el host mandó un código nuevo incompleto, se ignora")
		return s.snapshot()
	}
	if r == s.state.Room {
		return s.snapshot()
	}

	old := s.state.Room
	s.state.Room = r
	s.saveLastRoomLocked()

	s.deps.Log.Info("el host renovó el código de la sala",
		"antes", old.InviteID.String(), "ahora", r.InviteID.String())
	return s.snapshot()
}

// RenameRoom cambia el nombre visible y vuelve a publicar la tarjeta.
//
// El nombre es presentación pura: viaja cifrado dentro de la tarjeta y el seed
// no puede leerlo. Que la publicación falle no es un error del renombrado: el
// nombre ya cambió para todos los que están dentro, y lo único que se pierde
// es que la página de invitación muestre el nuevo.
func (s *Session) RenameRoom(ctx context.Context, name string) (domain.RoomState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.requireHost(); err != nil {
		return domain.RoomState{}, err
	}
	s.state.Name = domain.ClampRoomName(name)
	// Se acota al ESCRIBIR y no solo al leer: el host tiene que ver el mismo
	// nombre que van a ver los demás, en vez de uno que a todos les llega
	// recortado y a él entero.
	s.announceLocked(ctx)
	s.saveRoomLocked()

	sealed, key, err := domain.SealRoomCard(domain.RoomCard{Host: s.nick, Room: s.state.Name}, s.deps.Rand)
	if err != nil {
		return domain.RoomState{}, err
	}
	if err := s.deps.Directory.Publish(ctx, s.state.Room.InviteID, sealed); err != nil {
		s.deps.Log.Warn("no se pudo publicar el nombre nuevo de la sala", "error", err)
		// La clave y el blob NO se tocan: los que están en disco siguen siendo
		// los de la tarjeta que el registro tiene, y esa sigue siendo la buena.
		return s.snapshot(), nil
	}

	// Y se vuelve a guardar, que es lo que faltaba.
	//
	// # El desajuste que esto cierra
	//
	// El `saveRoomLocked` de arriba corre ANTES de sellar, así que grababa el
	// nombre nuevo con la clave VIEJA. La clave nueva se fijaba solo en memoria,
	// y un apagón después de renombrar dejaba en disco una clave que no abre la
	// tarjeta que el registro tiene: al reabrir, el enlace repartido mostraba la
	// tarjeta genérica sin que nada hubiera fallado.
	//
	// Queda una ventana entre publicar y guardar, más angosta que la anterior y
	// elegida a sabiendas. La alternativa, guardar antes de publicar, se repara
	// sola en la siguiente reapertura a cambio de que un publish fallido deje en
	// disco una clave que no abre lo que el registro sirve, que es el mismo daño
	// en el caso que SÍ ocurre.
	s.cardKey = key
	s.sealedCard = sealed
	s.saveRoomLocked()
	return s.snapshot(), nil
}

// InviteLink devuelve el enlace completo, con la clave de la tarjeta pegada en
// el fragmento.
//
// Es lo que se copia al portapapeles y se pega en Telegram.
// [domain.Room.InviteURL] es la otra forma, sin clave, que es la que se dicta
// por teléfono: quien la reciba entra igual y ve la tarjeta genérica, porque
// la clave no es lo que abre la sala.
func (s *Session) InviteLink() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state.Room.InviteID.IsZero() {
		return ""
	}
	return s.state.Room.InviteLink(s.cardKey)
}
