package setup

import (
	"net"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// TestPuertoLibreEsquivaLoOcupado cubre lo que el instalador promete: elegir un
// puerto que de verdad esté libre. Se comprueba ocupándolo, no simulándolo,
// porque una tabla de puertos puede mentir y un bind no.
func TestPuertoLibreEsquivaLoOcupado(t *testing.T) {
	ocupado, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("no se pudo ocupar un puerto para la prueba: %v", err)
	}
	defer ocupado.Close()
	_, p, _ := net.SplitHostPort(ocupado.Addr().String())
	tomado, _ := strconv.Atoi(p)

	elegido, err := PuertoLibre(tomado, tomado+20, tomado)
	if err != nil {
		t.Fatalf("PuertoLibre falló: %v", err)
	}
	if elegido == tomado {
		t.Fatalf("eligió el puerto %d, que está ocupado", tomado)
	}
	// Y lo elegido tiene que servir de verdad.
	l, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(elegido)))
	if err != nil {
		t.Fatalf("el puerto %d que eligió no se puede usar: %v", elegido, err)
	}
	l.Close()
}

func TestPuertoLibreRespetaElPreferido(t *testing.T) {
	// Un rango que no contiene al preferido: si aun así lo devuelve, es que
	// respeta la preferencia por encima del rango, que es lo que hace falta
	// para que reinstalar conserve el puerto que el proxy ya tiene apuntado.
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	_, p, _ := net.SplitHostPort(l.Addr().String())
	libre, _ := strconv.Atoi(p)
	l.Close()

	elegido, err := PuertoLibre(RangoInternoDesde, RangoInternoHasta, libre)
	if err != nil {
		t.Fatalf("PuertoLibre falló: %v", err)
	}
	if elegido != libre {
		t.Errorf("eligió %d en vez del preferido %d: reinstalar movería el puerto y rompería el proxy", elegido, libre)
	}
}

func TestPuertoLibreSeAgota(t *testing.T) {
	// Rango imposible: el instalador tiene que fallar con un mensaje, no
	// devolver un cero que luego produzca una unit con ":0".
	if _, err := PuertoLibre(70000, 70001, 0); err == nil {
		t.Fatal("un rango inválido debió dar error")
	}
}

