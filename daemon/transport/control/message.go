package control

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/daemon/transport/wire"
)

// MaxMessage es el tope de un mensaje ANTES de deserializar.
//
// Ocho kilobytes, mil veces menos que el del named pipe, y la diferencia es
// deliberada: por acá no pasa ningún catálogo. El mensaje más grande es una
// credencial sellada, que mide unos cientos de bytes.
//
// Pasarse **cierra la conexión**. Ver [wire.ErrTooLarge]: con líneas, lo que no
// cupo deja el flujo desincronizado.
const MaxMessage = 8 << 10

// Kind es el conjunto CERRADO de mensajes del canal.
//
// Cerrado y comprobado contra una tabla, jamás despachado por reflexión. Este
// código corre como SYSTEM y lee de gente que está en la sala: lo que no está en
// la tabla no se interpreta, no se registra su contenido y no se adivina.
type Kind string

const (
	// KindCredentialRequest es lo ÚNICO que se admite por la puerta.
	KindCredentialRequest Kind = "credential_request"
	// KindCredentialResponse es la respuesta del host, sellada.
	KindCredentialResponse Kind = "credential_response"

	// KindAnnounce es cómo se llama la sala y qué juego está activo.
	KindAnnounce Kind = "announce"
	// KindNotice es un aviso del host: expulsión o cierre de sala.
	KindNotice Kind = "notice"
	// KindCode es el invite ID nuevo tras renovarlo, sellado.
	KindCode Kind = "code"
	// KindAck es el acuse de un aviso. Es lo único que un miembro manda por la
	// sala.
	KindAck Kind = "ack"
)

// Las cuatro tablas de admisión. Son cuatro y no una porque hay dos oyentes con
// dos modelos de confianza distintos, y cada uno tiene su dirección.
//
// La puerta acepta desconocidos por definición: quien toca todavía no es
// miembro y viene justamente a pedir permiso para serlo. **Lo que la acota no es
// una lista de direcciones, es que ahí solo se puede pedir una cosa.**
var (
	// admitidoEnLaPuerta es lo que el HOST acepta por la dirección del
	// vestíbulo.
	admitidoEnLaPuerta = map[Kind]bool{KindCredentialRequest: true}
	// admitidoEnLaSala es lo que el HOST acepta por su dirección en la sala.
	// Un anuncio o un aviso que llegue por acá se descarta sin interpretarse:
	// aceptarlos le permitiría a un miembro modificado cambiarle el juego
	// activo a la máquina donde se abren los puertos.
	admitidoEnLaSala = map[Kind]bool{KindAck: true}
	// admitidoDeLaPuerta es lo que el INVITADO acepta mientras está en el
	// vestíbulo.
	admitidoDeLaPuerta = map[Kind]bool{KindCredentialResponse: true}
	// admitidoDelHost es lo que el INVITADO acepta por la conexión de la sala.
	admitidoDelHost = map[Kind]bool{KindAnnounce: true, KindNotice: true, KindCode: true}
)

