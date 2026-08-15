package registry

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// El registro de salas SOBREVIVE a reiniciar el proceso, y hasta el 2026-08-12
// no lo hacía.
//
// # El fallo, verificado de punta a punta en el código
//
// El almacén era un mapa en memoria y la unidad es `Restart=always`, así que
// cualquier reinicio lo vaciaba. A partir de ahí:
//
//  1. El invitado pasa por `checkRoomExists`, que le pregunta al registro y **se
//     cree el "no"**: corta antes de arrancar el motor.
//  2. El host no lo puede reponer. [Store.Publish] exige que la entrada exista y
//     devuelve `ErrNotFound`, y **no debe crear**, porque crear reabriría la
//     carrera que el fijado existe para cerrar.
//  3. Único arreglo desde el producto: renovar el código, que mata los enlaces
//     ya repartidos.
//
// O sea que un `systemctl restart` del registro, o un despliegue, dejaba fuera a
// todo invitado de toda sala abierta. Y la cabecera de `Store` afirmaba lo
// contrario, textual: *"Reiniciar el registro jamás impide entrar, porque entrar
// no pasa por acá"*. Eso fue cierto antes de que existiera `checkRoomExists`.
//
// # Por qué persistir y no relajar la comprobación
//
// Porque la política del producto es fallar rápido, y **fallar rápido exige que
// la autoridad sea confiable**. Un registro que olvida contesta "no existe"
// sobre salas abiertas y alcanzables, y endurecer la política encima de eso
// convierte una molestia en un muro. Persistir es lo que hace que su "no"
// signifique lo que dice.
//
// # Lo que NO cambia
//
// `Publish` sigue sin crear. Lo que se arregla es que la entrada siga estando,
// no que se pueda recrear desde fuera.

// stateFile es cómo se llama el fichero dentro del directorio de estado.
const stateFile = "rooms.json"

// stateVersion viaja dentro para que un formato futuro pueda reconocerse en vez
// de adivinarse. Una versión desconocida se descarta entera, que es lo mismo que
// arrancar sin fichero: el peor caso vuelve a ser el de hoy.
const stateVersion = 1

// snapshot es lo que se escribe. Es [Room] tal cual, sin campos nuevos: lo que
// el registro sabe de una sala ya es deliberadamente poco, y persistirlo no es
// excusa para saber más.
type snapshot struct {
	Version int              `json:"version"`
	Rooms   map[string]*Room `json:"rooms"`
}

// keeper escribe el estado a disco. Nil significa registro volátil, que es lo
// que usan los tests y `kanpseed serve` sin directorio de estado.
//
// El mutex es SUYO y no el del almacén, a propósito. Escribir con el lock del
// almacén tomado dejaría parados `/healthz`, la resolución de invite IDs y la
// página mientras el disco contesta, que es exactamente el fallo que ya obligó a
// sacar la derivación de Argon2id fuera del lock. Ver [redDeEncuentro].
type keeper struct {
	path string
	mu   sync.Mutex
}

