package wfp

// Las claves de los filtros, que son la identidad que WFP necesita y el modelo
// compartido no tiene.
//
// # Por qué esto no está en `gate`
//
// Porque cómo se reconoce un filtro puesto es problema de quien lo pone, y los
// dos sistemas lo resuelven distinto. WFP guarda filtros con una GUID y hay que
// poder borrar los de la ejecución anterior sin recordar nada entre arranques,
// así que la GUID se DERIVA de la ranura. nftables no necesita nada de esto:
// reconstruye la cadena entera en una transacción, así que la identidad por
// posición es implícita.
//
// Lo compartido es que la ranura ES la identidad. Ver [gate.Spec].
//
// # Por qué este fichero no lleva etiqueta de compilación
//
// Para que el CI de Linux compruebe que la derivación es estable. Una clave que
// cambiara entre versiones dejaría filtros huérfanos que no se ven: un filtro de
// WFP no aparece en `wf.msc` ni en `Get-NetFirewallRule`.

import (
	"crypto/sha256"
	"strconv"

	"github.com/accentiostudios/kanpachi/daemon/adapter/firewall/gate"
)

// keyForSlot deriva la GUID de un filtro a partir de la ranura que ocupa.
//
// Determinista a propósito: al arrancar hay que poder borrar lo que dejó la
// ejecución anterior, y recordar las claves en disco añadiría un fichero que
// puede desincronizarse del sistema. Enumerar el sublayer sería la alternativa,
// y trae bastante más superficie de API por la misma respuesta.
//
// El espacio de nombres lleva versión: el día que cambie qué ocupa cada ranura,
// subirla evita que una limpieza borre por clave un filtro que significaba otra
// cosa.
func keyForSlot(slot int) [16]byte {
	const namespace = "kanpachi/wfp/gate/v1\x00slot "
	sum := sha256.Sum256([]byte(namespace + strconv.Itoa(slot)))

	var key [16]byte
	copy(key[:], sum[:16])
	// Se marcan los bits de versión y variante de UUID v4 para que sea una GUID
	// bien formada. Windows no lo exige y las herramientas que la muestren sí lo
	// esperan.
	key[6] = (key[6] & 0x0f) | 0x40
	key[8] = (key[8] & 0x3f) | 0x80
	return key
}

// AllKeys son las claves de TODAS las ranuras, ocupadas o no.
//
// Es lo que barre la limpieza al arrancar, y por eso no depende del conjunto
// deseado: lo que hay que borrar es justo lo que sobró de un conjunto que ya no
// se recuerda.
func AllKeys() [][16]byte {
	out := make([][16]byte, 0, gate.MaxFilters)
	for i := 0; i < gate.MaxFilters; i++ {
		out = append(out, keyForSlot(i))
	}
	return out
}

// GateKey es la clave por la que se pregunta si la compuerta está puesta.
//
// Es la ranura cero, o sea el bloqueo de todo por adaptador de la sala.
// Preguntar por una clave conocida evita enumerar, y la ranura cero es la
// correcta porque es el filtro sin el cual la compuerta no contiene nada: con
// los permisos espejo puestos y el bloqueo ausente, la lista vuelve a ser
// aditiva.
func GateKey() [16]byte { return keyForSlot(gate.SlotRoomIface) }

// LobbyGateKey es la misma pregunta para el vestíbulo.
//
// Va aparte porque el vestíbulo puede no estar, y eso NO es un fallo: el
// invitado lo suelta al entrar. Quien mida tiene que poder distinguir "no
// aplica" de "falta", y con una sola clave las dos respuestas serían la misma.
func LobbyGateKey() [16]byte { return keyForSlot(gate.SlotLobbyIface) }
