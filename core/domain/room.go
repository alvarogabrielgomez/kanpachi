package domain

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
)

// NO HAY seed por defecto, y la ausencia es la decisión.
//
// Hubo uno, `DefaultSeedHost = "kanpachi.accentio.dev"`, y un invite ID pelado
// significaba esa máquina. Dejó de existir cuando el producto se abrió: **cada
// quien levanta el seed que quiera**, así que un código sin seed no identifica
// ninguna sala, identifica ocho caracteres. Ver [ErrSeedMissing].
//
// Lo que esa constante compraba, y por qué se pudo pagar de otra forma: era la
// comodidad de dictar ocho caracteres por teléfono. Ahora hay que dictar también
// el dominio, y a cambio desaparece el fallo que no se podía explicar, que era
// pegar el código de otro seed y aterrizar en una sala distinta con el mismo ID.
//
// **Ojo con la confusión que esto deja atrás:** el canal por el que este build
// pregunta por actualizaciones NO era esto, aunque compartía la cadena. Ese vive
// en `internal/selfupdate`, es una propiedad del build y no de una sala, y se
// nombra aparte a propósito.

// MaxInputLen es el tope de longitud que se aplica ANTES de parsear.
//
// El manejador kanpachi:// queda expuesto a toda la web, así que lo que entra
// por ahí es hostil por definición. Un tope temprano evita que una entrada de
// megabytes llegue siquiera a los bucles de normalización. 300 cubre con
// holgura la forma más larga: esquema, host de hasta 253, separador, invite ID
// y la clave de tarjeta en el fragmento.
const MaxInputLen = 300

const maxHostLen = 253

var (
	ErrInputTooLong = fmt.Errorf("la entrada pasa de %d caracteres", MaxInputLen)
	ErrInputShape   = errors.New("eso no tiene forma de código de Kanpachi")
	ErrSeedHost     = errors.New("el servidor del código no es un nombre válido")

	// ErrSeedMissing es un código con forma impecable al que le falta el
	// servidor.
	//
	// # Por qué es un centinela y no [ErrInputShape]
	//
	// Porque son dos cosas distintas para quien las lee. `HTTP-SKAN` no tiene
	// forma de código y no hay nada que enseñar; `A7K2-M9QX` la tiene entera, y
	// lo único que le falta es a QUÉ servidor pertenece. Un solo error para los
	// dos daría "eso no tiene forma de código" sobre ocho caracteres que se ven
	// perfectos, y quien lo lea va a mirar el código en vez de mirar lo que
	// falta. Separarlo es lo que permite contestar con la forma completa.
	ErrSeedMissing = errors.New("al código le falta el servidor")
)

// Room es un invite ID con el seed por el que se lo alcanza.
//
// **Un invite ID solo significa algo en el seed que lo emitió.** No es global,
// es local a un registro: el mismo ID en dos seeds son dos salas distintas que
// no se conocen. Por eso el seed viaja pegado y no es decoración.
type Room struct {
	InviteID InviteID
	Seed     string // host del seed, ya normalizado. Nunca vacío tras ParseRoom
}

