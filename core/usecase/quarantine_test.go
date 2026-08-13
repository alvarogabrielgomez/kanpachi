package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// La cuarentena de base en el arranque.
//
// Lo que se afirma acá es de ORDEN y de FATALIDAD, que son las dos cosas que un
// refactor bienintencionado mueve sin darse cuenta de lo que cuestan.

// TestLaCuarentenaSeEscribeAntesDeLaPurga.
//
// La purga es el instante de menos protección de todo el arranque: se lleva las
// reglas de la sala anterior y todavía no hay ninguna nueva. Escribir la
// cuarentena después dejaría ese hueco descubierto en cada arranque del
// servicio, que es varias veces al día en una máquina que se reinicia.
//
// El caso real es el que ya justifica que la purga exista: una muerte sucia del
// daemon. La máquina arranca, el servicio arranca, y entre la purga y la
// cuarentena no hay nada tapando el 445.
func TestLaCuarentenaSeEscribeAntesDeLaPurga(t *testing.T) {
	b := nuevoBanco(t)

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
	b := nuevoBanco(t)

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

// TestSinCuarentenaElDaemonNoArranca.
//
// Es fatal a propósito, igual que el fallo de la purga. Un daemon que no pudo
// escribir la cuarentena es un daemon con la promesa apagada, y arrancar igual
// le dejaría al usuario la app abierta diciendo que todo está bien encima de una
// máquina sin lo único que la protege con el servicio detenido.
//
// Un aviso en el log no sirve: nadie lee el log de un servicio que arrancó.
func TestSinCuarentenaElDaemonNoArranca(t *testing.T) {
	b := bancoSinSesión()
	b.firewall.errCuarentena = errors.New("acceso denegado")

	if _, err := NewSession(context.Background(), b.deps); err == nil {
		t.Fatal("la sesión arrancó con la cuarentena sin escribir. Eso deja al usuario " +
			"con la app diciendo que todo está bien sobre una máquina sin protección")
	}
}

// TestLaCuarentenaNoSeVuelveAEscribirEnCadaBarrido.
//
// Está en el arranque y en ningún otro sitio. Colgarla del barrido de un minuto
// sería una enumeración entera del almacén de reglas del firewall por minuto y
// para siempre, a cambio de reponer algo que solo un administrador puede haber
// quitado a mano.
func TestLaCuarentenaNoSeVuelveAEscribirEnCadaBarrido(t *testing.T) {
	b := nuevoBanco(t)
	antes := len(b.firewall.cuarentenaTrasPurgas)

	for i := 0; i < 3; i++ {
		b.session.RefreshAlerts(ctx())
	}

	if después := len(b.firewall.cuarentenaTrasPurgas); después != antes {
		t.Errorf("el barrido escribió la cuarentena %d veces de más. Es del arranque "+
			"y de ningún otro sitio", después-antes)
	}
}
