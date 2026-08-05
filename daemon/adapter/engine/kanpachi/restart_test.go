package kanpachi

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/netip"
	"sync"
	"testing"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// hijoDePrueba es un motor de mentira que contesta que sí a todo y ANOTA lo que
// le pidieron.
//
// Existe para poder afirmar lo único que importa de `Restart`, que es QUÉ
// órdenes se repiten. El fallo que congela no se ve leyendo el código: se ve en
// la lista de órdenes, comparada con la que hubo antes de la caída.
type hijoDePrueba struct {
	mu       sync.Mutex
	órdenes  []string
	entrada  *io.PipeWriter
	salida   *io.PipeReader
	haciaEl  *io.PipeReader
	desdeÉl  *io.PipeWriter
	muerto   chan struct{}
	unaVez   sync.Once
	fallando bool
}

func nuevoHijo() *hijoDePrueba {
	haciaEl, entrada := io.Pipe()
	salida, desdeÉl := io.Pipe()
	h := &hijoDePrueba{
		entrada: entrada, salida: salida,
		haciaEl: haciaEl, desdeÉl: desdeÉl,
		muerto: make(chan struct{}),
	}
	go h.atender()
	return h
}

// atender lee órdenes y contesta. Anota el nombre del comando, que es lo que
// permite comparar qué se repitió con qué había antes.
func (h *hijoDePrueba) atender() {
	sc := bufio.NewScanner(h.haciaEl)
	for sc.Scan() {
		var req request
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			continue
		}
		h.mu.Lock()
		h.órdenes = append(h.órdenes, nombreDeOrden(req.Cmd))
		fallando := h.fallando
		h.mu.Unlock()

		resp := response{ID: req.ID, OK: !fallando}
		if fallando {
			resp.Error = "el motor de prueba está fallando a propósito"
		}
		if req.Cmd.Peers != nil {
			resp.Data = &responseData{}
		}
		raw, _ := json.Marshal(resp)
		if _, err := h.desdeÉl.Write(append(raw, '\n')); err != nil {
			return
		}
	}
	h.matar()
}

func nombreDeOrden(c command) string {
	switch {
	case c.Host != nil:
		return "host"
	case c.JoinRendezvous != nil:
		return "vestíbulo"
	case c.LeaveRendezvous != nil:
		return "salir-vestíbulo"
	case c.Join != nil:
		return "invitado"
	case c.Leave != nil:
		return "salir"
	}
	return "otra"
}

func (h *hijoDePrueba) pedidas() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.órdenes...)
}

func (h *hijoDePrueba) Stdin() io.WriteCloser { return h.entrada }
func (h *hijoDePrueba) Stdout() io.Reader     { return h.salida }
func (h *hijoDePrueba) Wait() error           { <-h.muerto; return nil }

func (h *hijoDePrueba) Kill() error {
	h.matar()
	return nil
}

func (h *hijoDePrueba) matar() {
	h.unaVez.Do(func() {
		close(h.muerto)
		_ = h.desdeÉl.Close()
		_ = h.entrada.Close()
	})
}