// TestLasUnitsLlevanLoQueNoSeNegocia ata el generador de units a los
// invariantes. Son líneas que se pierden fácil al editar una plantilla, y
// ninguna deja rastro visible cuando falta: el servicio arranca igual.
func TestLasUnitsLlevanLoQueNoSeNegocia(t *testing.T) {
	c := Config{PuertoRegistro: 8042, PuertoMotor: 11010, PuertoRPC: 15888, Dominio: "x.test"}
	motor := UnitDelMotor(c)
	reg := UnitDelRegistro(c)

	t.Run("el motor no toca el router", func(t *testing.T) {
		// Viene ENCENDIDO por defecto en EasyTier, así que su ausencia no es
		// un olvido inocuo: el motor mapearía puertos en el router.
		if !strings.Contains(motor, "--disable-upnp true") {
			t.Error("falta --disable-upnp true: el motor mapea puertos por defecto")
		}
	})

	t.Run("el motor no se une a ninguna sala", func(t *testing.T) {
		for _, prohibido := range []string{"--network-name", "--network-secret"} {
			if strings.Contains(motor, prohibido) {
				t.Errorf("la unit del motor lleva %s: el seed jamás debe unirse a una sala, "+
					"es lo que garantiza que no vea el secreto de ninguna red", prohibido)
			}
		}
	})

	t.Run("el portal RPC solo en loopback", func(t *testing.T) {
		if !strings.Contains(motor, "--rpc-portal 127.0.0.1:15888") {
			t.Error("el portal RPC tiene que ir fijado a 127.0.0.1: es el panel de control del motor")
		}
		if strings.Contains(motor, "--rpc-portal 0.0.0.0") {
			t.Error("el portal RPC escucha fuera del loopback")
		}
	})

	t.Run("sin STUN de terceros", func(t *testing.T) {
		// El seed tiene IP pública directa y no tiene NAT que descubrir. Con
		// los valores por defecto mandaba tráfico saliente a servidores de
		// terceros desde el droplet de producción.
		if !strings.Contains(motor, `--stun-servers ""`) {
			t.Error("faltan los servidores STUN vacíos")
		}
	})

	t.Run("capacidades prohibidas ausentes", func(t *testing.T) {
		for _, prohibido := range []string{
			"--enable-exit-node", "--exit-nodes", "--proxy-networks",
			"--vpn-portal", "--socks5", "--accept-dns",
		} {
			if strings.Contains(motor, prohibido) {
				t.Errorf("la unit lleva %s, que expresa una capacidad prohibida", prohibido)
			}
		}
	})

	t.Run("el registro late para el watchdog", func(t *testing.T) {
		// Type=notify sin WatchdogSec no reinicia un proceso colgado, y
		// WatchdogSec sin Type=notify impide que el servicio arranque.
		if !strings.Contains(reg, "Type=notify") || !strings.Contains(reg, "WatchdogSec=") {
			t.Error("faltan Type=notify o WatchdogSec: sin los dos, un proceso vivo pero colgado se queda colgado")
		}
	})

	t.Run("los dos servicios van endurecidos", func(t *testing.T) {
		for nombre, unit := range map[string]string{"motor": motor, "registro": reg} {
			for _, obligatorio := range []string{
				"DynamicUser=yes", "NoNewPrivileges=yes",
				"CapabilityBoundingSet=", "ProtectSystem=strict",
			} {
				if !strings.Contains(unit, obligatorio) {
					t.Errorf("la unit del %s no lleva %s", nombre, obligatorio)
				}
			}
			if !strings.Contains(unit, "MemoryMax=") {
				t.Errorf("la unit del %s no lleva techo de memoria, y el droplet es compartido", nombre)
			}
		}
	})

	t.Run("el techo del registro cubre la derivación", func(t *testing.T) {
		// Esto existe por un fallo de producción, no por prolijidad. La unit
		// llevaba MemoryMax=96M, elegido a ojo, contra un Argon2id que pide 64
		// MiB y corre dos veces por sala creada. El primer POST real murió por
		// OOM: el kernel mata en silencio y systemd reinicia, así que lo único
		// visible era un servicio que "se reinicia solo". Un techo por debajo
		// del coste de derivar no rompe nada al arrancar ni al servir la
		// página; rompe exactamente cuando alguien crea la primera sala.
		m := regexp.MustCompile(`MemoryMax=(\d+)M`).FindStringSubmatch(reg)
		if m == nil {
			t.Fatal("la unit del registro no declara MemoryMax en MiB")
		}
		techo, _ := strconv.Atoi(m[1])
		derivacion := domain.ArgonMemoryKiB / 1024
		if techo < 2*derivacion {
			t.Errorf("MemoryMax=%dM, y cada sala creada pide %d MiB dos veces seguidas: "+
				"el registro muere por OOM al crear la primera", techo, derivacion)
		}
	})

	t.Run("el puerto elegido llega a la unit", func(t *testing.T) {
		if !strings.Contains(reg, "127.0.0.1:8042") {
			t.Error("el puerto de la configuración no aparece en la unit: habría dos verdades")
		}
	})
}

// TestElBloqueDeProxyDiceElPuerto cuida el dato por el que existe todo el
// resumen del instalador.
func TestElBloqueDeProxyDiceElPuerto(t *testing.T) {
	b := BloqueDeProxy(Config{PuertoRegistro: 8042, Dominio: "kanpachi.test"})
	if !strings.Contains(b, "8042") {
		t.Error("el bloque del proxy no menciona el puerto, que es su único motivo de existir")
	}
	if !strings.Contains(b, "kanpachi.test") {
		t.Error("el bloque del proxy no menciona el dominio")
	}
	if !strings.Contains(b, "X-Forwarded-For") {
		t.Error("falta X-Forwarded-For: sin esa cabecera el límite de tasa cuenta todas las visitas como una")
	}
}

func TestURLEasyTierEstaFijada(t *testing.T) {
	url, suma, err := URLEasyTier()
	if err != nil {
		t.Skipf("no hay build fijado para esta arquitectura: %v", err)
	}
	if strings.Contains(url, "latest") {
		t.Error("la URL apunta a latest: una actualización sorpresa cambia la red de todo el grupo")
	}
	if !strings.Contains(url, VersionEasyTier) {
		t.Errorf("la URL no lleva la versión fijada: %s", url)
	}
	if len(suma) != 64 {
		t.Errorf("el SHA256 fijado mide %d caracteres, deben ser 64", len(suma))
	}
}
