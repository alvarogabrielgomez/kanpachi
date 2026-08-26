package domain

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"time"
)

// ErrBookRolledBack es que el libro de credenciales que hay en disco es más
// viejo que el que esta máquina escribió por última vez.
var ErrBookRolledBack = errors.New("el libro de credenciales guardado es anterior al último que se escribió")

// ErrBookOtherRoom es que el libro de credenciales es de otra sala.
var ErrBookOtherRoom = errors.New("el libro de credenciales guardado es de otra sala")

// bookVersion es el sobre. Un número que no se reconoce invalida el fichero
// entero, por lo mismo que en [HostedRoom]: no hay recuperación parcial.
const bookVersion = 1

// CredentialBook es el libro de credenciales del host, en disco.
//
// # Qué compra, dicho sin inflar
//
// **No devuelve a nadie a la sala.** El motor es un proceso hijo que muere con
// el daemon, y su lista de credenciales muere con él: renovar un id anterior al
// reinicio es error duro por contrato. Compra otras dos cosas, las dos reales.
//
// La primera es la DIRECCIÓN ESTABLE. Quien vuelve tras un reinicio del host
// recupera la suya en vez de recibir otra, porque su llave de miembro sigue
// encontrando su entrada.
//
// La segunda es PODER EXPULSAR a quien ya estaba dentro. Sin libro, el host que
// reinicia pierde el lazo dirección-credencial y expulsar contesta que esa
// dirección no es de ningún miembro. Estaba escrito como precio asumido en
// [usecase.Session.credentialFor], y esto es lo que lo paga.
//
// La tercera consecuencia es de diseño y no de función: con el libro
// sobreviviendo al reinicio, la compuerta puede dejar de mirar también la tabla
// del motor. Ver la decisión 43.
//
// # Qué se guarda, y qué NO
//
// El token no. Es el único secreto de la ficha, y además es inútil tras
// reiniciar por lo de arriba. Lo prohíbe un test.
//
// La llave de miembro sí, y hay que decir lo que eso crea: es pública, pero es
// estable y enlazable, así que guardarla deja en disco un registro durable de
// quién jugó en esta sala. El lado invitado ya se niega a conservar su mitad
// tras una salida deliberada, con ese mismo argumento escrito. Acá se acepta
// porque el libro vive en el directorio de datos del host, con su ACL, y muere
// con la sala: cerrarla lo borra.
//
// # Qué le compra a un atacante una copia manipulada
//
// Menos de lo que parece, y por eso este párrafo existe. El fichero va sellado
// con [KPSEAL1], que es AES-256-GCM: cifra y autentica, y la llave sale de la
// identidad de esta máquina. Quien pueda forjarlo ya puede leer `identity.key`,
// y con esa llave tiene la sala entera sin necesidad de tocar el libro.
//
// Lo que el sello NO cubre es la REVERSIÓN, porque una copia sellada más vieja
// de la misma máquina autentica perfecto. Restaurar un libro anterior a una
// expulsión le devolvería al expulsado su dirección y su ranura pre-autorizada
// en el oyente que corre como SYSTEM. Contra eso está [CredentialBook.Gen], un
// contador que sube en cada escritura y que se compara contra el que guarda
// `hosted-room.json`. Quien pueda escribir los DOS ficheros ya tiene la
// identidad de red de la sala en el segundo, así que ahí no queda nada que
// defender.
type CredentialBook struct {
	// Gen sube en cada escritura y jamás baja. Ver el párrafo de arriba.
	Gen uint64
	// Room ata el libro a la sala que lo escribió. Sus direcciones son válidas,
	// así que cargar el de otra sala abriría el canal de control a las IP de
	// una sala ajena, que es el mismo fallo que evita vaciar la tabla al salir.
	Room Room
	// Entries son las fichas, sin token.
	Entries []BookEntry
}

// BookEntry es una ficha en disco.
type BookEntry struct {
	VirtualIP netip.Addr
	ID        CredentialID
	Name      Nickname
	MemberKey []byte
	IssuedAt  time.Time
	ExpiresAt time.Time
	Revoked   bool
}

// bookOnDisk es la forma del JSON, aparte del tipo del dominio por lo mismo que
// en [HostedRoom]: los tipos del dominio no cargan con etiquetas de
// serialización, y el fichero se trata como entrada hostil.
type bookOnDisk struct {
	Version int           `json:"version"`
	Gen     uint64        `json:"gen"`
	Code    string        `json:"code"`
	Seed    string        `json:"seed"`
	Entries []entryOnDisk `json:"entries"`
}

