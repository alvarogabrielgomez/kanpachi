// Package registry es kanpachi-registry, el registro de salas del seed.
//
// Es lo que hace que kanpachi-seed sea algo distinto de una instalación plana
// de EasyTier: emite invite IDs, guarda una tarjeta de presentación que no
// puede leer, y publica cuánta gente hay en cada sala. Ver la decisión 24 de
// docs/02-decisiones-de-diseno.md.
//
// Corre como proceso aparte de easytier-core y solo habla con él por su portal
// RPC en loopback. Esa separación no es estética: EasyTier es LGPL-3.0, y un
// proceso separado deja la licencia de Kanpachi intacta.
package registry

import (
	"context"
	"crypto/ed25519"
	"errors"
	"path/filepath"
	"sync"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// Ventanas de vida. Son distintas a propósito y la diferencia es el corazón de
// la reapertura.
//
// La tarjeta muere pronto, porque describe una sala que ya no existe. El
// FIJADO de la llave del host sobrevive semanas, porque es lo único que impide
// que un ex miembro, que conserva el invite ID, se adelante al host cuando la
// sala se reabre y registre su propia tarjeta. Sin esta asimetría, reabrir con
// el mismo código sería una carrera que gana el que esté más atento.
const (
	CardTTL = 6 * time.Hour
	PinTTL  = 21 * 24 * time.Hour
)

// MaxCardBytes acota lo que un host puede depositar. La tarjeta lleva un nick
// de 12 caracteres y un nombre de sala corto, cifrados: 512 bytes sobran. El
// tope existe porque este endpoint está abierto a internet y el registro vive
// en memoria, o sea que sin él una tarjeta es un vector de agotamiento.
const MaxCardBytes = 512

var (
	ErrNotFound   = errors.New("esa sala no existe")
	ErrPinned     = errors.New("ese invite ID pertenece a otra llave")
	ErrBadSig     = errors.New("la firma no valida contra la llave que trae")
	ErrCardTooBig = errors.New("la tarjeta pasa del tope")
	ErrExhausted  = errors.New("no se pudo emitir un invite ID libre")
)

// Room es lo que el registro sabe de una sala. Es deliberadamente poco.
//
// Card son bytes opacos: van cifrados con una clave derivada del enlace que
// solo tienen quienes lo recibieron, así que el operador del seed guarda y
// sirve el nombre de una sala sin poder leerlo jamás.
type Room struct {
	HostKey ed25519.PublicKey
	Card    []byte

	// Sig is the signature [Room.Card] was deposited with, kept so that it can
	// be SERVED.
	//
	// The registry already verified it on the way in and then threw it away,
	// which left the check living in exactly one place: here. What a client got
	// was a card and this server's word that somebody had signed it, which is
	// precisely what decision 24 says is not enough. Keeping it lets whoever
	// reads the card check it against the key this same registry pinned first,
	// and a compromised registry can no longer change the contents without it
	// showing.
	//
	// **Empty is a normal case, not an error.** Entries written before this
	// change come back from `rooms.json` with no signature, and they are served
	// all the same: the client treats them as unverified. Discarding them would
	// lock out the guests of every open room, which is the bug decision 33
	// fixed.
	Sig []byte

	Network   string // nombre de la red de ENCUENTRO, para contar miembros
	CardUntil time.Time
	PinUntil  time.Time
}

// Store guarda las salas vivas, en memoria, y las respalda en disco cuando se le
// da dónde.
//
// # Por qué el disco, si una sala muere cuando se va el último
//
// Esta cabecera decía que ser volátil era una elección y que **reiniciar el
// registro jamás impide entrar, porque entrar no pasa por acá**. Eso fue cierto
// hasta que existió `checkRoomExists`: hoy el invitado le pregunta al registro
// antes de levantar nada y **se cree el "no"**. Con el almacén en RAM y la unidad
// en `Restart=always`, cualquier reinicio contestaba que no conoce salas que
// están abiertas y alcanzables, y dejaba fuera a todos sus invitados.
//
// El host tampoco lo podía reponer: [Store.publish] exige que la entrada exista,
// y **no debe crear**, porque crear reabriría la carrera que el fijado existe
// para cerrar, o sea que quien se quedó con el código llegaría primero. El único
// arreglo desde el producto era renovar el código, que mata los enlaces ya
// repartidos.
//
// Persistir es lo que hace que su "no" signifique lo que dice, y eso es la
// condición de la política de fallar rápido. Ver la cabecera de `persist.go`.
//
// # Lo que sigue igual
//
// `Publish` no crea. Lo que se arregla es que la entrada siga estando.
type Store struct {
	mu          sync.RWMutex
	rooms       map[string]*Room
	currentTime func() time.Time
	// newID se inyecta para poder probar la emisión y el agotamiento sin
	// depender de la aleatoriedad real.
	newID func() (domain.InviteID, error)

	// disc es nil en un registro volátil, que es lo que usan los tests y
	// `kanpseed serve` sin directorio de estado. Ver [Store.respaldar].
	disc *keeper
	// notify cuenta lo que sale mal al escribir. Nil se traga los fallos en
	// silencio, y por eso el servidor de verdad siempre le pasa uno.
	notify func(error)
}

// NewStore construye un registro vacío y VOLÁTIL. ahora y nuevoID pueden ser
// nil, y en ese caso se usan el reloj real y la aleatoriedad real.
func NewStore(currentTime func() time.Time, newID func() (domain.InviteID, error)) *Store {
	if currentTime == nil {
		currentTime = time.Now
	}
	if newID == nil {
		newID = randomInviteID
	}
	return &Store{rooms: map[string]*Room{}, currentTime: currentTime, newID: newID}
}

// OpenStore construye un registro respaldado por `dir`, cargando lo que haya.
//
// Devuelve cuántas entradas se cargaron y cuántas se descartaron por no
// sostenerse, que es lo que el arranque anota. Un fichero ilegible NO impide
// arrancar: se dice y se sigue con el registro vacío, porque un registro que no
// arranca es peor que uno con una sala menos.
//
// `avisar` recibe los fallos de escritura posteriores. Se inyecta porque este
// paquete no tiene logger propio y no va a tenerlo: quien sabe adónde va una
// línea es el binario.
func OpenStore(dir string, notify func(error)) (*Store, int, int, error) {
	s := NewStore(nil, nil)
	s.disc = &keeper{path: filepath.Join(dir, stateFile)}
	s.notify = notify

	rooms, discardedRooms, err := s.disc.load(s.currentTime())
	if err != nil {
		// Se arranca igual, con el registro vacío. Quien llama decide qué tan
		// fuerte lo dice.
		return s, 0, discardedRooms, err
	}
	s.rooms = rooms
	return s, len(rooms), discardedRooms, nil
}

// respaldar vuelca el estado a disco. **Se llama con el lock SUELTO, siempre.**
//
// Escribir con el lock del almacén tomado dejaría parados `/healthz`, la
// resolución de invite IDs y la página mientras el disco contesta. Es el mismo
// fallo que ya obligó a sacar la derivación de Argon2id fuera del lock, y ahí
// además se disfrazaba de proceso colgado porque el watchdog late pidiendo
// `/healthz`. Ver [redDeEncuentro].
//
// El snapshot se toma bajo lectura y la escritura la serializa el mutex propio
// del keeper, así que dos mutaciones a la vez no se pisan el fichero.
func (s *Store) respaldar() {
	if s.disc == nil {
		return
	}
	s.mu.RLock()
	copy := make(map[string]*Room, len(s.rooms))
	for k, r := range s.rooms {
		copy[k] = r
	}
	s.mu.RUnlock()

	if err := s.disc.save(copy); err != nil && s.notify != nil {
		s.notify(err)
	}
}

// Issue emite un invite ID libre y lo fija a la llave del host.
//
// Lo emite el registro y no el host porque quien tiene que garantizar unicidad
// es el registro, así que emitir evita el ida y vuelta de proponer y ser
// rechazado. No hay nada que filtrar al hacerlo: un invite ID no deriva
// material criptográfico de la sala real, es una llave de búsqueda.
// The context is the request's, and it reaches all the way down to the
// derivation brake. An attempt that queues behind another one gives up when its
// caller hangs up, instead of holding a socket for work nobody is waiting on.
func (s *Store) Issue(ctx context.Context, hostKey ed25519.PublicKey, card, sig []byte) (domain.InviteID, error) {
	if len(card) > MaxCardBytes {
		return domain.InviteID{}, ErrCardTooBig
	}
	if !ed25519.Verify(hostKey, card, sig) {
		return domain.InviteID{}, ErrBadSig
	}

	// Ocho intentos. Con 40 bits y un puñado de salas vivas, chocar dos veces
	// seguidas ya es improbable hasta el absurdo; ocho es un techo que existe
	// para que un registro lleno falle rápido en vez de girar para siempre.
	for i := 0; i < 8; i++ {
		id, err := s.newID()
		if err != nil {
			return domain.InviteID{}, err
		}
		if s.ocupado(id) {
			continue
		}
		// Entre el sondeo y la inserción el lock queda suelto, así que otro
		// puede llevarse este mismo ID. No hace falta impedirlo: insertar lo
		// vuelve a comprobar con el lock de escritura tomado y, si perdió la
		// carrera, el bucle prueba con otro. Ese hueco es el precio de derivar
		// sin bloquear a nadie, y sale barato.
		red, err := rendezvous(ctx, id)
		if err != nil {
			return domain.InviteID{}, err
		}
		if s.insert(id, hostKey, card, sig, red) {
			// Fuera del lock que `insertar` acaba de soltar. Ver [Store.respaldar].
			s.respaldar()
			return id, nil
		}
	}
	return domain.InviteID{}, ErrExhausted
}

// derivaciones acota cuántas derivaciones de Argon2id corren a la vez.
//
// Cada una reserva domain.ArgonMemoryKiB, y DeriveRendezvous la hace dos veces,
// o sea del orden de 128 MiB de pico por sala creada. Issue está abierto a
// internet: sin este freno, N peticiones simultáneas piden N veces esa memoria
// y el registro muere por OOM antes de poder rechazar nada. La memoria es el
// recurso escaso del droplet, no la CPU.
//
// Con hueco para una sola, el pico deja de depender de la carga. El precio es
// que las creaciones de sala se encolan, y eso da igual: crear una sala es un
// acto humano y esporádico, mientras que unirse a una, que sí es frecuente, no
// pasa por acá.
//
// **Checking a password shares this slot**, and does not get one of its own.
// Two independent brakes would put the peak at two derivations at once and make
// the MemoryMax of the unit false, which is the shape of the failure that
// already killed this service by OOM. What that couples, measured rather than
// assumed: a burst of logins degrades CREATING rooms, which is exactly what the
// password was closing anyway. Looking a room up never gets here, because
// `Store.Lookup` reads a network that was already derived and stored.
var derivaciones = make(chan struct{}, 1)

// acquireDerivation takes the single slot, or gives up when the caller does.
//
// Waiting on the bare channel send was the earlier shape, and it cannot be
// interrupted: a request whose client already hung up would still sit in the
// queue, take its turn, and spend 64 MiB producing an answer for nobody. The
// rate limit runs BEFORE this, so an attempt that is being throttled never
// reaches the queue at all.
func acquireDerivation(ctx context.Context) error {
	select {
	case derivaciones <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func releaseDerivation() { <-derivaciones }

// rendezvous deriva la red de encuentro con el lock del store SUELTO.
//
// Estaba dentro, y eso convertía cada creación de sala en una parálisis de
// segundos para todo lo demás: /healthz, la resolución de invite IDs y la
// página comparten ese mutex. Como el latido del watchdog se comprueba pidiendo
// /healthz, una creación lenta además se disfrazaba de proceso colgado.
func rendezvous(ctx context.Context, id domain.InviteID) (string, error) {
	if err := acquireDerivation(ctx); err != nil {
		return "", err
	}
	defer releaseDerivation()
	return domain.DeriveRendezvous(id).NetworkName(), nil
}

// ocupado dice si ese invite ID le pertenece a alguien ahora mismo. Un fijado
// vencido no cuenta, igual que en limpiar: la entrada sigue en el mapa pero ya
// no reserva nada.
func (s *Store) ocupado(id domain.InviteID) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	r, hay := s.rooms[id.Raw()]
	return hay && !s.currentTime().After(r.PinUntil)
}

// insert registra la sala y dice si lo consiguió. Devuelve false cuando el
// invite ID se ocupó mientras se derivaba su red de encuentro.
func (s *Store) insert(id domain.InviteID, hostKey ed25519.PublicKey, card, sig []byte, red string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.limpiar()

	if _, ocupado := s.rooms[id.Raw()]; ocupado {
		return false
	}
	ahora := s.currentTime()
	s.rooms[id.Raw()] = &Room{
		HostKey:   append(ed25519.PublicKey(nil), hostKey...),
		Card:      append([]byte(nil), card...),
		Sig:       append([]byte(nil), sig...),
		Network:   red,
		CardUntil: ahora.Add(CardTTL),
		PinUntil:  ahora.Add(PinTTL),
	}
	return true
}

// publish actualiza la tarjeta de una sala existente, o revive una cuyo fijado
// sigue vivo. Es el camino de reabrir con el mismo invite ID.
//
// Acá se cierra el agujero que el cifrado no puede cerrar. La clave de la
// tarjeta se deriva del enlace, así que TODO el que recibió el enlace puede
// producir una tarjeta que la página descifra, y el registro no tiene forma de
// distinguir a un miembro del host. Lo que sí puede es exigir la firma de la
// llave que fijó ese invite ID la primera vez.
func (s *Store) publish(id domain.InviteID, hostKey ed25519.PublicKey, card, sig []byte) error {
	if len(card) > MaxCardBytes {
		return ErrCardTooBig
	}
	if !ed25519.Verify(hostKey, card, sig) {
		return ErrBadSig
	}

	if err := s.publishUnderLock(id, hostKey, card, sig); err != nil {
		return err
	}
	// Fuera del lock, y solo cuando algo cambió de verdad. Ver [Store.respaldar].
	s.respaldar()
	return nil
}

// publishUnderLock es el cuerpo de [Store.publish]. Va aparte para que el
// respaldo a disco ocurra con el lock ya soltado.
func (s *Store) publishUnderLock(id domain.InviteID, hostKey ed25519.PublicKey, card, sig []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.limpiar()

	ahora := s.currentTime()
	r, hay := s.rooms[id.Raw()]
	if !hay {
		return ErrNotFound
	}
	if !r.HostKey.Equal(hostKey) {
		return ErrPinned
	}
	r.Card = append([]byte(nil), card...)
	r.Sig = append([]byte(nil), sig...)
	r.CardUntil = ahora.Add(CardTTL)
	r.PinUntil = ahora.Add(PinTTL)
	return nil
}

// retire caduca la tarjeta de una sala AHORA, dejando el fijado en pie.
//
// # Por qué caducar y no borrar la entrada
//
// Porque son dos hechos distintos y el segundo no ocurrió. La sala se acabó, y
// el invite ID **sigue siendo de su host**: borrar la entrada lo devolvería al
// bote y reabriría la carrera que el fijado existe para cerrar, o sea que el ex
// miembro que se quedó con el código llegaría primero. Caducando la tarjeta,
// [Store.lookup] contesta que no existe al instante y [Store.publish] sigue
// funcionando, que es exactamente la asimetría de TTLs que este almacén ya
// tenía. Reabrir con el mismo código no se toca.
//
// La tarjeta y su firma se vacían además de vencerse. El vencimiento ya alcanza
// para que nadie las lea, y guardarlas sería conservar el nombre de una sala que
// se acabó en un servidor que no tiene por qué recordarlo.
//
// # Lo que exige, y es lo mismo que publicar
//
// Que la entrada exista, y que la llave sea la que fijó ese invite ID la primera
// vez. Sin lo segundo, cerrar salas ajenas sería una petición.
func (s *Store) retire(id domain.InviteID, hostKey ed25519.PublicKey) error {
	if err := s.retireUnderLock(id, hostKey); err != nil {
		return err
	}
	// Fuera del lock, y solo cuando algo cambió. Ver [Store.respaldar].
	s.respaldar()
	return nil
}

// retireUnderLock es el cuerpo de [Store.retire]. Va aparte para que el respaldo
// a disco ocurra con el lock ya soltado.
func (s *Store) retireUnderLock(id domain.InviteID, hostKey ed25519.PublicKey) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.limpiar()

	r, hay := s.rooms[id.Raw()]
	if !hay {
		return ErrNotFound
	}
	if !r.HostKey.Equal(hostKey) {
		return ErrPinned
	}
	// Ya cerrada es un no-op y NO un error. Cerrar lo llama el camino de salir de
	// la sala, que es idempotente a propósito y lo invocan tres sitios.
	r.Card = nil
	r.Sig = nil
	// Un segundo ATRÁS y no el instante exacto. [Store.lookup] descarta con
	// `After(CardUntil)`, así que dejarlo en el ahora deja la sala resolviendo
	// durante ese mismo segundo: el cierre tiene que valer desde la petición que
	// lo pidió, no desde el tick siguiente. No se pone el cero porque
	// [entradaSana] descarta las entradas con plazos vacíos, y eso se llevaría
	// también el fijado, que es lo único que esto conserva a propósito.
	r.CardUntil = s.currentTime().Add(-time.Second)
	return nil
}

// lookup devuelve la sala si su tarjeta sigue viva.
//
// Una sala cuya tarjeta expiró pero cuyo fijado sigue en pie NO se devuelve: al
// visitante le consta que no hay sala, que es la verdad, y al host le consta
// que su invite ID le sigue perteneciendo cuando la reabre. Las dos cosas a la
// vez, que es justo lo que la asimetría de TTLs existe para dar.
func (s *Store) lookup(id domain.InviteID) (Room, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	r, hay := s.rooms[id.Raw()]
	if !hay || s.currentTime().After(r.CardUntil) {
		return Room{}, ErrNotFound
	}
	return *r, nil
}

// networks lista las redes de encuentro vivas, que es lo que el contador
// necesita para saber qué mirar del RPC de EasyTier.
func (s *Store) networks() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ahora := s.currentTime()
	out := make([]string, 0, len(s.rooms))
	for _, r := range s.rooms {
		if !ahora.After(r.CardUntil) {
			out = append(out, r.Network)
		}
	}
	return out
}

// Sweep descarta lo que ya no le pertenece a nadie. Se llama desde un ticker.
//
// Solo respalda cuando borró algo. El barrido corre a menudo y casi siempre no
// encuentra nada, así que escribir el fichero en cada vuelta sería un disco
// ocupado para dejarlo igual que estaba.
func (s *Store) Sweep() int {
	s.mu.Lock()
	n := s.limpiar()
	s.mu.Unlock()

	if n > 0 {
		s.respaldar()
	}
	return n
}

// limpiar exige el lock tomado. Solo borra cuando el FIJADO expiró: mientras
// dure, la entrada se conserva aunque la tarjeta esté muerta, porque es lo que
// reserva el invite ID para su host.
func (s *Store) limpiar() int {
	ahora := s.currentTime()
	n := 0
	for k, r := range s.rooms {
		if ahora.After(r.PinUntil) {
			delete(s.rooms, k)
			n++
		}
	}
	return n
}

// Len informa cuántas entradas hay, fijados muertos incluidos. Para diagnóstico.
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.rooms)
}
