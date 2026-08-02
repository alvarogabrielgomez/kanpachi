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
}

// New apunta al directorio de datos, normalmente ProgramData\Kanpachi.
//
// No crea el directorio acá: lo crea el instalador con su ACL, que es escritura
// solo para SYSTEM y Administradores. Crearlo desde el daemon lo dejaría con
// los permisos que herede, y esos permisos son la mitad de la protección de
// estos archivos.
func New(dir string) *Store { return &Store{dir: dir} }

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
	return raw, nil
}

// save escribe de forma atómica.
//
// Sin respaldo de la escritura anterior, a diferencia del catálogo, y es
// deliberado: una sala guardada de hace dos arranques no sirve para nada, y
// tenerla en disco sería dejar la identidad de una red vieja donde no hace
// falta. El catálogo sí lo lleva porque ahí lo que se pierde es trabajo del
// usuario.
func (s *Store) save(nombre string, raw []byte) error {
	// 0600 y no 0644: estos archivos llevan la identidad de la red real, o sea
	// que son portadores de acceso a la sala. En Windows manda la ACL del
	// directorio y esto no cambia nada; en Linux, que es donde corre el test y
	// donde va a correr el host headless, sí.
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
