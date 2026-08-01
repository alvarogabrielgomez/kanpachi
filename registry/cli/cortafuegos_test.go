package cli

import "testing"

// Las salidas de abajo son literales de `ufw status`. El parseo es la parte
// frágil de la comprobación, y equivocarse aquí es peor que no comprobar nada:
// un falso "está abierto" deja el seed inalcanzable y además calla.
func TestInterpretarUFW(t *testing.T) {
	const activoConAmbos = `Status: active

To                         Action      From
--                         ------      ----
22/tcp                     ALLOW       Anywhere
11010/tcp                  ALLOW       Anywhere
11010/udp                  ALLOW       Anywhere
22/tcp (v6)                ALLOW       Anywhere (v6)
11010/tcp (v6)             ALLOW       Anywhere (v6)
11010/udp (v6)             ALLOW       Anywhere (v6)
`

	const activoSoloTCP = `Status: active

To                         Action      From
--                         ------      ----
11010/tcp                  ALLOW       Anywhere
`

	const activoSinNada = `Status: active

To                         Action      From
--                         ------      ----
22/tcp                     ALLOW       Anywhere
443                        ALLOW       Anywhere
`

	// Sin protocolo, ufw abre TCP y UDP a la vez.
	const activoSinProtocolo = `Status: active

To                         Action      From
--                         ------      ----
11010                      ALLOW       Anywhere
`

	const negado = `Status: active

To                         Action      From
--                         ------      ----
11010/tcp                  DENY        Anywhere
11010/udp                  REJECT      Anywhere
`

	// LIMIT deja pasar, solo que con freno por origen.
	const limitado = `Status: active

To                         Action      From
--                         ------      ----
11010/tcp                  LIMIT       Anywhere
11010/udp                  LIMIT       Anywhere
`

	const inactivo = "Status: inactive\n"

	casos := []struct {
		nombre           string
		salida           string
		activo, tcp, udp bool
	}{
		{"activo con los dos", activoConAmbos, true, true, true},
		{"activo solo TCP", activoSoloTCP, true, true, false},
		{"activo sin reglas para el puerto", activoSinNada, true, false, false},
		{"sin protocolo abre los dos", activoSinProtocolo, true, true, true},
		{"denegado no cuenta como abierto", negado, true, false, false},
		{"limitado sí deja pasar", limitado, true, true, true},
		{"inactivo", inactivo, false, false, false},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			activo, tcp, udp := interpretarUFW(c.salida, 11010)
			if activo != c.activo || tcp != c.tcp || udp != c.udp {
				t.Errorf("activo=%v tcp=%v udp=%v; se esperaba activo=%v tcp=%v udp=%v",
					activo, tcp, udp, c.activo, c.tcp, c.udp)
			}
		})
	}
}

// Un puerto no puede confundirse con otro que lo contenga como prefijo, ni con
// uno que lo termine. 1101 y 110100 no son 11010.
func TestInterpretarUFWNoConfundePuertosParecidos(t *testing.T) {
	const salida = `Status: active

To                         Action      From
--                         ------      ----
1101/tcp                   ALLOW       Anywhere
110100/udp                 ALLOW       Anywhere
`
	_, tcp, udp := interpretarUFW(salida, 11010)
	if tcp || udp {
		t.Errorf("dio el 11010 por abierto leyendo reglas de otros puertos: tcp=%v udp=%v", tcp, udp)
	}
}

func TestComandosUFWSoloPideLoQueFalta(t *testing.T) {
	c := comandosUFW(11010, reglasUFW{Instalado: true, Activo: true, Legible: true, TCP: true})
	if len(c) != 1 {
		t.Fatalf("pidió %d comandos y solo faltaba UDP: %v", len(c), c)
	}
	if c[0] != "sudo ufw allow 11010/udp" {
		t.Errorf("comando inesperado: %s", c[0])
	}
}
