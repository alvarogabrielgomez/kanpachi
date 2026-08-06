package domain

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"time"
)

// ErrPersistedShape es que el archivo de estado no se puede interpretar.
//
// Un solo centinela para todo el archivo, y a propósito: no hay recuperación
// parcial. Un campo que no se entiende invalida el archivo entero, por lo mismo
// que un perfil inválido se rechaza completo, porque cargar la mitad de una
// identidad de sala produciría una sala que no es la que se guardó.
var ErrPersistedShape = errors.New("el estado guardado no se puede interpretar")

// PersistedRoom es la sala del HOST, guardada para sobrevivir a un apagón.
//
// # Lo que lleva es IDENTIDAD y REFERENCIAS, jamás política
//
// Esa frase es la invariante de este archivo y hay que leerla antes de agregarle
// un campo. El archivo vive en ProgramData, cuya ACL da lectura a los usuarios
// de la máquina, y se trata como entrada hostil aunque escribir exija
// privilegios.
//
// Lo que NO se puede expresar acá: un puerto, un rango, una regla de firewall,
// un ejecutable, un plazo, una lista de miembros. Si no se puede escribir, un
// archivo manipulado no abre nada ni compra tiempo de más en ninguna sala.
//
// GameID es una REFERENCIA y por eso sí entra. Es exactamente lo mismo que
// viaja en [RoomAnnounce], donde la regla es que lleva el id del juego y jamás
// el perfil: al reanudar se resuelve contra el catálogo local, con sus
// invariantes y sus puertos prohibidos. Lo peor que logra un archivo manipulado
// es nombrar otro juego que ya está en el catálogo, o sea algo que el usuario
// podía elegir con dos clics.
//
// # Por qué el archivo es la señal de mal cierre
//
// Salir limpio lo borra y morir sucio lo deja. No hay bandera `dirty`, porque
// una bandera es un campo más que alguien puede escribir a mano y este hecho no
// se puede falsificar desde dentro del archivo.
type PersistedRoom struct {
	// Room es el invite ID vigente con su seed. Sobrevive porque la sala es un
	// objeto durable que el host posee, y honrar el mismo código tras un
	// reinicio es media decisión 2.
	Room Room
	Name string
	Host Nickname

	// NetworkID y NetworkSecret son la identidad de la red REAL. Sin ellos, al
	// reabrir la sala sería otra red y todas las credenciales emitidas dejarían
	// de valer, o sea nadie reconecta solo.
	NetworkID     [16]byte
	NetworkSecret [32]byte

	Subnet  netip.Prefix
	CardKey [CardKeyLen]byte

	// Card es la tarjeta SELLADA que el registro aceptó, tal cual se subió.
	//
	// Existe para poder republicarla al reabrir sin volver a sellarla: el
	// registro guarda las tarjetas en memoria y con vencimiento, así que un
	// daemon que vuelve encuentra su sala en pie y su tarjeta muerta. Subir los
	// MISMOS bytes conserva los enlaces que ya se repartieron, porque la clave
	// que los abre es [PersistedRoom.CardKey] y sigue siendo esta.
	//
	// **Vacío es válido y significa que no hay nada que republicar.** Es el caso
	// de un archivo escrito antes de que este campo existiera, y también el del
	// respaldo de crear: cuando el registro no contestó, el invite ID se generó
	// acá y él no lo emitió nunca, así que no hay tarjeta suya que restaurar.
	//
	// Es presentación cifrada, o sea bytes opacos, y jamás política: lo peor que
	// consigue un archivo manipulado es publicar basura firmada por la propia
	// llave de este equipo, que es exactamente lo que este equipo ya puede hacer.
	Card []byte

	// GameID es el juego que estaba activo. Vacío es válido y es lo normal.
	GameID string

	SavedAt time.Time
}

// String redacta la identidad de la red y la clave de la tarjeta, por el mismo
// motivo que [Rendezvous.String] y [HostSpec.String].
//
// Este tipo se lee al arrancar y se guarda en cuatro operaciones, así que aparece
// en mensajes de error de esos caminos. Un `%+v` sin esto imprime el secreto de
// la red REAL en los logs locales, que se copian al portapapeles con el botón de
// diagnóstico y se pegan en el grupo. Lo que va al log es identidad de sala, no
// portadores de acceso a ella.
func (p PersistedRoom) String() string {
	return fmt.Sprintf("PersistedRoom{%s, %q, host:%s, %s, juego:%q, secretos:REDACTADOS}",
		p.Room.InviteID, p.Name, p.Host, p.Subnet, p.GameID)
}

// LastRoom es la última sala de un INVITADO, para poder volver.
//
// # Lo que deliberadamente NO lleva
//
// Ni la credencial, ni el nombre de la red real, ni nada que sirva para entrar
// sin pasar por el host. Volver hace el mismo [ParseRoom] y el mismo canje que
// la primera vez: el host reemite y ve llegar a quien llega, y eso es lo que
// mantiene con sentido a la revocación. Un archivo con credencial dentro sería
// una llave de sala tirada en disco que sobrevive a la sesión.
type LastRoom struct {
	Room    Room
	Name    string
	Nick    Nickname
	SavedAt time.Time
}

