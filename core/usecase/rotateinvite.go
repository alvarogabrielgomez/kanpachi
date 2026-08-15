package usecase

import (
	"context"
	"errors"
	"fmt"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/port"
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

	// El registro que ya sirve esta sala, y no el propio: renovar cambia la llave
	// de búsqueda, jamás el registro donde la sala vive.
	dir, err := s.deps.Directories.For(old.Seed)
	if err != nil {
		return domain.RoomState{}, fmt.Errorf("%w: el registro %s de esta sala no se pudo usar: %v",
			ErrNoRegistry, old.Seed, err)
	}

	// # Acá había un respaldo, y era el mismo defecto que se quitó de crear
	//
	// Sin respuesta del registro se generaba el ID **en esta máquina** y se
	// seguía. Ese código no le sirve a nadie: el invitado le pregunta al mismo
	// registro, le contestan que no existe, y rechaza el ingreso antes de
	// arrancar el motor.
	//
	// Y acá era peor que en crear, porque renovar **mata el código anterior**: el
	// vestíbulo se rehospeda unas líneas más abajo con el nombre nuevo. O sea que
	// el host terminaba sin el código nuevo, que no sirve, y sin el viejo, que ya
	// no tiene puerta. Se cambiaba un código bueno por ninguno.
	//
	// Así que falla, y falla ANTES de tocar el vestíbulo. El código de antes
	// sigue siendo el bueno y la sala no se entera.
	room, err := dir.Open(ctx, sealed)
	// El centinela del password viaja tal cual, por lo mismo que en
	// [Session.reserveCode] de createroom.go, que lleva el motivo entero: el
	// registro contestó, y envolverlo en [ErrNoRegistry] borraba la única
	// respuesta accionable que dio.
	if errors.Is(err, port.ErrSeedPassword) {
		s.deps.Log.Warn("el registro pide password para renovar el código", "seed", old.Seed)
		return domain.RoomState{}, err
	}
	if err != nil {
		s.deps.Log.Error("el registro no contestó al renovar, así que el código no cambia",
			"seed", old.Seed, "error", err)
		return domain.RoomState{}, fmt.Errorf("%w: %s no contestó (%v).\n\n"+
			"El código de siempre sigue funcionando: renovar no llegó a cambiar nada.\n\n"+
			"Qué hacer: vuelve a intentarlo en un momento", ErrNoRegistry, old.Seed, err)
	}
	publicada := sealed

	// El vestíbulo se rehospeda con el nombre nuevo, y esto NO es opcional: el
	// nombre del vestíbulo deriva del invite ID, así que un código nuevo es un
	// vestíbulo nuevo. Quedarse en el viejo produciría exactamente el peor
	// resultado posible de esta operación, un código que la UI muestra y por el
	// que nadie puede entrar.
	//
	// La red REAL no se toca. Los que están dentro viven ahí y no en el
	// vestíbulo, así que la partida no se entera.
	//
	// Y el código nuevo cambia también el RANGO del vestíbulo, no solo su
	// nombre: los dos salen de la misma derivación. Ver
	// [domain.Rendezvous.LobbySubnet]. Eso es lo que convierte esta operación en
	// la salida para un invitado cuya máquina choca con el rango de esta sala.
	rdv := domain.DeriveRendezvous(room.InviteID)
	if err := s.deps.Engine.JoinRendezvous(ctx, domain.RendezvousSpec{
		Rendezvous: rdv,
		Address:    rdv.LobbyHostAddress(),
		Name:       s.nick,
		Seeds:      seedsFor(room),
	}); err != nil {
		// El código viejo sigue siendo el bueno: el vestíbulo que está
		// levantado es el suyo. Publicar el nuevo dejaría a la UI mostrando una
		// puerta que no existe.
		return domain.RoomState{}, fmt.Errorf("abriendo la puerta con el código nuevo: %w", err)
	}
	// La identidad de encuentro se guarda ANTES de reacotar, porque de ella
	// salen el rango y la dirección que las dos líneas de abajo van a leer.
	s.hostSpec.Rendezvous = rdv

	// La compuerta se vuelve a acotar al vestíbulo NUEVO, y esto es obligatorio.
	//
	// El motor acaba de reemplazar la red del vestíbulo, así que el bloqueo por
	// rango seguiría nombrando el /24 viejo. Lo que quedaría cubriendo al nuevo
	// es solo el bloqueo por adaptador, y en Linux eso NO alcanza: con el modelo
	// de host débil, un paquete que llega por la interfaz física con destino a la
	// dirección virtual no casa con una regla acotada por adaptador. Está medido
	// en la cabecera de [gate.Scope]. O sea que saltarse esto abre justo la
	// puerta por donde llega gente que todavía no es miembro.
	if err := s.deps.Firewall.BindRoom(
		ctx, s.state.Subnet, rdv.LobbySubnet(), domain.BindRoomAndLobby,
	); err != nil {
		return domain.RoomState{}, fmt.Errorf("acotando la contención al vestíbulo nuevo: %w", err)
	}

	// El oyente de la puerta se muda a la dirección nueva, y esto tampoco es
	// opcional desde que el rango se deriva del código: antes la .1 del vestíbulo
	// era la misma antes y después de renovar, así que el socket seguía valiendo.
	// Ahora no, y quedarse en la vieja deja la puerta escuchando en una dirección
	// que su propio adaptador ya no tiene.
	s.restrictControlChannel(ctx)
	// Y las reglas, que anclan el permiso de la puerta en esa misma dirección.
	// Que falle no deshace la renovación: el código nuevo ya es el bueno. Se
	// anota, que es lo que hace el resto de esta función con lo no fatal.
	if err := s.applyPolicy(ctx); err != nil {
		s.deps.Log.Error("no se pudieron rehacer las reglas tras renovar el código", "error", err)
	}

	s.state.Room = room
	s.cardKey = key
	s.sealedCard = publicada
	// Renovar es la salida del código muerto: el registro acaba de emitir una
	// entrada nueva, con sus dos relojes en cero. Con el respaldo `publicada`
	// viene vacía y no hay nada que el registro conozca, así que la bandera se
	// apaga igual: lo que la enciende es que RECHACE una sala, no que falte.
	if len(publicada) > 0 {
		s.lastPublish = s.deps.Clock.Now()
	}
	s.state.CodeLost = false
	s.cardPublishFailing = false
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

	// Y el código VIEJO se cierra en el registro, que es lo que hace cierto
	// "renovar cierra la puerta" en el mismo instante en que se pulsa.
	//
	// Sin esto tardaba hasta seis horas en serlo. El vestíbulo viejo ya no
	// existe, porque se rehospedó unas líneas más arriba con el nombre nuevo, y
	// aun así el registro seguía contestando que ese código existe: quien lo
	// tuviera levantaba una red virtual, marcaba a una puerta que nadie iba a
	// abrir, y esperaba el timeout del sistema operativo para nada. Renovar es
	// **la** operación con la que un host cierra la puerta, así que era justo la
	// que peor lo hacía.
	//
	// Va al FINAL y no es fatal, por lo mismo que el reparto de arriba: el código
	// nuevo ya es el bueno y la sala ya vive en él. Que falle deja el viejo
	// resolviendo hasta que venza su tarjeta, que es lo que pasaba siempre.
	//
	// El fijado del código viejo sobrevive, y da igual: era de esta misma
	// máquina, y nadie va a reabrir con él porque la sala ya se mudó.
	if err := dir.Close(ctx, old.InviteID); err != nil {
		s.deps.Log.Warn("el código viejo no se pudo cerrar en el registro, así que va a "+
			"seguir resolviendo hasta que venza su tarjeta",
			"código", old.InviteID.String(), "error", err)
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
	// El registro de la sala, igual que al renovar: la entrada existe solo ahí.
	//
	// Que esto falle NO deshace el renombrado, y por eso el fallo se traga: la
	// sala ya se llama así para los que están dentro, que se enteraron por el
	// canal de control unas líneas más arriba. Lo único que se pierde es que la
	// página de invitación enseñe el nombre nuevo, y el siguiente latido lo
	// reintenta.
	dir, err := s.deps.Directories.For(s.state.Room.Seed)
	if err != nil {
		s.deps.Log.Warn("no se pudo abrir el registro de la sala para publicar el nombre",
			"seed", s.state.Room.Seed, "error", err)
		return s.snapshot(), nil
	}
	if err := dir.Publish(ctx, s.state.Room.InviteID, sealed); err != nil {
		if errors.Is(err, port.ErrUnknownRoom) {
			s.state.CodeLost = true
		}
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
	s.lastPublish = s.deps.Clock.Now()
	s.state.CodeLost = false
	s.cardPublishFailing = false
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
// **NO toma el candado**, y eso es un requisito y no una optimización: esto
// viaja dentro de cada `status`, o sea que el latido de la interfaz lo pide
// cada dos segundos. Tomándolo, crear una sala dejaría a la ventana esperando
// el minuto entero que dura la operación por leer un campo de texto. Lee lo
// publicado, igual que [Session.Status], y lo publica [Session.snapshot].
func (s *Session) InviteLink() string {
	if link := s.publishedLink.Load(); link != nil {
		return *link
	}
	// Antes de la primera publicación no hay sala, y por lo tanto no hay
	// enlace. No es un error.
	return ""
}
