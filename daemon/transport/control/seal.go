package control

import (
	"crypto/rand"
	"errors"
	"fmt"

	"golang.org/x/crypto/nacl/box"
)

// El sellado del canal, y lo que compra exactamente.
//
// Es una caja sellada anónima: llave efímera del emisor por mensaje, abierta con
// el par del destinatario. Se usa en dos sitios, la credencial que el host emite
// por la puerta y el invite ID nuevo que reparte al renovarlo.
//
// **Qué compra.** Confidencialidad frente a un tercero que transporte los bytes.
// El motor puede relayar tráfico por otro peer, así que sin esto el intermediario
// se llevaría el token con el que se entra a la sala, y el código nuevo con el
// que se vuelve a entrar.
//
// **Qué NO compra, dicho para no vender lo que no hay.** Autenticación del host.
// Es anónima a propósito: sin la llave larga de la decisión 25 no existe una
// llave del host contra la cual verificar, y una firma hecha con una llave
// efímera que llega en el mismo mensaje no prueba absolutamente nada. En el
// vestíbulo las direcciones son autoasignadas, así que alguien que tenga el
// código puede ocupar la dirección del host y contestar por él. La respuesta
// disponible hoy es la que ya existe: renovar el código, que levanta un
// vestíbulo nuevo y deja al ocupante solo en el viejo.
//
// Las llaves son EFÍMERAS y de sesión. No tocan el disco, no identifican a nadie
// entre salas y se descartan al salir, así que no son identidad persistida y no
// habilitan ningún baneo.

// keyLen es el tamaño de una llave X25519, que es lo que usa nacl/box.
const keyLen = 32

var errSeal = errors.New("el sobre sellado no se pudo abrir")

// keyPair es el par efímero de esta sesión.
type keyPair struct {
	pub  *[keyLen]byte
	priv *[keyLen]byte
}

func newKeyPair() (keyPair, error) {
	pub, priv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		return keyPair{}, fmt.Errorf("generando la llave de sesión: %w", err)
	}
	return keyPair{pub: pub, priv: priv}, nil
}

// seal cierra el sobre contra la llave pública del destinatario.
func seal(destinatario []byte, plano []byte) ([]byte, error) {
	if len(destinatario) != keyLen {
		return nil, fmt.Errorf("la llave del destinatario mide %d bytes", len(destinatario))
	}
	var pub [keyLen]byte
	copy(pub[:], destinatario)

	sellado, err := box.SealAnonymous(nil, plano, &pub, rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("sellando el sobre: %w", err)
	}
	return sellado, nil
}

// open abre un sobre dirigido a este par.
//
// Un sobre que no abre se descarta entero y sin decir por qué al otro lado: el
// motivo del fallo es información que no le debemos a quien mandó algo que no
// era para nosotros.
func (k keyPair) open(sellado []byte) ([]byte, error) {
	plano, ok := box.OpenAnonymous(nil, sellado, k.pub, k.priv)
	if !ok {
		return nil, errSeal
	}
	return plano, nil
}
