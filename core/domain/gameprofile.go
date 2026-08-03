package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// SchemaVersion es la versión de esquema de perfil que este binario entiende.
//
// Un perfil con schema mayor se salta con un aviso de actualizar Kanpachi, y
// uno con schema menor se migra si hay migración definida. Ver 06-catalogo.md.
const SchemaVersion = 2

// MaxPortRanges es el tope de rangos por perfil, contando los del host y los
// del cliente juntos.
//
// Un perfil con cuarenta rangos está mal escrito o es malicioso. El tope no
// protege de un rango grande, para eso está la revisión humana del builtin:
// protege de que un archivo compartido por Telegram genere cien reglas de
// firewall en la máquina de alguien.
const MaxPortRanges = 8

// FirewallGroup etiqueta toda regla que el DAEMON crea en el Firewall de
// Windows. Al arrancar el servicio se purga todo lo que lleve esta etiqueta y
// después se aplica el estado deseado, así que una muerte sucia del daemon
// nunca deja puertos huérfanos abiertos.
//
// Son las reglas de la SALA: van y vienen con el juego activo y con quién está
// dentro. Su contraparte es [FirewallGroupBase], que es de la instalación.
const FirewallGroup = "Kanpachi"

// FirewallGroupBase etiqueta la cuarentena de base, que pone el INSTALADOR y el
// daemon jamás toca.
//
// # Por qué son dos grupos y no uno
//
// Con un solo grupo, la purga del arranque desarma la cuarentena en cada
// reinicio del servicio, y el fallo es invisible: todo sigue funcionando igual,
// solo que sin protección. Separarlas hace que el reparto se lea en una frase:
// el instalador pone la cuarentena, el daemon pone la sala, y ninguno toca lo
// del otro.
//
// La otra mitad del argumento es de tipos: la base necesita BLOQUEO y necesita
// SALIDA, y [FirewallRule] no puede expresar ninguno de los dos a propósito. Lo
// que no cabe en un RuleSet no puede ser el daemon quien lo reponga, porque el
// daemon solo sabe aplicar RuleSets.
//
// # Qué hay dentro, y por qué no es un deny-all
//
// La entrada ya viene bloqueada por defecto en los tres perfiles de Windows, o
// sea que la ausencia de reglas de permiso YA ES el deny-all. La base hace lo
// que la ausencia no puede: bloquear los puertos prohibidos en las dos
// direcciones, que es lo que gana contra una regla permisiva que dejó el
// instalador de un juego, más el permiso de ICMP echo para el diagnóstico.
//
// Un bloqueo total sobre la IP del adaptador NO sirve, y esto es documentación
// de Microsoft y no una opinión: los bloqueos explícitos ganan sobre los
// permisos, sin desempate por especificidad, así que la base taparía las reglas
// de juego del propio Kanpachi. Ver decisión 4.
//
// # Cómo se compara
//
// Por IGUALDAD EXACTA, jamás por prefijo. [FirewallGroup] es prefijo de esta
// cadena, así que una purga escrita con HasPrefix borraría la cuarentena y
// dejaría la máquina expuesta sin que nadie lo note. Lo vigila un guardián en
// internal/arch, que además comprueba que este nombre no aparezca en daemon/.
const FirewallGroupBase = "Kanpachi-base"

// IsOwnFirewallGroup dice si un grupo del firewall es de Kanpachi, sea el de la
// sala o el de la cuarentena.
//
// # Por qué es una función del dominio y no un `||` en el adaptador
//
// El adaptador necesita saberlo para lo contrario de lo que parece: para
// EXCLUIR las reglas propias de todo lo que hace con las ajenas. Una regla del
// grupo base no es una regla ajena, y desactivarla desarmaría lo único que
// protege la máquina con el servicio parado.
//
// Escrito en el adaptador, ese `||` obliga al daemon a nombrar
// [FirewallGroupBase], y hay un guardián en internal/arch que lo prohíbe con
// razón: nombrarlo es todo lo que hace falta para tocarlo, y el guardián no
// puede distinguir "lo nombro para escribirlo" de "lo nombro para saltármelo".
//
// Acá el daemon pregunta sin nombrar, la política de qué es propio queda en un
// solo sitio, y el guardián sigue tan estricto como antes.
func IsOwnFirewallGroup(group string) bool {
	return group == FirewallGroup || group == FirewallGroupBase
}

