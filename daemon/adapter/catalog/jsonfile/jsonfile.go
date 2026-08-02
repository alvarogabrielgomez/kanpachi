// Package jsonfile es el catálogo en disco.
//
// Implementa [port.CatalogStore] con dos archivos: el que vino en el instalador
// y el del usuario. No conoce Windows.
//
// **Devuelve bytes crudos y no perfiles.** Quien valida es el dominio, con sus
// nueve invariantes de carga: un adaptador que decidiera qué perfil es válido
// movería la política fuera de core, y esa política es la que impide que un
// perfil compartido por Telegram abra el 445.
package jsonfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/accentiostudios/kanpachi/core/port"
	"github.com/accentiostudios/kanpachi/daemon/adapter/safewrite"
)

const (
	// BuiltinFile vive en Program Files y es de SOLO LECTURA para el daemon.
	// Se actualiza con la app, y cada versión trae las correcciones de perfiles
	// que hizo falta.
	BuiltinFile = "builtin.json"
	// LocalFile vive en ProgramData: lo propio y lo importado.
	LocalFile = "local.json"
	// backupSuffix es la copia de la escritura anterior de LocalFile.
	backupSuffix = ".bak"
)

// ErrNoCatalog es que el archivo no está. Un catálogo local que falta es una
// instalación nueva, no un fallo.
var ErrNoCatalog = errors.New("no hay catálogo en disco")

// Store lee de dos sitios y escribe en uno.
//
// Dos rutas y no una porque los dos archivos tienen dueños distintos: el
// builtin es del instalador y el daemon no lo toca jamás, el local es del
// usuario. Mezclarlos haría que guardar un perfil propio pudiera pisar el que
// vino con la app, y eso es exactamente lo que la precedencia de capas del
// catálogo existe para no hacer.
type Store struct {
	builtinDir string
	localDir   string
	log        port.Logger
}

// New apunta a los dos directorios: el de los perfiles que vinieron con la app
// y el de datos del usuario.
func New(builtinDir, localDir string, log port.Logger) *Store {
	return &Store{builtinDir: builtinDir, localDir: localDir, log: log}
}

func (s *Store) LoadBuiltin() ([]byte, error) {
	return read(filepath.Join(s.builtinDir, BuiltinFile))
}

func (s *Store) LoadLocal() ([]byte, error) {
	return read(filepath.Join(s.localDir, LocalFile))
}

// SaveLocal escribe el catálogo del usuario, con respaldo de la escritura
// anterior.
//
// El respaldo existe acá y no en el estado de sala porque lo que se puede
// perder es distinto: una sala guardada de hace dos arranques no sirve para
// nada, y un catálogo son horas de alguien creando perfiles a mano. Es UNA
// copia, la anterior, y su único trabajo es que un archivo corrupto tenga de
// dónde volver a mano.
func (s *Store) SaveLocal(raw []byte) error {
	ruta := filepath.Join(s.localDir, LocalFile)
	if err := safewrite.Backup(ruta, backupSuffix); err != nil {
		// No corta la escritura, y queda en el log. Negarse a guardar porque no
		// se pudo copiar el archivo viejo le haría perder al usuario el perfil
		// que acaba de escribir, que es un daño seguro a cambio de evitar uno
		// hipotético.
		s.log.Warn("no se pudo respaldar el catálogo anterior", "error", err)
	}
	return safewrite.File(ruta, raw, 0o644)
}

func read(ruta string) ([]byte, error) {
	raw, err := os.ReadFile(ruta)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("%w: %s", ErrNoCatalog, filepath.Base(ruta))
	}
	if err != nil {
		return nil, fmt.Errorf("leyendo %s: %w", filepath.Base(ruta), err)
	}
	return raw, nil
}
