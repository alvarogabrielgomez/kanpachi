package domain

import (
	"encoding/hex"

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

// Salts versionados. El sufijo dice para qué sirve cada derivación, así que un
// networkID nunca puede coincidir con un secret aunque el código sea el mismo.
const (
	saltNetworkID = "kanpachi/v1/id"
	saltSecret    = "kanpachi/v1/secret"
)

const (
	networkIDLen = 16
	secretLen    = 32
)

// NetworkIdentity es lo que un código produce: la identidad de la red.
//
// El campo secret es privado a propósito y no hay getter que lo devuelva en
// crudo salvo [NetworkIdentity.EngineSecret], que existe solo para el
// adaptador del motor. Ver el String de más abajo.
type NetworkIdentity struct {
	networkID [networkIDLen]byte
	secret    [secretLen]byte
}

// DeriveIdentity produce el networkID y el secret a partir del código.
//
// Es la implementación del principio "el código es la credencial": ambos lados
// derivan lo mismo localmente, sin API, sin base de datos y sin registro. El
// host del seed no entra acá, es solo transporte, así que la misma sala es
// alcanzable por cualquier ruta.
//
// El salt es fijo y no aleatorio, y eso es correcto acá: un salt aleatorio
// exigiría comunicarlo, y no hay canal para hacerlo. Lo que sostiene la
// seguridad son los 60 bits de entropía del propio código, no el salt.
func DeriveIdentity(c Code) NetworkIdentity {
	pw := []byte(c.Raw())

	var out NetworkIdentity
	copy(out.networkID[:], argon2.IDKey(
		pw, []byte(saltNetworkID), argonTime, argonMemory, argonThreads, networkIDLen))
	copy(out.secret[:], argon2.IDKey(
		pw, []byte(saltSecret), argonTime, argonMemory, argonThreads, secretLen))
	return out
}

// NetworkName es el identificador que ve el seed. Es opaco y derivado, así que
// el servidor no aprende nada del código ni de quién juega qué.
func (n NetworkIdentity) NetworkName() string {
	return "kanpachi-" + hex.EncodeToString(n.networkID[:])
}

// EngineSecret devuelve el secreto en la forma que espera el motor.
//
// Único punto del programa que expone este valor. El seed jamás lo recibe: el
// motor manda un digest en el handshake, no el secreto.
func (n NetworkIdentity) EngineSecret() string {
	return hex.EncodeToString(n.secret[:])
}

// String redacta el secreto a propósito.
//
// Sin esto, un `log.Printf("%v", identity)` o un `%+v` en un mensaje de error
// filtraría el secreto de la sala a los logs locales, que además se copian al
// portapapeles con el botón de diagnóstico y se pegan en el grupo. El
// networkID sí se muestra porque es opaco y ayuda a diagnosticar.
func (n NetworkIdentity) String() string {
	return "NetworkIdentity{" + n.NetworkName() + ", secret:REDACTADO}"
}