// forbiddenPorts son los puertos que ningún perfil puede pedir, jamás.
//
// No hay campo en el esquema para saltárselos ni bandera que los habilite: la
// lista vive acá, en el código, porque el catálogo es la capa de conocimiento
// y esta es la capa de política. Un perfil que caiga sobre cualquiera de estos
// se rechaza entero, no se recorta.
//
//	22    SSH
//	135   RPC de Windows
//	137-139, 445  NetBIOS y SMB. Son el escenario que Kanpachi existe para evitar
//	3389  Escritorio remoto
//	5985, 5986  WinRM
var forbiddenPorts = [...]uint16{22, 135, 137, 138, 139, 445, 3389, 5985, 5986}

// Proto es el protocolo de transporte de un rango de puertos.
type Proto uint8

const (
	ProtoTCP Proto = iota + 1
	ProtoUDP
	// ProtoBoth es azúcar de escritura: el perfil declara un rango una vez y
	// [BuildRuleSet] lo expande a dos reglas de firewall. Nunca llega a
	// FirewallPort, porque una regla de Windows tiene un protocolo y solo uno.
	ProtoBoth
)

var (
	ErrProto          = errors.New("el protocolo debe ser tcp, udp o both")
	ErrPortRangeShape = errors.New("el rango de puertos no tiene forma de puerto ni de puerto-puerto")
	ErrPortRangeOrder = errors.New("el rango de puertos empieza después de donde termina")
	ErrPortZero       = errors.New("el puerto 0 no existe")
	ErrPortForbidden  = errors.New("el perfil pide un puerto prohibido")
	ErrTooManyRanges  = fmt.Errorf("un perfil no puede pasar de %d rangos de puertos", MaxPortRanges)
	ErrProfileID      = errors.New("el id del perfil solo admite minúsculas, dígitos y guiones")
	ErrProfileName    = errors.New("el perfil necesita un nombre")
	// Los dos sentidos se distinguen porque la regla 6 del importador los trata
	// distinto y la UI dice cosas opuestas: uno pide actualizar Kanpachi, el
	// otro pide una migración que todavía no existe. Mandar a alguien a
	// actualizar por un perfil viejo lo manda a buscar el fallo al otro lado.
	ErrProfileSchemaNewer = errors.New("el perfil es de una versión más nueva de Kanpachi")
	ErrProfileSchemaOlder = errors.New("el perfil es de un esquema viejo y no hay migración definida")
	ErrProfileNoPorts     = errors.New("el perfil no abre ningún puerto")
	ErrConnectHintKind    = errors.New("connect_hint.kind no es uno de los tres valores admitidos")
	ErrUnknownField       = errors.New("el perfil trae un campo que el esquema no define")
	ErrCatalogEnvelope    = errors.New("el archivo no es un catálogo de Kanpachi")
)

// ParseProto acepta lo que puede aparecer en el JSON de un perfil.
func ParseProto(s string) (Proto, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "tcp":
		return ProtoTCP, nil
	case "udp":
		return ProtoUDP, nil
	case "both", "tcp/udp":
		return ProtoBoth, nil
	default:
		return 0, fmt.Errorf("%w, llegó %q", ErrProto, s)
	}
}

func (p Proto) String() string {
	switch p {
	case ProtoTCP:
		return "tcp"
	case ProtoUDP:
		return "udp"
	case ProtoBoth:
		return "both"
	default:
		return "proto-inválido"
	}
}

