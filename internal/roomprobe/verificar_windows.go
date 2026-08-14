//go:build windows

package main

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
	kanpachiengine "github.com/accentiostudios/kanpachi/daemon/adapter/engine/kanpachi"
	"github.com/accentiostudios/kanpachi/daemon/adapter/identity"
)

// medidor lleva la cuenta y decide el código de salida del proceso.
//
// El vocabulario es el de las otras sondas del repo —`=== seccion ===`, `OK`,
// `MAL`— para que quien ya leyó una salida de `fwprobe` o de `netcfgprobe`
// entienda esta sin aprender nada nuevo.
type medidor struct {
	e entorno
}

func (m medidor) seccion(t string) { fmt.Printf("\n=== %s ===\n", t) }

func (m medidor) bien(f string, a ...any) { fmt.Printf("  OK   "+f+"\n", a...) }

// mal cuenta, y esa cuenta sale por el código de salida del proceso. Un `MAL`
// que solo se pinta se pierde en cuanto alguien cierra la ventana; además se
// escribe al log, que es lo que se manda por chat.
func (m medidor) mal(f string, a ...any) {
	fmt.Printf("  MAL  "+f+"\n", a...)
	m.e.log.Error("comprobación MAL: " + fmt.Sprintf(f, a...))
	*m.e.fallos++
}

func (m medidor) nota(f string, a ...any) { fmt.Printf("       "+f+"\n", a...) }

func menuVerificaciones(ctx context.Context, e entorno) error {
	const (
		adaptadores = "1. Adaptadores virtuales y sus direcciones"
		compuerta   = "2. Compuerta y huecos del canal de control"
		motor       = "3. Diagnostico de red del motor y los seeds"
		alertas     = "4. Alertas (incluye si el firewall esta apagado)"
		canario     = "5. Canario: comprobar la contencion desde la red"
		sondeo      = "6. Sondear al host desde aqui"
		firmas      = "7. Firmas y libreta de huellas (decision 25)"
		volcado     = "8. Volcar el diagnostico completo al log"
		todo        = "9. Todo lo que aplique"
		volver      = "0. << Volver"
	)

	for {
		e.c.cocido()
		sel, err := elegir("Verificaciones:", []string{
			adaptadores, compuerta, motor, alertas, canario, sondeo, firmas, volcado, todo, volver,
		})
		if err != nil {
			return err
		}
		m := medidor{e: e}
		antes := *e.fallos

		switch sel {
		case volver:
			return nil
		case adaptadores:
			verAdaptadores(m)
		case compuerta:
			verCompuerta(ctx, m)
		case motor:
			verMotor(ctx, m)
		case alertas:
			verAlertas(ctx, m)
		case canario:
			verCanario(ctx, m)
		case sondeo:
			verSondeo(ctx, m)
		case firmas:
			verFirmas(ctx, m)
		case volcado:
			volcarDiagnostico(ctx, e.s, e.log, e.op.seed)
			fmt.Println("\n  Volcado a", LogFile)
		case todo:
			verAdaptadores(m)
			verCompuerta(ctx, m)
			verMotor(ctx, m)
			verAlertas(ctx, m)
			verCanario(ctx, m)
			verFirmas(ctx, m)
			volcarDiagnostico(ctx, e.s, e.log, e.op.seed)
		}

		if d := *e.fallos - antes; d > 0 {
			fmt.Printf("\n=== %d comprobacion(es) MAL ===\n", d)
		}
		esperarEnter()
	}
}

// verAdaptadores mide contra el SISTEMA, no contra lo que la sesión cree.
//
// La comparación es el punto entero: la sesión puede tener una `LocalIP`
// perfecta y el adaptador puede no existir, que es lo que pasa cuando el motor
// se murió y el watchdog aún no lo notó. El síntoma para quien lo sufre es una
// sala que se ve bien y por la que no pasa un paquete.
//
// Corriendo esto ANTES de crear la sala y DESPUÉS de cerrarla se mide la
// transición, que es lo único que prueba algo: un adaptador presente en un
// momento suelto no dice si se creó ahora o si quedó de antes.
func verAdaptadores(m medidor) {
	m.seccion("adaptadores virtuales")
	st := m.e.s.Status()

	ifaces, err := net.Interfaces()
	if err != nil {
		m.mal("no se pudieron leer las interfaces: %v", err)
		return
	}
	dirs := map[string][]string{}
	for _, in := range ifaces {
		if in.Name != kanpachiengine.RoomDevice && in.Name != kanpachiengine.LobbyDevice {
			continue
		}
		if addrs, err := in.Addrs(); err == nil {
			for _, a := range addrs {
				dirs[in.Name] = append(dirs[in.Name], a.String())
			}
		}
		sort.Strings(dirs[in.Name])
		m.nota("%-11s mtu %-6d arriba %-5v %v",
			in.Name, in.MTU, in.Flags&net.FlagUp != 0, dirs[in.Name])
	}

	sala := kanpachiengine.RoomDevice
	_, existe := dirs[sala]
	switch {
	case st.Conn.InRoom() && !existe:
		m.mal("%s no existe y hay una sala abierta: el motor no levanto la red", sala)
	case st.Conn.InRoom() && !tieneLaDireccion(dirs[sala], st.LocalIP):
		m.mal("%s no tiene la direccion %s que la sesion cree tener", sala, st.LocalIP)
	case st.Conn.InRoom():
		m.bien("%s arriba con %s, dentro de %s", sala, st.LocalIP, st.Subnet)
	case existe:
		m.mal("%s sigue existiendo sin sala abierta: quedo de una salida sucia", sala)
	default:
		m.bien("%s no existe, que es lo correcto sin sala", sala)
	}

	if _, hay := dirs[kanpachiengine.LobbyDevice]; hay && st.Role == domain.RoleGuest {
		m.mal("%s sigue arriba en un invitado: el vestibulo se suelta al entrar", kanpachiengine.LobbyDevice)
	}
}

