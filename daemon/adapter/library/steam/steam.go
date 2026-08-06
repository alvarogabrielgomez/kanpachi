// Package steam detecta qué juegos de Steam están instalados en esta máquina.
//
// # Qué se lee, y por qué eso y no otra cosa
//
// Steam mantiene su propia contabilidad en texto plano, y es la única fuente
// que dice qué está instalado DE VERDAD en vez de qué está en la biblioteca de
// la cuenta. Son dos ficheros:
//
//   - `steamapps/libraryfolders.vdf`, la lista de discos donde el usuario
//     guarda juegos. Casi nadie los tiene todos en C:.
//   - `steamapps/appmanifest_<appid>.acf`, uno por juego, con su nombre, su
//     carpeta y sus banderas de estado.
//
// No se mira el registro por juego, no se recorre el disco, y no se le pregunta
// a Steam por red. Lo primero es incompleto desde hace años, lo segundo tarda
// minutos en un disco lleno, y lo tercero necesitaría credenciales del usuario
// para leer una lista que ya está en su disco sin pedirlas.
//
// # Esto ORDENA, jamás filtra
//
// `port.GameLibrary` lo dice y conviene repetirlo acá: lo que devuelve esto
// sube juegos arriba de una lista. Elegir un juego que la detección no vio
// funciona exactamente igual y sin advertencias. Por eso todo este archivo
// prefiere devolver de menos antes que fallar: un fallo acá no puede impedir
// crear una sala.
package steam

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/port"
)

// Library lee la contabilidad de Steam.
type Library struct {
	log port.Logger
	// root es la raíz de Steam. Vacío pide resolverla al sistema, y es lo que
	// usa el cableado; con valor se salta esa resolución.
	root string
}

// New arma el adaptador.
func New(log port.Logger) *Library { return &Library{log: log} }

// NewAt arma uno anclado a una raíz concreta, sin tocar el registro.
func NewAt(root string, log port.Logger) *Library { return &Library{root: root, log: log} }

// stateFullyInstalled es el bit de `StateFlags` que dice que el juego está
// completo en disco.
//
// Es una MÁSCARA y no un valor: un juego en cola de actualización lleva ese bit
// más el de "update required", así que comparar por igualdad lo perdería. Y
// perderlo es justo el caso peor, porque un juego que se está actualizando es
// uno que alguien piensa jugar ahora mismo.
const stateFullyInstalled = 4

// Installed devuelve lo que Steam dice tener instalado.
//
// Nunca devuelve error por "Steam no está": esa es una respuesta legítima de
// una máquina sin Steam, y convertirla en error haría que el log de cada
// arranque en una PC así dijera que algo se rompió.
func (l *Library) Installed(ctx context.Context) ([]domain.GameRef, error) {
	root := l.root
	if root == "" {
		r, err := steamRoot()
		if err != nil {
			// Sin Steam no hay nada que detectar, y no hay nada roto.
			l.log.Info("no se encontró Steam en esta máquina", "motivo", err)
			return nil, nil
		}
		root = r
	}

	libraries := l.libraryPaths(root)
	if len(libraries) == 0 {
		l.log.Warn("Steam está pero no se pudo leer ninguna biblioteca", "raíz", root)
		return nil, nil
	}

	// Por appid, porque el MISMO juego aparece una vez por biblioteca cuando
	// alguien lo movió de disco y Steam dejó el manifiesto viejo atrás.
	byID := make(map[int]domain.GameRef)
	for _, lib := range libraries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for _, ref := range l.manifestsIn(lib) {
			byID[ref.SteamAppID] = ref
		}
	}

	out := make([]domain.GameRef, 0, len(byID))
	for _, ref := range byID {
		out = append(out, ref)
	}
	// El orden final lo pone `domain.SortByInstalled`, que es dominio. Acá se
	// ordena por id solo para que dos arranques seguidos devuelvan lo mismo:
	// recorrer un mapa de Go da un orden distinto cada vez, y una lista que
	// baila entre arranques parece un fallo.
	sortByAppID(out)

	l.log.Info("juegos de Steam detectados", "cantidad", len(out), "bibliotecas", len(libraries))
	return out, nil
}