// PortRange es un rango contiguo de puertos con su protocolo.
//
// Rango y no puerto suelto porque la mayoría de los juegos abren varios
// contiguos (Zomboid 16261-16262, Valheim 2456-2458), y obligar a una entrada
// por puerto convertiría el alta manual en un trámite.
//
// El cero no es utilizable: se construye con [ParsePortRange] o por el decoder
// del perfil, así que todo PortRange que circule ya pasó las invariantes.
type PortRange struct {
	Proto Proto
	From  uint16
	To    uint16
}

// ParsePortRange acepta "27015" y "2456-2458", que son las dos formas que
// escribe una persona.
func ParsePortRange(proto Proto, spec string) (PortRange, error) {
	s := strings.TrimSpace(spec)
	if s == "" {
		return PortRange{}, ErrPortRangeShape
	}

	fromTxt, toTxt, hasDash := strings.Cut(s, "-")
	if !hasDash {
		toTxt = fromTxt
	}

	from, err := parsePort(fromTxt)
	if err != nil {
		return PortRange{}, err
	}
	to, err := parsePort(toTxt)
	if err != nil {
		return PortRange{}, err
	}
	if from > to {
		return PortRange{}, fmt.Errorf("%w: %s", ErrPortRangeOrder, s)
	}

	r := PortRange{Proto: proto, From: from, To: to}
	if bad, hit := r.hitsForbidden(); hit {
		return PortRange{}, fmt.Errorf("%w: %d, dentro de %s", ErrPortForbidden, bad, r)
	}
	return r, nil
}

func parsePort(s string) (uint16, error) {
	s = strings.TrimSpace(s)
	// ParseUint acepta "+27015" y espacios internos no, pero sí acepta formas
	// que en un archivo de configuración son ruido. Se exige que sean dígitos
	// y nada más, porque un perfil llega por Telegram y lo estricto es gratis.
	if s == "" {
		return 0, ErrPortRangeShape
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, fmt.Errorf("%w: %q", ErrPortRangeShape, s)
		}
	}
	n, err := strconv.ParseUint(s, 10, 16)
	if err != nil {
		return 0, fmt.Errorf("%w: %q", ErrPortRangeShape, s)
	}
	if n == 0 {
		return 0, ErrPortZero
	}
	return uint16(n), nil
}

// hitsForbidden dice si el rango CONTIENE algún puerto prohibido.
//
// Contiene, no "es igual a". Un perfil que pida 440-450 abre el 445 igual que
// si lo hubiera nombrado, y quien escribe un perfil malicioso lo escribiría
// exactamente así.
func (r PortRange) hitsForbidden() (uint16, bool) {
	for _, p := range forbiddenPorts {
		if p >= r.From && p <= r.To {
			return p, true
		}
	}
	return 0, false
}

func (r PortRange) String() string {
	if r.From == r.To {
		return fmt.Sprintf("%d/%s", r.From, r.Proto)
	}
	return fmt.Sprintf("%d-%d/%s", r.From, r.To, r.Proto)
}

// Spec devuelve el rango en la forma en que se escribe en el JSON, sin el
// protocolo. Es lo que la UI muestra en la fila de un alta manual.
func (r PortRange) Spec() string {
	if r.From == r.To {
		return strconv.Itoa(int(r.From))
	}
	return strconv.Itoa(int(r.From)) + "-" + strconv.Itoa(int(r.To))
}

// Origin dice de dónde salió un perfil, y se muestra en la lista de juegos.
// Nadie usa a ciegas un perfil que le llegó por Telegram.
type Origin uint8

const (
	// OriginBuiltin viene en el instalador, verificado por el autor.
	OriginBuiltin Origin = iota + 1
	// OriginMine se creó en esta máquina con el creador de perfiles.
	OriginMine
	// OriginImported llegó en un .json que compartió alguien.
	OriginImported
)

func (o Origin) String() string {
	switch o {
	case OriginBuiltin:
		return "builtin"
	case OriginMine:
		return "mine"
	case OriginImported:
		return "imported"
	default:
		return "origen-inválido"
	}
}

