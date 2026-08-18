package jsonfile

// El sellado de lo que queda en disco.
//
// # Por qué acá y no en el dominio
//
// Because the key comes from `identity.key`, and that file deliberately never
// reaches core: the domain does not know it exists, and the adapter that signs
// is the one that reads it. Protection at rest is the same kind of thing as the
// Windows ACL and the 0600 mode, and both of those already live in an adapter.
// Core keeps producing and consuming the same plain JSON, with its strict
// decoder, and never learns that the bytes travelled sealed.

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
)

// magia marca un fichero sellado, y existe para poder abrir los de antes.
//
// A file written before this existed starts with `{`, because it is plain JSON.
// So the first bytes say which of the two it is, with no flag to keep anywhere
// and no version file. An upgrade opens the old one, and the next save leaves
// it sealed.
var magia = []byte("KPSEAL1\n")

// nonceLen es el del GCM estándar, doce bytes.
var errSelloCorto = errors.New("el estado sellado está cortado")

const nonceLen = 12

// sellar cifra con AES-256-GCM y devuelve magia || nonce || cifrado.
//
// Sin datos autenticados aparte: el nombre del fichero no entra en el sello. Lo
// que se gana con AAD es que un blob no se pueda mover de un fichero a otro, y
// acá los cuatro que se sellan llevan cosas distintas y el decodificador
// estricto del dominio rechaza el que no le toca. Meterlo sería una defensa
// contra algo que ya falla ruidoso.
//
// Dos no pasan por acá, `seed.txt` y `profile.json`: van en claro a propósito,
// y el motivo está donde se guarda cada uno. Los dos pasan el mismo test —no
// son secretos, y sellarlos rompería algo que hace falta con el daemon caído o
// antes de que conteste. Ver [Store.SaveSeed] y [Store.SaveProfile].
func sellar(clave [32]byte, plano []byte) ([]byte, error) {
	gcm, err := nuevoGCM(clave)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, nonceLen)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("leyendo aleatoriedad para el sello: %w", err)
	}
	salida := make([]byte, 0, len(magia)+nonceLen+len(plano)+gcm.Overhead())
	salida = append(salida, magia...)
	salida = append(salida, nonce...)
	return gcm.Seal(salida, nonce, plano, nil), nil
}

// abrir descifra lo que escribió [sellar].
//
// **Un fichero sin la marca se devuelve TAL CUAL, y eso es la migración.** Es
// uno escrito por una versión anterior, en claro. Se lee, la sala se reabre, y
// el guardado siguiente ya lo deja sellado. La alternativa era negarse, o sea
// perderle la sala a quien actualiza, que es un precio que nadie pidió pagar.
func abrir(clave [32]byte, crudo []byte) ([]byte, error) {
	if !tieneMarca(crudo) {
		return crudo, nil
	}
	cuerpo := crudo[len(magia):]
	if len(cuerpo) <= nonceLen {
		return nil, errSelloCorto
	}
	gcm, err := nuevoGCM(clave)
	if err != nil {
		return nil, err
	}
	plano, err := gcm.Open(nil, cuerpo[:nonceLen], cuerpo[nonceLen:], nil)
	if err != nil {
		// GCM autentica, así que esto es que la llave no es la que selló o que
		// alguien tocó los bytes. Las dos terminan igual y no se distinguen a
		// propósito: contestar cuál de las dos fue es contestarle a quien probó.
		return nil, fmt.Errorf("el estado guardado no se pudo abrir con la llave de esta instalación")
	}
	return plano, nil
}

func tieneMarca(crudo []byte) bool {
	if len(crudo) < len(magia) {
		return false
	}
	for i := range magia {
		if crudo[i] != magia[i] {
			return false
		}
	}
	return true
}

func nuevoGCM(clave [32]byte) (cipher.AEAD, error) {
	bloque, err := aes.NewCipher(clave[:])
	if err != nil {
		return nil, fmt.Errorf("montando AES para el estado: %w", err)
	}
	return cipher.NewGCM(bloque)
}
