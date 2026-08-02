package domain

// RoomAnnounce es lo que el host les cuenta a los miembros sobre la sala.
//
// Existe porque hay dos cosas que solo el host sabe y que el invitado necesita:
// cómo se llama la sala, que viaja cifrado en la tarjeta y no llega por la red,
// y cuál es el juego activo, que el host elige y que decide qué abre cada uno
// en un perfil de malla. Sin esto, la pantalla en sala de un invitado no tiene
// juego que mostrar, no tiene guía de conexión, y `client_ports` es código que
// nunca corre.
//
// Va por el canal de control, por la dirección de la SALA, así que solo lo
// reciben los miembros presentes.
//
// **Lleva el id del juego y jamás el perfil.** Es la diferencia entre que el
// host diga "estamos jugando Zomboid" y que el host diga "abrí estos puertos".
// El invitado resuelve el id contra su propio catálogo, con sus propias
// invariantes, y si no lo tiene no abre nada. Mandar el perfil entero
// convertiría al host en alguien que puede dictar reglas de firewall en la
// máquina de los demás, que es exactamente lo que la capa de política existe
// para impedir.
type RoomAnnounce struct {
	RoomName string
	GameID   string
}

// Sanitize acota lo que llegó de otra máquina antes de que toque nada.
//
// El nombre por runas, con el mismo tope que la tarjeta. El id contra el mismo
// alfabeto aburrido que exige un perfil: si no lo cumple, no puede existir en
// ningún catálogo, así que se descarta acá en vez de buscarlo.
func (a RoomAnnounce) Sanitize() RoomAnnounce {
	out := RoomAnnounce{RoomName: ClampRoomName(a.RoomName)}
	if validProfileID(a.GameID) {
		out.GameID = a.GameID
	}
	return out
}

// NoticeKind son los avisos que el host le manda a un miembro.
//
// Ninguno de los dos es lo que hace que la cosa ocurra, y esa distinción es la
// decisión 22 entera. Expulsar lo ejecuta el host sobre sí mismo: revoca la
// credencial y recalcula sus reglas. El aviso es CORTESÍA, para que del otro
// lado la app cierre limpio y diga qué pasó, en vez de que la partida se caiga
// sola y el usuario se quede mirando una pantalla que no explica nada.
//
// Que sea cortesía es lo que permite mandarlo ANTES de cortar. Si fuera el
// mecanismo, mandarlo primero le daría al expulsado una ventana para ignorarlo;
// como no lo es, la ventana no le sirve de nada y la única diferencia es que se
// entera.
type NoticeKind uint8

const (
	// NoticeKicked: el host te está sacando. Llega antes de que te corte.
	NoticeKicked NoticeKind = iota + 1
	// NoticeRoomClosed: el host se va y la sala se termina.
	//
	// Lo peor que logra un miembro que lo falsifique es que a otros se les
	// cierre la app. Es molestia, no riesgo, y quien lo haría ya está dentro de
	// la sala. Ver el modelo de amenazas de la decisión 23.
	NoticeRoomClosed
)

func (k NoticeKind) String() string {
	switch k {
	case NoticeKicked:
		return "expulsado"
	case NoticeRoomClosed:
		return "la sala se cerró"
	default:
		return "aviso desconocido"
	}
}

// RoomNotice es un aviso del host. Lleva el motivo en texto porque la pantalla
// del otro lado tiene que poder decir algo mejor que "te desconectaste".
type RoomNotice struct {
	Kind   NoticeKind
	Reason string
}

// ExitReason dice por qué se está en Idle.
//
// Sobrevive a limpiar la sala, y eso es a propósito: es lo único que le queda a
// la pantalla de inicio para explicar por qué se volvió a ella. Sin esto, que
// te expulsen, que el host desaparezca veinte minutos y salir por tu cuenta se
// ven exactamente igual.
type ExitReason uint8

const (
	ExitNone ExitReason = iota
	// ExitUser: salió por su cuenta.
	ExitUser
	// ExitKicked: el host lo sacó.
	ExitKicked
	// ExitHostGone: veinte minutos sin host, decisión 20.
	ExitHostGone
	// ExitRoomClosed: el host cerró la sala.
	ExitRoomClosed
	// ExitFailed: no se llegó a entrar.
	ExitFailed
)

func (r ExitReason) String() string {
	switch r {
	case ExitUser:
		return "saliste de la sala"
	case ExitKicked:
		return "el host te sacó de la sala"
	case ExitHostGone:
		return "el host llevaba veinte minutos sin aparecer"
	case ExitRoomClosed:
		return "el host cerró la sala"
	case ExitFailed:
		return "no se pudo entrar a la sala"
	default:
		return ""
	}
}
