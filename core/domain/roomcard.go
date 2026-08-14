package domain

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// CardKeyLen y cardNonceLen fijan el formato que la página de invitación ya
// implementa: AES-256-GCM con los doce primeros bytes del blob como nonce.
//
// El formato lo dicta invite/index.html, que descifra con WebCrypto en el
// navegador. Cambiar cualquiera de estos dos números acá sin cambiarlo allá
// hace que la tarjeta deje de leerse, y el síntoma es mudo: la página se queda
// con la versión genérica sin decir por qué. Hay un vector dorado en el test
// por ese motivo.
const (
	CardKeyLen   = 32
	cardNonceLen = 12
)

// MaxRoomNameLen es el tope del nombre visible de una sala.
//
// Lo aplica la página con `clean(name, MAX_NAME)` y se aplica también acá, al
// abrir: la firma prueba quién escribió la tarjeta, no que lo que escribió
// quepa en la pantalla de nadie.
const MaxRoomNameLen = 40

// ClampRoomName acota el nombre visible de una sala.
//
// Recorta por RUNAS y no por bytes. El nombre es texto libre sin validar, a
// diferencia del nick, que es ASCII alfanumérico: cortarlo en el byte 40
// parte un acento o un emoji por la mitad y lo que llega a la pantalla del
// otro es un rombo con un signo de pregunta.
//
// Se aplica en los DOS lados. Al escribir, para que el host vea lo mismo que
// van a ver los demás en vez de un nombre que a todos les llega recortado. Al
// abrir, porque la firma prueba quién escribió la tarjeta y no que lo que
// escribió quepa en la pantalla de nadie.
func ClampRoomName(s string) string {
	r := []rune(s)
	if len(r) <= MaxRoomNameLen {
		return s
	}
	return string(r[:MaxRoomNameLen])
}

// MaxCardBytes es el tope que el registro también aplica del otro lado.
//
// La tarjeta lleva un nick de hasta 12 caracteres y un nombre de sala corto,
// así que 512 sobra. Existe para que el registro no se convierta en
// almacenamiento de nada, y se comprueba también acá para que un nombre de
// sala absurdo falle en la máquina del host, con un error que se puede
// mostrar, en vez de en una respuesta HTTP.
const MaxCardBytes = 512

var (
	ErrCardTooBig  = fmt.Errorf("la tarjeta pasa de %d bytes", MaxCardBytes)
	ErrCardShape   = errors.New("la tarjeta no tiene la forma esperada")
	ErrCardKeyLen  = fmt.Errorf("la clave de la tarjeta debe medir %d bytes", CardKeyLen)
	ErrCardDecrypt = errors.New("la tarjeta no se pudo descifrar: clave equivocada o contenido manipulado")
)

// RoomCard es la tarjeta de presentación de una sala.
//
// Es SOLO presentación: quién se identifica como host y cómo se llama la sala.
// Que no llegue no impide entrar, y lo único que se pierde es que la página
// muestre algo mejor que "una sala de Kanpachi".
//
// **Ojo con la lectura fácil de eso.** Que la tarjeta sea prescindible no hace
// prescindible al registro: sin registro no se entra a ninguna sala, ver la
// cabecera de `RoomDirectory` en core/port. Falta tarjeta también con el
// registro perfectamente
// vivo, por un código dictado por teléfono, un enlace sin fragmento o un seed
// ajeno, y esos son los casos que este párrafo describe.
//
// El seed guarda esto CIFRADO y no puede leerlo: la clave viaja en el
// fragmento de la URL, que el navegador no manda al servidor. Ver decisión 17.
type RoomCard struct {
	Host Nickname
	Room string
}

// cardJSON es la forma que la página espera dentro del texto cifrado. Las
// claves son cortas porque el tope de 512 bytes se reparte entre esto y el
// nonce, y porque nadie las lee a ojo: van cifradas.
type cardJSON struct {
	Host string `json:"host"`
	Room string `json:"room"`
}