type entryOnDisk struct {
	IP        string `json:"ip"`
	ID        string `json:"id"`
	Name      string `json:"name"`
	MemberKey string `json:"member_key"`
	IssuedAt  int64  `json:"issued_at"`
	ExpiresAt int64  `json:"expires_at"`
	Revoked   bool   `json:"revoked,omitempty"`
}

// Encode serializa el libro para guardarlo.
func (b CredentialBook) Encode() ([]byte, error) {
	out := bookOnDisk{
		Version: bookVersion,
		Gen:     b.Gen,
		Code:    b.Room.InviteID.String(),
		Seed:    b.Room.Seed,
		Entries: make([]entryOnDisk, 0, len(b.Entries)),
	}
	for _, e := range b.Entries {
		if !e.VirtualIP.IsValid() {
			continue
		}
		out.Entries = append(out.Entries, entryOnDisk{
			IP:        e.VirtualIP.String(),
			ID:        string(e.ID),
			Name:      e.Name.String(),
			MemberKey: base64.StdEncoding.EncodeToString(e.MemberKey),
			IssuedAt:  e.IssuedAt.Unix(),
			ExpiresAt: e.ExpiresAt.Unix(),
			Revoked:   e.Revoked,
		})
	}
	return json.Marshal(out)
}

// DecodeCredentialBook lee un libro y lo PODA.
//
// # Los tres filtros, y qué cierra cada uno
//
//   - **La generación**, contra la última que esta máquina escribió. Es lo único
//     que detecta una copia restaurada. Ver el bloque de [CredentialBook].
//   - **La sala**, contra la que se está reabriendo. Las direcciones de otra
//     sala son válidas y no son de nadie de esta.
//   - **La poda por entrada**: vencida no autoriza, y sin llave de miembro
//     tampoco, porque esa llave es lo único que le devuelve a alguien su ficha.
//     El precedente está medido: setenta y tres direcciones quemadas en cuatro
//     horas hasta agotar el /24. Un libro que solo crece deja una sala sin
//     direcciones libres teniendo el rango vacío.
//
// Una entrada REVOCADA sí se conserva mientras no venza. Es lo que retiene la
// dirección de un expulsado durante la ventana en que el motor todavía lo
// recuerda, y perderla al reiniciar es justo el hueco de la tarea.
func DecodeCredentialBook(raw []byte, knownGen uint64, room Room, now time.Time) (CredentialBook, error) {
	var d bookOnDisk
	if err := json.Unmarshal(raw, &d); err != nil {
		return CredentialBook{}, fmt.Errorf("%w: %v", ErrPersistedShape, err)
	}
	if d.Version != bookVersion {
		return CredentialBook{}, fmt.Errorf("%w: versión %d del libro de credenciales", ErrPersistedShape, d.Version)
	}
	if d.Gen < knownGen {
		return CredentialBook{}, fmt.Errorf("%w: generación %d contra %d", ErrBookRolledBack, d.Gen, knownGen)
	}
	if d.Code != room.InviteID.String() || d.Seed != room.Seed {
		return CredentialBook{}, fmt.Errorf("%w: %s@%s", ErrBookOtherRoom, d.Code, d.Seed)
	}

	out := CredentialBook{Gen: d.Gen, Room: room}
	for _, e := range d.Entries {
		ip, err := netip.ParseAddr(e.IP)
		if err != nil || !ip.IsValid() {
			continue
		}
		if !room.InviteID.IsZero() && !ip.Is4() {
			continue
		}
		vence := time.Unix(e.ExpiresAt, 0).UTC()
		if !vence.After(now) {
			continue
		}
		llave, err := base64.StdEncoding.DecodeString(e.MemberKey)
		if err != nil || len(llave) == 0 {
			continue
		}
		nombre, err := ParseNickname(e.Name)
		if err != nil {
			// El apodo lo eligió quien pidió la credencial y viene de otra
			// máquina, así que se trata como entrada hostil igual que en la
			// tabla del motor: se suelta el nombre, no la entrada. Sin nombre,
			// expulsar sigue funcionando por dirección.
			nombre = Nickname{}
		}
		out.Entries = append(out.Entries, BookEntry{
			VirtualIP: ip,
			ID:        CredentialID(e.ID),
			Name:      nombre,
			MemberKey: llave,
			IssuedAt:  time.Unix(e.IssuedAt, 0).UTC(),
			ExpiresAt: vence,
			Revoked:   e.Revoked,
		})
	}
	return out, nil
}
