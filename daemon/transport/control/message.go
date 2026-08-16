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
	// KindAck es el acuse de un aviso.
	KindAck Kind = "ack"

	// KindCanaryRequest es el host pidiéndole a un miembro que le marque al
	// canario. Ver [domain.CanaryRequest].
	KindCanaryRequest Kind = "canary_request"
	// KindCanaryReport es lo que ese miembro contesta.
	KindCanaryReport Kind = "canary_report"
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
	admitidoEnLaSala = map[Kind]bool{KindAck: true, KindCanaryReport: true}
	// admitidoDeLaPuerta es lo que el INVITADO acepta mientras está en el
	// vestíbulo.
	admitidoDeLaPuerta = map[Kind]bool{KindCredentialResponse: true}
	// admitidoDelHost es lo que el INVITADO acepta por la conexión de la sala.
	admitidoDelHost = map[Kind]bool{
		KindAnnounce: true, KindNotice: true, KindCode: true, KindCanaryRequest: true,
	}
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

	// Rendezvous is the rendezvous network the guest derived from the code that
	// was pasted into it, and it travels so that the guest can check the
	// signature against the same transcript the host signed.
	//
	// **The host does NOT sign this string: it signs its own.** A request naming
	// another room produces a signature that does not validate on the other
	// side, which is exactly what should happen. Empty on an older guest, and
	// then there is nothing to check. See [credentialTranscript].
	RendezvousName string `json:"rendezvous,omitempty"`

	// MemberKey and MemberSig are the guest's PER-ROOM identity and its proof
	// of possession over [memberTranscript]. Unlike PublicKey, this one is
	// stable across returns to the same room: it is what the host binds the
	// credential and the address to, so the one who returns gets THEIRS back.
	//
	// **Without them there is no credential.** The door refuses an unsigned
	// request outright; there are no older guests to keep working.
	MemberKey []byte `json:"member_key"`
	MemberSig []byte `json:"member_sig"`
}

