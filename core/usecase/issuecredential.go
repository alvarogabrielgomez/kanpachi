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

	taken, err := s.takenAddressesLocked(ctx)
	if err != nil {
		return domain.Credential{}, err
	}
	ip, err := nextFreeAddress(s.state.Subnet, taken)
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
// Asume el candado tomado.
func (s *Session) takenAddressesLocked(ctx context.Context) (map[netip.Addr]bool, error) {
	taken := map[netip.Addr]bool{
		// La red y el broadcast del /24 no son de nadie, y la .1 es del host.
		s.state.Subnet.Addr():              true,
		domain.HostAddress(s.state.Subnet): true,
		lastAddress(s.state.Subnet):        true,
	}
	for _, p := range s.state.Peers {
		taken[p.VirtualIP] = true
	}

	creds, err := s.deps.Engine.ListCredentials(ctx)
	if err != nil {
		return nil, fmt.Errorf("consultando las credenciales emitidas: %w", err)
	}
	now := s.deps.Clock.Now()
	for _, c := range creds {
		// Una vencida ya no autoriza a nadie, así que su dirección vuelve al
		// bote. Sin esto, una sala de mucho uso se quedaría sin direcciones
		// libres teniendo el /24 vacío.
		if c.Expired(now) {
			continue
		}
		taken[c.VirtualIP] = true
	}
	return taken, nil
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
