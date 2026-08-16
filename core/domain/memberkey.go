package domain

import (
	"crypto/ed25519"
	"fmt"
	"io"
)

// MemberKey es la identidad de un invitado DENTRO de una sala: un par Ed25519
// que nace en el primer ingreso, se reusa en cada vuelta a esa sala, y jamás
// se comparte entre salas.
//
// # Qué compra
//
// El host ata la credencial y su dirección virtual a esta llave pública, así
// que quien vuelve recibe LO SUYO en vez de quemar una dirección por vuelta. Y
// nadie puede ocupar el lugar de un miembro eligiendo su apodo, que era el
// motivo por el que las direcciones no se podían reusar. Ver
// [timing.ArrivalGrace].
//
// # Por sala y aleatoria A PROPÓSITO
//
// Una llave compartida entre salas dejaría que dos hosts se juntaran a
// reconocer a la misma máquina, que es exactamente el rastreo del que
// protegían las llaves efímeras de sesión. Esta conserva esa propiedad: lo que
// es estable dentro de una sala es inenlazable fuera de ella. Por lo mismo NO
// deriva de `identity.key`, y una carpeta portable copiada no clona la
// identidad de sala de nadie.
//
// # Qué NO compra, dicho para que nadie lo venda
//
// No entra a ninguna sala por su cuenta. Entrar sigue pasando por el canje: el
// host emite, la revocación significa lo mismo de siempre, y renovar el código
// sigue cerrando la puerta. La semilla persiste dentro del estado SELLADO del
// invitado, y perderla solo cuesta volver como miembro nuevo.
type MemberKey struct {
	priv ed25519.PrivateKey
}

// MemberSeedLen es lo que mide la semilla persistida.
const MemberSeedLen = ed25519.SeedSize

// NewMemberKey genera la llave de una sala nueva.
func NewMemberKey(r io.Reader) (MemberKey, error) {
	_, priv, err := ed25519.GenerateKey(r)
	if err != nil {
		return MemberKey{}, fmt.Errorf("generando la llave de miembro: %w", err)
	}
	return MemberKey{priv: priv}, nil
}

// MemberKeyFromSeed reconstruye la llave guardada con el estado de la sala.
//
// Una semilla del tamaño equivocado es un error y jamás una llave nueva: una
// llave regenerada en silencio es un miembro al que el host ya no reconoce,
// sin ninguna línea que diga por qué.
func MemberKeyFromSeed(seed []byte) (MemberKey, error) {
	if len(seed) != MemberSeedLen {
		return MemberKey{}, fmt.Errorf("la semilla de la llave de miembro mide %d bytes y no %d", len(seed), MemberSeedLen)
	}
	return MemberKey{priv: ed25519.NewKeyFromSeed(seed)}, nil
}

// IsZero dice si no hay llave.
func (k MemberKey) IsZero() bool { return len(k.priv) == 0 }

// Seed es lo que se persiste, y solo dentro del estado sellado.
func (k MemberKey) Seed() []byte {
	if k.IsZero() {
		return nil
	}
	return k.priv.Seed()
}

// Public es contra lo que el host ata la credencial.
func (k MemberKey) Public() []byte {
	if k.IsZero() {
		return nil
	}
	return []byte(k.priv.Public().(ed25519.PublicKey))
}

// Sign firma el transcript del pedido de credencial. El transcript lo arma el
// transporte y no core, porque lleva la llave efímera de la conexión, que core
// no ve.
func (k MemberKey) Sign(msg []byte) []byte {
	if k.IsZero() {
		return nil
	}
	return ed25519.Sign(k.priv, msg)
}

// VerifyMemberSig comprueba una firma contra la llave pública que llegó en el
// mismo pedido.
//
// Que la llave llegue junto a su firma acá NO es el error que sería en la
// respuesta del host, y la diferencia merece escribirse: al host no lo
// autentica nadie más que la llave que el registro fijó ANTES, mientras que al
// miembro no lo autentica nadie en absoluto, LA LLAVE ES la identidad. Lo que
// la firma prueba es posesión: que quien pidió tiene la privada de la llave a
// la que el host va a atar la credencial, así que nadie puede pedir a nombre
// de la llave de otro.
func VerifyMemberSig(pub, msg, sig []byte) bool {
	if len(pub) != ed25519.PublicKeySize {
		return false
	}
	return ed25519.Verify(ed25519.PublicKey(pub), msg, sig)
}