// precedence ordena las capas. Mayor gana. Ver la tabla de precedencia de
// 06-catalogo.md: mine sobre imported sobre builtin.
func (o Origin) precedence() int {
	switch o {
	case OriginMine:
		return 3
	case OriginImported:
		return 2
	case OriginBuiltin:
		return 1
	default:
		return 0
	}
}

// ConnectHintKind es el conjunto cerrado de formas de conectarse a una
// partida. Cerrado porque el texto de la pantalla en sala se elige a partir de
// él, y un valor nuevo sin pantalla que lo muestre es un perfil que no sirve.
type ConnectHintKind uint8

const (
	ConnectDirectIP ConnectHintKind = iota + 1
	ConnectLANBrowser
	ConnectSteamFriends
)

func ParseConnectHintKind(s string) (ConnectHintKind, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "direct_ip":
		return ConnectDirectIP, nil
	case "lan_browser":
		return ConnectLANBrowser, nil
	case "steam_friends":
		return ConnectSteamFriends, nil
	default:
		return 0, fmt.Errorf("%w, llegó %q", ErrConnectHintKind, s)
	}
}

func (k ConnectHintKind) String() string {
	switch k {
	case ConnectDirectIP:
		return "direct_ip"
	case ConnectLANBrowser:
		return "lan_browser"
	case ConnectSteamFriends:
		return "steam_friends"
	default:
		return ""
	}
}

// SystemTweaks son los ajustes de Windows que un juego concreto necesita.
//
// Es un conjunto CERRADO de cuatro booleanos, y esa es la invariante 8 de
// 06-catalogo.md. No existe forma de expresar "ejecuta este comando" ni
// "habilita este grupo del firewall", y un perfil con una clave desconocida se
// rechaza entero en vez de ignorar la clave. Ignorarla sería aceptar un
// archivo que pide algo que no entendemos.
type SystemTweaks struct {
	// BroadcastRoute instala 255.255.255.255/32 sobre kanpachi0.
	BroadcastRoute bool `json:"broadcast_route"`
	// MulticastRoute instala 224.0.0.0/4. mDNS, SSDP, buscadores de servidores.
	MulticastRoute bool `json:"multicast_route"`
	// PreferIPv4 escribe la política de prefijo ::ffff:0:0/96 100 4. NO
	// desactiva IPv6: solo reordena la selección de destino de RFC 6724.
	PreferIPv4 bool `json:"prefer_ipv4"`
	// DirectPlay habilita el componente legado de Windows.
	DirectPlay bool `json:"directplay"`
}

// Any dice si el perfil pide algún ajuste. Sin esto, netcfg tendría que
// comparar contra el cero para saber si hay algo que revertir al salir.
func (t SystemTweaks) Any() bool {
	return t.BroadcastRoute || t.MulticastRoute || t.PreferIPv4 || t.DirectPlay
}

// Detect es lo que se usa para ORDENAR la lista de juegos y para auditar
// reglas de firewall ajenas. Nunca para filtrar: un juego que la detección no
// vio se elige exactamente igual.
type Detect struct {
	SteamAppID  int      `json:"steam_appid,omitempty"`
	Executables []string `json:"executables,omitempty"`
}

// ConnectHint es el texto exacto que la pantalla en sala le muestra al jugador.
type ConnectHint struct {
	Kind   ConnectHintKind
	TextES string
}

// BindHint es información para el usuario avanzado. Kanpachi lo MUESTRA y
// jamás escribe en archivos de configuración de otros programas.
type BindHint struct {
	File   string `json:"file,omitempty"`
	Key    string `json:"key,omitempty"`
	NoteES string `json:"note_es,omitempty"`
}

// Verified es la constancia de que alguien jugó una partida real con este
// perfil. No se puede marcar a mano: lo rellena la pregunta que aparece al
// salir de una sala donde el juego estuvo activo y hubo dos personas o más.
type Verified struct {
	Date        string `json:"date"`
	By          string `json:"by"`
	Method      string `json:"method"`
	GameVersion string `json:"game_version,omitempty"`
}

