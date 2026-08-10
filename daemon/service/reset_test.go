package service

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// El diario compartido: lo que se prueba acá es el ORDEN, y con un diario por
// pieza el orden entre piezas no se vería.
type diarioReset struct{ pasos []string }

func (d *diarioReset) anota(p string) { d.pasos = append(d.pasos, p) }

func (d *diarioReset) índice(paso string) int {
	for i, p := range d.pasos {
		if p == paso {
			return i
		}
	}
	return -1
}

type fwReset struct {
	d           *diarioReset
	cuarentena  []domain.QuarantineRule
	errPurga    error
	errRestaura error
}

func (f *fwReset) ApplyBaseQuarantine(_ context.Context, r []domain.QuarantineRule) error {
	f.d.anota("cuarentena")
	f.cuarentena = r
	return nil
}
func (f *fwReset) PurgeOwned(context.Context) error {
	f.d.anota("purga")
	return f.errPurga
}
func (f *fwReset) RestoreForeign(context.Context) error {
	f.d.anota("restaura")
	return f.errRestaura
}
func (f *fwReset) UnbindRoom() { f.d.anota("suelta") }

type netcfgReset struct {
	d   *diarioReset
	err error
}

func (n *netcfgReset) RevertTweaks(context.Context) error {
	n.d.anota("tweaks")
	return n.err
}

type motorReset struct {
	d    *diarioReset
	mató int
}

func (m *motorReset) KillOrphans() int {
	m.d.anota("huérfanos")
	return m.mató
}

type estadoReset struct {
	d   *diarioReset
	err error
}

func (e *estadoReset) ClearRoom() error {
	e.d.anota("sala")
	return e.err
}

type logMudoReset struct{}

func (logMudoReset) Info(string, ...any)  {}
func (logMudoReset) Warn(string, ...any)  {}
func (logMudoReset) Error(string, ...any) {}

func armadoReset() (ResetDeps, *diarioReset, *fwReset, *netcfgReset, *motorReset, *estadoReset) {
	d := &diarioReset{}
	fw := &fwReset{d: d}
	nc := &netcfgReset{d: d}
	mt := &motorReset{d: d}
	st := &estadoReset{d: d}
	return ResetDeps{
		Firewall: fw, NetCfg: nc, Engine: mt, State: st, Log: logMudoReset{},
		// Cualquiera de los dos sirve acá: lo que este banco prueba es el ORDEN
		// del reset, no qué puertos cierra la cuarentena, que se prueba en el
		// dominio.
		Quarantine: domain.QuarantineWindows,
	}, d, fw, nc, mt, st
}

// El orden del reset no es de lectura: cada par tiene una dirección correcta y
// una que deja un hueco.
func TestElResetLimpiaEnElOrdenQueProtege(t *testing.T) {
	deps, d, _, _, _, _ := armadoReset()
	if err := Reset(context.Background(), deps); err != nil {
		t.Fatal(err)
	}

	// Los motores huérfanos PRIMERO. Mientras uno siga vivo la red virtual sigue
	// arriba, así que quitarle las reglas antes deja un adaptador con tráfico y
	// sin nada conteniéndolo.
	if d.índice("huérfanos") > d.índice("purga") {
		t.Errorf("se purgó el firewall con motores huérfanos todavía vivos. Diario: %v", d.pasos)
	}
	// La cuarentena ANTES de la purga. La purga es el instante de menos
	// protección: se lleva las reglas de la sala y todavía no hay nada nuevo.
	if d.índice("cuarentena") > d.índice("purga") {
		t.Errorf("la purga corrió antes que la cuarentena, o sea con la máquina "+
			"descubierta en el medio. Diario: %v", d.pasos)
	}
	// Y la compuerta se suelta después de purgar, igual que al salir de la sala.
	if d.índice("suelta") < d.índice("purga") {
		t.Errorf("se soltó la compuerta antes de purgar los permisos. Diario: %v", d.pasos)
	}
}

// Ningún fallo corta la secuencia, porque no hay un segundo intento.
//
// Alguien que pide un reset lo pide porque nada más funciona. Abortar en el
// primer paso que falla dejaría el resto puesto justo entonces.
func TestUnPasoQueFallaNoCancelaLosDemás(t *testing.T) {
	deps, d, fw, _, _, _ := armadoReset()
	fw.errPurga = errors.New("una regla la usa alguien más")

	err := Reset(context.Background(), deps)
	if err == nil {
		t.Fatal("un paso falló y el reset dijo que todo salió bien")
	}
	if !strings.Contains(err.Error(), "purgando") {
		t.Errorf("el error no dice qué paso falló: %v", err)
	}
	for _, paso := range []string{"tweaks", "sala", "restaura"} {
		if d.índice(paso) < 0 {
			t.Errorf("el paso %q no llegó a correr. Diario: %v", paso, d.pasos)
		}
	}
}

// El reset REPONE la cuarentena y no la quita, y esa asimetría es el diseño.
//
// Lo que hace valiosa a la cuarentena es seguir puesta con el daemon detenido.
// Un reset se pide cuando la configuración está corrupta y nada arranca:
// quitarla ahí destruiría exactamente lo que protege del caso que lo motivó.
func TestElResetReponeLaCuarentenaEnVezDeQuitarla(t *testing.T) {
	deps, _, fw, _, _, _ := armadoReset()
	if err := Reset(context.Background(), deps); err != nil {
		t.Fatal(err)
	}
	if len(fw.cuarentena) == 0 {
		t.Fatal("el reset no repuso la cuarentena de base")
	}
	if len(fw.cuarentena) != len(domain.BaseQuarantine()) {
		t.Errorf("repuso %d reglas y la cuarentena tiene %d",
			len(fw.cuarentena), len(domain.BaseQuarantine()))
	}
}

// `last-room.json` NO se toca. Resetear la configuración no tiene por qué
// borrar a qué sala volver, y juntar las dos cosas convierte una limpieza en una
// pérdida.
//
// Se afirma sobre la INTERFAZ y no sobre una llamada: lo que garantiza que no se
// borre es que el reset no tenga forma de pedirlo.
func TestElResetNoPuedeBorrarLaÚltimaSala(t *testing.T) {
	tipo := reflect.TypeOf((*ResetState)(nil)).Elem()
	if tipo.NumMethod() == 0 {
		t.Fatal("ResetState no declara métodos, así que este test no probaría nada")
	}
	for i := 0; i < tipo.NumMethod(); i++ {
		if nombre := tipo.Method(i).Name; strings.Contains(nombre, "Last") {
			t.Errorf("ResetState declara %q.\n"+
				"  Resetear la configuración no es olvidar a qué sala volver, y lo que\n"+
				"  garantiza que no pase es que el reset no tenga forma de pedirlo.", nombre)
		}
	}
}

func TestElResetNecesitaTodasSusPiezas(t *testing.T) {
	if err := Reset(context.Background(), ResetDeps{}); err == nil {
		t.Fatal("el reset corrió sin piezas, o sea sin limpiar nada, y dijo que sí")
	}
}
