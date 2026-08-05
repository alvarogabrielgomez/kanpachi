package domain

import "net/netip"

// Los nombres de los dos adaptadores virtuales.
//
// Los crea el MOTOR, uno por red, al levantar cada una, y mueren con ella: sin
// sala abierta no existe ninguno de los dos. Está medido, y este comentario
// decía antes que los creaba el instalador, que es falso.
//
// Son dos porque el host vive en dos redes a la vez, la sala y el vestíbulo.
// Los nombra el DOMINIO y no el motor, y esa dirección es de seguridad: la
// compuerta de WFP se acota a un adaptador POR NOMBRE, así que un motor que
// eligiera el suyo podría devolver uno que la compuerta no cubre, o sea una red
// virtual abierta sin nada conteniéndola.
//
// Son literales en todo el proyecto: docs, código y logs dicen lo mismo.
const (
	AdapterName      = "kanpachi0"
	LobbyAdapterName = "kanpachi1"
)

// Métricas del adaptador.
//
// La IPv4 en 1 para que los juegos prefieran la red virtual sobre la LAN o el
// WiFi. La IPv6 en 20, deprioritizada, para que no compita con el IPv6 nativo
// del usuario: Kanpachi no enruta internet y no tiene por qué ganarle a la
// pila que sí lo hace.
const (
	MetricIPv4 = 1
	MetricIPv6 = 20
)

// Los límites del MTU del túnel, y de dónde salen.
//
// # El síntoma que esto evita
//
// Es cruel para un juego y no se parece a un problema de red: el túnel levanta,
// el ping anda, la partida conecta, y el mundo no termina de cargar. Los
// paquetes chicos pasan y los grandes desaparecen en silencio, porque el ICMP
// que avisaría del tamaño va filtrado en algún salto del camino.
//
// # Los números NO son de nuestra cosecha
//
// `TunnelOverhead` sale de la aritmética del propio motor, para que en el caso
// común los dos escriban lo mismo y no se peleen. El motor arranca de 1380 y le
// resta 20 más cuando el cifrado está encendido, que en Kanpachi lo está
// siempre. Sobre un camino de 1500, que es lo que asume ese 1380, son
// 120 + 20 = 140 bytes, y el resultado da 1360: exactamente lo que el motor
// habría puesto solo. Así, sondear solo cambia algo cuando el camino de verdad
// es más chico, que es justo para lo que existe.
//
// El techo es ese mismo 1380 y el suelo es el mínimo que exige IPv6.
const (
	TunnelOverhead = 140
	MinTunnelMTU   = 1280
	MaxTunnelMTU   = 1380
)

// TunnelMTU traduce el MTU medido del CAMINO al que le toca al adaptador.
//
// Un camino sin sondear, o sea cero, devuelve cero: quien llama tiene que dejar
// el valor que haya en vez de escribir uno inventado. Escribir un cero apagaría
// la interfaz, y escribir el máximo a ciegas es justo el agujero negro que esto
// existe para evitar.
func TunnelMTU(path int) int {
	if path <= 0 {
		return 0
	}
	mtu := path - TunnelOverhead
	if mtu > MaxTunnelMTU {
		mtu = MaxTunnelMTU
	}
	if mtu < MinTunnelMTU {
		mtu = MinTunnelMTU
	}
	return mtu
}

// AdapterState es el estado que netcfg mantiene contra la voluntad de Windows.
//
// "Mantiene" y no "aplica": Windows revierte la métrica, la categoría de red y
// las rutas en cada evento de identificación de red, que se dispara al agregar
// o quitar una IP, al conectar o desconectar un adaptador y en eventos de
// DHCP. Aplicar esto una vez durante la instalación no alcanza, y por eso el
// supervisor se suscribe al canal NetworkProfile/Operational y lo reaplica
// entero. Sin eso, los ajustes se pierden solos y el usuario ve que "ayer
// funcionaba".
//
// Es declarativo por el mismo motivo que el RuleSet: se compara el deseado
// contra el aplicado y se ejecuta la diferencia.
type AdapterState struct {
	// Address es la IP virtual de esta máquina, dentro de Subnet.
	Address netip.Addr
	Subnet  netip.Prefix

	MetricIPv4 int
	MetricIPv6 int

	// PrivateCategory pide clasificar la red como Privada. Kanpachi intenta
	// fijarlo y NO depende de lograrlo: por eso las reglas se aplican a los
	// tres perfiles de firewall.
	PrivateCategory bool

	// MTU sondeado del camino. Cero significa "todavía no se sondeó" y netcfg
	// deja el valor que haya en vez de escribir un cero, que apagaría la
	// interfaz.
	MTU int

	// Los tres siguientes los pide el perfil del juego activo, no la sala, y
	// se revierten al salir. El estado previo se persiste igual que las reglas
	// ajenas, para poder restaurar tras una salida sucia.
	BroadcastRoute bool
	MulticastRoute bool
	PreferIPv4     bool
}

// AdapterStateFor arma el estado deseado del adaptador para una sala.
//
// Los ajustes por juego salen del perfil activo y se apagan solos cuando no
// hay juego, que es lo que hace que salir de la sala los revierta sin que
// nadie tenga que acordarse de deshacerlos uno por uno.
//
// DirectPlay no está acá a propósito: no es un ajuste del adaptador, es un
// componente opcional de Windows, y mezclarlo con las rutas haría que un
// reaplicado del adaptador tuviera que tocar la instalación de características
// del sistema en cada evento de identificación de red.
func AdapterStateFor(addr netip.Addr, subnet netip.Prefix, mtu int, game GameProfile) AdapterState {
	return AdapterState{
		Address:         addr,
		Subnet:          subnet,
		MetricIPv4:      MetricIPv4,
		MetricIPv6:      MetricIPv6,
		PrivateCategory: true,
		MTU:             mtu,
		BroadcastRoute:  game.Tweaks.BroadcastRoute,
		MulticastRoute:  game.Tweaks.MulticastRoute,
		PreferIPv4:      game.Tweaks.PreferIPv4,
	}
}