// envelope es el sobre. Kind primero, contenido opaco hasta saber que el tipo
// está admitido en esta dirección.
type envelope struct {
	Kind    Kind            `json:"kind"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// credentialRequestMsg es el pedido del que está entrando.
//
// PublicKey es la llave EFÍMERA de esa sesión, generada al pedir y descartada al
// salir. No toca el disco, no identifica a nadie entre salas y no habilita
// ningún baneo: su único trabajo es que el host tenga contra qué sellar lo que
// le va a mandar.
type credentialRequestMsg struct {
	Name      string `json:"name"`
	PublicKey []byte `json:"public_key"`
}

// credentialResponseMsg lleva la credencial SELLADA, o el motivo del rechazo.
//
// Sellada y no en claro porque el vestíbulo es público y el motor puede relayar
// tráfico por otro peer: en claro, el intermediario se llevaría el token con el
// que se entra a la sala.
type credentialResponseMsg struct {
	Sealed []byte `json:"sealed,omitempty"`
	Error  string `json:"error,omitempty"`
}

// credentialMsg es lo que va DENTRO del sobre sellado.
//
// Los tipos del dominio se reconstruyen al recibirlos y jamás se creen: las
// direcciones vuelven por netip, el nombre por [domain.ParseNickname]. Que venga
// sellado prueba para quién es, no que lo que dice sea coherente.
type credentialMsg struct {
	ID          string `json:"id"`
	Token       string `json:"token"`
	NetworkName string `json:"network_name"`
	Name        string `json:"name"`
	VirtualIP   string `json:"virtual_ip"`
	Subnet      string `json:"subnet"`
	IssuedAt    string `json:"issued_at"`
	ExpiresAt   string `json:"expires_at"`
}

// announceMsg lleva el id del juego y JAMÁS el perfil. Ver [domain.RoomAnnounce]:
// es la diferencia entre "estamos jugando Zomboid" y "abrí estos puertos".
type announceMsg struct {
	RoomName string `json:"room_name"`
	GameID   string `json:"game_id"`
}

// noticeMsg es un aviso del host, con su número de secuencia para el acuse.
type noticeMsg struct {
	Seq    uint64 `json:"seq"`
	Kind   string `json:"kind"`
	Reason string `json:"reason"`
}

// ackMsg es el acuse. Es lo único que viaja del miembro al host por la sala.
type ackMsg struct {
	Seq uint64 `json:"seq"`
}

// codeMsg lleva el invite ID nuevo, sellado contra la llave del destinatario.
type codeMsg struct {
	Sealed []byte `json:"sealed"`
}

// roomMsg es lo que va DENTRO del sobre sellado del código.
//
// El seed viaja pegado porque un invite ID solo significa algo en el registro
// que lo emitió. Lo que NO viaja, ni acá ni en ningún otro mensaje, es la
// identidad de la red real: el código es un ticket desechable, y ese es todo su
// punto.
type roomMsg struct {
	InviteID string `json:"invite_id"`
	Seed     string `json:"seed"`
}

// noticeKinds es la tabla cerrada de avisos, en las dos direcciones.
//
// Como cadenas y no como el número del iota: agregar un aviso en medio del
// bloque le cambiaría el significado a todos los de abajo en una versión ya
// instalada del otro lado.
var noticeKinds = map[string]domain.NoticeKind{
	"kicked":      domain.NoticeKicked,
	"room_closed": domain.NoticeRoomClosed,
}

func noticeKindName(k domain.NoticeKind) (string, bool) {
	for nombre, valor := range noticeKinds {
		if valor == k {
			return nombre, true
		}
	}
	return "", false
}

// errShape es cualquier mensaje que no se pudo interpretar. Nunca lleva el
// contenido: lo que llegó es entrada hostil y no tiene por qué terminar en un
// log que el usuario copia al portapapeles.
var errShape = errors.New("el mensaje no tiene la forma esperada")

// decodeEnvelope interpreta el sobre, estricto igual que todo lo demás.
func decodeEnvelope(linea []byte) (envelope, error) {
	e, err := wire.DecodeStrict[envelope](linea)
	if err != nil {
		return envelope{}, fmt.Errorf("%w: %v", errShape, err)
	}
	if e.Kind == "" {
		return envelope{}, fmt.Errorf("%w: sin tipo", errShape)
	}
	return e, nil
}

// payloadOf interpreta el contenido de un sobre ya admitido.
func payloadOf[T any](e envelope) (T, error) {
	out, err := wire.DecodeStrict[T](e.Payload)
	if err != nil {
		return out, fmt.Errorf("%w: %s: %v", errShape, e.Kind, err)
	}
	return out, nil
}

// wrap arma un sobre.
func wrap(k Kind, payload any) (envelope, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return envelope{}, fmt.Errorf("serializando %s: %w", k, err)
	}
	return envelope{Kind: k, Payload: raw}, nil
}

// credentialToWire aplana una credencial para el sobre sellado.
func credentialToWire(c domain.Credential) credentialMsg {
	return credentialMsg{
		ID:          string(c.ID),
		Token:       c.Token,
		NetworkName: c.NetworkName,
		Name:        c.Name.String(),
		VirtualIP:   c.VirtualIP.String(),
		Subnet:      c.Subnet.String(),
		IssuedAt:    stamp(c.IssuedAt),
		ExpiresAt:   stamp(c.ExpiresAt),
	}
}

// credentialFromWire reconstruye la credencial que mandó el host.
//
// Cada campo vuelve por su parser, y los que no calzan se rechazan enteros. Lo
// que sigue después de esto lo revisa el caso de uso: que la subred contenga la
// IP, que no sea la del vestíbulo, y el resto de las comprobaciones de coherencia
// que viven en JoinRoom. Acá solo se comprueba la FORMA.
func credentialFromWire(m credentialMsg) (domain.Credential, error) {
	if m.Token == "" || m.ID == "" {
		return domain.Credential{}, fmt.Errorf("%w: credencial sin token o sin id", errShape)
	}
	nick, err := domain.ParseNickname(m.Name)
	if err != nil {
		return domain.Credential{}, fmt.Errorf("%w: el nombre de la credencial: %v", errShape, err)
	}
	ip, err := netip.ParseAddr(m.VirtualIP)
	if err != nil {
		return domain.Credential{}, fmt.Errorf("%w: la dirección de la credencial: %v", errShape, err)
	}
	subnet, err := netip.ParsePrefix(m.Subnet)
	if err != nil {
		return domain.Credential{}, fmt.Errorf("%w: la subred de la credencial: %v", errShape, err)
	}
	emitida, err := unstamp(m.IssuedAt)
	if err != nil {
		return domain.Credential{}, err
	}
	vence, err := unstamp(m.ExpiresAt)
	if err != nil {
		return domain.Credential{}, err
	}
	return domain.Credential{
		ID:          domain.CredentialID(m.ID),
		Token:       m.Token,
		NetworkName: m.NetworkName,
		Name:        nick,
		VirtualIP:   ip,
		Subnet:      subnet,
		IssuedAt:    emitida,
		ExpiresAt:   vence,
	}, nil
}

func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func unstamp(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: la fecha %q", errShape, s)
	}
	return t, nil
}

// jsonBytes serializa lo que va DENTRO de un sobre sellado.
func jsonBytes(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("serializando el contenido sellado: %w", err)
	}
	return raw, nil
}
