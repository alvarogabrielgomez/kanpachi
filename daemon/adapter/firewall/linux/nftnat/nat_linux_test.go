//go:build linux

package nftnat

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// El enrutado a loopback se habilita SIN escribir en /proc/sys.
//
// # Por qué este test existe
//
// Porque la primera versión escribía en `/proc/sys/net/ipv4/conf/<iface>/
// route_localnet`, y eso funciona en una máquina y no en un contenedor, que es
// justo donde el desvío existe. Medido el 2026-08-26 contra el pod real:
//
//	no se pudo desviar hacia donde escucha el juego [hacia 127.0.1.1
//	error ... route_localnet: read-only file system]
//
// Y porque la segunda versión, ya por netlink, mandó el devconf como array
// plano de u32: el kernel contestó ACK, el log dijo que todo bien, y el bit se
// quedó en cero. Un ACK no es una medición. Lo único que prueba algo es leer el
// valor después, que es lo que hace esto.
//
// Corre solo como root y con `ip` a mano, o sea en el job de Linux y en WSL. En
// una máquina sin permisos se salta en vez de fallar: lo que se mide acá es el
// kernel, y sin root no hay kernel que medir.
func TestElEnrutadoALoopbackSeHabilitaPorNetlink(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("hace falta root para tocar el devconf de una interfaz")
	}
	if _, err := exec.LookPath("ip"); err != nil {
		t.Skip("hace falta `ip` para crear la interfaz de prueba")
	}

	const iface = "kptest0"
	_ = exec.Command("ip", "link", "del", iface).Run()
	if out, err := exec.Command("ip", "link", "add", iface, "type", "dummy").CombinedOutput(); err != nil {
		t.Skipf("no se pudo crear la interfaz de prueba: %v: %s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("ip", "link", "del", iface).Run() })

	if out, err := exec.Command("ip", "link", "set", iface, "up").CombinedOutput(); err != nil {
		t.Fatalf("no se pudo levantar %s: %v: %s", iface, err, out)
	}

	ruta := "/proc/sys/net/ipv4/conf/" + iface + "/route_localnet"
	if antes := leer(t, ruta); antes != "0" {
		t.Fatalf("la interfaz nació con route_localnet en %q, y el test no mediría nada", antes)
	}

	if err := permitirLoopback(iface); err != nil {
		t.Fatalf("permitirLoopback: %v", err)
	}

	// Lo que cuenta es el valor, no el ACK.
	if después := leer(t, ruta); después != "1" {
		t.Fatalf("route_localnet quedó en %q, se esperaba 1", después)
	}

	// Y solo esa interfaz: abrirlo en `all` valdría para cualquier tarjeta de
	// la máquina, que es exactamente lo que este adaptador no puede hacer.
	if todas := leer(t, "/proc/sys/net/ipv4/conf/all/route_localnet"); todas != "0" {
		t.Fatalf("también quedó abierto en `all` (%q), y eso alcanza a toda la máquina", todas)
	}
}

func leer(t *testing.T, ruta string) string {
	t.Helper()
	crudo, err := os.ReadFile(ruta)
	if err != nil {
		t.Fatalf("leyendo %s: %v", ruta, err)
	}
	return strings.TrimSpace(string(crudo))
}
