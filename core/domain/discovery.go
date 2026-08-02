package domain

import (
	"sort"
	"strings"
)

// GameRef es un juego detectado en disco.
//
// Sale de los archivos de Steam: libraryfolders.vdf para enumerar bibliotecas
// y appmanifest_*.acf cruzados por AppID. El juego ni se ejecuta.
//
// Lo que hace con esto el producto es ORDENAR la lista, nunca filtrarla. La
// detección es falible por diseño (juegos fuera de Steam, otras tiendas,
// unidades ilegibles) y ninguna de esas fallas puede impedir crear una sala.
type GameRef struct {
	SteamAppID  int
	Name        string
	InstallPath string
	Version     string
}

// SortByInstalled pone arriba lo que parece instalado, conservando el orden
// alfabético dentro de cada mitad.
//
// Es un atajo, jamás una puerta: la biblioteca completa sigue accesible y
// elegir un juego que la detección no vio funciona exactamente igual, sin
// advertencias.
func SortByInstalled(profiles []GameProfile, installed []GameRef) []GameProfile {
	byAppID := make(map[int]bool, len(installed))
	byName := make(map[string]bool, len(installed))
	for _, g := range installed {
		if g.SteamAppID != 0 {
			byAppID[g.SteamAppID] = true
		}
		if g.Name != "" {
			byName[strings.ToLower(g.Name)] = true
		}
	}

	out := append([]GameProfile(nil), profiles...)
	sortProfiles(out)

	hit := func(p GameProfile) bool {
		if p.Detect.SteamAppID != 0 && byAppID[p.Detect.SteamAppID] {
			return true
		}
		return byName[strings.ToLower(p.Name)]
	}
	// Estable para no deshacer el alfabético que acaba de aplicarse.
	sort.SliceStable(out, func(i, j int) bool { return hit(out[i]) && !hit(out[j]) })
	return out
}

// IsInstalled dice si un perfil corresponde a algo detectado. La UI lo usa
// para la marca de la portada.
func IsInstalled(p GameProfile, installed []GameRef) bool {
	for _, g := range installed {
		if p.Detect.SteamAppID != 0 && g.SteamAppID == p.Detect.SteamAppID {
			return true
		}
		if g.Name != "" && strings.EqualFold(g.Name, p.Name) {
			return true
		}
	}
	return false
}

// ProcessRef es la raíz del árbol de procesos que mira el modo observación.
//
// Se sigue el árbol y no solo el proceso porque muchos juegos arrancan desde
// un launcher y el servidor termina siendo un proceso hijo. La foto es
// puntual: la dispara un botón, no hay espera de fondo, y al cerrar el
// asistente no queda nada corriendo.
type ProcessRef struct {
	PID        int
	Executable string
}

// Listener es una entrada de la tabla de sockets, con el PID dueño. Es la
// misma información que muestra `netstat -ano`.
type Listener struct {
	Proto Proto
	Port  uint16
	// Address es a qué se ató el socket. Lo que escucha en 127.0.0.1 no sale
	// de la máquina y se descarta; lo que necesita regla es lo que escucha en
	// 0.0.0.0.
	Address string
	PID     int
}

// steamNoise son los puertos que Steam usa para sus cosas y que aparecen en la
// foto de casi cualquier juego de Steam sin tener nada que ver con la partida.
// Se descartan salvo que el usuario marque que ese juego sí los usa.
var steamNoise = [...][2]uint16{{27015, 27030}, {27036, 27036}}

// ObservedRanges convierte una foto de sockets en rangos listos para un
// perfil.
//
// Hace tres cosas y las tres son criterio, no formato:
//
//  1. Conserva SOLO lo que escucha en 0.0.0.0 y es del árbol de procesos
//     elegido. Un socket atado a 127.0.0.1 no sale de la máquina, y uno atado
//     a la IP de la LAN doméstica (192.168.1.5) tampoco va a escuchar en la
//     interfaz virtual, así que una regla para él no serviría de nada y sí
//     abriría un puerto por gusto.
//  2. Descarta el ruido conocido de Steam, salvo que se pida conservarlo.
//  3. Agrupa puertos contiguos del mismo protocolo en un solo rango, porque
//     un juego que abre 2456, 2457 y 2458 tiene que producir una fila y no
//     tres.
//
// Lo que NO hace es decidir si el perfil es válido: eso pasa después, en
// [NewGameProfile], con las mismas invariantes que cualquier otro perfil. Un
// juego que escuche en 445 no produce un perfil recortado, produce un perfil
// rechazado.
func ObservedRanges(ls []Listener, tree map[int]bool, keepSteam bool) []PortRange {
	type key struct {
		proto Proto
		port  uint16
	}
	seen := make(map[key]bool, len(ls))

	for _, l := range ls {
		// El árbol es obligatorio, no una mejora opcional. Sin él, la foto trae
		// todos los sockets de la máquina: el navegador, el antivirus, el
		// propio Kanpachi. Un árbol vacío es que no se encontró el proceso, y
		// la respuesta correcta a eso es "no vi nada", no "vi todo".
		if !tree[l.PID] {
			continue
		}
		if l.Port == 0 || !listensEverywhere(l.Address) {
			continue
		}
		if !keepSteam && isSteamNoise(l.Port) {
			continue
		}
		if l.Proto != ProtoTCP && l.Proto != ProtoUDP {
			continue
		}
		seen[key{l.Proto, l.Port}] = true
	}

	flat := make([]key, 0, len(seen))
	for k := range seen {
		flat = append(flat, k)
	}
	sort.Slice(flat, func(i, j int) bool {
		if flat[i].proto != flat[j].proto {
			return flat[i].proto < flat[j].proto
		}
		return flat[i].port < flat[j].port
	})

	var out []PortRange
	for _, k := range flat {
		if n := len(out); n > 0 && out[n-1].Proto == k.proto && out[n-1].To+1 == k.port {
			out[n-1].To = k.port
			continue
		}
		out = append(out, PortRange{Proto: k.proto, From: k.port, To: k.port})
	}
	return out
}

// listensEverywhere dice si el socket escucha en todas las interfaces.
//
// Es lo ÚNICO que necesita regla. Descartar solo el loopback dejaba entrar los
// sockets atados a la IP de la LAN doméstica, que no van a escuchar en la
// interfaz virtual haga lo que haga el firewall: la regla no arregla nada y el
// puerto queda abierto igual.
//
// Se compara por texto porque la tabla de sockets de Windows entrega la
// dirección ya formateada, y parsearla acá solo para volver a compararla no
// compra nada.
func listensEverywhere(addr string) bool {
	switch strings.TrimSpace(addr) {
	case "0.0.0.0", "*", "::", "[::]", "":
		return true
	default:
		return false
	}
}

func isSteamNoise(p uint16) bool {
	for _, r := range steamNoise {
		if p >= r[0] && p <= r[1] {
			return true
		}
	}
	return false
}
