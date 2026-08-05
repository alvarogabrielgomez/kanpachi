// Package kanpachi conduce `kanpachi-engine.exe`, el motor de red propio.
//
// # Qué es el motor y por qué no es `easytier-core.exe`
//
// El binario oficial de EasyTier abre un portal de administración **sin
// autenticación de ninguna clase**, y su default liga todas las interfaces. Por
// ahí cualquier proceso local de la máquina emite credenciales de la red real,
// agrega nodos, reenvía puertos por debajo del cálculo de reglas, y pide el
// secreto de la red en claro. La autenticación se pidió río arriba y se rechazó
// a propósito.
//
// El motor propio consume EasyTier como librería, y con eso el portal
// **desaparece por omisión**: `ApiRpcServer::new` se construye en un solo sitio
// de todo el árbol, dentro de ese binario de línea de comandos, y el arranque
// por librería no lo nombra. No hay bandera que olvidar. Ver decisión 1.
//
// # Por qué este fichero es puro
//
// Todo lo que se decide se decide acá, y lo prueba el CI de Linux. El fichero
// con `//go:build windows` solo lanza el proceso. Mismo corte que
// `adapter/firewall/wfp` y `adapter/firewall/windowscom`.
//
// Lo que se puede equivocar caro en esta capa es el contenido de una orden de
// arranque: una dirección de seed sin comprobar convierte al daemon en un
// escáner de la red de casa del usuario, y un secreto en el sitio equivocado se
// va a los logs.
package kanpachi

