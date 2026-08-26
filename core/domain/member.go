package domain

import (
	"net/netip"
	"sort"
	"time"
)

// Presence es de dónde sabemos que alguien está, y cuándo lo dijo cada fuente.
//
// # Por qué un vector y no un booleano
//
// Porque las fuentes saben cosas distintas y no se sustituyen. El motor sabe el
// camino y el RTT. El canal de control prueba presencia de primera mano y ANTES
// que el motor, medido con dos máquinas el 2026-08-13. El libro conoce a quien
// todavía no llegó.
//
// Fundirlas en un `presente bool` es lo que hacía que «el motor no me lo dice
// hace cuarenta segundos» y «se fue» fueran indistinguibles, que es como una
// regla del 57623 quedó abierta durante horas hacia alguien que ya no estaba.
//
// Cada fuente guarda CUÁNDO habló, y esa mitad es la que contesta cuánto lleva
// alguien fuera. Sin ella, salir de la tabla del motor borraría el único dato
// que separa un sondeo perdido de una tarde entera.
type Presence struct {
	// InMesh es que el motor lo reporta AHORA. MeshAt es la última vez que lo
	// hizo, y sobrevive a que InMesh se apague.
	InMesh bool
	MeshAt time.Time

	// HasChannel es que tiene el canal de la sala abierto con este host, y
	// ChannelSince desde cuándo. Solo lo sabe el host.
	HasChannel   bool
	ChannelSince time.Time

	// Kicked es el veto de [timing.KickGrace] tras una expulsión, y KickedAt
	// desde cuándo corre. Reemplaza al mapa `kicked` de la sesión.
	Kicked   bool
	KickedAt time.Time
}

// Member es todo lo que este host sabe de alguien.
//
// # Por qué un registro y no cinco mapas
//
// Porque eran cinco mapas por la misma dirección, bajo el mismo candado,
// barridos en los mismos sitios y leídos por las mismas funciones: el libro de
// credenciales, el veto de expulsión, el enfriamiento del aviso de credencial
// vieja, la última vez vista en la malla, y la lista fundida. Nada los
// reconciliaba, y la única función del árbol que detectaba el desacuerdo se
// alcanzaba solo por un evento que llega antes de tiempo.
//
// Lo que esto compra no es «una sola fuente», que es imposible y sería mentira:
// el motor, el canal de control y el libro saben cosas que ninguno de los otros
// dos sabe. Compra un solo REGISTRO, una sola ruta de actualización, y un solo
// sitio donde mirar cuando algo no cuadra.
type Member struct {
	IP   netip.Addr
	Name Nickname

	// Path y RTT salen de la malla y de ningún otro sitio. Un canal abierto
	// prueba que alguien está y no dice por dónde llega.
	Path PathKind
	RTT  time.Duration

	Presence Presence

	// Cred es la ficha, o nil. Reemplaza al mapa `issued`.
	Cred *Credential

	// StaleNext y StaleTries son el enfriamiento del aviso de credencial vieja.
	// Reemplazan al mapa `staleProxAviso`.
	StaleNext  time.Time
	StaleTries int

	Self bool
	Host bool
}

// NoteMesh anota que el motor lo ve, con lo que solo el motor sabe.
func (m *Member) NoteMesh(at time.Time, path PathKind, rtt time.Duration) {
	m.Presence.InMesh = true
	m.Presence.MeshAt = at
	m.Path = path
	m.RTT = rtt
}

// NoteOutOfMesh anota que el motor dejó de verlo, CONSERVANDO cuándo lo vio.
//
// Conservarlo es el punto entero: es lo que separa «hace cuarenta segundos que
// no lo dice» de «se fue», y lo que le permite a la pantalla decir cuánto lleva
// fuera en vez de hacerlo desaparecer.
//
// El camino y el RTT se sueltan al cero del enum, que es el sin valor, porque
// son mediciones de un camino que ya no existe. Enseñar la latencia de la última
// vez sería enseñar un número de algo que ya no está.
func (m *Member) NoteOutOfMesh() {
	m.Presence.InMesh = false
	m.Path = 0
	m.RTT = 0
}

// NoteChannel anota que abrió su canal de control con este host.
func (m *Member) NoteChannel(at time.Time) {
	if !m.Presence.HasChannel {
		m.Presence.ChannelSince = at
	}
	m.Presence.HasChannel = true
}

// NoteNoChannel anota que su canal se cerró.
func (m *Member) NoteNoChannel() {
	m.Presence.HasChannel = false
	m.Presence.ChannelSince = time.Time{}
}

