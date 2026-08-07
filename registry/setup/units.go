package setup

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// Las dos units. Se generan desde la configuración en vez de vivir como
// archivos sueltos, para que cambiar un puerto con `kanpseed config` las
// reescriba y no queden dos verdades: una en el JSON y otra en /etc/systemd.

// MemoriaDelRegistroMiB es el techo de memoria del registro, CALCULADO a partir
// de lo que cuesta derivar una identidad de encuentro.
//
// No es un número redondo elegido a ojo, y esa es toda la cuestión. Antes lo
// era: 96 MiB, contra un Argon2id que pide 64 MiB y se ejecuta dos veces por
// sala. El primer POST real en el droplet murió por OOM, systemd reinició, y el
// síntoma visible fue "el servicio se reinicia solo" — un límite mal puesto
// disfrazado de proceso inestable, sin nada en pantalla que apuntara a la causa.
//
// Cuatro veces el coste de una derivación: dos por las dos llamadas seguidas, y
// otro tanto para el heap del registro, el recolector y el mapa de salas. El
// freno de concurrencia de registry.redDeEncuentro es lo que mantiene el pico
// en una derivación a la vez, así que este techo no escala con la carga.
const MemoriaDelRegistroMiB = 4 * domain.ArgonMemoryKiB / 1024

// UnitDelMotor devuelve la unit de easytier-core.
//
// El endurecimiento importa más de lo que parece: este proceso escucha en un
// puerto público y habla con desconocidos. Corre como usuario efímero, sin
// capacidades, sin poder escribir en el disco y sin ver /home.
//
// # Por qué `--secure-mode true`, medido
//
// Sin ella el seed **rechaza a todo invitado**, y con eso ninguna sala tiene
// más de una persona. Un invitado entra con credencial y no con el secreto de
// la red, y una credencial obliga a abrir con un handshake de Noise. El
// servidor solo lo atiende si él mismo tiene modo seguro:
//
//	// easytier/src/peers/peer_conn.rs
//	if self.is_secure_mode_enabled() && hdr.packet_type == PacketType::NoiseHandshakeMsg1 as u8 {
//	    ... noise ...
//	} else if hdr.packet_type == PacketType::HandShake as u8 {
//	    ... el de siempre ...
//	} else {
//	    return Err(...("unexpected packet type during handshake: {}", hdr.packet_type));
//	}
//
// Con el modo seguro apagado caía en el `else` y cerraba la conexión, 236
// veces seguidas en la última medición del 2026-08-07. El invitado se queda sin
// ningún peer, y sin peers el DHCP de EasyTier no reparte dirección, y sin
// dirección no se crea el adaptador: el síntoma que llegaba al usuario era «el
// adaptador kanpachi0 no tomó la dirección en 30s», treinta segundos de espera
// por una conexión rechazada en el primer paquete.
//
// **No rompe a nadie de los que ya entraban.** El host y el vestíbulo abren con
// `PacketType::HandShake`, que sigue teniendo su rama. Y sin clave declarada,
// `process_secure_mode_cfg` genera un par al arrancar: el seed no guarda
// identidad criptográfica entre reinicios y no hace falta que la guarde, porque
// a quien se autentica acá es al invitado y no al servidor.
func UnitDelMotor(c Config) string {
	motor := filepath.Join(DirLib, "easytier-core")
	return fmt.Sprintf(`# Generado por kanpseed init. Los cambios a mano se pierden:
# usa `+"`kanpseed config`"+`, que reescribe esto y recarga systemd.
[Unit]
Description=Kanpachi seed: motor de red (EasyTier)
Documentation=https://github.com/alvarogabrielgomez/kanpachi
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s \
  --listeners tcp://0.0.0.0:%d \
  --listeners udp://0.0.0.0:%d \
  --disable-upnp true \
  --no-tun true \
  --secure-mode true \
  --rpc-portal 127.0.0.1:%d \
  --rpc-portal-whitelist 127.0.0.1 \
  --stun-servers "" \
  --stun-servers-v6 "" \
  --console-log-level info
Restart=always
RestartSec=2s

# Este proceso escucha en un puerto público y habla con desconocidos, así que
# se le quita todo lo que no necesita. Con --no-tun no hay interfaz virtual que
# crear, o sea que no le hace falta NET_ADMIN ni ninguna otra capacidad.
DynamicUser=yes
NoNewPrivileges=yes
CapabilityBoundingSet=
AmbientCapabilities=
ProtectSystem=strict
ProtectHome=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectControlGroups=yes
PrivateTmp=yes
PrivateDevices=yes
RestrictSUIDSGID=yes
RestrictNamespaces=yes
RestrictRealtime=yes
LockPersonality=yes
SystemCallArchitectures=native
# El seed no necesita hablar por ningún otro protocolo.
RestrictAddressFamilies=AF_INET AF_INET6

# El droplet comparte casa con Vaultwarden, Logto y varias bases de datos. Un
# relay sin techo es el único costo variable del proyecto y el único vector de
# abuso, así que el límite no es opcional.
MemoryMax=256M
CPUQuota=50%%

[Install]
WantedBy=multi-user.target
`, motor, c.PuertoMotor, c.PuertoMotor, c.PuertoRPC)
}