import (
	"fmt"
	"net/netip"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// request es una orden hacia el motor, con su identificador.
//
// El `id` es lo que permite multiplexar: el motor contesta con el mismo, y sus
// eventos llegan SIN id. Esa ausencia es lo que distingue una respuesta de un
// aviso, sin tener que adivinarlo por el contenido.
type request struct {
	ID  uint64  `json:"id"`
	Cmd command `json:"cmd"`
}

// command lleva exactamente una de estas, y el motor rechaza el mensaje entero
// si llegan dos o ninguna.
//
// Es una estructura de punteros y no una interfaz porque así el JSON que sale
// se puede afirmar en un test sin montar nada.
type command struct {
	Host             *hostArgs   `json:"host,omitempty"`
	JoinRendezvous   *lobbyArgs  `json:"join_rendezvous,omitempty"`
	LeaveRendezvous  *emptyArgs  `json:"leave_rendezvous,omitempty"`
	Join             *guestArgs  `json:"join,omitempty"`
	Leave            *emptyArgs  `json:"leave,omitempty"`
	IssueCredential  *issueArgs  `json:"issue_credential,omitempty"`
	RevokeCredential *revokeArgs `json:"revoke_credential,omitempty"`
	ListCredentials  *emptyArgs  `json:"list_credentials,omitempty"`
	Peers            *emptyArgs  `json:"peers,omitempty"`
	Diagnostics      *emptyArgs  `json:"diagnostics,omitempty"`
}

type emptyArgs struct{}

// commonArgs es lo que necesita cualquier arranque.
type commonArgs struct {
	DevName  string   `json:"dev_name"`
	Hostname string   `json:"hostname"`
	Peers    []string `json:"peers"`
	MTU      *uint32  `json:"mtu,omitempty"`
}

type hostArgs struct {
	Common        commonArgs `json:"common"`
	NetworkName   string     `json:"network_name"`
	NetworkSecret string     `json:"network_secret"`
	IPv4          string     `json:"ipv4"`
}

type lobbyArgs struct {
	Common        commonArgs `json:"common"`
	NetworkName   string     `json:"network_name"`
	NetworkSecret string     `json:"network_secret"`
	IPv4          string     `json:"ipv4"`
}

type guestArgs struct {
	Common           commonArgs `json:"common"`
	NetworkName      string     `json:"network_name"`
	CredentialSecret string     `json:"credential_secret"`
}

type issueArgs struct {
	TTLSeconds int64 `json:"ttl_seconds"`
}

type revokeArgs struct {
	CredentialID string `json:"credential_id"`
}

// response es lo que el motor contesta a una orden.
type response struct {
	ID    uint64        `json:"id"`
	OK    bool          `json:"ok"`
	Error string        `json:"error,omitempty"`
	Data  *responseData `json:"data,omitempty"`
}

type responseData struct {
	Credential  *credentialOut      `json:"credential,omitempty"`
	Credentials []credentialSummary `json:"credentials,omitempty"`
	Peers       []peerOut           `json:"peers,omitempty"`
	Diagnostics *diagnosticsOut     `json:"diagnostics,omitempty"`
}

type credentialOut struct {
	CredentialID     string `json:"credential_id"`
	CredentialSecret string `json:"credential_secret"`
}

type credentialSummary struct {
	CredentialID string `json:"credential_id"`
	ExpiryUnix   int64  `json:"expiry_unix"`
}

type peerOut struct {
	VirtualIP string `json:"virtual_ip"`
	Hostname  string `json:"hostname"`
	Path      string `json:"path"`
	RTTMs     int32  `json:"rtt_ms"`
}

type diagnosticsOut struct {
	NATKind    string   `json:"nat_kind"`
	UDPBlocked bool     `json:"udp_blocked"`
	PublicIPs  []string `json:"public_ips"`
	VirtualIP  string   `json:"virtual_ip"`
	MTU        uint32   `json:"mtu"`
}

// event es lo que el motor empuja sin que nadie lo pida.
type event struct {
	Event  string `json:"event"`
	Reason string `json:"reason,omitempty"`
}

// incoming es una línea del motor antes de saber qué es.
//
// Se distingue por el `id`: una respuesta lo lleva, un evento no. Por eso el
// campo es un puntero, y por eso no se puede colapsar con `omitempty` del otro
// lado: un `id` cero legítimo tiene que seguir siendo distinguible de su
// ausencia.
type incoming struct {
	ID     *uint64       `json:"id"`
	OK     bool          `json:"ok"`
	Error  string        `json:"error,omitempty"`
	Data   *responseData `json:"data,omitempty"`
	Event  string        `json:"event,omitempty"`
	Reason string        `json:"reason,omitempty"`
}

// RoomDevice y LobbyDevice son los nombres de los dos adaptadores virtuales.
//
// Son dos porque el host está en dos redes a la vez, y cada red del motor trae
// su propio adaptador. Los nombra el daemon y no el motor porque la compuerta
// de WFP se acota a un adaptador POR NOMBRE: si el motor eligiera el suyo,
// podría devolver un adaptador que la compuerta no cubre, o sea una red virtual
// abierta sin nada conteniéndola.
const (
	RoomDevice  = "kanpachi0"
	LobbyDevice = "kanpachi1"
)

// seedURIs resuelve los seeds y devuelve las URI que el motor va a marcar.
//
// # Por qué se resuelve acá y no se le pasa el nombre
//
// `domain.CheckSeedAddr` está escrita y probada desde hace tiempo, y **hasta
// hoy no la llamaba nadie**, porque no existía ningún adaptador. Comprobar no
// alcanza: si al motor se le pasa el nombre, lo resuelve él por su cuenta y la
// comprobación no gobierna nada. Hay que resolver acá, comprobar acá, y
// entregarle la dirección ya elegida.
//
// El caso que esto cierra es concreto: nada impide registrar un dominio cuyo
// registro A apunte a `192.168.1.1`. Un código de invitación fabricado con ese
// seed convertiría al daemon, que corre como SYSTEM, en un escáner de la red de
// casa de quien lo pegue.
//
// Se comprueba en CADA uso y no solo la primera vez, porque `last-room.json`
// guarda el código con su seed y volver a la última sala vuelve a marcarlo, con
// un DNS que pudo cambiar entre una vez y la otra.
func seedURIs(seeds []string, resolve func(string) ([]netip.Addr, error)) ([]string, error) {
	if len(seeds) == 0 {
		return nil, fmt.Errorf("no hay ningún seed al que marcar")
	}

	var out []string
	var rejected []string

	for _, host := range seeds {
		addrs, err := resolve(host)
		if err != nil {
			rejected = append(rejected, fmt.Sprintf("%s no resolvió: %v", host, err))
			continue
		}
		if len(addrs) == 0 {
			rejected = append(rejected, fmt.Sprintf("%s no resolvió a ninguna dirección", host))
			continue
		}
		for _, a := range addrs {
			if err := domain.CheckSeedAddr(a); err != nil {
				rejected = append(rejected, fmt.Sprintf("%s → %s: %v", host, a, err))
				continue
			}
			out = append(out, "tcp://"+netip.AddrPortFrom(a, SeedPort).String())
		}
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("ningún seed quedó utilizable: %v", rejected)
	}
	return out, nil
}

// SeedPort es donde escucha el seed, y no se negocia.
//
// El host de un código de invitación es un NOMBRE sin puerto, y eso lo decide
// `domain.parseSeedHost` a propósito: aceptar un puerto sería superficie de
// parseo a cambio de nada, porque el seed siempre escucha acá.
const SeedPort = 11010

// hostRequest arma la orden de crear una sala.
//
// El secreto de la red real viaja acá dentro y en ningún otro sitio. No entra
// en el argv, que es legible con el Administrador de tareas por cualquier
// usuario de la máquina, y no vuelve a `core`.
func hostRequest(id uint64, spec domain.HostSpec, uris []string) request {
	return request{
		ID: id,
		Cmd: command{Host: &hostArgs{
			Common: commonArgs{
				DevName:  RoomDevice,
				Hostname: spec.Name.String(),
				Peers:    uris,
			},
			NetworkName:   spec.RealNetworkName(),
			NetworkSecret: engineSecret(spec.NetworkSecret),
			IPv4:          hostAddress(spec.Subnet),
		}},
	}
}

// lobbyRequest arma la orden de entrar al vestíbulo.
//
// Va con su propio adaptador porque es OTRA red, no un modo de la sala. El
// invitado tiene que poder soltar el vestíbulo y quedarse en la sala, y con una
// sola red eso sería imposible de expresar.
func lobbyRequest(id uint64, spec domain.RendezvousSpec, uris []string) request {
	return request{
		ID: id,
		Cmd: command{JoinRendezvous: &lobbyArgs{
			Common: commonArgs{
				DevName:  LobbyDevice,
				Hostname: spec.Name.String(),
				Peers:    uris,
			},
			NetworkName:   spec.Rendezvous.NetworkName(),
			NetworkSecret: spec.Rendezvous.EngineSecret(),
			IPv4:          netip.PrefixFrom(spec.Address, domain.RendezvousSubnet.Bits()).String(),
		}},
	}
}

// guestRequest arma la orden de entrar a la sala con credencial.
//
// **No lleva el secreto de la red, y esa ausencia es la decisión 2 entera.** Un
// nodo temporal se autentica con la clave de su credencial y jamás conoce el
// secreto, que es lo que hace que revocar sirva de verdad: quien entró así
// nunca tuvo con qué volver por su cuenta.
//
// Tampoco lleva el `credential_id`, aunque el daemon lo tenga. El motor
// identifica al nodo por la clave pública que deriva del secreto, así que un id
// sería un campo que se manda y se ignora.
func guestRequest(id uint64, spec domain.GuestSpec, uris []string) request {
	return request{
		ID: id,
		Cmd: command{Join: &guestArgs{
			Common: commonArgs{
				DevName:  RoomDevice,
				Hostname: spec.Name.String(),
				Peers:    uris,
			},
			NetworkName:      spec.Credential.NetworkName,
			CredentialSecret: spec.Credential.Token,
		}},
	}
}

// hostAddress es la primera dirección utilizable de la subred de la sala.
//
// El host siempre la `.1`, igual que en el vestíbulo, y por el mismo motivo:
// quien entra tiene que poder marcar a una dirección conocida antes de haber
// hablado con nadie.
func hostAddress(subnet netip.Prefix) string {
	return netip.PrefixFrom(subnet.Masked().Addr().Next(), subnet.Bits()).String()
}

// engineSecret pasa el secreto a la forma que espera el motor.
func engineSecret(s [32]byte) string {
	const hexDigits = "0123456789abcdef"
	out := make([]byte, 0, len(s)*2)
	for _, b := range s {
		out = append(out, hexDigits[b>>4], hexDigits[b&0x0f])
	}
	return string(out)
}

// toPeers traduce lo que contestó el motor a lo que entiende el dominio.
//
// Un `path` desconocido es un ERROR y no un valor por defecto. El conjunto es
// cerrado de los dos lados, así que uno que no esté acá significa que el motor
// y el daemon se separaron, y elegir un default silencioso pintaría de
// "directo" un camino que a lo mejor va por relay.
func toPeers(raw []peerOut) ([]domain.Peer, error) {
	out := make([]domain.Peer, 0, len(raw))
	for _, p := range raw {
		var kind domain.PathKind
		switch p.Path {
		case "direct":
			kind = domain.PathDirect
		case "relay":
			kind = domain.PathRelay
		case "self":
			kind = domain.PathSelf
		default:
			return nil, fmt.Errorf("el motor reportó un camino que este daemon no conoce: %q", p.Path)
		}

		// La dirección vacía se rechaza igual que la inválida, y a propósito.
		//
		// Antes se dejaba pasar y quedaba una dirección cero, que se pinta como
		// "invalid IP" en la pantalla y no sirve para nada de lo que se hace con
		// un miembro: las reglas del firewall se abren HACIA su IP y el expulsar
		// se pide POR su IP. Un miembro sin dirección no es un miembro, es un
		// hueco con nombre.
		//
		// Así apareció el seed en la lista: el motor lo reportaba como miembro
		// porque relevaba para la sala, sin vivir en su espacio de direcciones.
		// El motor ya no lo manda, y esto es la red que lo hace visible si
		// vuelve a pasar, en vez de un hueco silencioso.
		ip, err := netip.ParseAddr(p.VirtualIP)
		if err != nil {
			return nil, fmt.Errorf("el motor reportó la dirección %q, que no es una dirección: %w", p.VirtualIP, err)
		}

		// Un nombre que no pasa la validación se queda VACÍO, y no rompe la
		// lista entera.
		//
		// Las dos mitades importan. El nombre lo elige cada máquina y llega por
		// la red, así que es texto de otra persona a punto de pintarse en la
		// pantalla de todo el mundo: `ParseNickname` es lo que impide que ahí
		// entre un espacio, un carácter de control, o un alfabeto con el que
		// dibujar un nombre visualmente idéntico al de otro miembro. Y devolver
		// error dejaría a la sala sin lista de miembros porque UNA persona puso
		// algo raro, que es convertir una molestia en una caída.
		name, err := domain.ParseNickname(p.Hostname)
		if err != nil {
			name = domain.Nickname{}
		}

		out = append(out, domain.Peer{
			VirtualIP: ip,
			Name:      name,
			Path:      kind,
			Self:      kind == domain.PathSelf,
		})
	}
	return out, nil
}

// toEventKind traduce un evento del motor.
//
// `died` no está y no puede estar: el motor no puede avisar de su propia
// muerte. Ese evento lo levanta este adaptador cuando el proceso hijo termina.
func toEventKind(name string) (domain.EngineEventKind, error) {
	switch name {
	case "connected":
		return domain.EngineConnected, nil
	case "peers_changed":
		return domain.EnginePeersChanged, nil
	case "degraded":
		return domain.EngineDegraded, nil
	case "disconnected":
		return domain.EngineDisconnected, nil
	default:
		return 0, fmt.Errorf("el motor empujó un evento que este daemon no maneja: %q", name)
	}
}