// tieneLaDireccion compara contra lo que devuelve `net`, que da los prefijos en
// forma `a.b.c.d/n`.
func tieneLaDireccion(dirs []string, ip netip.Addr) bool {
	if !ip.IsValid() {
		return false
	}
	for _, d := range dirs {
		if host, _, err := net.ParseCIDR(d); err == nil && host.String() == ip.String() {
			return true
		}
		if d == ip.String() {
			return true
		}
	}
	return false
}

// verCompuerta enseña lo que el sistema tiene PUESTO.
//
// `Applied` en falso con una medición buena significa que Kanpachi pidió abrir
// algo y el firewall no lo tiene abierto, o sea que alguien no va a poder
// entrar.
//
// El veredicto del canal de la sala se saca CONTRASTANDO dos listas: las
// direcciones que este host repartió y las que la regla deja pasar. Una sola
// de las dos no dice nada. Sin nadie dentro no hay regla de sala y eso es lo
// correcto, y llamarlo fallo —como se hizo hasta el 2026-08-08— enseña a
// ignorar la pantalla justo en la comprobación que más importa.
func verCompuerta(ctx context.Context, m medidor) {
	m.seccion("compuerta y huecos")
	st := m.e.s.Status()
	rep := m.e.s.Exposure(ctx)
	if rep.Blind() {
		m.mal("no se pudo medir lo que la maquina tiene puesto: esto es ceguera, no un cero")
		return
	}
	m.nota("compuerta: %s   huecos: %d   medido %s",
		rep.Gate.String(), len(rep.Ports), rep.MeasuredAt.Format("15:04:05"))

	dejaPasar := map[netip.Addr]bool{}
	for _, p := range rep.Ports {
		linea := fmt.Sprintf("%-38s %s %-11s hacia %v %v",
			p.Name, p.Proto.String(), rangoPuertos(p.From, p.To), p.Members, p.Nets)
		if !p.Applied {
			m.mal("PEDIDO Y NO PUESTO  %s", linea)
			continue
		}
		m.bien("%s", linea)
		if p.Control {
			for _, ip := range p.Members {
				dejaPasar[ip] = true
			}
		}
	}
	for _, u := range rep.Unexpected {
		m.mal("regla del grupo de Kanpachi que nadie pidio: %s", u)
	}

	if st.Role != domain.RoleHost || !st.Conn.InRoom() {
		return
	}
	emitidas := m.e.s.IssuedAddresses()
	var sinHueco []netip.Addr
	for _, ip := range emitidas {
		if !dejaPasar[ip] {
			sinHueco = append(sinHueco, ip)
		}
	}
	switch {
	case len(emitidas) == 0:
		m.nota("credenciales repartidas: 0. Todavia no entro nadie, asi que no hay")
		m.nota("regla de sala y es lo correcto: aparece al canjear el primer codigo")
	case len(sinHueco) > 0:
		m.mal("%d de %d direcciones repartidas NO tienen hueco de control: %v",
			len(sinHueco), len(emitidas), sinHueco)
		m.nota("esos invitados se quedan esperando en un dial que nadie contesta.")
		m.nota("Es exactamente el fallo de la v0.1.6")
	default:
		m.bien("las %d direcciones repartidas tienen hueco de control", len(emitidas))
	}
}