// GameProfile describe QUÉ NECESITA un juego para funcionar en una LAN.
//
// No describe qué se le concede: eso lo decide [BuildRuleSet] con las
// invariantes de este archivo. La separación es lo que permite compartir
// perfiles sin que compartir sea un riesgo, y su consecuencia práctica es la
// regla que gobierna la capa entera: si un perfil está corrupto, mal escrito o
// es malicioso, el peor resultado posible es que ese juego no conecte.
//
// El cero no es utilizable. Se construye con [ParseGameProfile] o con
// [NewGameProfile], así que todo perfil que circule ya pasó las ocho
// invariantes y nadie tiene que revalidarlo aguas abajo.
type GameProfile struct {
	ID     string
	Name   string
	Origin Origin

	Detect Detect

	// HostPorts son las reglas en la máquina que hospeda.
	HostPorts []PortRange
	// ClientPorts son las reglas en quien se une, y está VACÍO en la enorme
	// mayoría. Es el campo que decide la topología: vacío es estrella, o sea
	// nadie alcanza la máquina de nadie; no vacío es malla, y expande el radio
	// de explosión de todos los miembros. Ver 06-catalogo.md.
	ClientPorts []PortRange

	LANDiscovery bool
	Tweaks       SystemTweaks
	Connect      ConnectHint
	Bind         BindHint

	// Verified en nil es un perfil sin probar. Funciona igual, con aviso.
	Verified *Verified
}

// IsMesh dice si el perfil conecta a todos con todos. La UI lo advierte ANTES
// de entrar, con las palabras de 05-ui.md.
func (p GameProfile) IsMesh() bool { return len(p.ClientPorts) > 0 }

// IsZero informa si el perfil no fue construido.
func (p GameProfile) IsZero() bool { return p.ID == "" }

// NewGameProfile construye y valida. Es el camino del creador de perfiles, que
// arma los rangos desde la foto de sockets en vez de desde un archivo.
func NewGameProfile(p GameProfile) (GameProfile, error) {
	if err := p.validate(); err != nil {
		return GameProfile{}, err
	}
	return p, nil
}

// validate corre las invariantes que no tienen campo equivalente en el JSON.
//
// Un perfil inválido se rechaza COMPLETO. Nunca se carga a medias ni se
// degrada a algo más permisivo: recortar el rango prohibido y quedarse con el
// resto sería cargar un archivo distinto del que alguien escribió, y el error
// dejaría de verse.
func (p GameProfile) validate() error {
	if !validProfileID(p.ID) {
		return fmt.Errorf("%w: %q", ErrProfileID, p.ID)
	}
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("%w: %s", ErrProfileName, p.ID)
	}
	if p.Origin == 0 {
		return fmt.Errorf("el perfil %s no declara de dónde salió", p.ID)
	}
	if len(p.HostPorts) == 0 && len(p.ClientPorts) == 0 {
		return fmt.Errorf("%w: %s", ErrProfileNoPorts, p.ID)
	}
	if n := len(p.HostPorts) + len(p.ClientPorts); n > MaxPortRanges {
		return fmt.Errorf("%w: %s trae %d", ErrTooManyRanges, p.ID, n)
	}
	for _, r := range append(append([]PortRange{}, p.HostPorts...), p.ClientPorts...) {
		if r.Proto == 0 || r.From == 0 || r.To < r.From {
			return fmt.Errorf("%w: %s trae %v", ErrPortRangeShape, p.ID, r)
		}
		if bad, hit := r.hitsForbidden(); hit {
			return fmt.Errorf("%w: %s pide %d dentro de %s", ErrPortForbidden, p.ID, bad, r)
		}
	}
	if p.Connect.Kind == 0 {
		return fmt.Errorf("%w: %s", ErrConnectHintKind, p.ID)
	}
	return nil
}