// save escribe el estado entero, de forma atómica.
//
// Temporal en el MISMO directorio y luego rename, que es el único patrón que
// deja el fichero o entero o intacto: `rename` dentro de un sistema de ficheros
// es atómico, y escribir sobre el destino directamente deja una ventana en la
// que un corte de luz produce un fichero a medias que al arrancar no parsea.
// Es el mismo patrón de `daemon/adapter/safewrite` y el de `identity.key`.
//
// 0600 porque dentro hay tarjetas cifradas y llaves públicas de hosts. Nada de
// eso es secreto de verdad, y el fichero tampoco tiene por qué ser legible por
// toda la máquina.
func (k *keeper) save(rooms map[string]*Room) error {
	if k == nil {
		return nil
	}
	k.mu.Lock()
	defer k.mu.Unlock()

	datos, err := json.Marshal(snapshot{Version: stateVersion, Rooms: rooms})
	if err != nil {
		return fmt.Errorf("serializando el registro de salas: %w", err)
	}

	dir := filepath.Dir(k.path)
	tmp, err := os.CreateTemp(dir, ".rooms-*.json")
	if err != nil {
		return fmt.Errorf("creando el temporal del registro en %s: %w", dir, err)
	}
	nombre := tmp.Name()
	// La limpieza cubre todo camino de fallo. En el camino bueno el rename ya se
	// llevó el nombre, así que este borrado no encuentra nada y no molesta.
	defer func() { _ = os.Remove(nombre) }()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("ajustando los permisos del temporal: %w", err)
	}
	if _, err := tmp.Write(datos); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("escribiendo el registro de salas: %w", err)
	}
	// Sync antes de cerrar. Sin él, el rename puede quedar visible con el
	// contenido todavía en el caché, y un corte de luz deja un fichero que
	// existe, tiene el tamaño correcto, y está lleno de ceros.
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sincronizando el registro de salas: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("cerrando el temporal del registro: %w", err)
	}
	if err := os.Rename(nombre, k.path); err != nil {
		return fmt.Errorf("reemplazando %s: %w", k.path, err)
	}
	return nil
}

// load lee lo que haya, descartando lo que no se sostenga.
//
// **Se trata como entrada hostil**, y no por paranoia: este proceso corre con
// `DynamicUser=yes` y el fichero vive en un directorio de estado que sobrevive a
// versiones distintas del binario. Una entrada rota no puede tumbar el arranque,
// porque un registro que no arranca es peor que un registro con una sala menos.
//
// Devuelve además cuántas se descartaron, que es lo que se anota en el log.
func (k *keeper) load(ahora time.Time) (map[string]*Room, int, error) {
	if k == nil {
		return map[string]*Room{}, 0, nil
	}
	k.mu.Lock()
	defer k.mu.Unlock()

	datos, err := os.ReadFile(k.path)
	if os.IsNotExist(err) {
		// El primer arranque. No es un fallo y no se avisa como tal.
		return map[string]*Room{}, 0, nil
	}
	if err != nil {
		return nil, 0, fmt.Errorf("leyendo %s: %w", k.path, err)
	}

	var s snapshot
	if err := json.Unmarshal(datos, &s); err != nil {
		return nil, 0, fmt.Errorf("el registro guardado en %s no parsea: %w", k.path, err)
	}
	if s.Version != stateVersion {
		return nil, 0, fmt.Errorf("el registro guardado es de la versión %d y esta entiende la %d",
			s.Version, stateVersion)
	}

	out := make(map[string]*Room, len(s.Rooms))
	descartadas := 0
	for raw, r := range s.Rooms {
		if !entradaSana(raw, r, ahora) {
			descartadas++
			continue
		}
		out[raw] = r
	}
	return out, descartadas, nil
}

// entradaSana comprueba una entrada leída del disco.
//
// El fijado vencido se descarta acá además de en [Store.limpiar], y no es
// redundante: limpiar corre con el lock tomado en las mutaciones, y una sala con
// el fijado muerto no reserva nada, así que cargarla solo para borrarla en la
// primera escritura es trabajo y ruido.
func entradaSana(raw string, r *Room, ahora time.Time) bool {
	switch {
	case r == nil:
		return false
	case len(r.HostKey) != ed25519.PublicKeySize:
		return false
	case len(r.Card) > MaxCardBytes:
		return false
	case r.Rendezvous == "":
		return false
	case r.PinUntil.IsZero():
		return false
	case ahora.After(r.PinUntil):
		return false
	}
	// La clave del mapa tiene que ser un invite ID de verdad. Sin esto, un
	// fichero manipulado mete claves que nada del producto puede volver a
	// nombrar, y que se sirven igual.
	if _, err := domain.ParseInviteID(raw); err != nil {
		return false
	}
	return true
}