// credentialResponseMsg lleva la credencial SELLADA, o el motivo del rechazo.
//
// Sellada y no en claro porque el vestíbulo es público y el motor puede relayar
// tráfico por otro peer: en claro, el intermediario se llevaría el token con el
// que se entra a la sala.
type credentialResponseMsg struct {
	Sealed []byte `json:"sealed,omitempty"`
	Error  string `json:"error,omitempty"`

	// HostKey and Sig are the host's long-term key and its signature over the
	// transcript of THIS exchange. See [credentialTranscript].
	//
	// **The key arriving here proves nothing on its own**, and that is the part
	// worth saying out loud: a key that arrives in the same message it signs is
	// a seal that signs itself. It is worth something because the guest compares
	// it against the one the REGISTRY pinned for that invite ID, which arrived
	// by another road and earlier. Without that comparison, this is decoration.
	//
	// Empty when the host does not sign yet. The guest treats that as
	// unverified, never as false.
	HostKey []byte `json:"host_key,omitempty"`
	Sig     []byte `json:"sig,omitempty"`
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

// canaryRequestMsg es el host pidiéndole a un miembro que le marque al canario.
//
// # Lo que NO lleva, y es toda la invariante
//
// **No hay campo de dirección.** El invitado marca a la dirección de la conexión
// que ya tiene abierta con el host, y no hay forma de pedirle otra cosa.
//
// Con un campo de dirección acá, este mensaje convertiría el canal de la sala en
// un escáner de puertos por encargo: cualquiera de la sala le pediría a los
// demás que marcaran a una máquina de fuera, y el tráfico saldría de las casas
// de otros. Lo que lo impide no es una comprobación que alguien pueda quitar: es
// que el tipo no lo puede expresar.
type canaryRequestMsg struct {
	Port  uint16 `json:"port"`
	Nonce []byte `json:"nonce"`
}

// canaryReportMsg es lo que ese miembro contesta.
//
// Tampoco lleva quién lo manda, por lo mismo: el host lo saca de la conexión. Si
// viajara en el mensaje, un miembro podría informar en nombre de otro.
//
// Lleva el puerto para que el host descarte una respuesta que llega tarde, de un
// canario que ya cerró. Sin eso, un informe atrasado se leería como el de la
// comprobación en curso.
type canaryReportMsg struct {
	Port uint16 `json:"port"`
	TCP  string `json:"tcp"`
	UDP  string `json:"udp"`
}

// probeOutcomes es la tabla cerrada de resultados, en las dos direcciones.
//
// Como cadenas y no como el número del iota, por lo mismo que [noticeKinds]:
// agregar un resultado en medio del bloque le cambiaría el significado a todos
// los de abajo en una versión ya instalada del otro lado.
var probeOutcomes = map[string]domain.ProbeOutcome{
	"answered": domain.ProbeAnswered,
	"refused":  domain.ProbeRefused,
	"silent":   domain.ProbeSilent,
	"failed":   domain.ProbeFailed,
}

func probeOutcomeName(o domain.ProbeOutcome) (string, bool) {
	for nombre, valor := range probeOutcomes {
		if valor == o {
			return nombre, true
		}
	}
	return "", false
}

// probeOutcomeFrom interpreta lo que llegó, y lo desconocido cae en
// [domain.ProbeFailed].
//
// En "no se pudo preguntar" y jamás en "silencio", que es el lado seguro: un
// resultado que no se sabe leer no puede contarse como que el puerto está
// cerrado. Lo desconocido tiene que restar confianza, no darla.
func probeOutcomeFrom(s string) domain.ProbeOutcome {
	if o, ok := probeOutcomes[s]; ok {
		return o
	}
	return domain.ProbeFailed
}

// canaryRequestFromWire reconstruye el pedido PONIENDO la dirección desde la
// conexión, que es el único sitio de donde puede salir.
func canaryRequestFromWire(host netip.Addr, m canaryRequestMsg) (domain.CanaryRequest, error) {
	if len(m.Nonce) != domain.CanaryNonceSize {
		return domain.CanaryRequest{}, fmt.Errorf("%w: el número del canario mide %d y "+
			"tiene que medir %d", errShape, len(m.Nonce), domain.CanaryNonceSize)
	}
	req := domain.CanaryRequest{Host: host, Port: m.Port}
	copy(req.Nonce[:], m.Nonce)

	// El dominio recomprueba lo suyo. Los tipos del dominio se reconstruyen por
	// sus validadores y jamás se creen, que es la regla 5 de este paquete.
	if err := req.Valid(); err != nil {
		return domain.CanaryRequest{}, fmt.Errorf("%w: %v", errShape, err)
	}
	return req, nil
}

// canaryReportFromWire reconstruye el informe, con el remitente sacado de la
// conexión por el mismo motivo.
func canaryReportFromWire(from netip.Addr, m canaryReportMsg) domain.CanaryReport {
	return domain.CanaryReport{
		From: from,
		Port: m.Port,
		TCP:  probeOutcomeFrom(m.TCP),
		UDP:  probeOutcomeFrom(m.UDP),
	}
}

// noticeKinds es la tabla cerrada de avisos, en las dos direcciones.
//
// Como cadenas y no como el número del iota: agregar un aviso en medio del
// bloque le cambiaría el significado a todos los de abajo en una versión ya
// instalada del otro lado.
var noticeKinds = map[string]domain.NoticeKind{
	"kicked":      domain.NoticeKicked,
	"room_closed": domain.NoticeRoomClosed,
	"stale":       domain.NoticeStale,
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

// credentialTranscript builds what the host signs and the guest verifies.
//
// # What is inside, and why each piece is there
//
//		"kanpachi-cred-v1" 0x00 <rendezvous network> 0x00 <ephemeral key> 0x00 <envelope>
//
//	  - **The prefix** separates domains: a signature from this exchange cannot
//	    pass as the signature of a room card, which is the other thing the same
//	    key signs.
//	  - **The rendezvous network** binds the answer to THIS room. It is the only
//	    thing in here the guest knows before receiving anything, because it
//	    derived it from the pasted code, and it is what stops a good answer from
//	    another room being reused: the ephemeral key lasts the whole session, so
//	    without this field an answer recorded in room A would verify just fine
//	    when entering room B.
//	  - **The ephemeral key** binds the answer to THIS request.
//	  - **The envelope** is what is being authenticated.
//
// The separators are 0x00 and they are enough: the network name is hexadecimal
// with a fixed prefix, the key is always 32 bytes, and the envelope goes last.
// There is no second way to split the same bytes.
//
// The version lives INSIDE the prefix on purpose. The day the transcript
// changes, an old signature stops validating against the new one, which is the
// right outcome: it falls to unverified instead of to verified by accident.
func credentialTranscript(rendezvous string, guestKey, sealed []byte) []byte {
	out := make([]byte, 0, len(rendezvous)+len(guestKey)+len(sealed)+32)
	out = append(out, "kanpachi-cred-v1"...)
	out = append(out, 0)
	out = append(out, rendezvous...)
	out = append(out, 0)
	out = append(out, guestKey...)
	out = append(out, 0)
	return append(out, sealed...)
}

// memberTranscript builds what the GUEST signs in its request and the host
// verifies at the door. The other direction of [credentialTranscript], with
// its own prefix so neither signature can pass as the other.
//
// # What is inside, and what each piece prevents
//
//	"kanpachi-member-v1" 0x00 <rendezvous> 0x00 <ephemeral key> 0x00 <member key> 0x00 <name>
//
//   - **The rendezvous** binds the request to THIS room: a signed request
//     recorded in room A does not verify when replayed against room B.
//   - **The ephemeral key** binds it to THIS connection, and it is the piece
//     that matters most: without it, anybody in the lobby could replay a
//     member's signed request with their OWN ephemeral key in it, and the
//     host would hand them that member's credential — same secret, same
//     address — sealed against the thief's key.
//   - **The member key** is what is being claimed.
//   - **The name** rides along so a relay cannot swap the nickname the host
//     writes into its book.
//
// The separators are 0x00 and they are enough: every field before the name is
// fixed-length or 0x00-free, and the name goes last.
func memberTranscript(rendezvous string, ephemeralKey, memberKey []byte, name string) []byte {
	out := make([]byte, 0, len(rendezvous)+len(ephemeralKey)+len(memberKey)+len(name)+32)
	out = append(out, "kanpachi-member-v1"...)
	out = append(out, 0)
	out = append(out, rendezvous...)
	out = append(out, 0)
	out = append(out, ephemeralKey...)
	out = append(out, 0)
	out = append(out, memberKey...)
	out = append(out, 0)
	return append(out, name...)
}