// AwayFor es cuánto lleva sin aparecer en la malla. Cero es que está.
//
// Sin marca previa cuenta desde que se le emitió la ficha, que es lo único que
// este host sabe de alguien que nunca llegó a aparecer.
func (m *Member) AwayFor(now time.Time) time.Duration {
	if m.Presence.InMesh {
		return 0
	}
	desde := m.Presence.MeshAt
	if desde.IsZero() {
		if m.Cred == nil {
			return 0
		}
		desde = m.Cred.IssuedAt
	}
	if now.Before(desde) {
		return 0
	}
	return now.Sub(desde)
}

// SeatFreesIn es cuánto le queda a su ficha antes de vencer y soltar su
// dirección. Cero es que no aplica, porque no tiene ficha o ya venció.
func (m *Member) SeatFreesIn(now time.Time) time.Duration {
	if m.Cred == nil {
		return 0
	}
	queda := m.Cred.ExpiresAt.Sub(now)
	if queda <= 0 {
		return 0
	}
	return queda
}

// IsMember dice si esta dirección pertenece a la sala.
//
// # Membresía, no presencia
//
// Tres filtros del libro y uno de la malla. Vencida no autoriza, revocada no
// autoriza, expulsado no autoriza. Y quien está en la tabla del motor cuenta
// aunque no haya ficha.
//
// Esa última rama SIGUE haciendo falta con el libro ya persistido, y esto se
// pensó al revés primero. El libro puede faltar, y sus dos casos son
// ordinarios: la primera vez que se arranca una versión que lo escribe, y una
// carga rechazada por reversión detectada o por fichero ilegible. Sin la rama de
// la malla, ese host reabre y echa en silencio a todo el que estaba dentro,
// hasta que cada uno vuelva a entrar. Es evidencia más débil que una ficha, y la
// alternativa a usarla es una expulsión masiva que nadie pidió.
//
// Quien no se fue formalmente NO salió de la sala: está desconectado, y su
// silla sigue siendo suya hasta que su ficha venza. El latido deja de renovar a
// quien no está, así que ese vencimiento llega solo. Ver la decisión 43.
func (m *Member) IsMember(now time.Time) bool {
	if m.Presence.Kicked {
		return false
	}
	if m.Presence.InMesh {
		return true
	}
	return m.Cred != nil && !m.Cred.Revoked && !m.Cred.Expired(now)
}

// IsAway es tener silla y no estar en la malla. Ver [Peer.Away].
func (m *Member) IsAway(now time.Time) bool {
	return !m.Presence.InMesh && m.IsMember(now)
}

// MemberTable es todo lo que el host sabe de su sala, por dirección.
type MemberTable map[netip.Addr]*Member

// At devuelve el registro de una dirección, creándolo si hace falta.
func (t MemberTable) At(ip netip.Addr) *Member {
	if m, ok := t[ip]; ok {
		return m
	}
	m := &Member{IP: ip}
	t[ip] = m
	return m
}

// MemberIPsOf son las direcciones que cuentan como miembros, ordenadas.
//
// El orden no es cosmético: la lista alimenta la firma del conjunto de reglas,
// y recorrer un mapa da un orden distinto en cada pasada, así que sin ordenar
// la firma cambiaría sola y reaplicaría reglas por nada.
func (t MemberTable) MemberIPsOf(now time.Time, self netip.Addr) []netip.Addr {
	out := make([]netip.Addr, 0, len(t))
	for ip, m := range t {
		if ip == self || !ip.IsValid() || !m.IsMember(now) {
			continue
		}
		out = append(out, ip)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Compare(out[j]) < 0 })
	return out
}

// Forget suelta lo que ya no dice nada de nadie.
//
// Una entrada sobrevive mientras tenga ficha viva, esté en la malla, tenga
// canal, o le quede veto de expulsión. Sin ninguna de las cuatro no es un
// miembro ausente, es basura: nadie la va a consultar y nada la va a rellenar.
func (t MemberTable) Forget(now time.Time, kickGrace time.Duration) {
	for ip, m := range t {
		if m.Presence.Kicked && now.Sub(m.Presence.KickedAt) >= kickGrace {
			m.Presence.Kicked = false
		}
		vivo := m.Presence.InMesh || m.Presence.HasChannel || m.Presence.Kicked ||
			(m.Cred != nil && !m.Cred.Expired(now))
		if !vivo {
			delete(t, ip)
		}
	}
}
