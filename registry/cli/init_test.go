package cli

import (
	"net"
	"strconv"
	"testing"

	"github.com/accentiostudios/kanpachi/registry/setup"
)

// puertoLibreDePrueba devuelve un puerto que nadie tiene tomado. Se abre y se
// cierra de verdad, porque un número inventado puede estar en uso.
func puertoLibreDePrueba(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("no se pudo pedir un puerto libre: %v", err)
	}
	defer l.Close()
	_, p, _ := net.SplitHostPort(l.Addr().String())
	n, _ := strconv.Atoi(p)
	return n
}

// puertoOcupadoDePrueba deja un listener ABIERTO durante todo el test, que es
// justo la situación de reinstalar: el registro sigue corriendo y tiene su
// propio puerto tomado.
func puertoOcupadoDePrueba(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("no se pudo ocupar un puerto: %v", err)
	}
	t.Cleanup(func() { l.Close() })
	_, p, _ := net.SplitHostPort(l.Addr().String())
	n, _ := strconv.Atoi(p)
	return n
}

// TestReinstalarConservaElPuertoAunqueEsteOcupado cubre un fallo que llegó a
// producción.
//
// init sondeaba el puerto preferido con un bind para ver si estaba libre. Al
// reinstalar, quien lo tenía tomado era el propio registro todavía en marcha,
// así que el sondeo lo daba por ocupado y elegía el siguiente: el servicio se
// mudó del 8010 al 8011 y el proxy inverso quedó apuntando a un puerto muerto.
// No hubo error ni aviso, el resumen imprimió el puerto nuevo como si nada.
func TestReinstalarConservaElPuertoAunqueEsteOcupado(t *testing.T) {
	ocupado := puertoOcupadoDePrueba(t)
	previa := setup.Config{PuertoRegistro: ocupado, PuertoRPC: 15888, PuertoMotor: 11010}

	c, err := decidirPuertos(previa, true, 0, 0)
	if err != nil {
		t.Fatalf("decidirPuertos falló al reinstalar: %v", err)
	}
	if c.PuertoRegistro != ocupado {
		t.Errorf("reinstalar movió el puerto de %d a %d: el proxy inverso queda apuntando al vacío",
			ocupado, c.PuertoRegistro)
	}
	if c.PuertoRPC != previa.PuertoRPC {
		t.Errorf("reinstalar movió el puerto del RPC de %d a %d", previa.PuertoRPC, c.PuertoRPC)
	}
	if c.PuertoMotor != previa.PuertoMotor {
		t.Errorf("reinstalar movió el puerto del motor de %d a %d, y los clientes lo llevan compilado",
			previa.PuertoMotor, c.PuertoMotor)
	}
}

// Pedir explícitamente el puerto que ya se está usando tampoco puede fallar:
// es lo que uno escribe para asegurarse de que no se mueva.
func TestPedirElMismoPuertoQueYaSeUsaNoFalla(t *testing.T) {
	ocupado := puertoOcupadoDePrueba(t)
	previa := setup.Config{PuertoRegistro: ocupado, PuertoRPC: 15888, PuertoMotor: 11010}

	c, err := decidirPuertos(previa, true, ocupado, 0)
	if err != nil {
		t.Fatalf("pedir el puerto que ya se usa dio error: %v", err)
	}
	if c.PuertoRegistro != ocupado {
		t.Errorf("devolvió %d en vez del %d pedido", c.PuertoRegistro, ocupado)
	}
}

// Y pedir uno que tiene OTRO tiene que fallar con un mensaje, no mudarse en
// silencio a uno cualquiera.
func TestPedirUnPuertoAjenoFalla(t *testing.T) {
	ocupado := puertoOcupadoDePrueba(t)
	previa := setup.Config{PuertoRegistro: 8010, PuertoRPC: 15888, PuertoMotor: 11010}

	if _, err := decidirPuertos(previa, true, ocupado, 0); err == nil {
		t.Error("pedir un puerto ocupado por otro proceso debió dar error")
	}
}

// En una instalación nueva sí hay que esquivar lo ocupado: no hay proxy
// configurado todavía, así que elegir otro puerto no rompe nada.
func TestInstalacionNuevaEsquivaLoOcupado(t *testing.T) {
	// El del motor se pide libre a propósito: si estuviera tomado, init
	// preguntaría por consola y un test no tiene quien conteste.
	motor := puertoLibreDePrueba(t)

	c, err := decidirPuertos(setup.Config{}, false, 0, motor)
	if err != nil {
		t.Fatalf("decidirPuertos falló en instalación nueva: %v", err)
	}
	if c.PuertoRegistro < setup.RangoInternoDesde || c.PuertoRegistro > setup.RangoInternoHasta {
		t.Errorf("eligió el puerto %d, fuera del rango interno", c.PuertoRegistro)
	}
	l, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(c.PuertoRegistro)))
	if err != nil {
		t.Fatalf("el puerto %d que eligió no se puede usar: %v", c.PuertoRegistro, err)
	}
	l.Close()
}
