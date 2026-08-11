package domain

import (
	"encoding/hex"
	"net/netip"

	"golang.org/x/crypto/argon2"
)

// Parámetros de Argon2id. CONGELADOS PARA v1.
//
// Los dos lados de una sala derivan por separado y sin hablarse, así que
// cualquier cambio en estos números produce un networkID distinto y las dos
// personas dejan de verse, con un síntoma inexplicable: "pegué el mismo código
// y estoy solo en la sala". No hay negociación de versión que lo rescate.
//
// Por eso existe el vector dorado en identity_test.go: cambiar cualquiera de
// estas constantes rompe el test de forma ruidosa. Un esquema nuevo se agrega
// como saltV2, jamás editando estos valores.
const (
	argonTime    = 3
	argonMemory  = 64 * 1024 // 64 MiB
	argonThreads = 4
)

// ArgonMemoryKiB expone el coste de memoria porque quien hospeda una derivación
// tiene que dimensionarse para ella, y adivinar ese número sale caro.
//
// Se exporta a raíz de un fallo real: la unit de systemd del registro llevaba
// MemoryMax=96M, elegido a ojo, y el primer POST que creó una sala murió por
// OOM. El kernel mata sin avisar y systemd reinicia, así que desde fuera parece
// un servicio que "se reinicia solo" y no un límite mal puesto. El generador de
// units calcula ahora su techo a partir de esta constante, y un test ata las
// dos cosas para que no vuelvan a separarse.
//
// DeriveRendezvous llama a argon2.IDKey DOS veces, así que el pico real es del
// orden del doble de este valor.
const ArgonMemoryKiB = argonMemory

// Salts versionados. El sufijo dice para qué sirve cada derivación, así que un
// networkID nunca puede coincidir con un secret aunque el invite ID sea el
// mismo.
const (
	saltNetworkID = "kanpachi/v1/id"
	saltSecret    = "kanpachi/v1/secret"
)

const (
	networkIDLen = 16
	secretLen    = 32
)

// Rendezvous es la identidad de ENCUENTRO de una sala.
//
// LEER ESTO ANTES DE USARLO. Es un vestíbulo público y desechable, no la sala.
// Cualquiera que tenga el invite ID puede derivarla, el seed incluido, porque
// la derivación es una función pura de un valor que viaja en la URL. Eso no es
// una fuga, es el diseño: lo único que ocurre acá es el pedido de credencial al
// host, y ese intercambio va firmado y cifrado por su cuenta.
//
// La red REAL de la sala tiene identidad propia, generada aleatoriamente por el
// host, que no deriva de ningún string y no vive en ningún servidor. Ver la
// decisión 2 en docs/02-decisiones-de-diseno.md.
type Rendezvous struct {
	networkID [networkIDLen]byte
	secret    [secretLen]byte
}

// DeriveRendezvous produce la identidad de encuentro a partir del invite ID.
//
// Corre en el cliente y no le pregunta al seed. El seed podría decir cuál es,
// derivarla localmente hace que llegar al vestíbulo no dependa de que su API
// esté viva ni de que diga la verdad.
//
// El salt es fijo y no aleatorio, y eso es correcto acá: un salt aleatorio
// exigiría comunicarlo, y no hay canal para hacerlo. Tampoco hay nada que
// proteger con él, porque el valor derivado es alcanzable por quien tenga el
// invite ID por definición. Argon2id se conserva por costo de enumeración: aun
// con límite de tasa en el registro, encarece barrer el espacio de 40 bits
// contra el seed.
func DeriveRendezvous(id InviteID) Rendezvous {
	pw := []byte(id.Raw())

	var out Rendezvous
	copy(out.networkID[:], argon2.IDKey(
		pw, []byte(saltNetworkID), argonTime, argonMemory, argonThreads, networkIDLen))
	copy(out.secret[:], argon2.IDKey(
		pw, []byte(saltSecret), argonTime, argonMemory, argonThreads, secretLen))
	return out
}

// NetworkName es el identificador que ve el seed. Es lo que el registro usa de
// clave para el contador de miembros, y lo que aparece en `peer list-foreign`.
func (r Rendezvous) NetworkName() string {
	return "kanpachi-" + hex.EncodeToString(r.networkID[:])
}

// EngineSecret devuelve el secreto de encuentro en la forma que espera el
// motor. Único punto del programa que expone este valor.
func (r Rendezvous) EngineSecret() string {
	return hex.EncodeToString(r.secret[:])
}

// LobbySubnet es el /24 donde se encuentran host e invitado, derivado del mismo
// invite ID que el resto de la identidad de encuentro.
//
// # Qué problema resuelve que sea derivado y no una constante
//
// Los dos lados tienen que llegar al MISMO /24 sin haber hablado antes: el
// invitado necesita una dirección a la que marcar para pedir la credencial, y la
// subred de la sala viene dentro de esa credencial, o sea después. Una constante
// cumple eso, y por eso fue lo primero. Lo que no cumple es poder moverse.
//
// Derivarlo del código cumple las dos cosas. Sigue siendo una función pura de un
// valor que las dos máquinas ya tienen, así que nadie negocia nada, y **renovar
// el código cambia el vestíbulo**. Eso convierte un rango que le choca a alguien
// de "este producto no le sirve" a "que el host le dé al botón que ya existe".
//
// La diferencia importa porque no se puede acertar a ciegas: no hay forma de
// saber qué rangos tiene la máquina de cada invitado, así que el diseño no puede
// depender de haber elegido bien. Elegir [LobbySpace] fuera de CGNAT baja mucho
// la probabilidad; poder moverse es lo que hace que equivocarse tenga salida.
//
// Sale de networkID, que ya está calculado, y no de un Argon2id nuevo: es el
// mismo secreto derivado del mismo código, y gastar otro pase de memoria dura
// para elegir 256 posibilidades no compra nada.
//
// Que el /24 sea público no filtra nada, por lo mismo que la red de encuentro no
// filtra nada: quien tenga el invite ID puede derivar las dos, y una IP dentro de
// un overlay cifrado no dice de qué sala es.
func (r Rendezvous) LobbySubnet() netip.Prefix {
	base := LobbySpace.Addr().As4()
	// El tercer octeto elige entre los 256 /24 de la mitad alta de RFC 2544. Un
	// byte del networkID basta: no es una elección con requisitos
	// criptográficos, es un reparto parejo.
	base[2] = r.networkID[0]
	base[3] = 0
	return netip.PrefixFrom(netip.AddrFrom4(base), RoomPrefixBits)
}

// LobbyHostAddress es la .1 del vestíbulo, que es a la que marca el invitado.
//
// Va junto a la subred y no como constante aparte por lo que enseñó tenerlas
// separadas: eran dos verdades sobre la misma cosa y nada obligaba a que
// coincidieran.
func (r Rendezvous) LobbyHostAddress() netip.Addr {
	return HostAddress(r.LobbySubnet())
}

// String redacta el secreto a propósito.
//
// Sin esto, un `log.Printf("%v", rdv)` o un `%+v` en un mensaje de error
// filtraría el secreto a los logs locales, que además se copian al portapapeles
// con el botón de diagnóstico y se pegan en el grupo. Que este secreto en
// concreto sea derivable por cualquiera con el invite ID no cambia el hábito:
// el mismo tipo de valor en la red real sí es sensible, y la única defensa que
// funciona es que redactar sea lo normal.
func (r Rendezvous) String() string {
	return "Rendezvous{" + r.NetworkName() + ", secret:REDACTADO}"
}
