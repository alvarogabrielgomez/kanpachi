// Command fwprobe corre el firewall DE VERDAD, para poder medirlo.
//
// # Por qué existe
//
// El adaptador del firewall está escrito y lo prueban los tests puros en Linux,
// que cubren lo que se decide. Lo que ningún test puede cubrir es si Windows
// hace lo que la documentación dice, y eso solo se sabe con dos máquinas y un
// puerto de verdad. Este binario es el que las junta, y usa el MISMO código que
// el daemon, sin atajos: si un camino de acá no funciona, el del producto
// tampoco.
//
// Es hermano del spike que decidió el diseño, con una diferencia que es todo el
// punto: aquel tenía llamadas propias a WFP y este no tiene ninguna.
//
// # La disciplina, que no es opcional
//
// Poner y quitar son subcomandos SEPARADOS, y entre medias se mide desde la otra
// máquina. Un estado suelto no prueba nada: si el firewall ya estaba abierto de
// una corrida anterior, "conecta" no dice quién lo abrió. Lo que se mide es la
// TRANSICIÓN.
//
// `purge` deja la máquina como estaba y se puede correr las veces que haga
// falta. La red de seguridad final es que los filtros de la compuerta no son
// persistentes, así que reiniciar se los lleva igual.
//
// Vive en `internal/` para que el producto no lo pueda importar y el instalador
// no lo distribuya.
package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

func main() {
	flag.Usage = usage
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	if err := run(os.Args[1], os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "fwprobe:", err)
		os.Exit(1)
	}
}

func run(cmd string, args []string) error {
	switch cmd {
	case "adapters":
		return adapters()
	case "audit":
		return audit(args)
	case "enabled":
		return enabled(args)
	case "state":
		return state(args)
	case "apply":
		return apply(args)
	case "purge":
		return purge(args)
	case "listen":
		return listen(args)
	case "probe":
		return probe(args)
	case "help", "-h", "--help":
		usage()
		return nil
	}
	usage()
	return fmt.Errorf("no existe el subcomando %q", cmd)
}

func usage() {
	fmt.Fprint(os.Stderr, `fwprobe corre el firewall de verdad, con el mismo código que el daemon.

  Sin elevar, y en cualquier máquina:
    adapters                       lista los adaptadores con su LUID y su IPv4
    listen  -ports 45871,45872     queda escuchando. NO se para hasta el final
    probe   -host IP -ports 1,2    marca desde la otra máquina y dice qué pasó

  Elevado, en el host:
    audit                          reglas ajenas de escritorio remoto, clasificadas
    enabled                        si el Firewall de Windows está encendido, por perfil
    state   -data DIR              qué hay puesto AHORA, medido en las dos capas
    apply   -data DIR -adapter N -peer IP -open PUERTO -yes
    purge   -data DIR              deja la máquina como estaba

  El guion de dos máquinas:

    1. host      fwprobe adapters                     y se anota el nombre
    2. host      fwprobe apply -adapter "Wi-Fi" -peer <IP de la otra> -open 45871 -yes -data C:\ruta
    3. host      fwprobe listen -ports 45871,45872    en OTRA ventana, y se deja
    4. otra PC   fwprobe probe -host <IP del host> -ports 45871,45872
    5. host      fwprobe purge -data C:\ruta
    6. otra PC   fwprobe probe -host <IP del host> -ports 45871,45872

  Se espera que 45871 conecte y 45872 no, en el paso 4, y que los DOS conecten
  en el paso 6. El paso 6 no es opcional: sin él, "no conecta" y "no había nadie
  escuchando" se ven idénticos.

`)
}

// listen atiende los puertos y no contesta nada.
//
// Aceptar y cerrar basta: lo que se mide es si el paquete LLEGA, y para eso el
// apretón de manos de TCP ya terminó.
func listen(args []string) error {
	fs := flag.NewFlagSet("listen", flag.ExitOnError)
	ports := fs.String("ports", "", "puertos separados por coma")
	if err := fs.Parse(args); err != nil {
		return err
	}

	list, err := parsePorts(*ports)
	if err != nil {
		return err
	}

	for _, p := range list {
		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", p))
		if err != nil {
			return fmt.Errorf("escuchando en %d: %w", p, err)
		}
		defer func() { _ = ln.Close() }()

		fmt.Printf("escuchando en %d\n", p)
		go func(ln net.Listener, p uint16) {
			for {
				c, err := ln.Accept()
				if err != nil {
					return
				}
				fmt.Printf("  %d <- %s\n", p, c.RemoteAddr())
				_ = c.Close()
			}
		}(ln, p)
	}

	fmt.Println("\nEsta ventana NO se para hasta el final de la medición.")
	fmt.Println("Si se para, la otra máquina falla igual que si el bloqueo funcionara.")
	select {}
}

// probe marca a cada puerto y dice qué pasó, con el tiempo que tardó.
//
// El tiempo importa: un rechazo inmediato y un timeout son cosas distintas. Lo
// primero suele ser que no hay nadie escuchando y lo segundo es un bloqueo que
// se traga el paquete, que es lo que hace la compuerta.
func probe(args []string) error {
	fs := flag.NewFlagSet("probe", flag.ExitOnError)
	host := fs.String("host", "", "IP de la máquina que escucha")
	ports := fs.String("ports", "", "puertos separados por coma")
	espera := fs.Duration("timeout", 6*time.Second, "cuánto esperar por puerto")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *host == "" {
		return fmt.Errorf("falta -host")
	}

	list, err := parsePorts(*ports)
	if err != nil {
		return err
	}

	for _, p := range list {
		destino := net.JoinHostPort(*host, strconv.Itoa(int(p)))
		inicio := time.Now()
		c, err := net.DialTimeout("tcp", destino, *espera)
		tardó := time.Since(inicio).Round(time.Millisecond)

		if err != nil {
			fmt.Printf("%-24s NO CONECTA (%v)  %v\n", destino, tardó, err)
			continue
		}
		_ = c.Close()
		fmt.Printf("%-24s CONECTA (%v)\n", destino, tardó)
	}
	return nil
}

func parsePorts(spec string) ([]uint16, error) {
	if strings.TrimSpace(spec) == "" {
		return nil, fmt.Errorf("falta -ports")
	}

	var out []uint16
	for _, campo := range strings.Split(spec, ",") {
		campo = strings.TrimSpace(campo)
		n, err := strconv.ParseUint(campo, 10, 16)
		if err != nil || n == 0 {
			return nil, fmt.Errorf("%q no es un puerto", campo)
		}
		out = append(out, uint16(n))
	}
	return out, nil
}
