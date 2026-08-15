package domain

import (
	"encoding/binary"
	"fmt"
	"time"
)

// Cerrar una sala en el registro: qué se firma, y por qué lleva un reloj.
//
// Vive en el dominio por lo mismo que [SeedAuthProof]: lo necesitan los dos
// lados, el cliente para firmar y el registro para comprobar, y ninguno de los
// dos puede preguntarle al otro cómo se arma. Una segunda versión de estos
// bytes en el otro extremo es una firma que no valida y un cierre que nunca
// ocurre.
//
// # Por qué el cierre se firma, si el registro ya tiene una puerta
//
// Porque la puerta es del OPERADOR del seed y esto es del HOST de la sala. Un
// seed abierto no pide nada, y ahí el password no protege nada; un seed cerrado
// lo pide a todo el que hospeda, o sea a cualquiera que tenga la credencial
// compartida. Lo que dice que esta sala es tuya es la llave que el registro fijó
// para ese invite ID la primera vez, igual que al publicar la tarjeta.
//
// # Por qué lleva marca de tiempo, que la publicación no lleva
//
// Porque cerrar es el único mensaje del que una copia grabada sigue sirviendo
// más tarde. Publicar una tarjeta vieja deja la sala con una tarjeta vieja, que
// es lo que ya había. Reproducir un cierre después de que el host REABRA la
// misma sala con el mismo código mata una sala viva, y reabrir con el mismo
// código es justo lo que el host headless hace en cada arranque. La marca es lo
// que le pone fecha de caducidad a esa copia.

// roomCloseLabel mantiene esta firma separada de cualquier otro uso de la misma
// llave. Versionada, como el resto de separadores de dominio del proyecto.
const roomCloseLabel = "kanpachi/room-close/v1"

// RoomCloseSkew es cuánto se le tolera al reloj del que firma.
//
// Cinco minutos para los dos lados. No hay nada que sincronizar entre las dos
// máquinas y un reloj de escritorio se va unos segundos sin que nadie lo note,
// así que un margen corto convertiría el cierre en algo que falla según qué PC
// lo pida. Más ancho no compra nada: lo que acota es cuánto vale una copia
// grabada, y cinco minutos es mucho menos que las seis horas que dura la
// tarjeta.
const RoomCloseSkew = 5 * time.Minute

// RoomCloseMessage arma los bytes que se firman para cerrar una sala.
//
// El cero separa los tres campos y ninguno puede contenerlo: la etiqueta es una
// constante, el invite ID sale del alfabeto de 32 símbolos, y el tiempo va en
// ocho bytes de ancho fijo. Con eso la lectura es inequívoca sin prefijos de
// longitud.
//
// El invite ID va DENTRO, y sin él una firma buena de una sala serviría para
// cerrar cualquier otra del mismo host.
func RoomCloseMessage(id InviteID, at time.Time) []byte {
	raw := id.Raw()
	msg := make([]byte, 0, len(roomCloseLabel)+1+len(raw)+1+8)
	msg = append(msg, roomCloseLabel...)
	msg = append(msg, 0)
	msg = append(msg, raw...)
	msg = append(msg, 0)
	return binary.BigEndian.AppendUint64(msg, uint64(at.Unix()))
}

// CheckRoomCloseTime dice si esa marca de tiempo sigue sirviendo.
//
// Rechaza por los dos lados y no solo por el pasado. Una marca en el futuro es
// un reloj mal puesto o alguien alargándole la vida a una firma a propósito, y
// las dos terminan igual: un mensaje que valdría más de lo que este código
// acepta que valga nada.
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
