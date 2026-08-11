package usecase

import (
	"context"
	"fmt"
	"net/netip"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// CredentialTTL es cuánto vale una credencial emitida.
//
// Larga a propósito. No es un control de seguridad: lo que saca a alguien de la
// sala es la revocación, que es inmediata y no cooperativa, y lo que cierra la
// puerta es renovar el código. Un vencimiento corto solo lograría echar a
// alguien a mitad de partida por haber entrado temprano.
//
// Existe igual porque una credencial sin fecha es una que sobrevive a la sala,
// y la sala deja de existir cuando se va el último nodo.
const CredentialTTL = 24 * time.Hour

// RenewInterval es cada cuánto el host empuja el vencimiento de las
// credenciales de quienes están dentro.
//
// # Qué arregla, que hasta ahora no lo arreglaba nadie
//
// Nada renovaba, así que a las [CredentialTTL] exactas de haber entrado, el
// motor del host sacaba a cada invitado de su lista de confianza y le cerraba la
// sesión. Uno por uno, sin que nadie hubiera tocado nada. Mientras una sala
// duraba una tarde no se veía; desde que la sala sobrevive al apagado y dura
// semanas, es seguro.
//
// Una hora contra veinticuatro da VEINTICUATRO intentos antes de que un fallo
// importe, que es el mismo criterio y el mismo número que [RepublishInterval]
// contra la ventana del registro. Para que alguien se caiga de verdad, el motor
// tiene que estar sin contestar casi un día entero, y a esa altura hace rato que
// no hay sala.
//
// El plazo se cuenta desde AHORA y no desde el vencimiento anterior, así que
// renovar no acumula: una credencial renovada cada hora vale siempre
// [CredentialTTL] desde la última renovación, nunca más.
const RenewInterval = time.Hour

// ArrivalGrace es cuánto se le da a quien acaba de recibir credencial para
// aparecer en la sala antes de que su ausencia signifique algo.
//
// Existe porque hay una ventana en la que alguien tiene credencial y NO está en
// la tabla de miembros, y no se ha ido: su ingreso está en curso. Entre recibir
// la credencial y aparecer hay una comprobación de subred, una salida del
// vestíbulo, el arranque de la red real, la espera a que el adaptador tome la
// dirección y el marcado del canal de control. Los plazos propios de ese camino
// suman alrededor de minuto y medio, y diez minutos son casi siete veces eso.
//
// Lo único que decide es a quién renueva el latido. Pasarse por arriba cuesta
// renovar de más a alguien que ya se fue, y su credencial vence igual en la
// vuelta siguiente. Quedarse corto cuesta dejar sin renovar a alguien que estaba
// entrando, que es el lado caro.
//
// # Por qué NO se usa para devolverle la dirección a quien vuelve
//
// Porque la única forma de reconocerlo sería el apodo, y el apodo no autentica
// nada: lo elige quien pide. El pedido de credencial lleva además una llave
// EFÍMERA, generada al pedir y descartada al salir, así que este producto no
// tiene con qué reconocer a nadie entre dos ingresos, y eso es deliberado: es lo
// mismo que hace que expulsar no sea banear.
//
// Reusar la dirección exige revocar la credencial anterior, porque si no quedan
// dos vivas para la misma dirección. Con el apodo como llave, eso le da a
// CUALQUIERA con el código la capacidad de tumbarle la credencial a un miembro
// real con solo ponerse su nombre, y le basta con que el host no lo esté viendo
// en ese instante. Con el defecto de conteo abierto, eso convierte un fallo de
// pantalla en echar a alguien.
//
// El precio de no hacerlo es cosmético: quien sale y vuelve recibe otra
// dirección mientras su reserva anterior siga viva. No se acumula, porque el
// latido solo renueva a los presentes y la reserva de un ausente vence sola.
const ArrivalGrace = 10 * time.Minute

// renewCredentialsLocked empuja el vencimiento de los que están dentro.
//
// # Por qué solo a los presentes
//
// Porque una credencial que se renueva sola para siempre es una dirección
// reservada para siempre. `takenAddressesLocked` cuenta ocupada toda credencial
// no vencida, así que renovar a quien ya no está convertiría la reserva de 24 h
// en eterna, y una sala con movimiento se quedaría sin direcciones libres con el
// /24 vacío. Que la de un ausente venza es lo que la libera, y es además la
// definición de estar fuera que ya usa el producto.
//
// La excepción es quien acaba de recibirla y todavía no terminó de entrar: no
// está en la tabla del motor porque su ingreso está en curso, no porque se haya
// ido. Ese caso se reconoce por la fecha de emisión y no por una lista aparte.
//
// **Esto depende de que la lista de miembros sea correcta**, y hay un defecto
// abierto que la hace contar de menos en el host. Mientras ese defecto exista,
// un miembro invisible deja de renovarse y se cae a las 24 h, que es exactamente
// lo que pasa hoy con todos. O sea que esto nunca empeora lo que había, y lo
// arregla del todo cuando la lista deje de mentir.
//
// # Qué se hace cuando el motor dice que no
//
// Nada, y a propósito. Un id que ya no existe es una credencial revocada o
// vencida entre dos latidos, y su entrada se limpia sola cuando pase su fecha,
// en [forgetExpiredCredentialsLocked]. Distinguir ese caso de "el motor no
// contesta" pediría un error tipado que cruzara el adaptador, para decidir entre
// borrar ahora o borrar dentro de un rato.
//
// Asume el candado tomado.
func (s *Session) renewCredentialsLocked(ctx context.Context) {
	// El reloj se sella pase lo que pase, igual que en [Session.announceLocked]:
	// si el motor no contesta, machacarlo en cada latido no lo arregla.
	ahora := s.deps.Clock.Now()
	s.lastRenew = ahora

	presentes := make(map[netip.Addr]bool, len(s.state.Peers))
	for _, p := range s.state.Peers {
		presentes[p.VirtualIP] = true
	}

	renovadas, ausentes, fallidas := 0, 0, 0
	// primero es el vencimiento más cercano que queda tras la ronda, o sea
	// cuánto margen hay de verdad. Es el número que contesta "¿esto está
	// aguantando?" sin tener que deducirlo de que nadie se haya caído.
	var primero time.Time

	for ip, c := range s.issued {
		if c.Expired(ahora) {
			continue
		}
		if !presentes[ip] && ahora.Sub(c.IssuedAt) >= ArrivalGrace {
			ausentes++
			continue
		}
		vence, err := s.deps.Engine.RenewCredential(ctx, c.ID, CredentialTTL)
		if err != nil {
			fallidas++
			s.deps.Log.Warn("no se pudo renovar la credencial de un miembro",
				"nombre", c.Name.String(), "ip", ip.String(), "vence", c.ExpiresAt, "error", err)
			continue
		}
		// La fecha es la que el motor GUARDÓ. Calcularla acá como `ahora + ttl`
		// sería una segunda cuenta del mismo valor, que es cómo el host y el
		// motor terminaron discrepando sobre la dirección del invitado.
		c.ExpiresAt = vence
		s.issued[ip] = c
		renovadas++
		if primero.IsZero() || vence.Before(primero) {
			primero = vence
		}
	}

	// # Por qué esta línea existe, y por qué solo cuando hay algo que contar
	//
	// Porque una renovación que funciona no deja ninguna huella, igual que el
	// anuncio: solo se registra el fallo. Con eso, la única forma de saber que
	// el latido está corriendo es que nadie se caiga, o sea deducir que algo
	// funciona por la ausencia de su síntoma. En una sala de semanas, esa
	// deducción tarda un día en poder hacerse.
	//
	// El silencio cuando no hay credenciales vivas es deliberado. Un host solo
	// en su sala escribiría una línea por hora para decir que no hizo nada, y
	// ese es exactamente el ruido que tapa lo que sí importa. Mismo criterio que
	// [Session.logInvitadosQueNoLleganLocked].
	if renovadas+ausentes+fallidas == 0 {
		return
	}
	s.deps.Log.Info("credenciales renovadas",
		"renovadas", renovadas, "ausentes", ausentes, "fallidas", fallidas,
		"la primera vence", primero, "siguiente ronda en", RenewInterval)
}

// forgetExpiredCredentialsLocked saca del mapa lo que ya no autoriza a nadie.
//
// Sin esto el mapa solo crece: cada persona que entra deja una entrada, y las
// dos cosas que lo recorren, el reparto de direcciones y la pre-autorización del
// canal de control, ya saltan lo vencido pero lo siguen leyendo. Borrarlo es
// además lo que permite reusar la dirección sin cuidados.
//
// Asume el candado tomado.
func (s *Session) forgetExpiredCredentialsLocked(now time.Time) {
	for ip, c := range s.issued {
		if c.Expired(now) {
			delete(s.issued, ip)
		}
	}
}

// IssueCredentialFor es la otra mitad de la puerta: alguien tocó, el host
// decide.
//
// Lo llama el adaptador del canal de control cuando llega un pedido por la
// dirección del vestíbulo. Vive acá y no en el adaptador porque todo lo que
// decide es política: si esta máquina puede emitir, qué dirección le toca al
// que entra, cuánto vale la credencial, y qué se le cuenta de la red.
//
// SOLO el host. Un invitado no tiene con qué emitir, y que el método exista en
// su sesión sin esta comprobación sería una forma de que dos máquinas de la
// misma sala repartieran credenciales distintas.
//
// **Lo que devuelve NO lleva el secreto de la red.** Lleva su nombre, la
// subred, una IP y un token del motor. Ver decisión 2: quien entró nunca tuvo
// con qué volver por su cuenta, y eso es lo que hace que revocar sirva.
func (s *Session) IssueCredentialFor(ctx context.Context, req domain.CredentialRequest) (domain.Credential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.requireHost(); err != nil {
		return domain.Credential{}, err
	}
	if req.Name.IsZero() {
		// El nombre es obligatorio, decisión 21: sin nombres, expulsar a
		// alguien es adivinar. Llega de otra máquina, así que se exige acá y no
		// se confía en que el otro lado lo haya validado.
		return domain.Credential{}, domain.ErrNicknameEmpty
	}

	ip, err := nextFreeAddress(s.state.Subnet, s.takenAddressesLocked())
	if err != nil {
		return domain.Credential{}, err
	}

	// El motor emite el token. Es lo único de la credencial que no decide este
	// código, y tiene que ser así: revocarlo es lo que corta la sesión, y solo
	// quien la abrió puede cerrarla.
	cred, err := s.deps.Engine.IssueCredential(ctx, req)
	if err != nil {
		return domain.Credential{}, fmt.Errorf("emitiendo la credencial para %s: %w", req.Name, err)
	}
	if cred.Token == "" {
		return domain.Credential{}, fmt.Errorf("el motor devolvió una credencial sin token")
	}

	now := s.deps.Clock.Now()
	cred.Name = req.Name
	cred.VirtualIP = ip
	cred.Subnet = s.state.Subnet
	cred.NetworkName = s.hostSpec.RealNetworkName()
	cred.IssuedAt = now
	cred.ExpiresAt = now.Add(CredentialTTL)

	s.issued[ip] = cred

	s.deps.Log.Info("credencial emitida", "nombre", req.Name.String(), "ip", ip.String())

	// Pre-autorizamos el canal de control abriéndolo para esta IP de inmediato, en
	// lugar de esperar a que el motor reporte a la persona como miembro activo.
	if err := s.applyPolicy(ctx); err != nil {
		s.deps.Log.Warn("no se pudo pre-autorizar el canal de control en el firewall", "error", err)
	}
	s.restrictControlChannel(ctx)

	return cred, nil
}

// takenAddressesLocked junta lo que ya está ocupado en la subred de la sala.
//
// Las dos fuentes hacen falta y ninguna sobra. Los peers son quien está
// conectado AHORA; las credenciales emitidas incluyen a quien la pidió hace un
// segundo y todavía no terminó de entrar. Mirar solo los peers repartiría la
// misma dirección dos veces a dos personas que entran a la vez, que es
// exactamente lo que pasa cuando alguien manda el código al grupo.
//
// Las emitidas salen de [Session.issued] y no del motor, por lo mismo que en
// [Session.credentialFor]: el motor no sabe qué dirección lleva cada
// credencial. Preguntárselo devolvía una lista de ceros, o sea que la mitad
// que existe para no repartir dos veces la misma dirección no estaba mirando
// nada.
//
// Asume el candado tomado.
func (s *Session) takenAddressesLocked() map[netip.Addr]bool {
	taken := map[netip.Addr]bool{
		// La red y el broadcast del /24 no son de nadie, y la .1 es del host.
		s.state.Subnet.Addr():              true,
		domain.HostAddress(s.state.Subnet): true,
		lastAddress(s.state.Subnet):        true,
	}
	for _, p := range s.state.Peers {
		taken[p.VirtualIP] = true
	}

	now := s.deps.Clock.Now()
	for ip, c := range s.issued {
		// Una vencida ya no autoriza a nadie, así que su dirección vuelve al
		// bote. Sin esto, una sala de mucho uso se quedaría sin direcciones
		// libres teniendo el /24 vacío.
		if c.Expired(now) {
			continue
		}
		taken[ip] = true
	}
	return taken
}

// nextFreeAddress recorre la subred de menor a mayor y devuelve la primera
// libre.
//
// En orden y no al azar: la lista de miembros se lee a ojo y se dicta por
// teléfono al escribir la IP en el juego, así que direcciones consecutivas son
// más fáciles de manejar que un reparto disperso. La ocupación real es de
// cuatro o cinco personas sobre 253 huecos.
func nextFreeAddress(subnet netip.Prefix, taken map[netip.Addr]bool) (netip.Addr, error) {
	if !subnet.IsValid() {
		return netip.Addr{}, fmt.Errorf("la sala no tiene subred asignada")
	}
	for a := subnet.Addr(); subnet.Contains(a); a = a.Next() {
		if !taken[a] {
			return a, nil
		}
	}
	return netip.Addr{}, fmt.Errorf("la sala %s no tiene direcciones libres", subnet)
}

// lastAddress es el broadcast del prefijo. Se calcula sobre los cuatro bytes
// porque una sala siempre es IPv4: los juegos que este producto existe para
// conectar hablan IPv4 y nada más.
func lastAddress(subnet netip.Prefix) netip.Addr {
	if !subnet.IsValid() || !subnet.Addr().Is4() {
		return netip.Addr{}
	}
	b := subnet.Addr().As4()
	host := 32 - subnet.Bits()
	for i := 0; i < host; i++ {
		b[3-i/8] |= 1 << (i % 8)
	}
	return netip.AddrFrom4(b)
}