// persistedRoomJSON es el esquema en disco. Cerrado, y cada campo con un tipo
// que no admite sorpresas.
type persistedRoomJSON struct {
	InviteID      string `json:"invite_id"`
	Seed          string `json:"seed"`
	Name          string `json:"name"`
	Host          string `json:"host"`
	NetworkID     string `json:"network_id"`
	NetworkSecret string `json:"network_secret"`
	Subnet        string `json:"subnet"`
	CardKey       string `json:"card_key"`
	Card          string `json:"card"`
	GameID        string `json:"game_id"`
	SavedAt       string `json:"saved_at"`
}

type lastRoomJSON struct {
	InviteID string `json:"invite_id"`
	Seed     string `json:"seed"`
	Name     string `json:"name"`
	Nick     string `json:"nick"`
	SavedAt  string `json:"saved_at"`
}

// Encode serializa la sala del host.
func (p PersistedRoom) Encode() ([]byte, error) {
	if p.Room.InviteID.IsZero() {
		return nil, fmt.Errorf("%w: no hay invite ID que guardar", ErrPersistedShape)
	}
	return json.MarshalIndent(persistedRoomJSON{
		InviteID:      p.Room.InviteID.String(),
		Seed:          p.Room.Seed,
		Name:          p.Name,
		Host:          p.Host.String(),
		NetworkID:     base64.StdEncoding.EncodeToString(p.NetworkID[:]),
		NetworkSecret: base64.StdEncoding.EncodeToString(p.NetworkSecret[:]),
		Subnet:        p.Subnet.String(),
		CardKey:       base64.StdEncoding.EncodeToString(p.CardKey[:]),
		Card:          base64.StdEncoding.EncodeToString(p.Card),
		GameID:        p.GameID,
		SavedAt:       p.SavedAt.UTC().Format(time.RFC3339),
	}, "", "  ")
}

// DecodePersistedRoom es el ÚNICO decodificador de la sala del host.
//
// Estricto por las dos vías, igual que el del catálogo: campos desconocidos
// rechazan el archivo, y contenido después del objeto también. Esa segunda
// comprobación existe porque `json.Decode` se detiene feliz en el primer valor
// completo, así que sin ella un archivo con un segundo objeto pegado detrás
// cargaría el primero y nadie vería el resto.
func DecodePersistedRoom(raw []byte) (PersistedRoom, error) {
	j, err := decodeStrict[persistedRoomJSON](raw)
	if err != nil {
		return PersistedRoom{}, err
	}

	room, err := roomFrom(j.InviteID, j.Seed)
	if err != nil {
		return PersistedRoom{}, err
	}
	host, err := ParseNickname(j.Host)
	if err != nil {
		return PersistedRoom{}, fmt.Errorf("%w: el nick del host: %v", ErrPersistedShape, err)
	}

	out := PersistedRoom{Room: room, Name: ClampRoomName(j.Name), Host: host, GameID: j.GameID}

	if err := fixedFromBase64(j.NetworkID, out.NetworkID[:], "network_id"); err != nil {
		return PersistedRoom{}, err
	}
	if err := fixedFromBase64(j.NetworkSecret, out.NetworkSecret[:], "network_secret"); err != nil {
		return PersistedRoom{}, err
	}
	if err := fixedFromBase64(j.CardKey, out.CardKey[:], "card_key"); err != nil {
		return PersistedRoom{}, err
	}

	// La tarjeta es de largo variable y OPCIONAL: un archivo escrito antes de
	// que este campo existiera carga igual, y sin ella lo único que se pierde es
	// la republicación al reabrir. El tope es el mismo que aplica el registro del
	// otro lado, así que un archivo inflado a mano se rechaza acá y no allá.
	if j.Card != "" {
		card, err := base64.StdEncoding.DecodeString(j.Card)
		if err != nil {
			return PersistedRoom{}, fmt.Errorf("%w: la tarjeta: %v", ErrPersistedShape, err)
		}
		if len(card) > MaxCardBytes {
			return PersistedRoom{}, fmt.Errorf("%w: la tarjeta mide %d bytes y el tope es %d",
				ErrPersistedShape, len(card), MaxCardBytes)
		}
		out.Card = card
	}

	// La subred se comprueba contra las mismas reglas que la eligen. Una sala
	// restaurada en el /24 del vestíbulo, o fuera del espacio previsto, es la
	// forma de que un archivo editado a mano mande el tráfico a otra parte.
	if out.Subnet, err = netip.ParsePrefix(j.Subnet); err != nil {
		return PersistedRoom{}, fmt.Errorf("%w: la subred: %v", ErrPersistedShape, err)
	}
	if err := checkRoomSubnet(out.Subnet); err != nil {
		return PersistedRoom{}, err
	}

	// Un id que no cumple las reglas del catálogo no puede casar con ningún
	// perfil, así que se rechaza acá en vez de dejar que falle silenciosamente
	// en el `Find` y la sala reabra sin juego sin decir por qué.
	if out.GameID != "" && !validProfileID(out.GameID) {
		return PersistedRoom{}, fmt.Errorf("%w: %v: %q", ErrPersistedShape, ErrProfileID, out.GameID)
	}

	if out.SavedAt, err = parseSavedAt(j.SavedAt); err != nil {
		return PersistedRoom{}, err
	}
	return out, nil
}

