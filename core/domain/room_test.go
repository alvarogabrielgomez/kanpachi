package domain

import (
	"errors"
	"net/netip"
	"testing"
)

// TestElSeedEsUnNombreYJamásUnaDirección.
//
// El host del código lo elige quien manda el código. De ahí salen dos destinos
// reales: el cliente HTTP que consulta el registro, y los `--peers` con los que
// arranca el motor. Un literal de IP los apunta a la red de casa del usuario o a
// la propia máquina, con el daemon corriendo como SYSTEM.
//
// La tabla cubre la familia entera de formas de escribir una IPv4, no solo la
// obvia: el resolver del sistema acepta varias que `netip.ParseAddr` rechaza.
func TestElSeedEsUnNombreYJamásUnaDirección(t *testing.T) {
	rechazados := map[string]string{
		"A7K2M9QX@127.0.0.1":       "loopback en la forma normal",
		"A7K2M9QX@127.1":           "loopback abreviado, que el resolver expande",
		"A7K2M9QX@0x7f.0.0.1":      "loopback en hexadecimal",
		"A7K2M9QX@192.168.1.10":    "la LAN de casa",
		"A7K2M9QX@10.0.0.1":        "red privada",
		"A7K2M9QX@169.254.169.254": "el endpoint de metadatos de las nubes",
		"A7K2M9QX@100.87.3.1":      "el espacio donde viven las salas",
		"A7K2M9QX@1.2.3.4":         "una IP pública sigue siendo una IP",
		"A7K2M9QX@localhost":       "sin punto, ya se rechazaba",
	}
	for entrada, motivo := range rechazados {
		if r, err := ParseRoom(entrada); err == nil {
			t.Errorf("%s: se aceptó %q con seed %q", motivo, entrada, r.Seed)
		}
	}

	aceptados := []string{
		"A7K2M9QX@kanpachi.accentio.dev",
		"A7K2M9QX@seed.midominio.com",
		"A7K2M9QX@1a.example.org",
		"A7K2M9QX@x1.co",
	}
	for _, entrada := range aceptados {
		if _, err := ParseRoom(entrada); err != nil {
			t.Errorf("se rechazó un nombre legítimo %q: %v", entrada, err)
		}
	}
}

// TestLaSegundaCapaMiraLoQueResolvióElNombre.
//
// Hace falta aparte del parseo porque nada impide registrar un dominio cuyo A
// apunte a 192.168.1.1. La llaman los adaptadores antes de conectar, y en cada
// uso: `last-room.json` guarda el código con su seed, así que volver a la última
// sala vuelve a hablarle y el DNS pudo cambiar entre una vez y la otra.
func TestLaSegundaCapaMiraLoQueResolvióElNombre(t *testing.T) {
	reservadas := []string{
		"127.0.0.1", "10.1.2.3", "192.168.0.1", "172.16.0.1",
		"169.254.169.254", "100.87.3.1", "0.0.0.0", "224.0.0.1",
		"::1", "fe80::1", "fd00::1", "::ffff:127.0.0.1",
	}
	for _, s := range reservadas {
		a := netip.MustParseAddr(s)
		if err := CheckSeedAddr(a); !errors.Is(err, ErrSeedAddrReserved) {
			t.Errorf("CheckSeedAddr(%s) = %v", s, err)
		}
	}

	públicas := []string{"1.1.1.1", "203.0.114.1", "2606:4700::1111"}
	for _, s := range públicas {
		if err := CheckSeedAddr(netip.MustParseAddr(s)); err != nil {
			t.Errorf("CheckSeedAddr(%s) rechazó una dirección de internet: %v", s, err)
		}
	}

	if err := CheckSeedAddr(netip.Addr{}); !errors.Is(err, ErrSeedAddrReserved) {
		t.Errorf("CheckSeedAddr de una dirección vacía = %v", err)
	}
}

// TestLasCuatroFormasDeMeterUnaIPv4DentroDeUnaIPv6.
//
// Salieron de una revisión adversarial y las siete entradas de acá PASABAN. El
// código solo llamaba a Unmap, que desenvuelve una sola de las cuatro familias,
// mientras el comentario afirmaba que cerraba el disfraz entero. Las otras tres
// existen justamente para transportar una IPv4, así que sin ellas la tabla de
// rangos privados no gobernaba nada: `64:ff9b::192.168.1.1` es la LAN de casa
// escrita de otra forma.
func TestLasCuatroFormasDeMeterUnaIPv4DentroDeUnaIPv6(t *testing.T) {
	disfraces := map[string]string{
		"::ffff:192.168.1.1":         "IPv4 mapeada, la que Unmap sí desenvuelve",
		"::ffff:169.254.169.254":     "IPv4 mapeada, metadatos de la nube",
		"::127.0.0.1":                "IPv4 compatible, deprecada",
		"::192.168.1.1":              "IPv4 compatible, la LAN",
		"64:ff9b::192.168.1.1":       "NAT64 con el prefijo bien conocido",
		"64:ff9b::a9fe:a9fe":         "NAT64 apuntando a los metadatos",
		"2002:c0a8:0101::1":          "6to4, que además encapsula hacia esa IPv4",
		"2002:7f00:0001::1":          "6to4 hacia loopback",
		"2001:0:53aa:64c::3f57:fefd": "Teredo",
	}
	for s, forma := range disfraces {
		if err := CheckSeedAddr(netip.MustParseAddr(s)); !errors.Is(err, ErrSeedAddrReserved) {
			t.Errorf("%s (%s) pasó la comprobación: %v", s, forma, err)
		}
	}
}

// TestUnaZonaNoDesactivaLaTabla.
//
// `netip.Prefix.Contains` devuelve false para cualquier dirección con zona, así
// que antes de quitarla `fd00::1%eth0` no coincidía con NINGÚN prefijo y salía
// aprobada. No fallaba: aprobaba, que es la peor forma de romperse.
func TestUnaZonaNoDesactivaLaTabla(t *testing.T) {
	for _, s := range []string{"fd00::1%eth0", "fe80::1%wifi", "::ffff:10.0.0.1%0"} {
		a, err := netip.ParseAddr(s)
		if err != nil {
			t.Fatal(err)
		}
		if err := CheckSeedAddr(a); !errors.Is(err, ErrSeedAddrReserved) {
			t.Errorf("%s pasó la comprobación con zona: %v", s, err)
		}
	}
}

// TestLosRangosQueNingúnHelperDeNetipCubre: los que hay que escribir a mano
// porque la biblioteca no los conoce.
func TestLosRangosQueNingúnHelperDeNetipCubre(t *testing.T) {
	aMano := map[string]string{
		"fec0::1":     "site-local, deprecado y sin helper",
		"192.88.99.1": "anycast de relé 6to4",
		"100.87.3.1":  "el espacio compartido donde viven las salas",
		"100::1":      "agujero negro",
		"198.18.0.1":  "pruebas de rendimiento",
		"3fff::1":     "documentación nueva",
		"5f00::1":     "segmentos de SRv6",
	}
	for s, motivo := range aMano {
		if err := CheckSeedAddr(netip.MustParseAddr(s)); !errors.Is(err, ErrSeedAddrReserved) {
			t.Errorf("%s (%s) pasó: %v", s, motivo, err)
		}
	}
}
