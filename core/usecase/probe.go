package usecase

import (
	"context"
	"errors"
	"net/netip"
	"sync"

	"github.com/accentiostudios/kanpachi/core/domain"
)

var (
	// ErrProbeSelf es pedir el sondeo siendo el host.
	//
	// No es una limitación de implementación: marcarse a uno mismo no atraviesa
	// ningún firewall. El tráfico a la propia dirección no sale por el
	// adaptador y no pasa por [FWPM_LAYER_ALE_AUTH_RECV_ACCEPT_V4], así que
	// contestaría que todo está abierto en una máquina blindada. Un sondeo local
	// sería un generador de sustos.
	ErrProbeSelf = errors.New("esto lo tiene que pulsar alguien más de la sala")
	// ErrProbeNoHost es que no se sabe la dirección del host. Pasa mientras el
	// ingreso está a medias y cuando el host se fue.
	ErrProbeNoHost = errors.New("no se sabe dónde está el host para poder marcarle")
)

// probeConcurrency es cuántos puertos se marcan a la vez.
//
// La lista son unas dos docenas y el plazo de cada uno son [timing.ProbeDeadline]
// segundos, así que en serie el usuario esperaría más de un minuto mirando un
// botón. Con esto son dos tandas.
//
// No es más alto porque cada marcado es una conexión TCP saliente a la misma
// máquina, y abrir las veinticuatro de golpe se parece más a un escaneo que a
// una comprobación. Ver [domain.ProbeTargets] para el mismo criterio del lado de
// la lista.
const probeConcurrency = 16

// ProbeHost marca los puertos del host desde ESTA máquina, que es la única
// medición del producto que atraviesa la red de verdad.
//
// # Por qué solo el invitado, y solo contra el host
//
// Porque hace falta una REFERENCIA viva, y el único puerto que se sabe abierto
// en una sala es el canal de la sala, que solo escucha en el host. Sin una
// respuesta que llegue, "no contesta nada" se lee igual con la máquina blindada
// que con la máquina apagada, y la pantalla estaría afirmando lo que no sabe.
//
// Sondear a un invitado daría silencio en todo, que es a la vez lo que tiene que
// pasar y lo que pasa con la PC apagada. Es una medición que no puede fallar, o
// sea que no mide.
//
// # Lo que esto le da al host, que es quien más lo necesita
//
// La frase de vuelta. El host no puede correrlo contra sí mismo, y su pantalla
// se lo dice con esas palabras: quien pulsa es otro, y le cuenta. Que el
// resultado viaje de una persona a otra es una limitación de esta versión y está
// escrita como tal, no disfrazada de decisión.
//
// # Por qué el candado se suelta antes de marcar
//
// Por lo mismo que en [Session.Exposure], y acá pesa más: esto tarda segundos
// contra la red, y sostener el candado dejaría cualquier `Status` de la UI
// esperando todo ese rato.
func (s *Session) ProbeHost(ctx context.Context) (domain.ProbeReport, error) {
	s.mu.Lock()
	if !s.state.Conn.InRoom() {
		s.mu.Unlock()
		return domain.ProbeReport{}, ErrNoRoom
	}
	if s.state.IsHost() {
		s.mu.Unlock()
		return domain.ProbeReport{}, ErrProbeSelf
	}
	host, ok := s.state.HostPeer()
	if !ok || !host.VirtualIP.IsValid() {
		s.mu.Unlock()
		return domain.ProbeReport{}, ErrProbeNoHost
	}
	targets := domain.ProbeTargets(s.state.Game)
	s.mu.Unlock()

	results := s.probeAll(ctx, host.VirtualIP, targets)

	r := domain.ProbeReport{
		Target:     host.VirtualIP,
		Name:       host.Name,
		MeasuredAt: s.deps.Clock.Now(),
		Results:    results,
	}
	s.deps.Log.Info("sondeo desde otra máquina",
		"a", host.VirtualIP, "puertos", len(results), "veredicto", r.Verdict())
	return r, nil
}

// probeAll marca todos los objetivos y conserva el ORDEN de la lista.
//
// El orden se conserva escribiendo cada resultado en su posición y no
// acumulándolos según terminan: la lista que entra viene ordenada por puerto
// desde el dominio, y una pantalla que reordenara sola según qué puerto
// contestó primero cambiaría en cada corrida.
func (s *Session) probeAll(ctx context.Context, at netip.Addr, targets []domain.ProbeTarget) []domain.ProbeResult {
	out := make([]domain.ProbeResult, len(targets))
	sem := make(chan struct{}, probeConcurrency)

	var wg sync.WaitGroup
	for i, t := range targets {
		wg.Add(1)
		go func(i int, t domain.ProbeTarget) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			outcome, rtt := s.deps.Prober.Probe(ctx, netip.AddrPortFrom(at, t.Port))
			out[i] = domain.ProbeResult{ProbeTarget: t, Outcome: outcome, RTT: rtt}
		}(i, t)
	}
	wg.Wait()
	return out
}
