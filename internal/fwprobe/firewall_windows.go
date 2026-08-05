//go:build windows

package main

// Los subcomandos que tocan el firewall de verdad.
//
// Ni una llamada al sistema propia: todo pasa por `adapter/firewall`, que es el
// mismo objeto que usa el daemon. Si acá hiciera falta un atajo, sería la señal
// de que al adaptador le falta algo, y el atajo taparía justo eso.

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/netip"
	"os"
	"sort"
	"strings"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/daemon/adapter/firewall"
	"github.com/accentiostudios/kanpachi/daemon/adapter/firewall/wfp"
	"github.com/accentiostudios/kanpachi/daemon/adapter/firewall/windowscom"
)

// abrir monta el firewall compuesto igual que lo montaría el daemon.
//
// Exige elevación, porque abre la sesión de WFP y eso WFP lo pide hasta para
// mirar. Los subcomandos que solo leen reglas usan [abrirPermisos] en vez de
// esto: pedir administrador para enseñar una lista sería pedirlo para nada, y de
// paso convertiría en incómodo justo el comando que enseña el agujero vivo.
func abrir(dataDir string) (*firewall.Firewall, func() error, error) {
	if dataDir == "" {
		return nil, nil, fmt.Errorf("falta -data. Es donde se anota el estado previo de las " +
			"reglas ajenas antes de tocarlas, y sin eso no habría cómo devolverlas")
	}
	if _, err := os.Stat(dataDir); err != nil {
		return nil, nil, fmt.Errorf("el directorio %s no está: %w", dataDir, err)
	}
	return firewall.NewWindows(dataDir, logConsola{})
}

// abrirPermisos abre SOLO la capa de las reglas del Firewall de Windows.
//
// El directorio de datos no se comprueba acá: quien lo necesita es suspender
// reglas ajenas, y desde estos subcomandos no se suspende nada.
func abrirPermisos(dataDir string) (*windowscom.Firewall, error) {
	if dataDir == "" {
		dataDir = os.Getenv("ProgramData") + `\Kanpachi`
	}
	return windowscom.New(dataDir, "", logConsola{})
}

// adapters lista lo que hace falta para elegir un alcance.
//
// El LUID es lo que WFP entiende, y no el nombre ni el índice. Se saca por el
// mismo camino que usará el daemon.
func adapters() error {
	ifaces, err := net.Interfaces()
	if err != nil {
		return err
	}
	sort.Slice(ifaces, func(i, j int) bool { return ifaces[i].Name < ifaces[j].Name })

	fmt.Printf("%-34s %-18s %-8s %s\n", "NOMBRE", "LUID", "ESTADO", "IPv4")
	for _, i := range ifaces {
		luid, err := wfp.LUIDOf(i.Name)
		if err != nil {
			// Sin LUID no se puede acotar, y saltarla es mejor que mostrar cero:
			// WFP lee un LUID cero como TODAS las interfaces.
			continue
		}

		estado := "abajo"
		if i.Flags&net.FlagUp != 0 {
			estado = "arriba"
		}

		var ips []string
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil {
				ips = append(ips, ipn.IP.String())
			}
		}
		fmt.Printf("%-34s 0x%-16X %-8s %s\n", i.Name, luid, estado, strings.Join(ips, " "))
	}
	return nil
}

// audit es el agujero que está vivo hoy, hecho visible.
//
// Busca reglas permisivas de escritorio remoto que Kanpachi no creó. Es el único
// camino conocido por el que alguien de la sala consigue teclado, pantalla y
// sistema de archivos del host: la cuarentena tapa el escritorio remoto ESTÁNDAR
// por puerto, y estas herramientas escuchan donde el usuario les diga.
func audit(args []string) error {
	fs := flag.NewFlagSet("audit", flag.ExitOnError)
	datos := fs.String("data", "", "directorio de datos. Vacío usa ProgramData\\Kanpachi")
	if err := fs.Parse(args); err != nil {
		return err
	}

	fw, err := abrirPermisos(*datos)
	if err != nil {
		return err
	}
	defer func() { _ = fw.Close() }()

	// Sin perfil de juego a propósito: la auditoría de control remoto vale igual,
	// y es la mitad que de verdad importa.
	reglas, err := fw.AuditForeign(context.Background(), domain.GameProfile{})
	if err != nil {
		return err
	}
	if len(reglas) == 0 {
		fmt.Println("No hay reglas ajenas de escritorio remoto en el almacén.")
		return nil
	}

	fmt.Printf("%d regla(s) ajena(s):\n\n", len(reglas))
	for _, r := range reglas {
		fmt.Printf("  %s\n", r.Name)
		fmt.Printf("    ejecutable  %s\n", r.Executable)
		fmt.Printf("    perfiles    %v\n", r.Profiles)
		fmt.Printf("    encendida   %v\n", r.WasEnabled)
		if r.Blocking() {
			fmt.Println("    ESTO ENTREGA TECLADO, PANTALLA Y FICHEROS a quien alcance el host")
		}
		fmt.Println()
	}

	bloqueantes := domain.BlockingForeign(reglas)
	if len(bloqueantes) > 0 {
		fmt.Printf("%d de ellas hay que resolverlas ANTES de abrir una sala.\n", len(bloqueantes))
	}
	return nil
}