// Encode serializa la última sala de un invitado.
func (l LastRoom) Encode() ([]byte, error) {
	if l.Room.InviteID.IsZero() {
		return nil, fmt.Errorf("%w: no hay invite ID que guardar", ErrPersistedShape)
	}
	return json.MarshalIndent(lastRoomJSON{
		InviteID: l.Room.InviteID.String(),
		Seed:     l.Room.Seed,
		Name:     l.Name,
		Nick:     l.Nick.String(),
		SavedAt:  l.SavedAt.UTC().Format(time.RFC3339),
	}, "", "  ")
}

// DecodeLastRoom es el único decodificador de la última sala del invitado.
func DecodeLastRoom(raw []byte) (LastRoom, error) {
	j, err := decodeStrict[lastRoomJSON](raw)
	if err != nil {
		return LastRoom{}, err
	}
	room, err := roomFrom(j.InviteID, j.Seed)
	if err != nil {
		return LastRoom{}, err
	}
	nick, err := ParseNickname(j.Nick)
	if err != nil {
		return LastRoom{}, fmt.Errorf("%w: el nick: %v", ErrPersistedShape, err)
	}
	saved, err := parseSavedAt(j.SavedAt)
	if err != nil {
		return LastRoom{}, err
	}
	return LastRoom{Room: room, Name: ClampRoomName(j.Name), Nick: nick, SavedAt: saved}, nil
}

// decodeStrict es el rechazo de campos desconocidos, compartido por los dos
// esquemas. Uno solo por el mismo motivo que en el catálogo: dos copias de esta
// función es cómo una de las dos deja de correr sin que nadie se entere.
func decodeStrict[T any](raw []byte) (T, error) {
	var out T
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return out, fmt.Errorf("%w: %v", ErrPersistedShape, err)
	}
	if dec.More() {
		return out, fmt.Errorf("%w: hay contenido después del objeto", ErrPersistedShape)
	}
	return out, nil
}

// roomFrom rearma la sala pasando por [ParseRoom].
//
// Por ParseRoom y no por un constructor propio: es la frontera de entrada
// hostil que ya existe, con su tope de longitud y su alfabeto, y un archivo de
// disco merece exactamente la misma desconfianza que un enlace de Telegram.
func roomFrom(id, seed string) (Room, error) {
	input := id
	if seed != "" {
		input = id + "@" + seed
	}
	room, err := ParseRoom(input)
	if err != nil {
		return Room{}, fmt.Errorf("%w: el código guardado: %v", ErrPersistedShape, err)
	}
	return room, nil
}

// fixedFromBase64 decodifica un valor de longitud FIJA.
//
// La longitud se comprueba contra el destino y no contra una constante suelta,
// así agregar un campo de otro tamaño no puede heredar la comprobación del
// anterior.
func fixedFromBase64(s string, dst []byte, field string) error {
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return fmt.Errorf("%w: %s no es base64: %v", ErrPersistedShape, field, err)
	}
	if len(raw) != len(dst) {
		return fmt.Errorf("%w: %s mide %d bytes y tiene que medir %d",
			ErrPersistedShape, field, len(raw), len(dst))
	}
	copy(dst, raw)
	return nil
}

// checkRoomSubnet aplica a una subred restaurada las mismas reglas que rigen
// para una recién elegida.
func checkRoomSubnet(p netip.Prefix) error {
	switch {
	case !p.IsValid() || !p.Addr().Is4():
		return fmt.Errorf("%w: la subred guardada no es IPv4", ErrPersistedShape)
	case p.Bits() != RoomPrefixBits:
		return fmt.Errorf("%w: la subred guardada no es un /%d", ErrPersistedShape, RoomPrefixBits)
	case p == RendezvousSubnet:
		return fmt.Errorf("%w: la subred guardada es el rango reservado del vestíbulo", ErrPersistedShape)
	case !SharedSpace.Overlaps(p) && !FallbackSpace.Overlaps(p):
		return fmt.Errorf("%w: la subred guardada, %s, está fuera de los rangos que Kanpachi usa", ErrPersistedShape, p)
	}
	return nil
}

// parseSavedAt lee la fecha. Vacía es válida: describe cuándo se guardó y
// ninguna decisión cuelga de ella, así que un archivo viejo sin fecha se carga
// igual en vez de perderse una sala por un campo de presentación.
func parseSavedAt(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: la fecha: %v", ErrPersistedShape, err)
	}
	return t, nil
}