// verMotor junta dos cosas que se leen juntas y se miden por separado: lo que
// el motor sabe de esta máquina, y si desde acá se llega al seed.
//
// Lo segundo NO se le pregunta al motor. Ver [medirSeed].
func verMotor(ctx context.Context, m medidor) {
	m.seccion("red de esta maquina, segun el motor")
	// Sin sala no hay motor corriendo, y preguntarle da "there is no room
	// running". Eso no es un fallo: es la respuesta correcta. Contarlo como
	// `MAL` fue lo que hizo que un invitado que no pudo entrar cerrara con dos
	// fallos, uno de ellos inventado.
	if !m.e.s.Status().Conn.InRoom() {
		m.nota("no hay sala abierta, asi que no hay motor al que preguntar")
	} else if chk, err := m.e.s.Diagnose(ctx); err != nil {
		m.mal("el motor no contesto: %v", err)
	} else {
		verDiagnostico(m, chk)
	}

	m.seccion("camino hasta el seed, medido desde aqui")
	rs, err := medirSeed(ctx, m.e.op.seed)
	if err != nil {
		m.mal("%s no se pudo ni resolver: %v", m.e.op.seed, err)
		return
	}
	for _, r := range rs {
		if r.Err != nil {
			m.mal("%s:%d no contesto: %v", r.Dir, kanpachiengine.SeedPort, r.Err)
			continue
		}
		m.bien("%s:%d contesto en %s", r.Dir, kanpachiengine.SeedPort, r.RTT.Round(time.Millisecond))
	}
	if !alguienContesto(rs) {
		m.nota("sin seed no hay por donde encontrarse, salvo que las dos")
		m.nota("maquinas esten en la misma LAN")
	}
}

func verDiagnostico(m medidor, chk domain.NetCheck) {
	m.nota("nat %s   udp bloqueado %v   mtu %d", chk.NATKind, chk.UDPBlocked, chk.MTU)
	if chk.Subnet.IsValid() {
		m.nota("subred %s (%s)", chk.Subnet, chk.SubnetReason)
	}
	if chk.UDPBlocked {
		m.mal("el UDP de salida parece bloqueado: no habra camino directo")
	}
}

// verAlertas corre el módulo de exposición entero, que es lo que descubre el
// firewall apagado y las reglas alteradas. Por eso no hay una comprobación
// aparte para el firewall: sería preguntar dos veces lo mismo.
func verAlertas(ctx context.Context, m medidor) {
	m.seccion("alertas del modulo de exposicion")
	st := m.e.s.RefreshAlerts(ctx)
	if len(st.Alerts) == 0 {
		m.bien("ninguna")
		return
	}
	for _, a := range st.Alerts {
		m.mal("%s", a.Detail)
	}
}

func verCanario(ctx context.Context, m medidor) {
	m.seccion("canario")
	st := m.e.s.Status()
	switch {
	case st.Role != domain.RoleHost:
		m.nota("solo lo corre el host: es su compuerta la que se comprueba")
		return
	case len(st.Peers) < 2:
		m.nota("hace falta alguien mas en la sala: el canario lo marca otro, no uno mismo")
		return
	}
	// Puede solaparse con una ronda del supervisor, que tiene su propio
	// single-flight y no ve este. En ese caso una de las dos abre un socket de
	// más durante diez segundos y ninguna miente. Coordinarlas exigiría tocar
	// el supervisor, que es código de producto.
	fmt.Println("  ...abriendo el canario y esperando hasta diez segundos")
	c := m.e.s.RunCanaryRound(ctx, false)
	if c.Blind() {
		m.nota("no se llego a comprobar")
		return
	}
	m.nota("puerto %d, se pregunto a %d, contestaron %d", c.Port, len(c.Asked), len(c.Answers))
	switch c.Verdict() {
	case domain.CanaryLeaking:
		m.mal("%s: alguien alcanzo el canario, la compuerta no contiene", c.Verdict())
	case domain.CanaryClean:
		m.bien("%s", c.Verdict())
	default:
		// "Sin confirmar" no dice nada de la compuerta, así que no es un fallo.
		m.nota("%s", c.Verdict())
	}
}

func verSondeo(ctx context.Context, m medidor) {
	m.seccion("sondeo al host")
	rep, err := m.e.s.ProbeHost(ctx)
	if err != nil {
		m.nota("no se pudo sondear: %v", err)
		m.nota("esto lo tiene que pulsar alguien mas de la sala, no el host")
		return
	}
	m.nota("destino %s, medido %s", rep.Target, rep.MeasuredAt.Format("15:04:05"))
	for _, r := range rep.Results {
		m.nota("%-28s puerto %-6d %v", r.Label, r.Port, r.Outcome)
	}
}

