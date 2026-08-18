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

// MaxCardBytes es el tope de la tarjeta, y es EL MISMO a los dos lados.
//
// El registro lo aplica leyendo esta constante, no una copia suya: dos topes
// con el mismo nombre y el mismo número, sin nada que obligara a que
// coincidieran, es como se consigue que uno suba y el otro no.
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
	// Name is the room's VISIBLE name. The wire key stays `room`, in cardJSON.
	Name string
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
	plain, err := json.Marshal(cardJSON{Host: card.Host.String(), Room: card.Name})
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
	return RoomCard{Host: nick, Name: ClampRoomName(j.Room)}, nil
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
//
// # Sin clave devuelve la forma sin fragmento, y eso no es un caso raro
//
// Es el caso de todo INVITADO. La clave de la tarjeta la guardan solo los
// caminos del host —crear, renovar y reabrir—, así que quien entró a la sala de
// otro tiene el valor cero, y pegarlo producía un enlace terminado en cuarenta y
// tres "A", que son treinta y dos ceros en base64url. **No era cosmético**: quien
// lo recibiera traía un fragmento que no abre ninguna tarjeta.
//
// La forma sin fragmento sí sirve: quien la reciba entra igual y ve la tarjeta
// genérica, porque la clave nunca fue lo que abre la sala. Reportado en vivo
// sobre el CLI de Linux el 2026-08-18.
func (r Room) InviteLink(key [CardKeyLen]byte) string {
	if key == ([CardKeyLen]byte{}) {
		return "https://" + r.InviteURL()
	}
	return "https://" + r.InviteURL() + "#" + CardKeyFragment(key)
}

// ---- Who wrote the card the registry served ------------------------------

// CardTrust is what is known about a card's ORIGIN, which is a different
// question from whether it opened.
//
// Opening it authenticates it against the fragment key, and that only proves it
// was written by somebody holding the link. Which of the people holding it is
// answered by the signature of the host's long-term key, the one the registry
// PINS for that invite ID the first time it sees it. See decision 24.
type CardTrust uint8

const (
	// CardUnverified means there was nothing to check against: the registry
	// sent no signature, or no pinned key. It is the case of a room published
	// before the registry stored signatures, and it is **not** an accusation.
	CardUnverified CardTrust = iota
	// CardSigned means the signature validates against the key that registry
	// pinned.
	CardSigned
	// CardUnbacked means there is a signature, there is a key, and they do not
	// match.
	//
	// It does not mean "somebody tampered with the bytes on the wire": a
	// tampered card does not open at all, because AES-GCM authenticates. It
	// means **the registry is serving a card that the key it pinned itself does
	// not back**, which can only happen if that registry is compromised.
	CardUnbacked
)

// InviteLookup is what a registry answers about an invite ID.
//
// It is a struct rather than four loose return values because four is where a
// function signature stops being readable, and because the two new fields
// travel together: a signature with no key to verify it against says nothing.
type InviteLookup struct {
	// Sealed is the encrypted card, opaque to the registry and to this.
	Sealed []byte
	// Members is how many people are in, or -1 when the registry declined to
	// say.
	Members int
	// HostKey is the long-term key THIS ANSWER carried for this invite ID.
	//
	// The wording matters: it is what that registry says it pinned, not a fact
	// checked from here. A compromised one can serve its own. What notices is
	// the fingerprint book of decision 25, which remembers the key this host
	// arrived with the previous times.
	HostKey []byte
	// Sig is the signature the card was deposited with.
	Sig []byte
}

// Trust says what is known about this card's origin.
//
// **The key comes from the same place the card does, and that is assumed
// here.** A compromised registry can serve its own key with its own signature
// and this will answer [CardSigned]: what it buys today is that the card cannot
// be changed WITHOUT also changing the key, so the lie leaves a trace. Who
// notices that trace is the fingerprint book of decision 25, which remembers
// which key this host was seen with the previous times. Without it, this is
// continuity within a single session, and it is stated that way.
func (l InviteLookup) Trust() CardTrust {
	if len(l.Sig) == 0 || len(l.HostKey) != ed25519.PublicKeySize {
		return CardUnverified
	}
	if ed25519.Verify(ed25519.PublicKey(l.HostKey), l.Sealed, l.Sig) {
		return CardSigned
	}
	return CardUnbacked
}
