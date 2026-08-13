package usecase

import (
	"errors"
	"net/netip"
	"runtime"
	"testing"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// salaDeInvitado deja la sesión dentro de una sala, como invitado, con el host
// presente en la lista de miembros.
func salaDeInvitado(t *testing.T) (*bank, netip.Addr) {
	t.Helper()

	b := nuevoBanco(t)
	yo := netip.MustParseAddr("100.87.3.5")
	host := netip.MustParseAddr("100.87.3.1")

	b.control.credencial = domain.Credential{
		ID: "c1", Token: "t", NetworkName: "kanpachi-real",
		VirtualIP: yo,
		Subnet:    netip.MustParsePrefix("100.87.3.0/24"),
	}
	if _, err := b.session.JoinRoom(ctx(), "A7K2M9QX@seed.midominio.com", nick(t, "humberto")); err != nil {
		t.Fatal(err)
	}
	b.motor.peers = []domain.Peer{
		{VirtualIP: yo, Name: nick(t, "humberto"), Self: true},
		{VirtualIP: host, Name: nick(t, "alvaro"), Host: true},
	}
	if _, err := b.session.OnPeersChanged(ctx()); err != nil {
		t.Fatal(err)
	}
	return b, host
}

func TestElSondeoMarcaAlHostYNoAOtro(t *testing.T) {
	b, host := salaDeInvitado(t)
	b.sonda.contesta(domain.ControlPort, domain.ProbeAnswered)

	r, err := b.session.ProbeHost(ctx())
	if err != nil {
		t.Fatal(err)
	}

	if r.Target != host {
		t.Fatalf("se marcó a %s y el host es %s", r.Target, host)
	}
	if r.Name.String() != "alvaro" {
		t.Errorf("nombre = %q, se esperaba el del host", r.Name)
	}
	if r.Blind() {
		t.Fatal("un sondeo que corrió no puede quedar ciego")
	}
	if got := r.Verdict(); got != domain.VerdictSealed {
		t.Fatalf("veredicto = %v, se esperaba VerdictSealed", got)
	}

	b.sonda.mu.Lock()
	defer b.sonda.mu.Unlock()
	if len(b.sonda.marcados) == 0 {
		t.Fatal("no se marcó ningún puerto")
	}
	for _, at := range b.sonda.marcados {
		if at.Addr() != host {
			t.Fatalf("se marcó a %s, que no es el host. El sondeo no puede salirse "+
				"de la máquina que se pidió", at)
		}
	}
}

func TestElSondeoConservaElOrdenDeLaLista(t *testing.T) {
	b, _ := salaDeInvitado(t)
	b.sonda.contesta(domain.ControlPort, domain.ProbeAnswered)

	r, err := b.session.ProbeHost(ctx())
	if err != nil {
		t.Fatal(err)
	}

	// Se marcan en paralelo, así que sin cuidado el orden saldría según qué
	// puerto contestó antes y la pantalla cambiaría en cada corrida.
	for i := 1; i < len(r.Results); i++ {
		if r.Results[i-1].Port >= r.Results[i].Port {
			t.Fatalf("en la posición %d el orden se rompió: %d antes de %d",
				i, r.Results[i-1].Port, r.Results[i].Port)
		}
	}
	if len(r.Results) != len(domain.ProbeTargets(domain.GameProfile{})) {
		t.Fatalf("salieron %d resultados y la lista tiene %d objetivos",
			len(r.Results), len(domain.ProbeTargets(domain.GameProfile{})))
	}
}

// La fila que da todo el valor: un puerto que nadie pidió contestando.
func TestUnPuertoAbiertoQueNadiePidioSaleComoFuga(t *testing.T) {
	b, _ := salaDeInvitado(t)
	b.sonda.contesta(domain.ControlPort, domain.ProbeAnswered)
	b.sonda.contesta(445, domain.ProbeAnswered)

	r, err := b.session.ProbeHost(ctx())
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Verdict(); got != domain.VerdictLeaky {
		t.Fatalf("veredicto = %v, se esperaba VerdictLeaky", got)
	}
	leaks := r.Leaks()
	if len(leaks) != 1 || leaks[0].Port != 445 {
		t.Fatalf("fugas = %v, se esperaba solo el 445", leaks)
	}
}

// Sin referencia el sondeo no puede afirmar nada, y decir "cerrado" acá sería
// pintar de verde una máquina apagada.
func TestSinRespuestaDelCanalElSondeoNoAfirmaNada(t *testing.T) {
	b, _ := salaDeInvitado(t)

	r, err := b.session.ProbeHost(ctx())
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Verdict(); got != domain.VerdictUnreachable {
		t.Fatalf("veredicto = %v, se esperaba VerdictUnreachable", got)
	}
}

func TestElHostNoSePuedeSondearASiMismo(t *testing.T) {
	b := salaCreada(t)

	if _, err := b.session.ProbeHost(ctx()); !errors.Is(err, ErrProbeSelf) {
		t.Fatalf("error = %v, se esperaba ErrProbeSelf", err)
	}
	// Y no se marcó nada: el tráfico a la propia dirección no atraviesa el
	// firewall, así que un sondeo local diría que está todo abierto en una
	// máquina blindada.
	if n := b.sonda.cuántos(); n != 0 {
		t.Fatalf("se marcaron %d puertos y no se tenía que marcar ninguno", n)
	}
}

func TestSinSalaNoHayNadaQueSondear(t *testing.T) {
	b := nuevoBanco(t)

	if _, err := b.session.ProbeHost(ctx()); !errors.Is(err, ErrNoRoom) {
		t.Fatalf("error = %v, se esperaba ErrNoRoom", err)
	}
	if n := b.sonda.cuántos(); n != 0 {
		t.Fatalf("se marcaron %d puertos sin sala", n)
	}
}

func TestSinHostEnLaListaNoSeInventaUnaDireccion(t *testing.T) {
	b, _ := salaDeInvitado(t)
	b.motor.peers = []domain.Peer{
		{VirtualIP: netip.MustParseAddr("100.87.3.5"), Name: nick(t, "humberto"), Self: true},
	}
	if _, err := b.session.OnPeersChanged(ctx()); err != nil {
		t.Fatal(err)
	}

	if _, err := b.session.ProbeHost(ctx()); !errors.Is(err, ErrProbeNoHost) {
		t.Fatalf("error = %v, se esperaba ErrProbeNoHost", err)
	}
}

// El candado no se sostiene mientras se marca, que son segundos contra la red.
// Sin esto, cualquier Status de la UI se quedaría esperando todo ese rato.
func TestElSondeoNoSostieneElCandadoDeLaSesion(t *testing.T) {
	b, _ := salaDeInvitado(t)
	suelta := make(chan struct{})
	b.sonda.mu.Lock()
	b.sonda.espera = suelta
	b.sonda.mu.Unlock()

	listo := make(chan struct{})
	go func() {
		defer close(listo)
		if _, err := b.session.ProbeHost(ctx()); err != nil {
			t.Errorf("el sondeo falló: %v", err)
		}
	}()

	// Se espera a que el sondeo esté DENTRO de la sonda antes de preguntar.
	// Sin esto, Status podría contestar antes de que ProbeHost llegue siquiera
	// a tomar el candado, y el test pasaría sin probar nada.
	for b.sonda.cuántos() == 0 {
		runtime.Gosched()
	}

	// Con el sondeo a medias, la sala se tiene que poder consultar igual.
	if st := b.session.Status(); !st.Conn.InRoom() {
		t.Fatal("Status contestó fuera de la sala mientras el sondeo corría")
	}
	close(suelta)
	<-listo
}