// verFirmas mide la decisión 25 de punta a punta, que son tres preguntas
// distintas y conviene no mezclarlas:
//
//  1. **Esta máquina, ¿con qué cara la ven las demás?** Es la huella de su
//     `identity.key`, lo único que un amigo puede comparar contra lo que su
//     ventana le enseñe.
//  2. **El registro, ¿sirve la firma?** Un seed anterior a este cambio devuelve
//     la tarjeta sin `sig`, y entonces todo lo demás se cae a «sin verificar»
//     con razón. Es la primera cosa que hay que descartar cuando el aviso no
//     aparece: el cliente puede estar entero y el servidor no.
//  3. **La libreta, ¿qué recuerda?** Se enseña entera, con la huella de cada
//     entrada, para poder compararla con lo que la otra máquina dice de sí.
func verFirmas(ctx context.Context, m medidor) {
	m.seccion("Firmas y libreta de huellas")

	// 1. La propia.
	priv, err := identity.LoadOrCreate(m.e.op.datos, sinACL)
	if err != nil {
		m.mal("no se pudo leer identity.key: %v", err)
	} else {
		pub := priv.Public().(ed25519.PublicKey)
		m.bien("esta máquina firma con la huella %s", domain.Fingerprint(pub))
		m.nota("es lo que ven los que entran a TU sala; compárala con lo que su ventana les enseñe")
	}

	// 2. El registro. Se le pregunta por la sala que haya a mano: la abierta si
	// se hospeda, y si no la última a la que se entró. Sin ninguna de las dos no
	// hay nada que resolver, y eso no es un fallo.
	if code, seed, hay := salaAMano(m.e); !hay {
		m.nota("no hay ninguna sala a mano para resolver: crea una o entra a una y vuelve")
	} else {
		verFirmaDelRegistro(ctx, m, code, seed)
	}

	// 3. La libreta.
	libreta := m.e.s.KnownHosts()
	if len(libreta.Hosts) == 0 {
		m.nota("la libreta está vacía: esta máquina todavía no entró a ninguna sala con firma verificada")
		return
	}
	m.bien("la libreta tiene %d host(s)", len(libreta.Hosts))
	for _, h := range libreta.Hosts {
		m.nota("%-14s %s   salas=%d   visto por última vez %s",
			h.Nick, domain.Fingerprint(h.Key), h.Rooms, h.LastSeen.Format(time.RFC3339))
	}
}

// salaAMano devuelve un código con el que preguntarle al registro.
//
// La sala abierta manda sobre la última: si se está hospedando, lo interesante
// es qué está sirviendo el registro AHORA de la propia sala, que es lo que ven
// los invitados.
func salaAMano(e entorno) (domain.InviteID, string, bool) {
	st := e.s.Status()
	if st.Conn.InRoom() && !st.Room.InviteID.IsZero() {
		return st.Room.InviteID, st.Room.Seed, true
	}
	if last, ok := e.s.LastRoom(); ok {
		return last.Room.InviteID, last.Room.Seed, true
	}
	return domain.InviteID{}, "", false
}

// verFirmaDelRegistro pregunta por un código y cuenta qué vino, campo a campo.
//
// Va por el MISMO adaptador que el producto, así que lo que se lee acá es lo que
// leería la app: si aquí falta la firma, allá también falta.
func verFirmaDelRegistro(ctx context.Context, m medidor, code domain.InviteID, seed string) {
	dir, err := m.e.registros.For(seed)
	if err != nil {
		m.mal("no se pudo abrir el registro %s: %v", seed, err)
		return
	}
	plazo, fin := context.WithTimeout(ctx, plazoSeed)
	defer fin()
	vista, err := dir.Lookup(plazo, code)
	if err != nil {
		m.mal("%s no resolvió %s: %v", seed, code, err)
		return
	}
	m.bien("%s resolvió %s: tarjeta de %d bytes", seed, code, len(vista.Sealed))
	switch {
	case len(vista.HostKey) == 0:
		m.mal("ese registro NO sirve la llave del host: es anterior a la decisión 25 y hay que actualizarlo")
	case len(vista.Sig) == 0:
		m.mal("ese registro sirve la llave pero NO la firma: es anterior a este cambio y hay que actualizarlo")
		m.nota("consecuencia: los clientes tratan la tarjeta como «sin verificar» y el aviso de huella nunca aparece")
	default:
		m.bien("sirve llave y firma; huella de la llave fijada: %s", domain.Fingerprint(vista.HostKey))
	}
	switch vista.Trust() {
	case domain.CardSigned:
		m.bien("la tarjeta VALIDA contra la llave que ese registro fijó")
	case domain.CardForged:
		m.mal("la tarjeta NO valida contra la llave fijada: ese registro sirve algo que su propia llave no respalda")
	default:
		m.nota("no hay con qué comprobar la tarjeta, así que queda «sin verificar»")
	}
	if v, h := m.e.s.KnownHosts().Judge(vista.HostKey, ""); v != domain.HostUnverified {
		m.nota("la libreta dice: %s%s", v.String(), sufijoDeLibreta(h))
	}
}

func sufijoDeLibreta(h domain.KnownHost) string {
	if h.Nick == "" {
		return ""
	}
	return fmt.Sprintf(" (%s, %d sala(s))", h.Nick, h.Rooms)
}