// validProfileID exige minúsculas, dígitos y guiones.
//
// El id es la clave de precedencia entre las tres capas y termina en el nombre
// de una regla de firewall, así que su alfabeto tiene que ser aburrido: sin
// espacios, sin mayúsculas que hagan que "Valheim" y "valheim" sean dos
// perfiles, y sin nada que necesite escaparse en ningún sitio.
func validProfileID(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	if s[0] == '-' || s[len(s)-1] == '-' {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '-':
		default:
			return false
		}
	}
	return true
}

// profileJSON es la forma en disco del esquema v2 de 06-catalogo.md.
//
// Existe separada del tipo del dominio a propósito, y no contradice la regla
// de "nada de DTOs entre capas": esto no mapea entre anillos, es el formato de
// intercambio con el mundo, que tiene claves con guion bajo, protocolos como
// texto y campos que el dominio guarda ya parseados. Que sean dos tipos es lo
// que permite cambiar el esquema sin tocar el dominio.
type profileJSON struct {
	ID     string `json:"id"`
	Schema int    `json:"schema"`
	Name   string `json:"name"`

	Detect Detect `json:"detect"`

	HostPorts   []portRangeJSON `json:"host_ports"`
	ClientPorts []portRangeJSON `json:"client_ports"`

	LANDiscovery bool         `json:"lan_discovery"`
	Tweaks       SystemTweaks `json:"system_tweaks"`

	Connect struct {
		Kind   string `json:"kind"`
		TextES string `json:"text_es"`
	} `json:"connect_hint"`

	Bind     BindHint  `json:"bind_hint"`
	Verified *Verified `json:"verified"`

	// Origin solo lo escribe local.json, que es la única capa que mezcla
	// perfiles propios con importados en el mismo archivo. En un archivo de
	// intercambio va ausente, y si viene se ignora: la capa la fija quien
	// carga, jamás el archivo. Sin esto, un .json compartido podría declararse
	// "mine" y ganarle en precedencia a un builtin verificado.
	Origin string `json:"origin,omitempty"`
}

type portRangeJSON struct {
	Proto string `json:"proto"`
	Range string `json:"range"`
}

// ParseGameProfile decodifica y valida un perfil suelto.
//
// El decoder rechaza campos desconocidos, y eso es la invariante 8 hecha
// código: un perfil que trae una clave que no existe en el esquema pide algo
// que este binario no sabe interpretar, y aceptarlo ignorando la clave sería
// cargar un archivo distinto del que alguien escribió.
func ParseGameProfile(raw []byte, origin Origin) (GameProfile, error) {
	j, err := decodeProfileStrict(raw)
	if err != nil {
		return GameProfile{}, err
	}
	return j.toDomain(origin)
}

// decodeProfileStrict es el ÚNICO decodificador de perfiles del programa.
//
// Es único a propósito, y esa unicidad es la invariante 8. Hubo una versión de
// este archivo en que el sobre del catálogo decodificaba su lista de perfiles
// de una sola pasada con json.Unmarshal, así que DisallowUnknownFields vivía
// acá y no corría en NINGÚN camino real de carga: un perfil con
// `"run_command": "cmd.exe /c calc"` entraba por builtin.json, por local.json y
// por un archivo compartido, y el único sitio que lo rechazaba eran los tests.
// Mientras todo pase por esta función, eso no puede volver a pasar.
func decodeProfileStrict(raw []byte) (profileJSON, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()

	var j profileJSON
	if err := dec.Decode(&j); err != nil {
		return profileJSON{}, fmt.Errorf("%w: %v", ErrUnknownField, err)
	}
	// Decode se detiene al cerrar el primer objeto y lo que venga detrás lo
	// ignora. Un archivo con `{perfil bueno}{perfil malo}` pasaría entero
	// mostrando solo el primero, que es la forma clásica de colar contenido
	// que el revisor humano no ve. Un perfil es UN objeto y nada más.
	if dec.More() {
		return profileJSON{}, fmt.Errorf("%w: hay contenido después del perfil", ErrUnknownField)
	}
	return j, nil
}