// motorConHijos arma un motor cuyo `spawn` entrega un hijo nuevo cada vez, y
// devuelve la lista de hijos creados.
func motorConHijos(t *testing.T) (*Engine, func() []*hijoDePrueba) {
	t.Helper()

	var mu sync.Mutex
	var hijos []*hijoDePrueba

	e, err := New(Deps{
		Exe:     "motor-de-prueba.exe",
		Log:     logMudoMotor{},
		Resolve: func(string) ([]netip.Addr, error) { return []netip.Addr{addr("93.184.216.34")}, nil },
		// La dirección aparece de inmediato: lo que se prueba acá es qué
		// órdenes se repiten, no la espera, que ya tiene sus propios tests.
		Addrs: func(string) ([]netip.Addr, error) {
			return []netip.Addr{
				addr("100.64.1.1"),    // el host en la sala
				addr("100.64.1.5"),    // el invitado en la sala
				addr("100.127.255.1"), // el host en el vestíbulo
			}, nil
		},
		spawn: func(context.Context, string) (child, error) {
			h := nuevoHijo()
			mu.Lock()
			hijos = append(hijos, h)
			mu.Unlock()
			return h, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Close() })

	return e, func() []*hijoDePrueba {
		mu.Lock()
		defer mu.Unlock()
		return append([]*hijoDePrueba(nil), hijos...)
	}
}

type logMudoMotor struct{}

func (logMudoMotor) Info(string, ...any)  {}
func (logMudoMotor) Warn(string, ...any)  {}
func (logMudoMotor) Error(string, ...any) {}

func specDeHost() domain.HostSpec {
	return domain.HostSpec{
		NetworkID:     [16]byte{0xa1, 0xb2},
		NetworkSecret: [32]byte{0xde, 0xad, 0xbe, 0xef},
		Name:          domain.Nickname{},
		Subnet:        netip.MustParsePrefix("100.64.1.0/24"),
		Seeds:         []string{"seed.ejemplo"},
	}
}

func specDeVestíbulo() domain.RendezvousSpec {
	return domain.RendezvousSpec{
		Rendezvous: domain.Rendezvous{},
		Address:    addr("100.127.255.1"),
		Seeds:      []string{"seed.ejemplo"},
	}
}

func specDeInvitado() domain.GuestSpec {
	return domain.GuestSpec{
		Credential: domain.Credential{
			ID:          "c1",
			Token:       "t",
			NetworkName: "kanpachi-real",
			VirtualIP:   addr("100.64.1.5"),
			Subnet:      netip.MustParsePrefix("100.64.1.0/24"),
		},
		Seeds: []string{"seed.ejemplo"},
	}
}

// Reiniciar devuelve LAS DOS redes que estaban arriba.
//
// # El fallo que congela, medido con el producto entero
//
// El motor guardaba UNA sola orden de arranque, así que la última pisaba a la
// anterior. Al matar el motor a lo bruto con una sala de host abierta, volvía
// `kanpachi0` y NO volvía `kanpachi1`, y a partir de ahí la regla de la puerta
// no se podía escribir porque su adaptador ya no existía: la sala seguía en pie
// con la puerta cerrada para siempre, sin nada en pantalla que lo dijera.
//
// El comentario que lo justificaba decía que el vestíbulo es "un paso de paso".
// Es cierto para el INVITADO, que lo suelta al entrar, y falso para el host: es
// su puerta y dura lo que dura la sala.
func TestReiniciarDevuelveLasDosRedesDelHost(t *testing.T) {
	e, hijos := motorConHijos(t)
	ctx := context.Background()

	if err := e.HostNetwork(ctx, specDeHost()); err != nil {
		t.Fatal(err)
	}
	if err := e.JoinRendezvous(ctx, specDeVestíbulo()); err != nil {
		t.Fatal(err)
	}
	if err := e.Restart(ctx); err != nil {
		t.Fatal(err)
	}

	lista := hijos()
	if len(lista) != 2 {
		t.Fatalf("se esperaban 2 procesos (el original y el reiniciado), hubo %d", len(lista))
	}
	repetidas := lista[1].pedidas()
	if len(repetidas) != 2 || repetidas[0] != "host" || repetidas[1] != "vestíbulo" {
		t.Errorf("tras reiniciar se pidió %v.\n"+
			"  Tienen que volver las DOS, y la sala primero: es lo que la gente está\n"+
			"  jugando, y el vestíbulo es la puerta.", repetidas)
	}
}

// Y el vestíbulo del INVITADO no vuelve, que es la mitad que hace segura a la
// otra.
//
// El invitado sale del vestíbulo antes de entrar a la sala, a propósito:
// quedarse ahí mantendría abierta una vía por la que un desconocido con el
// código ve que esta máquina está en esa sala. Un reinicio que lo levantara otra
// vez deshace esa decisión sin que nadie lo pida.
func TestReiniciarNoDevuelveElVestíbuloQueElInvitadoSoltó(t *testing.T) {
	e, hijos := motorConHijos(t)
	ctx := context.Background()

	if err := e.JoinRendezvous(ctx, specDeVestíbulo()); err != nil {
		t.Fatal(err)
	}
	if err := e.LeaveRendezvous(ctx); err != nil {
		t.Fatal(err)
	}
	if err := e.JoinWithCredential(ctx, specDeInvitado()); err != nil {
		t.Fatal(err)
	}
	if err := e.Restart(ctx); err != nil {
		t.Fatal(err)
	}

	lista := hijos()
	if len(lista) != 2 {
		t.Fatalf("se esperaban 2 procesos, hubo %d", len(lista))
	}
	repetidas := lista[1].pedidas()
	if len(repetidas) != 1 || repetidas[0] != "invitado" {
		t.Errorf("tras reiniciar se pidió %v, y el invitado ya había soltado el vestíbulo",
			repetidas)
	}
}

// Sin ningún arranque previo, reiniciar es un error y NO una muerte.
//
// Es el fallo del canal de eventos visto desde el otro lado: el watchdog gastaba
// sus ocho intentos contra una sala que no existía.
func TestReiniciarSinSalaEsUnError(t *testing.T) {
	e, _ := motorConHijos(t)
	if err := e.Restart(context.Background()); err == nil {
		t.Fatal("se reinició una sala que nunca existió")
	}
}
