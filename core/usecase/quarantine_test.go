package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// La cuarentena de base en el arranque, desde que es la DECISIÓN del usuario.
//
// Lo que se afirma acá cambió con la doctrina: antes era orden y fatalidad, y
// ahora es orden, obediencia y tolerancia. El arranque escribe la cuarentena
// SOLO con la decisión en sí, la escribe antes de la purga, no muere si no
// puede, y con la decisión sin tomar no escribe nada. Quitarla jamás pasa por
// el arranque; eso lo vigila el guardián del grupo base por llamador.

// decisiónGuardada arma los bytes que el almacén le daría al arranque.
func decisiónGuardada(t interface{ Fatalf(string, ...any) }, d domain.QuarantineDecision) []byte {
	raw, err := domain.EncodeQuarantineDecision(d, time.Date(2026, 8, 2, 20, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("no se pudo codificar la decisión de prueba: %v", err)
	}
	return raw
}

// TestConLaDecisiónEnSíLaCuarentenaVaAntesDeLaPurga.
//
// La purga es el instante de menos protección de todo el arranque: se lleva las
// reglas de la sala anterior y todavía no hay ninguna nueva. Escribir la
// cuarentena después dejaría ese hueco descubierto en cada arranque del
// servicio, que es varias veces al día en una máquina que se reinicia.
func TestConLaDecisiónEnSíLaCuarentenaVaAntesDeLaPurga(t *testing.T) {
	b := bancoSinSesión()
	b.state.cuarentena = decisiónGuardada(t, domain.QuarantineAccepted)
	if _, err := NewSession(context.Background(), b.deps); err != nil {
		t.Fatalf("no se pudo montar la sesión: %v", err)
	}

	tras := b.firewall.cuarentenaTrasPurgas
	if len(tras) != 1 {
		t.Fatalf("la cuarentena se escribió %d veces en el arranque, se esperaba 1", len(tras))
	}
	if tras[0] != 0 {
		t.Errorf("la cuarentena se escribió con %d purgas ya hechas. Tiene que ir ANTES: "+
			"la purga deja la máquina sin las reglas de la sala anterior, y ese hueco "+
			"es justo lo que la cuarentena tapa", tras[0])
	}
}

// TestLaCuarentenaDelArranqueEsLaDelDominio.
//
// El caso de uso no puede recortarla ni construir la suya. Si alguien le pasara
// una lista propia, ampliar `forbiddenPorts` dejaría de ampliar la cuarentena y
// el fallo sería silencioso.
func TestLaCuarentenaDelArranqueEsLaDelDominio(t *testing.T) {
	b := bancoSinSesión()
	b.state.cuarentena = decisiónGuardada(t, domain.QuarantineAccepted)
	if _, err := NewSession(context.Background(), b.deps); err != nil {
		t.Fatalf("no se pudo montar la sesión: %v", err)
	}

	puesta := b.firewall.cuarentenaPuesta()
	quería := domain.BaseQuarantine()
	if len(puesta) != len(quería) {
		t.Fatalf("se escribieron %d reglas de cuarentena y el dominio produce %d",
			len(puesta), len(quería))
	}
	for i := range quería {
		if puesta[i] != quería[i] {
			t.Fatalf("la regla %d de la cuarentena no es la del dominio: %+v contra %+v",
				i, puesta[i], quería[i])
		}
	}
}

// TestElFalloDeLaCuarentenaYaNoImpideArrancar.
//
// La invariante vieja era la contraria, y se derogó con la decisión: un daemon
// que no arranca por no poder escribirla deja al usuario sin producto entero
// por una protección que él mismo eligió. Arrancar diciéndolo deja el barrido
// midiendo, y el interruptor a mano para reintentar.
func TestElFalloDeLaCuarentenaYaNoImpideArrancar(t *testing.T) {
	b := bancoSinSesión()
	b.state.cuarentena = decisiónGuardada(t, domain.QuarantineAccepted)
	b.firewall.errCuarentena = errors.New("acceso denegado")

	if _, err := NewSession(context.Background(), b.deps); err != nil {
		t.Fatalf("la sesión no arrancó por el fallo de la cuarentena, y eso deja al usuario "+
			"sin Kanpachi entero por una protección que puede reintentar: %v", err)
	}
}

// TestSinDecisiónElArranqueNoEscribeNada.
//
// "Sin decidir" no es "sí": el arranque que escribiera igual convertiría la
// pregunta en teatro. Y "no" tampoco escribe, ni QUITA: reponer sería decidir
// por el usuario, y quitar desde un arranque es justo lo que el guardián del
// grupo base impide por llamador.
func TestSinDecisiónElArranqueNoEscribeNada(t *testing.T) {
	casos := map[string][]byte{
		"sin decidir": nil,
		"decidió no":  decisiónGuardada(t, domain.QuarantineDeclined),
	}
	for nombre, decisión := range casos {
		t.Run(nombre, func(t *testing.T) {
			b := bancoSinSesión()
			b.state.cuarentena = decisión
			if _, err := NewSession(context.Background(), b.deps); err != nil {
				t.Fatalf("no se pudo montar la sesión: %v", err)
			}
			if n := len(b.firewall.cuarentenaTrasPurgas); n != 0 {
				t.Errorf("el arranque escribió la cuarentena %d veces sin que el usuario "+
					"la pidiera", n)
			}
			if n := b.firewall.cuarentenaRetirada; n != 0 {
				t.Errorf("el arranque RETIRÓ la cuarentena %d veces, y quitarla es siempre "+
					"el acto de una persona", n)
			}
		})
	}
}

// TestLaDecisiónEsLaOperaciónEnLosDosSentidos.
//
// No hay una preferencia guardada por un lado y unas reglas por otro: decir que
// sí las escribe, decir que no las quita, y las dos quedan persistidas para el
// arranque siguiente.
func TestLaDecisiónEsLaOperaciónEnLosDosSentidos(t *testing.T) {
	b := nuevoBanco(t)

	if _, err := b.session.DecideQuarantine(ctx(), true); err != nil {
		t.Fatalf("decir que sí falló: %v", err)
	}
	if len(b.firewall.cuarentenaPuesta()) != len(domain.BaseQuarantine()) {
		t.Fatal("decir que sí no escribió la cuarentena entera")
	}
	if got := b.session.QuarantineDecision(); got != domain.QuarantineAccepted {
		t.Fatalf("tras decir que sí la decisión quedó en %v", got)
	}

	if _, err := b.session.DecideQuarantine(ctx(), false); err != nil {
		t.Fatalf("decir que no falló: %v", err)
	}
	if b.firewall.cuarentenaRetirada != 1 {
		t.Fatalf("decir que no retiró %d veces, se esperaba 1", b.firewall.cuarentenaRetirada)
	}
	if got := b.session.QuarantineDecision(); got != domain.QuarantineDeclined {
		t.Fatalf("tras decir que no la decisión quedó en %v", got)
	}

	// Y lo persistido es lo que el arranque siguiente va a obedecer.
	decisión, _, err := domain.DecodeQuarantineDecision(b.state.cuarentena)
	if err != nil {
		t.Fatalf("lo guardado no se pudo decodificar: %v", err)
	}
	if decisión != domain.QuarantineDeclined {
		t.Fatalf("quedó guardado %v y la última palabra fue que no", decisión)
	}
}

// TestLaCuarentenaNoSeVuelveAEscribirEnCadaBarrido.
//
// Con la decisión en sí, está en el arranque y en ningún otro sitio. Colgarla
// del barrido de un minuto sería una enumeración entera del almacén de reglas
// del firewall por minuto y para siempre, a cambio de reponer algo que solo un
// administrador puede haber quitado a mano; el barrido la MIDE y las caras lo
// cuentan, que es el arreglo que no cuesta eso.
func TestLaCuarentenaNoSeVuelveAEscribirEnCadaBarrido(t *testing.T) {
	b := bancoSinSesión()
	b.state.cuarentena = decisiónGuardada(t, domain.QuarantineAccepted)
	s, err := NewSession(context.Background(), b.deps)
	if err != nil {
		t.Fatalf("no se pudo montar la sesión: %v", err)
	}
	antes := len(b.firewall.cuarentenaTrasPurgas)

	for i := 0; i < 3; i++ {
		s.RefreshAlerts(ctx())
	}

	if después := len(b.firewall.cuarentenaTrasPurgas); después != antes {
		t.Errorf("el barrido escribió la cuarentena %d veces de más. Es del arranque "+
			"y de ningún otro sitio", después-antes)
	}
	if b.firewall.cuarentenaRetirada != 0 {
		t.Error("el barrido retiró la cuarentena, y quitarla es siempre el acto de una persona")
	}
}