func enabled(args []string) error {
	fs := flag.NewFlagSet("enabled", flag.ExitOnError)
	datos := fs.String("data", "", "directorio de datos. Vacío usa ProgramData\\Kanpachi")
	if err := fs.Parse(args); err != nil {
		return err
	}

	fw, err := abrirPermisos(*datos)
	if err != nil {
		return err
	}
	defer func() { _ = fw.Close() }()

	perfiles, err := windowscom.NewAudit(fw).FirewallEnabled(context.Background())
	if err != nil {
		return err
	}
	for _, p := range perfiles {
		estado := "APAGADO"
		if p.Enabled {
			estado = "encendido"
		}
		fmt.Printf("%-10v %s\n", p.Profile, estado)
	}
	return nil
}

// state es la medición de las dos capas.
//
// La compuerta sale AUSENTE si este proceso no es el que la puso, y eso no es un
// fallo: la sesión de WFP no es dinámica, así que los filtros sobreviven al
// proceso, y lo que no sobrevive es saber por qué reglas preguntar. El estado de
// la compuerta en sí se mide por una clave fija, así que ese sí es real.
func state(args []string) error {
	fs := flag.NewFlagSet("state", flag.ExitOnError)
	datos := fs.String("data", "", "directorio de datos")
	if err := fs.Parse(args); err != nil {
		return err
	}

	fw, cerrar, err := abrir(*datos)
	if err != nil {
		return err
	}
	defer func() { _ = cerrar() }()

	e, err := fw.Enforcement(context.Background())
	if err != nil {
		return err
	}

	fmt.Printf("compuerta: %v\n", e.Gate)
	if len(e.Rules) == 0 {
		fmt.Println("reglas:    ninguna del grupo propio")
		return nil
	}
	fmt.Println("reglas:")
	for _, r := range e.Rules {
		estado := "apagada"
		if r.Enabled {
			estado = "puesta"
		}
		fmt.Printf("  [%v] %-50s %s\n", r.Layer, r.Name, estado)
	}
	return nil
}

// apply pone las dos capas sobre un adaptador de verdad.
//
// El conjunto es sintético, y tiene que serlo: el adaptador virtual todavía no
// existe porque el motor no está escrito. Lo que se mide igual es lo que importa
// de la compuerta, que es el arbitraje: el bloqueo de todo acotado a un
// adaptador, y un permiso espejo que le gana en un puerto.
func apply(args []string) error {
	fs := flag.NewFlagSet("apply", flag.ExitOnError)
	datos := fs.String("data", "", "directorio de datos")
	nombre := fs.String("adapter", "", "nombre del adaptador, de `fwprobe adapters`")
	sala := fs.String("room", "100.64.1.0/24", "rango de la sala")
	peer := fs.String("peer", "", "IP de la otra máquina")
	puerto := fs.Uint("open", 0, "el único puerto que queda abierto")
	proto := fs.String("proto", "tcp", "tcp o udp")
	sí := fs.Bool("yes", false, "confirmar que se entiende lo que bloquea")
	if err := fs.Parse(args); err != nil {
		return err
	}

	switch {
	case *nombre == "":
		return fmt.Errorf("falta -adapter. Corre `fwprobe adapters`")
	case *peer == "":
		return fmt.Errorf("falta -peer, la IP de la otra máquina")
	case *puerto == 0 || *puerto > 65535:
		return fmt.Errorf("falta -open, el puerto que queda abierto")
	}

	if !*sí {
		fmt.Printf(`ATENCIÓN, esto no es un simulacro.

Bloquea TODO lo entrante del adaptador %q salvo el puerto %d, que solo queda
abierto para %s. Mientras esté puesto, compartir archivos, impresoras y
cualquier cosa que reciba conexiones dejan de funcionar POR ESE ADAPTADOR.

La SALIDA no se toca, así que navegar, Discord y jugar de cliente siguen igual.

Se deshace con:  fwprobe purge -data %s
Y si algo sale mal, reiniciar la máquina también: los filtros no son persistentes.

Vuelve a correrlo con -yes cuando lo tengas claro.
`, *nombre, *puerto, *peer, *datos)
		return fmt.Errorf("falta -yes")
	}

	rs, err := conjuntoSintético(*nombre, *peer, uint16(*puerto), *proto)
	if err != nil {
		return err
	}
	red, err := netip.ParsePrefix(*sala)
	if err != nil {
		return fmt.Errorf("-room %q: %w", *sala, err)
	}
	luid, err := wfp.LUIDOf(*nombre)
	if err != nil {
		return err
	}

	fw, cerrar, err := abrir(*datos)
	if err != nil {
		return err
	}
	defer func() { _ = cerrar() }()

	if err := fw.SetScopeForMeasurement(*nombre, luid, red); err != nil {
		return err
	}
	if err := fw.Apply(context.Background(), rs); err != nil {
		return err
	}

	fmt.Printf("\nPuesto sobre %s (LUID 0x%X).\n", *nombre, luid)
	fmt.Printf("Abierto: %s/%d para %s. Todo lo demás entrante de ese adaptador, cerrado.\n",
		*proto, *puerto, *peer)
	fmt.Println("\nAhora, en OTRA ventana:  fwprobe listen -ports " + fmt.Sprint(*puerto) + ",<otro puerto>")
	return nil
}