// ParseRoom acepta las formas que LLEVAN SEED y devuelve siempre lo mismo.
//
//	A7K2M9QX@seed.midominio.com               → ese seed
//	a7k2-m9qx@seed.midominio.com              → ese seed
//	kanpachi://A7K2M9QX@seed.midominio.com    → ese seed
//	seed.midominio.com/A7K2M9QX               → ese seed
//	https://seed.midominio.com/A7K2M9QX       → ese seed
//	https://seed.midominio.com/A7K2M9QX#clave → ese seed, clave descartada acá
//
// El usuario pega lo que le llegó por Telegram y funciona, sin tener que saber
// cuál es la correcta.
//
// # Un código sin seed se RECHAZA, y antes valía
//
// `A7K2M9QX` a secas devuelve [ErrSeedMissing]. Un invite ID solo significa algo
// en el registro que lo emitió, así que sin seed no identifica una sala:
// identifica ocho caracteres que existen en tantas salas como registros haya.
//
// Antes caía al seed compilado, y eso convertía el caso peor en el silencioso:
// pegar el código de un amigo que hospeda en su propio servidor **entraba a otra
// sala** con el mismo ID, sin un solo error. Fallar acá es lo que lo hace
// visible, y el mensaje puede enseñar la forma completa porque el centinela es
// propio.
//
// Es la frontera de entrada hostil del producto. No interpreta rutas, no
// acepta argumentos, no sigue redirecciones y no adivina: cualquier cosa que
// no calce con una de las formas —incluida la barra vacía con que Windows
// canonicaliza el esquema propio— se rechaza entera.
func ParseRoom(input string) (Room, error) {
	// El tope va primero, antes de tocar el contenido.
	if len(input) > MaxInputLen {
		return Room{}, fmt.Errorf("%w, llegaron %d", ErrInputTooLong, len(input))
	}
	s := strings.TrimSpace(input)
	if s == "" {
		return Room{}, ErrInputShape
	}

	// El fragmento se descarta ANTES de mirar la forma, y no dentro de una de
	// las ramas. Lleva la clave con que la PÁGINA descifra la tarjeta de
	// presentación: a la app no le sirve para nada, el nombre de la sala lo
	// recibe del host por el canal de control. Recortarlo solo en la forma con
	// barra hacía que `kanpachi://A7K2M9QX#clave`, que es exactamente lo que
	// produce el botón de la página, se rechazara por tener un carácter que no
	// existe en el alfabeto.
	if i := strings.IndexByte(s, '#'); i >= 0 {
		s = s[:i]
	}

	// Los esquemas se quitan por delante. Se comparan en minúsculas porque un
	// esquema es insensible a mayúsculas.
	customScheme := false
	for _, scheme := range []string{"kanpachi://", "https://", "http://"} {
		if len(s) >= len(scheme) && strings.EqualFold(s[:len(scheme)], scheme) {
			s = s[len(scheme):]
			customScheme = scheme == "kanpachi://"
			break
		}
	}

	// Chromium/Windows convierte la autoridad sin ruta
	// `kanpachi://A7K2M9QX#clave` en `kanpachi://A7K2M9QX/#clave` antes de
	// entregársela al manejador registrado. Esa única barra no es una ruta: es
	// la ruta vacía canonicalizada del URI. Solo se admite para nuestro esquema;
	// en las formas HTTP una barra final sí forma parte de la ruta recibida.
	if customScheme && strings.HasSuffix(s, "/") {
		s = strings.TrimSuffix(s, "/")
	}

	hasAt := strings.Contains(s, "@")
	hasSlash := strings.Contains(s, "/")

	// Las dos formas con host son distintas y excluyentes. Traer las dos
	// marcas a la vez no es ninguna de las seis, así que se descarta en vez de
	// intentar entenderlo.
	if hasAt && hasSlash {
		return Room{}, ErrInputShape
	}

	var idPart, hostPart string
	switch {
	case hasAt:
		// inviteID@host
		parts := strings.Split(s, "@")
		if len(parts) != 2 {
			return Room{}, ErrInputShape
		}
		idPart, hostPart = parts[0], parts[1]
		// Un @ escrito con nada detrás es una entrada rota, y es [ErrSeedHost] y
		// no [ErrSeedMissing]: el seed se intentó escribir y salió mal, que es
		// otra cosa que no haberlo puesto. Un ID truncado a mitad de copiar cae
		// acá.
		if hostPart == "" {
			return Room{}, ErrSeedHost
		}

	case hasSlash:
		// host/inviteID, con el fragmento de la clave de tarjeta opcional
		parts := strings.SplitN(s, "/", 2)
		hostPart, idPart = parts[0], parts[1]
		// Una barra de más significa una ruta, y por este canal no entran
		// rutas. Ver el modelo de amenaza del manejador de protocolo.
		if strings.ContainsAny(idPart, "/?&") {
			return Room{}, ErrInputShape
		}

	default:
		idPart = s
	}

	id, err := ParseInviteID(idPart)
	if err != nil {
		return Room{}, err
	}

	// El ID se valida ANTES que la falta de seed, y ese orden es el que hace útil
	// al centinela. Al revés, `HOLA@` y `A7K2M9QX` contestarían los dos que falta
	// el servidor, y al primero lo que le falta es ser un código.
	if hostPart == "" {
		return Room{}, ErrSeedMissing
	}
	seed, err := parseSeedHost(hostPart)
	if err != nil {
		return Room{}, err
	}
	return Room{InviteID: id, Seed: seed}, nil
}

