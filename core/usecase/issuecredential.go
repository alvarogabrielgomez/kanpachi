package usecase

import (
	"bytes"
	"context"
	"fmt"
	"net/netip"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/timing"
)

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
// **Esto depende de que la lista de miembros sea correcta**, y hasta el
// 2026-08-13 no lo era: contaba de menos en el host, así que un miembro
// invisible dejaba de renovarse y se caía a las 24 h. Lo cierra
// [Session.withAdmittedLocked], que suma a quien tiene el canal de la sala
// abierto.
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
		// Una revocada no se renueva jamás: el motor ya la rechazó, y contarla
		// como fallida escribiría una advertencia por ronda sobre alguien que
		// el host echó a propósito. Su entrada muere sola por su vencimiento
		// acortado. Ver [Session.KickMember].
		if c.Revoked {
			continue
		}
		if !presentes[ip] && ahora.Sub(c.IssuedAt) >= timing.ArrivalGrace {
			ausentes++
			continue
		}
		vence, err := s.deps.Engine.RenewCredential(ctx, c.ID, timing.CredentialTTL)
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
		"la primera vence", primero, "siguiente ronda en", timing.RenewInterval)
}

// withAdmittedLocked le suma a la tabla del motor los miembros que ESTE host
// admitió y que tienen el canal de la sala abierto.
//
// # El defecto que cierra, medido con dos máquinas el 2026-08-13
//
// Host de Windows, invitado de Linux, los dos con binarios del día. El invitado
// contaba dos miembros; el host contaba UNO, solo él mismo. Sostenido treinta
// segundos muestreando cada cinco, o sea que no era retraso. Al mismo tiempo, en
// el host había un socket `Established 10.99.186.1 → 10.99.186.2` y una regla de
// firewall `toward 10.99.186.2`: las dos sabían del invitado con su dirección.
//
// **La consecuencia no era cosmética.** Los puertos del juego se abren hacia los
// miembros presentes, y los miembros presentes salían solo de la tabla del
// motor. Con Counter-Strike activo, la exposición del host tenía dos reglas y
// las dos eran del canal de control: **ninguna del juego**. O sea que un
// invitado conectado, al que el host ya le había abierto el canal, no podía
// jugar.
//
// # Por qué el canal de control puede decidir esto
//
// Porque el host tiene conocimiento de primera mano y la tabla del motor es de
// segunda. La dirección la asignó él al emitir la credencial, su oyente solo
// acepta a las direcciones autorizadas, y lo que se comprueba es que hay un
// socket abierto desde ahí. Ver [port.ControlChannel.ConnectedMembers].
//
// Ya se le confiaba MÁS que esto: la misma pareja de fuentes decide desde
// siempre a quién se le abre el canal de control, que es el puerto que escucha
// código corriendo como SYSTEM. Abrirle además el puerto de un juego a quien ya
// puede hablarle a eso no amplía ninguna confianza; lo que hace es dejar de
// contestar dos cosas distintas a la misma pregunta en la misma función, cinco
// líneas más abajo. Ver [Session.desiredRuleSetLocked].
//
// # Los tres filtros, y qué caso cierra cada uno
//
//   - **Ya reportado por el motor**: se salta, para no duplicar a nadie. Manda
//     lo que dice el motor, que además trae el camino y el RTT de verdad.
//   - **Credencial vencida**: se salta. Una credencial vencida ya no autoriza, y
//     su entrada se limpia sola en [Session.forgetExpiredCredentialsLocked].
//   - **Expulsado hace poco**: se salta. Es el mismo motivo por el que existe
//     [Session.kicked]: el motor tarda alrededor de un segundo en cerrarle la
//     sesión, y sin esto la relectura de miembros le devolvería el puerto
//     durante esa ventana.
//
// No hace falta un cuarto filtro por "todavía no llegó": quien pidió la
// credencial y no terminó de entrar no tiene el canal de la sala abierto, así
// que no entra por acá. Su puerta la abre [Session.authorizedControlIPsLocked],
// que es lo que le permite llegar a abrirlo.
//
// Asume el candado tomado.
func (s *Session) withAdmittedLocked(peers []domain.Peer) []domain.Peer {
	if !s.state.IsHost() {
		return peers
	}
	conectados := s.deps.Control.ConnectedMembers()
	if len(conectados) == 0 {
		return peers
	}

	now := s.deps.Clock.Now()
	s.forgetOldKicks(now)

	visto := make(map[netip.Addr]bool, len(peers))
	for _, p := range peers {
		visto[p.VirtualIP] = true
	}

	// Se recorre `conectados` y no `issued`, y el orden importa: la lista del
	// canal viene ordenada por dirección, así que la de miembros sale igual en
	// cada relectura. Recorrer el mapa daría un orden distinto cada vez, y con
	// él una firma de reglas distinta cada vez.
	for _, ip := range conectados {
		if visto[ip] {
			continue
		}
		c, ok := s.issued[ip]
		if !ok || c.Expired(now) {
			continue
		}
		if _, kicked := s.kicked[ip]; kicked {
			continue
		}
		visto[ip] = true
		peers = append(peers, domain.Peer{
			VirtualIP: ip,
			// El nombre sale de la credencial, que es donde el host lo guardó al
			// emitirla. El motor lo trae del otro lado; acá no hay otro lado del
			// que traerlo, y sin nombre la lista tendría un hueco.
			Name: c.Name,
			Path: domain.PathUnconfirmed,
		})
	}
	return peers
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
	if len(req.MemberKey) == 0 {
		// La firma la comprobó el adaptador de la puerta; esto es la mitad de
		// core de la misma exigencia, para que un llamador nuevo no pueda
		// emitir credenciales sueltas sin identidad a la que atarlas.
		return domain.Credential{}, fmt.Errorf("el pedido no trae llave de miembro")
	}

	// El que vuelve recibe LO SUYO: misma credencial, misma dirección.
	//
	// Es lo que apaga el gasto de una dirección por vuelta, y lo que ninguna
	// suplantación puede explotar: la llave la probó la firma de la puerta, así
	// que nadie reclama la credencial de otro eligiendo su apodo, que era el
	// argumento entero de no reusar direcciones en [timing.ArrivalGrace].
	if ip, prev, ok := s.credentialByMemberLocked(req.MemberKey); ok {
		vence, err := s.deps.Engine.RenewCredential(ctx, prev.ID, timing.CredentialTTL)
		if err == nil {
			prev.ExpiresAt = vence
			prev.Name = req.Name
			s.issued[ip] = prev
			s.deps.Log.Info("credencial devuelta al que vuelve",
				"nombre", req.Name.String(), "ip", ip.String())
			if err := s.applyPolicy(ctx); err != nil {
				s.deps.Log.Warn("no se pudo pre-autorizar el canal de control en el firewall", "error", err)
			}
			s.restrictControlChannel(ctx)
			return prev, nil
		}
		// El motor ya no la reconoce: revocada o vencida entre dos latidos. La
		// entrada se suelta y el pedido sigue como uno nuevo, que es lo que es.
		s.deps.Log.Warn("la credencial guardada del que vuelve ya no vale en el motor, se emite una nueva",
			"nombre", req.Name.String(), "ip", ip.String(), "error", err)
		delete(s.issued, ip)
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
	cred.ExpiresAt = now.Add(timing.CredentialTTL)
	cred.MemberKey = req.MemberKey

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

// credentialByMemberLocked busca la credencial viva atada a esa llave de
// miembro.
//
// Descarta lo vencido y lo REVOCADO, y el segundo filtro es la mitad de la
// expulsión: un expulsado que vuelve encuentra su entrada revocada, no la
// recupera, y entra como miembro nuevo con dirección nueva. Su entrada vieja
// sigue reteniendo la dirección hasta su vencimiento acortado, para que no se
// reparta mientras la tabla de rutas del motor todavía lo recuerda.
//
// Lineal sobre el mapa y alcanza: una sala son cinco personas, no quinientas.
//
// Asume el candado tomado.
func (s *Session) credentialByMemberLocked(memberKey []byte) (netip.Addr, domain.Credential, bool) {
	now := s.deps.Clock.Now()
	for ip, c := range s.issued {
		if c.Revoked || c.Expired(now) || !bytes.Equal(c.MemberKey, memberKey) {
			continue
		}
		return ip, c, true
	}
	return netip.Addr{}, domain.Credential{}, false
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