// SealRoomCard cifra la tarjeta y devuelve el blob y la clave.
//
// La clave se genera acá y NUNCA se manda al registro: se pega al final del
// enlace de invitación, en el fragmento, que es la parte de una URL que el
// navegador no transmite. Eso es lo que hace que el servidor guarde algo que
// no puede leer.
//
// El blob es nonce || ciphertext, en ese orden, porque es lo que hace la
// página: `crypto.subtle.decrypt({iv: blob.slice(0,12)}, k, blob.slice(12))`.
func SealRoomCard(card RoomCard, rand io.Reader) (blob []byte, key [CardKeyLen]byte, err error) {
	plain, err := json.Marshal(cardJSON{Host: card.Host.String(), Room: card.Room})
	if err != nil {
		return nil, key, fmt.Errorf("domain: serializando la tarjeta: %w", err)
	}

	if _, err := io.ReadFull(rand, key[:]); err != nil {
		return nil, key, fmt.Errorf("domain: leyendo aleatoriedad para la clave de tarjeta: %w", err)
	}
	nonce := make([]byte, cardNonceLen)
	if _, err := io.ReadFull(rand, nonce); err != nil {
		return nil, key, fmt.Errorf("domain: leyendo aleatoriedad para el nonce de tarjeta: %w", err)
	}

	gcm, err := newCardGCM(key[:])
	if err != nil {
		return nil, key, err
	}

	// El nonce va delante y también como prefijo del destino, así que Seal
	// escribe el ciphertext justo detrás y el blob sale armado de una sola
	// asignación.
	blob = gcm.Seal(nonce, nonce, plain, nil)
	if len(blob) > MaxCardBytes {
		return nil, key, fmt.Errorf("%w, mide %d", ErrCardTooBig, len(blob))
	}
	return blob, key, nil
}

// OpenRoomCard descifra. Lo usa el propio host para releer su tarjeta al
// reabrir una sala, y los tests para cerrar el círculo contra la página.
//
// AES-GCM autentica, así que un fallo acá significa que el contenido no es de
// fiar y se descarta ENTERO. No hay descifrado parcial ni campo que se
// aproveche: o la tarjeta es la que el host escribió, o no hay tarjeta.
func OpenRoomCard(blob []byte, key [CardKeyLen]byte) (RoomCard, error) {
	if len(blob) <= cardNonceLen {
		return RoomCard{}, ErrCardShape
	}
	gcm, err := newCardGCM(key[:])
	if err != nil {
		return RoomCard{}, err
	}
	plain, err := gcm.Open(nil, blob[:cardNonceLen], blob[cardNonceLen:], nil)
	if err != nil {
		return RoomCard{}, ErrCardDecrypt
	}

	var j cardJSON
	if err := json.Unmarshal(plain, &j); err != nil {
		return RoomCard{}, ErrCardShape
	}
	// El nick se revalida al salir del sobre. Viene de otra máquina, aunque
	// venga autenticado: la firma prueba quién lo escribió, no que lo que
	// escribió sea un nick legal, y este valor termina en la pantalla de
	// alguien.
	nick, err := ParseNickname(j.Host)
	if err != nil {
		return RoomCard{}, fmt.Errorf("%w: %v", ErrCardShape, err)
	}
	// El nombre de la sala también se acota. Es texto libre que escribió otra
	// máquina y termina en la pantalla de alguien: el tope de 512 bytes del
	// sobre no lo acota solo, porque un nombre de 400 caracteres cabe.
	return RoomCard{Host: nick, Room: ClampRoomName(j.Room)}, nil
}

func newCardGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != CardKeyLen {
		return nil, ErrCardKeyLen
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("domain: montando AES para la tarjeta: %w", err)
	}
	return cipher.NewGCM(block)
}

// CardKeyFragment codifica la clave como la espera la página: base64url sin
// relleno, que es lo que `atob` reconstruye tras cambiar - por + y _ por /.
func CardKeyFragment(key [CardKeyLen]byte) string {
	return base64.RawURLEncoding.EncodeToString(key[:])
}