// InviteURL es la forma que la app GENERA, con el invite ID en la ruta.
//
// En la ruta y no en el fragmento, y eso cambió respecto del diseño anterior.
// El motivo de esconderlo era que el servidor no lo recibiera, y ese argumento
// se apoyaba en que el código era el secreto de la red. Con la decisión 2 dejó
// de serlo, y el registro del seed tiene que conocerlo igual para resolverlo.
// En la ruta la página se renderiza en el servidor, el chat muestra vista
// previa, y la URL se dicta por teléfono sin explicar qué es un `#`.
//
// No incluye la clave de la tarjeta. Esa la agrega quien arma el enlace de
// invitación, que es el único que la tiene. Ver decisión 17.
func (r Room) InviteURL() string {
	return r.Seed + "/" + r.InviteID.String()
}

// NO HAY `UsesDefaultSeed`, y la borró este cambio.
//
// Contestaba si la sala iba por la semilla de Accentio, para que la interfaz
// resaltara el caso contrario. Sin seed por defecto la pregunta no significa
// nada: **todo seed es el de alguien**, y no hay uno que sea el normal.
//
// Se borró en vez de reescribirse contra el seed propio porque **no la llamaba
// nadie**: estaba escrita, probada y muerta, que es el fallo que
// `internal/arch/cableado_test.go` vigila en los adaptadores. Lo que reemplaza a
// resaltar el seed ajeno es el diálogo de confianza, que aparece SIEMPRE y no
// solo cuando el destino es raro.

// ParseOwnSeed lee el registro propio guardado en disco, o lo que alguien
// escriba en la configuración.
//
// # Por qué pasa por la MISMA comprobación que el seed de un código
//
// Porque termina en los mismos dos sitios: el cliente HTTP que pide el código y
// los `--peers` del motor. Que el valor venga de un fichero nuestro en vez de un
// enlace de Telegram no lo hace menos peligroso, y en Windows ese fichero vive
// en un directorio que todos los usuarios de la máquina pueden leer. Un fichero
// de disco merece la misma desconfianza que una entrada hostil, que es lo mismo
// que ya hace `roomFrom` con la sala guardada.
//
// Devuelve cadena vacía sin error cuando no hay nada escrito: no haber
// configurado registro todavía es el estado normal de quien nunca hospedó.
func ParseOwnSeed(raw []byte) (string, error) {
	s := strings.TrimSpace(string(raw))
	if s == "" {
		return "", nil
	}
	if len(s) > maxHostLen {
		return "", ErrSeedHost
	}
	return parseSeedHost(s)
}