// conjuntoSintético arma la regla con la dirección REAL del adaptador.
//
// La dirección local sale de la máquina y no se inventa: un permiso espejo con
// una dirección que no existe en el adaptador no casa con nada, y el resultado
// se vería igual que un bloqueo funcionando.
func conjuntoSintético(adaptador, peer string, puerto uint16, proto string) (domain.RuleSet, error) {
	var rs domain.RuleSet

	local, err := ipv4De(adaptador)
	if err != nil {
		return rs, err
	}
	remoto, err := netip.ParseAddr(peer)
	if err != nil {
		return rs, fmt.Errorf("-peer %q: %w", peer, err)
	}

	var p domain.Proto
	switch strings.ToLower(proto) {
	case "tcp":
		p = domain.ProtoTCP
	case "udp":
		p = domain.ProtoUDP
	default:
		return rs, fmt.Errorf("-proto %q: solo tcp o udp. `both` lo expande el dominio "+
			"en dos reglas antes de llegar al firewall", proto)
	}

	rs.Rules = append(rs.Rules, domain.FirewallRule{
		Name:   fmt.Sprintf("%s: medición %s %d", domain.FirewallGroup, proto, puerto),
		Proto:  p,
		From:   puerto,
		To:     puerto,
		Local:  local,
		Remote: []netip.Addr{remoto},
	})
	return rs, nil
}

func ipv4De(nombre string) (netip.Addr, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return netip.Addr{}, err
	}
	for _, i := range ifaces {
		if !strings.EqualFold(i.Name, nombre) {
			continue
		}
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			if v4 := ipn.IP.To4(); v4 != nil {
				addr, ok := netip.AddrFromSlice(v4)
				if !ok {
					continue
				}
				return addr, nil
			}
		}
		return netip.Addr{}, fmt.Errorf("el adaptador %q no tiene IPv4", nombre)
	}
	return netip.Addr{}, fmt.Errorf("no hay ningún adaptador llamado %q", nombre)
}

// purge deja la máquina como estaba.
//
// Es idempotente: que no haya nada que quitar es el resultado normal, y se puede
// correr las veces que haga falta.
func purge(args []string) error {
	fs := flag.NewFlagSet("purge", flag.ExitOnError)
	datos := fs.String("data", "", "directorio de datos")
	if err := fs.Parse(args); err != nil {
		return err
	}

	fw, cerrar, err := abrir(*datos)
	if err != nil {
		return err
	}
	defer func() { _ = cerrar() }()

	if err := fw.PurgeOwned(context.Background()); err != nil {
		return err
	}
	if err := fw.RestoreForeign(context.Background()); err != nil {
		return err
	}

	e, err := fw.Enforcement(context.Background())
	if err != nil {
		return err
	}
	if e.Gate == domain.GatePresent || len(e.Rules) > 0 {
		return fmt.Errorf("después de purgar sigue habiendo cosas puestas: compuerta %v, "+
			"%d regla(s). Eso no debería pasar", e.Gate, len(e.Rules))
	}
	fmt.Println("La máquina está limpia.")
	return nil
}
