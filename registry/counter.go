package registry

import (
	"context"
	"encoding/json"
	"os/exec"
	"sync"
	"time"
)

// Counter publica cuánta gente hay en cada red de encuentro.
//
// El dato sale del propio EasyTier, sin cooperación del host y sin unirse a
// nada: `peer list-foreign` sobre el portal RPC devuelve los peers agrupados
// por nombre de red. Verificado contra la v2.6.4 fijada.
//
// El mismo JSON confirma lo que hace falta que sea cierto: ahí NO hay
// hostnames, solo peer_id y direcciones. Los nicks viajan dentro de la red
// cifrada y el seed relaya sin descifrar, así que ni queriendo podría
// publicarlos. Esa es exactamente la razón de que la tarjeta cifrada exista.
type Counter struct {
	cli    string // ruta a easytier-cli
	portal string // el rpc-portal de easytier-core

	mu     sync.RWMutex
	conteo map[string]int
	visto  time.Time
	err    error
}

// NewCounter no habla con nadie todavía. Arranca con Run.
func NewCounter(cli, portal string) *Counter {
	return &Counter{cli: cli, portal: portal, conteo: map[string]int{}}
}

// Run refresca el conteo cada intervalo hasta que el contexto se cancele.
//
// Se sondea en un ticker y se cachea, en vez de invocar el CLI por petición.
// Un `GET /api/i/...` cuesta lo que cuesta leer un mapa, y una avalancha de
// visitas no se traduce en una avalancha de procesos hijos. El precio es que
// el contador puede ir un intervalo por detrás, lo cual para "4 en la sala" no
// significa nada.
func (c *Counter) Run(ctx context.Context, intervalo time.Duration) {
	t := time.NewTicker(intervalo)
	defer t.Stop()
	c.refrescar(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.refrescar(ctx)
		}
	}
}

// For devuelve cuántos peers hay en esa red y si el dato es utilizable.
//
// Devuelve false cuando nunca se pudo hablar con EasyTier, para que la API
// omita el campo en vez de publicar un cero. Un cero es una afirmación
// ("no hay nadie") y sería falsa; ausente es la verdad ("no lo sé").
func (c *Counter) For(red string) (int, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.visto.IsZero() {
		return 0, false
	}
	return c.conteo[red], true
}

// Err devuelve el último fallo al hablar con EasyTier, para el endpoint de
// salud. Es nil cuando el último sondeo funcionó.
func (c *Counter) Err() error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.err
}

// respuesta modela lo justo de `peer list-foreign -o json`. El resto de campos
// (conns, tunnel, stats) se ignoran a propósito: cuanto menos de la forma del
// JSON ajeno dependa este código, menos lo rompe una actualización del motor.
type respuesta map[string]struct {
	Peers []struct {
		PeerID uint64 `json:"peer_id"`
	} `json:"peers"`
}

func (c *Counter) refrescar(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	salida, err := exec.CommandContext(ctx, c.cli, "-p", c.portal, "-o", "json", "peer", "list-foreign").Output()
	if err != nil {
		c.mu.Lock()
		c.err = err
		c.mu.Unlock()
		return
	}

	var r respuesta
	if err := json.Unmarshal(salida, &r); err != nil {
		c.mu.Lock()
		c.err = err
		c.mu.Unlock()
		return
	}

	nuevo := make(map[string]int, len(r))
	for red, v := range r {
		nuevo[red] = len(v.Peers)
	}

	c.mu.Lock()
	c.conteo = nuevo
	c.visto = time.Now()
	c.err = nil
	c.mu.Unlock()
}