// libraryPaths lee `libraryfolders.vdf` y devuelve las raíces de biblioteca.
//
// # Los dos formatos, que conviven
//
// El fichero cambió de forma y las dos versiones siguen vivas en máquinas
// reales, así que se aceptan las dos:
//
//	"libraryfolders" { "1"  "D:\\SteamLibrary" }          el viejo
//	"libraryfolders" { "0" { "path" "C:\\..." ... } }     el de ahora
//
// La raíz de Steam entra SIEMPRE, esté o no en el fichero. En instalaciones de
// una sola biblioteca el fichero llega a no listarla, y perderla dejaría sin
// detectar justo la máquina más común.
func (l *Library) libraryPaths(root string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" {
			return
		}
		p = filepath.Clean(p)
		key := strings.ToLower(p)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, p)
	}
	add(root)

	path := filepath.Join(root, "steamapps", "libraryfolders.vdf")
	raw, err := os.ReadFile(path)
	if err != nil {
		// Solo la raíz. Es lo correcto y es la instalación por defecto.
		l.log.Info("sin libraryfolders.vdf, se usa solo la raíz de Steam", "ruta", path, "error", err)
		return out
	}

	tree, err := parseKeyValues(string(raw))
	if err != nil {
		l.log.Warn("no se pudo leer libraryfolders.vdf", "ruta", path, "error", err)
		return out
	}

	// El nombre de la clave raíz cambió de mayúsculas entre versiones, así que
	// no se busca por nombre: se toma el único hijo que hay.
	folders := firstObject(tree)
	if folders == nil {
		return out
	}

	for _, k := range folders.order {
		// Las claves que no son un número son metadatos del propio fichero
		// (`TimeNextStatsReport`, `ContentStatsID`), no bibliotecas.
		if _, err := strconv.Atoi(k); err != nil {
			continue
		}
		c := folders.child(k)
		if c == nil {
			continue
		}
		if c.isObject() {
			add(c.str("path"))
			continue
		}
		add(c.leaf)
	}
	return out
}

// manifestsIn lee los `appmanifest_*.acf` de una biblioteca.
func (l *Library) manifestsIn(library string) []domain.GameRef {
	apps := filepath.Join(library, "steamapps")
	entries, err := os.ReadDir(apps)
	if err != nil {
		// Una biblioteca en un disco externo desconectado es esto, y es
		// normal: el fichero la sigue listando y la carpeta no está.
		l.log.Info("biblioteca de Steam ilegible", "ruta", apps, "error", err)
		return nil
	}

	var out []domain.GameRef
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "appmanifest_") || !strings.HasSuffix(name, ".acf") {
			continue
		}
		ref, err := l.readManifest(apps, name)
		if err != nil {
			l.log.Info("manifiesto de Steam ilegible", "archivo", name, "error", err)
			continue
		}
		if ref == nil {
			continue
		}
		out = append(out, *ref)
	}
	return out
}

// readManifest saca un juego de un `appmanifest_*.acf`.
//
// Devuelve nil sin error cuando el manifiesto está bien y el juego no cuenta,
// que es lo que pasa con lo que está descargándose o quedó a medio borrar.
func (l *Library) readManifest(dir, file string) (*domain.GameRef, error) {
	raw, err := os.ReadFile(filepath.Join(dir, file))
	if err != nil {
		return nil, err
	}
	tree, err := parseKeyValues(string(raw))
	if err != nil {
		return nil, err
	}

	state := tree.child("AppState")
	if state == nil {
		// Igual que arriba: el nombre de la clave no se da por hecho.
		state = firstObject(tree)
	}
	if state == nil {
		return nil, fmt.Errorf("no tiene bloque AppState")
	}

	appID, err := strconv.Atoi(state.str("appid"))
	if err != nil || appID <= 0 {
		return nil, fmt.Errorf("appid ilegible: %q", state.str("appid"))
	}

	flags, err := strconv.Atoi(state.str("StateFlags"))
	if err != nil {
		// Sin banderas legibles se acepta: un manifiesto existe porque alguien
		// instaló ese juego, y descartarlo por un campo que no se entendió
		// sería perder el dato por el detalle.
		flags = stateFullyInstalled
	}
	if flags&stateFullyInstalled == 0 {
		return nil, nil
	}

	name := state.str("name")
	if name == "" {
		return nil, fmt.Errorf("sin nombre")
	}

	ref := domain.GameRef{
		SteamAppID: appID,
		Name:       name,
		Version:    state.str("buildid"),
	}
	if dirName := state.str("installdir"); dirName != "" {
		ref.InstallPath = filepath.Join(dir, "common", dirName)
	}
	return &ref, nil
}

// firstObject devuelve el primer hijo que sea un objeto.
//
// Existe porque los dos ficheros que este paquete lee tienen UNA clave raíz
// cuyo nombre ha cambiado de mayúsculas entre versiones de Steam. Buscarla por
// nombre haría que una versión distinta devolviera cero juegos en silencio.
func firstObject(n *node) *node {
	for _, k := range n.order {
		if c := n.child(k); c != nil && c.isObject() {
			return c
		}
	}
	return nil
}

// sortByAppID ordena en el sitio, por id.
func sortByAppID(refs []domain.GameRef) {
	for i := 1; i < len(refs); i++ {
		for j := i; j > 0 && refs[j].SteamAppID < refs[j-1].SteamAppID; j-- {
			refs[j], refs[j-1] = refs[j-1], refs[j]
		}
	}
}

var _ port.GameLibrary = (*Library)(nil)