// ParseCardKeyFragment es el inverso, para cuando la app recibe un enlace
// completo con la clave pegada.
func ParseCardKeyFragment(s string) ([CardKeyLen]byte, error) {
	var key [CardKeyLen]byte
	// RawURLEncoding no acepta relleno, y un enlace copiado a mano puede
	// traerlo. Se prueban las dos formas antes de rendirse, porque rechazar un
	// "=" al final sería fallar por un detalle que el usuario no puede ver.
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		raw, err = base64.URLEncoding.DecodeString(s)
	}
	if err != nil {
		return key, fmt.Errorf("%w: la clave no es base64url", ErrCardShape)
	}
	if len(raw) != CardKeyLen {
		return key, ErrCardKeyLen
	}
	copy(key[:], raw)
	return key, nil
}

// InviteLink arma el enlace completo que la app GENERA, con la clave de la
// tarjeta pegada en el fragmento.
//
// [Room.InviteURL] devuelve la forma sin clave, que es la que se dicta por
// teléfono. Esta es la que se copia al portapapeles y se pega en Telegram.
func (r Room) InviteLink(key [CardKeyLen]byte) string {
	return "https://" + r.InviteURL() + "#" + CardKeyFragment(key)
}

// ---- Quién escribió la tarjeta que el registro sirvió ----------------------

// CardTrust es lo que se sabe del ORIGEN de una tarjeta, que es distinto de si
// se pudo abrir.
//
// Abrirla la autentica contra la clave del fragmento, y eso solo prueba que la
// escribió alguien que tenía el enlace. Quién de todos los que lo tienen, lo
// contesta la firma de la llave larga del host, que es la que el registro FIJA
// para ese invite ID la primera vez que la ve. Ver la decisión 24.
type CardTrust uint8

const (
	// CardUnverified es que no había con qué comprobar: el registro no mandó
	// firma, o no mandó la llave fijada. Es el caso de una sala publicada antes
	// de que el registro guardara la firma, y **no** es una acusación.
	CardUnverified CardTrust = iota
	// CardSigned es que la firma valida contra la llave que ese registro fijó.
	CardSigned
	// CardForged es que hay firma, hay llave, y no se corresponden.
	//
	// No significa "alguien manipuló los bytes en el cable": una tarjeta
	// manipulada no abre, porque AES-GCM autentica. Significa que **el registro
	// está sirviendo una tarjeta que la llave que él mismo fijó no respalda**, y
	// eso solo puede pasar si ese registro está comprometido.
	CardForged
)

// InviteLookup es lo que un registro contesta sobre un invite ID.
//
// Es un struct y no cuatro retornos sueltos porque cuatro es donde una firma de
// función deja de leerse, y porque los dos campos nuevos viajan juntos: una
// firma sin la llave contra la que verificarla no dice nada.
type InviteLookup struct {
	// Sealed es la tarjeta cifrada, opaca para el registro y para esto.
	Sealed []byte
	// Members es cuánta gente hay, o -1 cuando el registro se negó a decirlo.
	Members int
	// HostKey es la llave larga que ese registro FIJÓ para este invite ID.
	HostKey []byte
	// Sig es la firma con la que se depositó la tarjeta.
	Sig []byte
}

// Trust dice qué se sabe del origen de esta tarjeta.
//
// **La llave viene del mismo sitio que la tarjeta, y eso está asumido.** Un
// registro comprometido puede servir su propia llave con su propia firma y esto
// contestará [CardSigned]: lo que compra hoy es que no pueda cambiar la tarjeta
// SIN cambiar también la llave, o sea que la mentira deje rastro. Quien nota ese
// rastro es la libreta de huellas de la decisión 25, que recuerda con qué llave
// se vio a este host las veces anteriores. Sin ella, esto es continuidad de una
// sola sesión y se dice así.
func (l InviteLookup) Trust() CardTrust {
	if len(l.Sig) == 0 || len(l.HostKey) != ed25519.PublicKeySize {
		return CardUnverified
	}
	if ed25519.Verify(ed25519.PublicKey(l.HostKey), l.Sealed, l.Sig) {
		return CardSigned
	}
	return CardForged
}
