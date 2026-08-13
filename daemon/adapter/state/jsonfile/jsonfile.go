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
	HostedRoomFile = "hosted-room.json"
	LastRoomFile   = "last-room.json"

	// SeedFile es el registro en el que ESTA máquina abre sus salas.
	//
	// Ver [Store.LoadSeed] para lo único que lo hace distinto de los otros dos,
	// que es no ir sellado.
	SeedFile = "seed.txt"

	// SeedTokenFile is the refresh token for a seed that asks for a password.
	// It is the only file here that gets BOTH the seal and an ACL of its own;
	// see [Store.SaveSeedToken].
	SeedTokenFile = "seed-token.json"
)

// ErrNoState es que el archivo no está.
//
// **No es un error del programa.** Que falte `hosted-room.json` es lo normal
// en una instalación nueva y en TODA salida limpia: la ausencia del archivo es
// justamente la señal de que la última vez se cerró bien.
var ErrNoState = errors.New("no hay estado guardado")

// Store es el almacén.
type Store struct {
	dir string
	// clave sella lo que se escribe. En cero, se escribe en claro.
	clave  [32]byte
	sellar bool
	// protect le pone a un fichero su propia ACL, y solo lo usa el token del
	// seed. Es un `func(string) error` y no algo de Windows: este paquete sigue
	// sin conocer ningún sistema operativo. Nil no protege, que es lo que usan
	// los tests y las herramientas. Ver [Store.SaveSeedToken].
	protect func(path string) error
}

// Protect le dice al almacén cómo restringir un fichero.
//
// Se inyecta después de construir en vez de en [New] o [NewSealed] porque hay un
// solo fichero que lo necesita, y obligar a los otros dos constructores a
// pedirlo haría que todos los llamadores tuvieran que contestar algo sobre una
// defensa que no les toca.
func (s *Store) Protect(f func(path string) error) *Store {
	s.protect = f
	return s
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
// `hosted-room.json` y leía `NetworkSecret`, que es la identidad de la red REAL, y
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

func (s *Store) LoadRoom() ([]byte, error) { return s.load(HostedRoomFile) }
func (s *Store) SaveRoom(raw []byte) error { return s.save(HostedRoomFile, raw) }
func (s *Store) ClearRoom() error          { return s.clear(HostedRoomFile) }

// LoadSeed y SaveSeed son el registro propio, y son el ÚNICO par que NO sella.
//
// # Por qué este fichero se escribe en claro
//
// Por dos motivos que apuntan igual, y conviene decirlos porque la regla del
// resto del paquete es la contraria.
//
//  1. **No es un secreto.** El nombre del registro va dentro de cada código que
//     esta máquina reparte, así que ya lo tiene todo el que recibió una
//     invitación. Sellarlo protegería de nadie.
//  2. **Y sellarlo rompería algo que hace falta.** El sello se abre con una
//     llave derivada de `identity.key`, y `kanpachi upgrade` **tiene que
//     funcionar con el daemon caído**, que es cuando más falta hace. Necesita
//     este dato para saber a qué registro preguntarle por la versión, y con el
//     fichero sellado tendría que cargar la identidad de la máquina para leer un
//     nombre de dominio público.
//
// El modo 0600 se conserva igual, por lo mismo que el resto: no protege el
// contenido, evita que otro usuario lo REESCRIBA y mueva a dónde publica esta
// máquina.
//
// Devuelve bytes crudos como el resto del puerto: quien decide si eso es un
// nombre de registro válido es el dominio, con la misma comprobación que aplica
// al seed de un código pegado.
func (s *Store) LoadSeed() ([]byte, error) { return s.loadPlain(SeedFile) }
func (s *Store) SaveSeed(raw []byte) error { return s.savePlain(SeedFile, raw) }

func (s *Store) LoadLast() ([]byte, error) { return s.load(LastRoomFile) }
func (s *Store) SaveLast(raw []byte) error { return s.save(LastRoomFile, raw) }
func (s *Store) ClearLast() error          { return s.clear(LastRoomFile) }

// LoadSeedToken y SaveSeedToken guardan el refresh token de un seed cerrado.
//
// # Por qué este fichero lleva las DOS defensas y los demás una
//
// Porque el directorio donde vive da lectura a todos los usuarios de la máquina
// **a propósito**: la interfaz lee de ahí sin elevar, y `identity.key` es hoy la
// única excepción con ACL propia. Un fichero nuevo escrito ahí hereda esa ACL,
// así que un refresh token quedaría legible por cualquiera que se siente en esa
// máquina.
//
// El sello ya lo vuelve ilegible sin `identity.key`, y aun así se le pone además
// la ACL. Las dos, porque **el valor por omisión es el permisivo**: el día que
// alguien cambie de dónde sale la llave, o añada un camino que escriba en claro,
// lo que queda es lo que el directorio conceda. Es la trampa que `pipe.go` ya
// nombra para los dos sistemas.
//
// # Y el password no está acá
//
// Ni acá ni en ningún fichero. Lo que sobrevive a un reinicio es un token que
// caduca y que el operador revoca cambiando el password, jamás un password que
// la persona reusa en otros sitios. Es también lo que hace que la pantalla de
// volver a escribirlo tenga sentido: aparece cuando el refresh muere y no antes.
func (s *Store) LoadSeedToken() ([]byte, error) { return s.load(SeedTokenFile) }
func (s *Store) SaveSeedToken(raw []byte) error { return s.saveProtected(SeedTokenFile, raw) }
func (s *Store) ClearSeedToken() error          { return s.clear(SeedTokenFile) }

// saveProtected escribe y después le pone al fichero su propia ACL.
//
// El orden importa: primero existe el fichero, después se restringe. Al revés no
// hay a qué aplicárselo. Si el protector falla, **se borra lo escrito** en vez de
// dejar un token legible por toda la máquina, que es el fallo que se estaba
// evitando.
func (s *Store) saveProtected(nombre string, raw []byte) error {
	if err := s.save(nombre, raw); err != nil {
		return err
	}
	if s.protect == nil {
		return nil
	}
	if err := s.protect(s.path(nombre)); err != nil {
		_ = s.clear(nombre)
		return fmt.Errorf("no se pudo restringir %s, así que no se guarda: %w", nombre, err)
	}
	return nil
}

// loadPlain lee sin abrir el sello, y es lo que usan los ficheros que se
// escriben en claro a propósito. Ver [Store.LoadSeed].
func (s *Store) loadPlain(nombre string) ([]byte, error) {
	raw, err := os.ReadFile(s.path(nombre))
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("%w: %s", ErrNoState, nombre)
	}
	if err != nil {
		return nil, fmt.Errorf("leyendo %s: %w", nombre, err)
	}
	return raw, nil
}

// savePlain escribe sin sellar, con el mismo modo que el resto.
func (s *Store) savePlain(nombre string, raw []byte) error {
	return safewrite.File(s.path(nombre), raw, 0o600)
}

func (s *Store) load(nombre string) ([]byte, error) {
	raw, err := s.loadPlain(nombre)
	if err != nil {
		return nil, err
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
	return s.savePlain(nombre, raw)
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