// identifyProfile saca el id y el nombre de un perfil que NO se pudo decodificar.
//
// Decodifica flojo y a propósito: lo que se busca es con qué etiquetar el
// rechazo en la pantalla, y exigirle rigor a un objeto que ya se sabe malo
// dejaría al usuario con una fila que dice "un perfil" y nada más. Lo que sale
// de acá no llega jamás a una regla de firewall.
func identifyProfile(raw []byte) (id, name string) {
	var flojo struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	_ = json.Unmarshal(raw, &flojo)
	return flojo.ID, flojo.Name
}

func (j profileJSON) toDomain(origin Origin) (GameProfile, error) {
	switch {
	case j.Schema > SchemaVersion:
		return GameProfile{}, fmt.Errorf("%w: %s declara schema %d y este Kanpachi entiende %d",
			ErrProfileSchemaNewer, j.ID, j.Schema, SchemaVersion)
	case j.Schema < SchemaVersion:
		return GameProfile{}, fmt.Errorf("%w: %s declara schema %d y este Kanpachi entiende %d",
			ErrProfileSchemaOlder, j.ID, j.Schema, SchemaVersion)
	}

	host, err := decodeRanges(j.HostPorts)
	if err != nil {
		return GameProfile{}, fmt.Errorf("perfil %s, host_ports: %w", j.ID, err)
	}
	client, err := decodeRanges(j.ClientPorts)
	if err != nil {
		return GameProfile{}, fmt.Errorf("perfil %s, client_ports: %w", j.ID, err)
	}
	kind, err := ParseConnectHintKind(j.Connect.Kind)
	if err != nil {
		return GameProfile{}, fmt.Errorf("perfil %s: %w", j.ID, err)
	}

	return NewGameProfile(GameProfile{
		ID:           j.ID,
		Name:         j.Name,
		Origin:       origin,
		Detect:       j.Detect,
		HostPorts:    host,
		ClientPorts:  client,
		LANDiscovery: j.LANDiscovery,
		Tweaks:       j.Tweaks,
		Connect:      ConnectHint{Kind: kind, TextES: j.Connect.TextES},
		Bind:         j.Bind,
		Verified:     j.Verified,
	})
}

func decodeRanges(in []portRangeJSON) ([]PortRange, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]PortRange, 0, len(in))
	for _, r := range in {
		proto, err := ParseProto(r.Proto)
		if err != nil {
			return nil, err
		}
		pr, err := ParsePortRange(proto, r.Range)
		if err != nil {
			return nil, err
		}
		out = append(out, pr)
	}
	return out, nil
}

// toJSON es el camino inverso, para exportar y para guardar local.json.
func (p GameProfile) toJSON() profileJSON {
	var j profileJSON
	j.ID = p.ID
	j.Schema = SchemaVersion
	j.Name = p.Name
	j.Detect = p.Detect
	j.HostPorts = encodeRanges(p.HostPorts)
	j.ClientPorts = encodeRanges(p.ClientPorts)
	j.LANDiscovery = p.LANDiscovery
	j.Tweaks = p.Tweaks
	j.Connect.Kind = p.Connect.Kind.String()
	j.Connect.TextES = p.Connect.TextES
	j.Bind = p.Bind
	j.Verified = p.Verified
	return j
}

// encodeRanges devuelve un slice vacío y no nil a propósito: el JSON tiene que
// llevar `"client_ports": []` escrito, porque ese campo es el que declara la
// topología y su ausencia se lee distinto que su vacío por quien revisa el
// archivo a ojo.
func encodeRanges(in []PortRange) []portRangeJSON {
	out := make([]portRangeJSON, 0, len(in))
	for _, r := range in {
		out = append(out, portRangeJSON{Proto: r.Proto.String(), Range: r.Spec()})
	}
	return out
}
