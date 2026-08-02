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