// UnitDelRegistro devuelve la unit de kanpseed serve.
//
// Type=notify y WatchdogSec son lo que reemplaza al vigilante externo:
// Restart=always solo actúa cuando el proceso MUERE, y un proceso vivo pero
// colgado se queda colgado. El registro late mientras esté sano, y dejar de
// latir es la señal.
func UnitDelRegistro(c Config) string {
	binario := filepath.Join(DirBin, Binario)
	pagina := filepath.Join(DirLib, "index.html")
	cli := filepath.Join(DirLib, "easytier-cli")
	return fmt.Sprintf(`# Generado por kanpseed init. Los cambios a mano se pierden:
# usa `+"`kanpseed config`"+`, que reescribe esto y recarga systemd.
[Unit]
Description=Kanpachi seed: registro de salas
Documentation=https://github.com/alvarogabrielgomez/kanpachi
After=network-online.target %s
Wants=network-online.target
# BindsTo y no Requires: si el motor se detiene, el registro se detiene con él
# y vuelve con él. Evita quedar sirviendo páginas sin contador para siempre.
BindsTo=%s

[Service]
Type=notify
ExecStart=%s serve \
  --addr 127.0.0.1:%d \
  --page %s \
  --easytier-cli %s \
  --rpc-portal 127.0.0.1:%d
Restart=always
RestartSec=2s

# Recargar es releer la PAGINA, y nada mas. El registro de salas vive en
# memoria: reiniciar para publicar un cambio de la pagina tiraria todas las
# salas registradas, y quien tuviera un enlace repartido se quedaria con un
# codigo que el servidor ya no conoce.
ExecReload=/bin/kill -HUP $MAINPID

# Un proceso vivo pero colgado no lo detecta Restart=always. El registro manda
# WATCHDOG=1 mientras responda, y si deja de hacerlo systemd lo reinicia.
WatchdogSec=30s

# Guarda todo en memoria y no escribe nada, así que puede correr sin casa,
# sin disco escribible y sin ninguna capacidad.
DynamicUser=yes
NoNewPrivileges=yes
CapabilityBoundingSet=
AmbientCapabilities=
ProtectSystem=strict
ProtectHome=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectControlGroups=yes
PrivateTmp=yes
PrivateDevices=yes
RestrictSUIDSGID=yes
RestrictNamespaces=yes
RestrictRealtime=yes
LockPersonality=yes
SystemCallArchitectures=native
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX

MemoryMax=%dM
CPUQuota=25%%

[Install]
WantedBy=multi-user.target
`, UnitMotor, UnitMotor, binario, c.PuertoRegistro, pagina, cli, c.PuertoRPC, MemoriaDelRegistroMiB)
}

// BloqueDeProxy es lo que hay que pegar en el proxy inverso. Se imprime al
// instalar y lo repite `kanpseed nginx`, para no tener que recordar en
// qué puerto quedó.
func BloqueDeProxy(c Config) string {
	dominio := c.Dominio
	if dominio == "" {
		dominio = "kanpachi.tudominio.com"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Nginx Proxy Manager, o el proxy que uses:\n\n")
	fmt.Fprintf(&b, "  Domain Names     %s\n", dominio)
	fmt.Fprintf(&b, "  Scheme           http\n")
	fmt.Fprintf(&b, "  Forward Host     127.0.0.1\n")
	fmt.Fprintf(&b, "  Forward Port     %d        <-- ESTE\n", c.PuertoRegistro)
	fmt.Fprintf(&b, "  SSL              Let's Encrypt, con Force SSL\n\n")
	fmt.Fprintf(&b, "Force SSL no es cosmético: la página usa navigator.clipboard\n")
	fmt.Fprintf(&b, "para el botón de copiar, y esa API solo existe en contexto seguro.\n\n")
	fmt.Fprintf(&b, "En nginx a mano sería:\n\n")
	fmt.Fprintf(&b, "  location / {\n")
	fmt.Fprintf(&b, "      proxy_pass http://127.0.0.1:%d;\n", c.PuertoRegistro)
	fmt.Fprintf(&b, "      proxy_set_header Host $host;\n")
	fmt.Fprintf(&b, "      proxy_set_header X-Forwarded-For $remote_addr;\n")
	fmt.Fprintf(&b, "  }\n\n")
	fmt.Fprintf(&b, "X-Forwarded-For no es opcional: el límite de tasa del registro\n")
	fmt.Fprintf(&b, "cuenta por IP, y sin esa cabecera todas las visitas parecen una sola.\n")
	return b.String()
}
