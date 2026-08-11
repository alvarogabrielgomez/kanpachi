// Package jsonfile guarda en disco lo que tiene que sobrevivir a un arranque.
//
// Implementa [port.StateStore] con dos archivos JSON en ProgramData. No conoce
// Windows: son rutas y bytes, así que corre y se prueba en Linux igual que en
// la máquina de verdad.
//
// **Devuelve bytes crudos y no structs**, que es el contrato del puerto. Quien
// decide si una sala guardada es válida es el dominio, con su decodificador
// estricto: un adaptador que lo decidiera movería la política fuera de core, y
// este archivo es entrada hostil aunque escribir en él exija privilegios.
package jsonfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/accentiostudios/kanpachi/daemon/adapter/safewrite"
)

// Los nombres son los de 03-arquitectura.md, y están acá como constantes para
// que el documento y el disco no se puedan separar en silencio.
const (
	RoomFile = "room.json"
	LastFile = "last-room.json"
)

// ErrNoState es que el archivo no está.
//
// **No es un error del programa.** Que falte `room.json` es lo normal en una
// instalación nueva y en TODA salida limpia: la ausencia del archivo es
// justamente la señal de que la última vez se cerró bien.
var ErrNoState = errors.New("no hay estado guardado")

// Store es el almacén.
type Store struct {
	dir string
	// clave sella lo que se escribe. En cero, se escribe en claro.
	clave  [32]byte
	sellar bool
}

// New apunta al directorio de datos, normalmente ProgramData\Kanpachi.
//
// No crea el directorio acá: lo crea el instalador con su ACL, que es escritura
// solo para SYSTEM y Administradores. Crearlo desde el daemon lo dejaría con
// los permisos que herede, y esos permisos son la mitad de la protección de
// estos archivos.
//
// **Escribe en CLARO.** Lo usan los tests y las herramientas que no tienen
// identidad de la que derivar una llave. El producto usa [NewSealed].
func New(dir string) *Store { return &Store{dir: dir} }

// NewSealed es [New] con la llave que cifra lo que queda en disco.
//
// # Qué protege, y contra quién no
//
// Contra los demás usuarios de la máquina, y ese es el caso que lo hizo
// necesario: `ProgramData\Kanpachi` da lectura a todos ellos a propósito, porque
// la interfaz lee de ahí sin elevar. Antes de esto, cualquiera de ellos abría
// `room.json` y leía `NetworkSecret`, que es la identidad de la red REAL, y
// `CardKey`. Ahora leen un blob.
//
// Contra quien puede leer `identity.key`, no, y es correcto: de ahí sale esta
// llave, y quien tiene aquélla puede además firmar como este equipo, que es
// peor. Esa es la única puerta, y es la que sí lleva ACL propia.
//
// Se volvió mucho más necesario cuando la sala pasó a sobrevivir al apagado:
// antes el fichero existía solo entre una muerte sucia y el arranque siguiente,
// y ahora vive tanto como la sala.
func NewSealed(dir string, clave [32]byte) *Store {
	return &Store{dir: dir, clave: clave, sellar: true}
}

func (s *Store) LoadRoom() ([]byte, error) { return s.load(RoomFile) }
func (s *Store) SaveRoom(raw []byte) error { return s.save(RoomFile, raw) }
func (s *Store) ClearRoom() error          { return s.clear(RoomFile) }

func (s *Store) LoadLast() ([]byte, error) { return s.load(LastFile) }
func (s *Store) SaveLast(raw []byte) error { return s.save(LastFile, raw) }
func (s *Store) ClearLast() error          { return s.clear(LastFile) }

func (s *Store) load(nombre string) ([]byte, error) {
	raw, err := os.ReadFile(s.path(nombre))
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("%w: %s", ErrNoState, nombre)
	}
	if err != nil {
		return nil, fmt.Errorf("leyendo %s: %w", nombre, err)
	}
	if !s.sellar {
		return raw, nil
	}
	// Abrir tolera lo escrito en claro por una versión anterior, y ahí está la
	// migración: se lee, y el guardado siguiente ya lo deja sellado. Ver [abrir].
	plano, err := abrir(s.clave, raw)
	if err != nil {
		return nil, fmt.Errorf("abriendo %s: %w", nombre, err)
	}
	return plano, nil
}

// save escribe de forma atómica.
//
// Sin respaldo de la escritura anterior, a diferencia del catálogo, y es
// deliberado: una sala guardada de hace dos arranques no sirve para nada, y
// tenerla en disco sería dejar la identidad de una red vieja donde no hace
// falta. El catálogo sí lo lleva porque ahí lo que se pierde es trabajo del
// usuario.
func (s *Store) save(nombre string, raw []byte) error {
	if s.sellar {
		sellado, err := sellar(s.clave, raw)
		if err != nil {
			return fmt.Errorf("sellando %s: %w", nombre, err)
		}
		raw = sellado
	}
	// 0600 y no 0644: estos archivos llevan la identidad de la red real, o sea
	// que son portadores de acceso a la sala. En Windows manda la ACL del
	// directorio y esto no cambia nada; en Linux, que es donde corre el test y
	// donde va a correr el host headless, sí.
	//
	// El modo se pone IGUAL con el fichero sellado. Las dos defensas son
	// distintas y ninguna sustituye a la otra: el modo impide leerlo, y el sello
	// impide entenderlo si el modo falla, que es exactamente lo que pasa en
	// Windows, donde el modo de Go no gobierna nada.
	return safewrite.File(s.path(nombre), raw, 0o600)
}

// clear borra. Que no esté NO es un error: borrar dos veces es lo que pasa
// cuando se sale de la sala y después se apaga el servicio, y las dos veces la
// intención se cumplió.
func (s *Store) clear(nombre string) error {
	err := os.Remove(s.path(nombre))
	if err == nil || os.IsNotExist(err) {
		return nil
	}
	return fmt.Errorf("borrando %s: %w", nombre, err)
}

func (s *Store) path(nombre string) string { return filepath.Join(s.dir, nombre) }