// parseSeedHost valida un nombre de host de forma conservadora.
//
// Sin puerto a propósito: ninguna de las formas documentadas lo lleva, el seed
// siempre escucha en 11010, y aceptarlo sería superficie de parseo a cambio de
// nada.
//
// # Es un NOMBRE, jamás una dirección
//
// El host del código lo elige quien manda el código, así que es un destino que
// un desconocido apunta a donde quiera. De acá salen DOS cosas: el cliente HTTP
// que consulta el registro, y los `--peers` con los que arranca el motor. Un
// literal de IP privada o de loopback convertiría a las dos en una forma de
// hacer que el daemon, que corre como SYSTEM, hable con la red de casa del
// usuario o consigo mismo.
//
// Se exige que la última etiqueta lleve al menos una letra, y eso es lo que
// cierra la familia entera de formas legadas de escribir una IP, no solo la
// obvia. El resolver del sistema acepta cosas que `netip.ParseAddr` rechaza:
// `127.1` es 127.0.0.1, `0x7f.0.0.1` también, y las dos pasarían un filtro que
// solo mirara si el texto es una IP bien formada. Ninguna forma de escribir una
// dirección IPv4 termina en una etiqueta con letras, y ningún dominio real
// termina en una etiqueta sin ellas.
//
// **El costo aceptado:** quien hospede su propio seed necesita un nombre, no le
// alcanza con la IP. Un nombre es gratis y esta comprobación cubre a los dos
// consumidores de una vez, así que el cambio vale lo que cuesta.
//
// Esto es la PRIMERA capa. La segunda vive en los adaptadores y es [CheckSeedAddr]:
// un nombre impecable puede resolver a 192.168.1.1, y eso solo se ve al resolver.
func parseSeedHost(h string) (string, error) {
	h = strings.ToLower(strings.TrimSuffix(h, "."))
	if h == "" || len(h) > maxHostLen {
		return "", ErrSeedHost
	}
	// Se exige un punto para que un ID mal pegado no termine interpretándose
	// como un host de una sola etiqueta.
	if !strings.Contains(h, ".") {
		return "", ErrSeedHost
	}
	etiquetas := strings.Split(h, ".")
	for _, label := range etiquetas {
		if label == "" || len(label) > 63 {
			return "", ErrSeedHost
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return "", ErrSeedHost
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			isDigit := c >= '0' && c <= '9'
			isLower := c >= 'a' && c <= 'z'
			if !isDigit && !isLower && c != '-' {
				return "", ErrSeedHost
			}
		}
	}
	if !tieneLetra(etiquetas[len(etiquetas)-1]) {
		return "", fmt.Errorf("%w: %q no es un nombre, y el seed tiene que serlo", ErrSeedHost, h)
	}
	return h, nil
}

func tieneLetra(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 'a' && s[i] <= 'z' {
			return true
		}
	}
	return false
}

// ErrSeedAddrReserved es que el seed resolvió a una dirección que no es de
// internet.
var ErrSeedAddrReserved = errors.New("el servidor del código apunta a una dirección reservada")

