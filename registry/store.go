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
	"crypto/ed25519"
	"errors"
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
	HostKey   ed25519.PublicKey
	Card      []byte
	Network   string // nombre de la red de ENCUENTRO, para contar miembros
	CardUntil time.Time
	PinUntil  time.Time
}

// Store guarda las salas vivas en memoria, sin base de datos y sin disco.
//
// Que sea volátil es una elección, no una limitación: una sala muere cuando se
// va el último, así que persistir su tarjeta solo alargaría la vida de un dato
// que ya no describe nada. Reiniciar el registro cuesta que los invitados vean
// la tarjeta genérica hasta que el host vuelva a publicar, y jamás impide
// entrar, porque entrar no pasa por acá.
type Store struct {
	mu    sync.RWMutex
	rooms map[string]*Room
	ahora func() time.Time
	// nuevoID se inyecta para poder probar la emisión y el agotamiento sin
	// depender de la aleatoriedad real.
	nuevoID func() (domain.InviteID, error)
}

// NewStore construye un registro vacío. ahora y nuevoID pueden ser nil, y en
// ese caso se usan el reloj real y la aleatoriedad real.
func NewStore(ahora func() time.Time, nuevoID func() (domain.InviteID, error)) *Store {
	if ahora == nil {
		ahora = time.Now
	}
	if nuevoID == nil {
		nuevoID = randomInviteID
	}
	return &Store{rooms: map[string]*Room{}, ahora: ahora, nuevoID: nuevoID}
}

// Issue emite un invite ID libre y lo fija a la llave del host.
//
// Lo emite el registro y no el host porque quien tiene que garantizar unicidad
// es el registro, así que emitir evita el ida y vuelta de proponer y ser
// rechazado. No hay nada que filtrar al hacerlo: un invite ID no deriva
// material criptográfico de la sala real, es una llave de búsqueda.
func (s *Store) Issue(hostKey ed25519.PublicKey, card, sig []byte) (domain.InviteID, error) {
	if len(card) > MaxCardBytes {
		return domain.InviteID{}, ErrCardTooBig
	}
	if !ed25519.Verify(hostKey, card, sig) {
		return domain.InviteID{}, ErrBadSig
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.limpiar()

	// Ocho intentos. Con 40 bits y un puñado de salas vivas, chocar dos veces
	// seguidas ya es improbable hasta el absurdo; ocho es un techo que existe
	// para que un registro lleno falle rápido en vez de girar para siempre.
	for i := 0; i < 8; i++ {
		id, err := s.nuevoID()
		if err != nil {
			return domain.InviteID{}, err
		}
		if _, ocupado := s.rooms[id.Raw()]; ocupado {
			continue
		}
		ahora := s.ahora()
		s.rooms[id.Raw()] = &Room{
			HostKey:   append(ed25519.PublicKey(nil), hostKey...),
			Card:      append([]byte(nil), card...),
			Network:   domain.DeriveRendezvous(id).NetworkName(),
			CardUntil: ahora.Add(CardTTL),
			PinUntil:  ahora.Add(PinTTL),
		}
		return id, nil
	}
	return domain.InviteID{}, ErrExhausted
}

// Publish actualiza la tarjeta de una sala existente, o revive una cuyo fijado
// sigue vivo. Es el camino de reabrir con el mismo invite ID.
//
// Acá se cierra el agujero que el cifrado no puede cerrar. La clave de la
// tarjeta se deriva del enlace, así que TODO el que recibió el enlace puede
// producir una tarjeta que la página descifra, y el registro no tiene forma de
// distinguir a un miembro del host. Lo que sí puede es exigir la firma de la
// llave que fijó ese invite ID la primera vez.
func (s *Store) Publish(id domain.InviteID, hostKey ed25519.PublicKey, card, sig []byte) error {
	if len(card) > MaxCardBytes {
		return ErrCardTooBig
	}
	if !ed25519.Verify(hostKey, card, sig) {
		return ErrBadSig
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.limpiar()

	ahora := s.ahora()
	r, hay := s.rooms[id.Raw()]
	if !hay {
		return ErrNotFound
	}
	if !r.HostKey.Equal(hostKey) {
		return ErrPinned
	}
	r.Card = append([]byte(nil), card...)
	r.CardUntil = ahora.Add(CardTTL)
	r.PinUntil = ahora.Add(PinTTL)
	return nil
}

// Lookup devuelve la sala si su tarjeta sigue viva.
//
// Una sala cuya tarjeta expiró pero cuyo fijado sigue en pie NO se devuelve: al
// visitante le consta que no hay sala, que es la verdad, y al host le consta
// que su invite ID le sigue perteneciendo cuando la reabre. Las dos cosas a la
// vez, que es justo lo que la asimetría de TTLs existe para dar.
func (s *Store) Lookup(id domain.InviteID) (Room, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	r, hay := s.rooms[id.Raw()]
	if !hay || s.ahora().After(r.CardUntil) {
		return Room{}, ErrNotFound
	}
	return *r, nil
}

// Networks lista las redes de encuentro vivas, que es lo que el contador
// necesita para saber qué mirar del RPC de EasyTier.
func (s *Store) Networks() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ahora := s.ahora()
	out := make([]string, 0, len(s.rooms))
	for _, r := range s.rooms {
		if !ahora.After(r.CardUntil) {
			out = append(out, r.Network)
		}
	}
	return out
}

// Sweep descarta lo que ya no le pertenece a nadie. Se llama desde un ticker.
func (s *Store) Sweep() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.limpiar()
}

// limpiar exige el lock tomado. Solo borra cuando el FIJADO expiró: mientras
// dure, la entrada se conserva aunque la tarjeta esté muerta, porque es lo que
// reserva el invite ID para su host.
func (s *Store) limpiar() int {
	ahora := s.ahora()
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
