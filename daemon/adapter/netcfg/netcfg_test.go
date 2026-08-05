package netcfg

import (
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/accentiostudios/kanpachi/core/domain"
)

func TestWantedRoutesFollowTheProfileAndNothingElse(t *testing.T) {
	casos := []struct {
		nombre string
		estado domain.AdapterState
		quiere []netip.Prefix
	}{
		{"sin juego, ninguna", domain.AdapterState{}, nil},
		{"solo broadcast", domain.AdapterState{BroadcastRoute: true}, []netip.Prefix{BroadcastRoute}},
		{"solo multicast", domain.AdapterState{MulticastRoute: true}, []netip.Prefix{MulticastRoute}},
		{"las dos", domain.AdapterState{BroadcastRoute: true, MulticastRoute: true},
			[]netip.Prefix{BroadcastRoute, MulticastRoute}},
		{
			// El resto del estado no puede inventar rutas. Es lo que impide que
			// un ajuste que no habla de rutas termine escribiendo una.
			"lo demás no añade rutas",
			domain.AdapterState{PreferIPv4: true, PrivateCategory: true, MTU: 1500,
				MetricIPv4: 1, Address: netip.MustParseAddr("100.87.4.1")},
			nil,
		},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			got := wantedRoutes(c.estado)
			if len(got) == 0 && len(c.quiere) == 0 {
				return
			}
			if !slices.Equal(got, c.quiere) {
				t.Errorf("quería %v, obtuvo %v", c.quiere, got)
			}
		})
	}
}

// TestTheOnlyRoutesAreTheTwoConstants is the invariant behind the whole design:
// a profile declares an intent, and this layer decides what that intent may
// become. A profile can never ask for a route to a destination of its choosing.
func TestTheOnlyRoutesAreTheTwoConstants(t *testing.T) {
	// Se recorren las cuatro combinaciones, que es el espacio entero.
	for _, b := range []bool{false, true} {
		for _, m := range []bool{false, true} {
			for _, p := range wantedRoutes(domain.AdapterState{BroadcastRoute: b, MulticastRoute: m}) {
				if p != BroadcastRoute && p != MulticastRoute {
					t.Fatalf("apareció una ruta que no es ninguna de las dos constantes: %v", p)
				}
			}
		}
	}
}

func TestDefaultRoutesAreRecognised(t *testing.T) {
	// Kanpachi jamás enruta internet. Reconocer las dos formas importa porque
	// quien las escribe puede ser el motor con lo que aprendió de la red, no
	// nosotros.
	for _, s := range []string{"0.0.0.0/0", "::/0"} {
		if !isDefaultRoute(netip.MustParsePrefix(s)) {
			t.Errorf("%s es una ruta por defecto y no se reconoció", s)
		}
	}
	for _, s := range []string{"255.255.255.255/32", "224.0.0.0/4", "100.87.4.0/24", "10.0.0.0/8"} {
		if isDefaultRoute(netip.MustParsePrefix(s)) {
			t.Errorf("%s NO es una ruta por defecto y se tomó por una", s)
		}
	}
	if isDefaultRoute(netip.Prefix{}) {
		t.Error("un prefijo inválido no es una ruta por defecto")
	}
}

// TestMTUAgreesWithTheEngineOnACleanPath is the reason the overhead is the
// number it is. On a 1500 path the engine writes 1360 by itself, and netcfg has
// to arrive at the same value or the two would fight over the interface every
// time the supervisor reapplies.
func TestMTUAgreesWithTheEngineOnACleanPath(t *testing.T) {
	if got := mtuFor(domain.AdapterState{MTU: 1500}); got != 1360 {
		t.Errorf("sobre un camino de 1500 el motor pone 1360 y netcfg puso %d", got)
	}
}

func TestMTUStaysWithinItsLimits(t *testing.T) {
	casos := []struct{ camino, quiere int }{
		{0, 0},       // sin sondear: no se escribe nada
		{-1, 0},      // basura: tampoco
		{1500, 1360}, // Ethernet
		{1492, 1352}, // PPPoE, el caso que el sondeo existe para cazar
		{9000, 1380}, // jumbo: se topa en el máximo del túnel
		{1300, 1280}, // camino angosto: no baja del mínimo de IPv6
		{100, 1280},  // absurdo: tampoco
	}
	for _, c := range casos {
		if got := mtuFor(domain.AdapterState{MTU: c.camino}); got != c.quiere {
			t.Errorf("camino %d: quería %d, obtuvo %d", c.camino, c.quiere, got)
		}
	}
}

func TestTheBookRemembersThePreviousValueAndForgetsWhenDone(t *testing.T) {
	b := newBook(t.TempDir())

	// Una instalación nueva no tiene libro, y eso NO es un error: es lo normal
	// y también lo que deja toda salida limpia.
	t0, err := b.load()
	if err != nil {
		t.Fatalf("un libro que no existe tenía que leerse como vacío: %v", err)
	}
	if !t0.empty() {
		t.Fatal("un libro que no existe tiene que estar vacío")
	}

	estaba := false
	if err := b.save(tweaks{DirectPlayWas: &estaba, PrefixPolicyAdded: true}); err != nil {
		t.Fatal(err)
	}

	t1, err := b.load()
	if err != nil {
		t.Fatal(err)
	}
	if t1.DirectPlayWas == nil || *t1.DirectPlayWas != false || !t1.PrefixPolicyAdded {
		t.Fatalf("el libro no sobrevivió al viaje: %+v", t1)
	}

	// Vaciarlo BORRA el fichero. Uno que exista le dice al arranque siguiente
	// que hay algo pendiente, y decirlo en falso cuesta revertir cosas que nadie
	// cambió.
	if err := b.save(tweaks{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(b.path); !os.IsNotExist(err) {
		t.Error("sin nada que deshacer, el libro tiene que desaparecer del disco")
	}
}

// TestACorruptBookIsAnErrorAndNotAnEmptyOne guards the direction that matters.
// Reading a broken book as empty would silently drop the only record of what
// has to be undone, and the machine would keep a setting nobody can trace.
func TestACorruptBookIsAnErrorAndNotAnEmptyOne(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, TweakFile), []byte(`{"direct_play_was":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := newBook(dir).load(); err == nil {
		t.Fatal("un libro roto tiene que dar error y no leerse como vacío")
	}
}

func TestTheBookRejectsFieldsItDoesNotKnow(t *testing.T) {
	// Decodificación estricta, igual que en el resto del proyecto: un campo de
	// más es una versión distinta escribiendo aquí, y aceptarlo callado sería
	// perder lo que ese campo quería decir.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, TweakFile),
		[]byte(`{"direct_play_was":true,"algo_nuevo":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := newBook(dir).load(); err == nil {
		t.Fatal("un campo desconocido tiene que rechazar el libro entero")
	}
}