// reservados son los rangos a los que el seed NO puede resolver, con el motivo
// por el que cada uno está.
//
// La lista es explícita en vez de delegar entera en los helpers de netip, porque
// dos de las entradas no tienen helper y son justo las que más importan acá: el
// espacio compartido de RFC 6598, que es donde viven las salas de Kanpachi, y el
// endpoint de metadatos de las nubes, que es el destino clásico de este ataque y
// cae dentro del enlace local.
var reservados = []struct {
	red    netip.Prefix
	motivo string
}{
	{netip.MustParsePrefix("0.0.0.0/8"), "esta red"},
	{netip.MustParsePrefix("10.0.0.0/8"), "red privada"},
	{netip.MustParsePrefix("100.64.0.0/10"), "espacio compartido, que es donde viven las salas"},
	{netip.MustParsePrefix("127.0.0.0/8"), "loopback, o sea la propia máquina"},
	{netip.MustParsePrefix("169.254.0.0/16"), "enlace local, incluye el endpoint de metadatos de las nubes"},
	{netip.MustParsePrefix("172.16.0.0/12"), "red privada"},
	{netip.MustParsePrefix("192.0.0.0/24"), "asignaciones de protocolo"},
	{netip.MustParsePrefix("192.0.2.0/24"), "documentación"},
	{netip.MustParsePrefix("192.88.99.0/24"), "anycast de relé 6to4"},
	{netip.MustParsePrefix("192.168.0.0/16"), "red privada"},
	{netip.MustParsePrefix("198.18.0.0/15"), "pruebas de rendimiento"},
	{netip.MustParsePrefix("198.51.100.0/24"), "documentación"},
	{netip.MustParsePrefix("203.0.113.0/24"), "documentación"},
	{netip.MustParsePrefix("240.0.0.0/4"), "reservado, incluye el broadcast"},

	{netip.MustParsePrefix("::1/128"), "loopback"},
	{netip.MustParsePrefix("fc00::/7"), "direcciones locales únicas"},
	{netip.MustParsePrefix("fe80::/10"), "enlace local"},
	{netip.MustParsePrefix("fec0::/10"), "site-local, deprecado y sin helper en netip"},
	{netip.MustParsePrefix("100::/64"), "agujero negro por definición"},
	{netip.MustParsePrefix("2001:db8::/32"), "documentación"},
	{netip.MustParsePrefix("3fff::/20"), "documentación, la ampliación de 2024"},
	{netip.MustParsePrefix("5f00::/16"), "identificadores de segmento de SRv6"},

	// Las cuatro familias de meter una IPv4 DENTRO de una IPv6.
	//
	// Existen justamente para eso, así que sin ellas la tabla de arriba no
	// gobierna nada: los 32 bits bajos de una dirección NAT64 son una IPv4 que
	// elige quien escribe el registro AAAA, y `64:ff9b::192.168.1.1` es la LAN
	// de casa del usuario escrita de otra forma.
	//
	// Se bloquea el prefijo entero en vez de extraer la IPv4 y volver a
	// comprobarla, y es deliberado: un seed que solo se alcanza por un mecanismo
	// de transición no es un seed que este producto quiera, así que la respuesta
	// correcta es la misma en los dos casos y el código que la da es más corto.
	{netip.MustParsePrefix("::/96"), "IPv4 compatible, deprecada, lleva una IPv4 dentro"},
	{netip.MustParsePrefix("64:ff9b::/96"), "NAT64, lleva una IPv4 dentro"},
	{netip.MustParsePrefix("64:ff9b:1::/48"), "NAT64 local, lleva una IPv4 dentro"},
	{netip.MustParsePrefix("2002::/16"), "6to4, lleva una IPv4 dentro y la encapsula hacia ella"},
	{netip.MustParsePrefix("2001::/23"), "asignaciones de protocolo IETF, Teredo entre ellas"},
}

// CheckSeedAddr es la SEGUNDA capa, y la llama el adaptador con lo que resolvió
// el nombre, antes de conectar.
//
// Hace falta aparte de [parseSeedHost] porque un nombre bien formado puede
// apuntar a donde quiera: nada impide registrar un dominio cuyo A apunte a
// 192.168.1.1, y ese es el camino por el que un código fabricado convertiría al
// daemon en un escáner de la red de casa del usuario.
//
// Vive en el dominio y no en cada adaptador porque son al menos dos los que
// tienen que llamarla, el cliente del registro y el motor, y una política
// repetida en dos sitios es una política que un día difiere.
//
// **Se comprueba en CADA uso, no solo la primera vez.** `last-room.json` guarda
// el código con su seed, así que volver a la última sala vuelve a hablarle, y el
// DNS pudo cambiar entre una vez y la otra.
func CheckSeedAddr(a netip.Addr) error {
	if !a.IsValid() {
		return fmt.Errorf("%w: dirección vacía", ErrSeedAddrReserved)
	}

	// Las dos normalizaciones van ANTES de mirar nada, y las dos son necesarias.
	//
	// Unmap desenvuelve `::ffff:a.b.c.d`, que es UNA de las cuatro formas de
	// disfrazar una IPv4. Las otras tres son prefijos y están en la tabla.
	//
	// La zona se quita porque `netip.Prefix.Contains` devuelve false para
	// CUALQUIER dirección zonificada, o sea que `fd00::1%eth0` desactivaba la
	// tabla entera en silencio. Silencio es la palabra: no fallaba, aprobaba.
	a = a.Unmap().WithZone("")

	if a.IsMulticast() || a.IsUnspecified() || a.IsLoopback() || a.IsLinkLocalUnicast() {
		return fmt.Errorf("%w: %s", ErrSeedAddrReserved, a)
	}
	for _, r := range reservados {
		if r.red.Contains(a) {
			return fmt.Errorf("%w: %s cae en %s, %s", ErrSeedAddrReserved, a, r.red, r.motivo)
		}
	}
	return nil
}
