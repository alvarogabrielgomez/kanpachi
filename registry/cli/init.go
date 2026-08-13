package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/accentiostudios/kanpachi/registry"
	"github.com/accentiostudios/kanpachi/registry/setup"
)

func cmdInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	dominio := fs.String("domain", "", "domain the page is served on, only used to print the proxy block")
	puerto := fs.Int("port", 0, "internal port of the registry. Defaults to the first free one from 8010")
	puertoMotor := fs.Int("engine-port", 0, "public port of the engine. Defaults to 11010")
	pagina := fs.String("page", "", "path to index.html. Defaults to the one next to the binary")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if err := requiereLinux(); err != nil {
		return err
	}
	if err := requiereRoot("init"); err != nil {
		return err
	}

	banner("Kanpachi seed", "one run configures everything")

	// Si ya está instalado, se respeta lo que hay. Reinstalar no puede mover
	// el puerto en silencio: el proxy inverso de la máquina apunta a él.
	previa, err := setup.Cargar()
	yaInstalado := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if yaInstalado {
		aviso("there is already a seed installed here, listening on port %d", previa.PuertoRegistro)
		// El texto tiene que decir lo que va a pasar de verdad. Anunciar que se
		// conservan los puertos mientras se está moviendo uno a petición del
		// usuario es peor que no decir nada: quien lo lee deja de mirar el
		// número del resumen, que es el único dato por el que existe.
		if *puerto != 0 && *puerto != previa.PuertoRegistro {
			tenue("  it is being moved to %d, so the reverse proxy needs updating", *puerto)
		} else {
			tenue("  the current ports are kept. To change them: kanpseed config")
		}
		fmt.Println()
	}

	cfg, err := decidirPuertos(previa, yaInstalado, *puerto, *puertoMotor)
	if err != nil {
		return err
	}
	cfg.Dominio = *dominio
	if cfg.Dominio == "" {
		cfg.Dominio = previa.Dominio
	}
	if cfg.Dominio == "" {
		cfg.Dominio = preguntar("What domain will the page be served on?", "kanpachi.accentio.dev")
	}

	seccion("Installing")

	descargado, err := setup.InstalarEasyTier(setup.DirLib, func(m string) { tenue("  %s", m) })
	if err != nil {
		return err
	}
	if descargado {
		ok("EasyTier %s installed in %s", setup.VersionEasyTier, setup.DirLib)
	} else {
		ok("EasyTier %s was already installed", setup.VersionEasyTier)
	}

	if err := instalarPagina(*pagina); err != nil {
		return err
	}
	ok("invitation page in %s", filepath.Join(setup.DirLib, "index.html"))

	if err := instalarseAsiMismo(); err != nil {
		return err
	}

	if err := cfg.Guardar(); err != nil {
		return err
	}
	ok("configuration in %s", setup.RutaConfig())

	if _, err := setup.EscribirUnits(cfg); err != nil {
		return err
	}
	ok("services written to %s", setup.DirUnits)

	seccion("Starting")
	for _, u := range []string{setup.UnitMotor, setup.UnitReg} {
		if err := setup.Systemctl("enable", u); err != nil {
			return err
		}
	}
	// Se reinicia en vez de arrancar, para que una segunda ejecución de init
	// aplique los cambios en vez de no hacer nada porque ya estaba corriendo.
	if err := setup.Systemctl("restart", setup.UnitMotor); err != nil {
		return err
	}
	if err := setup.Systemctl("restart", setup.UnitReg); err != nil {
		return fmt.Errorf("%w\n\nLast lines of the journal:\n%s", err, setup.LogsDeUnit(setup.UnitReg, 15))
	}

	if err := esperarSalud(cfg, 20*time.Second); err != nil {
		return fmt.Errorf("%w\n\nLast lines of the journal:\n%s", err, setup.LogsDeUnit(setup.UnitReg, 15))
	}
	ok("the registry answers on 127.0.0.1:%d", cfg.PuertoRegistro)
	ok("the engine listens on %d, TCP and UDP", cfg.PuertoMotor)

	if err := pasoDelPassword(cfg); err != nil {
		return err
	}

	imprimirResumen(cfg)
	avisarCortafuegos(cfg)
	return nil
}

// pasoDelPassword ofrece cerrar el seed, al final y no al principio.
//
// # Por qué al final, y por qué preguntando
//
// Al final porque el password se guarda en el directorio de estado que systemd
// acaba de crear, y porque cerrar un seed que todavía no arrancó es una promesa
// sobre algo que no se ha visto funcionar.
//
// Preguntando, y con «no» por defecto, porque **abierto es el caso normal**: un
// seed que se levanta para tres amigos no gana nada con un password, y el roce
// de tenerlo lo paga cada vez que alguien abre una sala. Cerrar es para quien
// publica el suyo y no quiere hospedar a internet entera.
//
// Que falle NO deshace la instalación. El seed quedó levantado y sirviendo, y
// lo que falta se arregla con un comando que existe: se dice cuál.
func pasoDelPassword(cfg setup.Config) error {
	if !interactivo() {
		return nil
	}
	seccion("Hosting")
	tenue("  Anyone who reaches this seed can open rooms on it. Entering a room")
	tenue("  never asks for anything, and it stays that way either way.")
	fmt.Println()
	if !confirmar("Ask for a password to HOST on this seed?", false) {
		tenue("  left open. To close it later: sudo kanpseed password")
		return nil
	}

	auth, err := registry.OpenAuth(setup.DirState)
	if err != nil {
		return err
	}
	if err := closeTheSeed(auth, cfg); err != nil {
		aviso("the password was not set: %v", err)
		tenue("  the seed is up and open. To try again: sudo kanpseed password")
	}
	return nil
}

// decidirPuertos elige puertos libres, respetando lo ya instalado y lo pedido.
//
// El del motor se avisa fuerte si hay que moverlo: los clientes lo llevan
// compilado, así que cambiarlo obliga a releasear el cliente.
func decidirPuertos(previa setup.Config, yaInstalado bool, pedido, pedidoMotor int) (setup.Config, error) {
	c := setup.Config{}

	// Un puerto ya instalado se CONSERVA sin comprobar si está libre.
	//
	// Comprobarlo era el error: quien lo tiene tomado mientras init se ejecuta
	// es el propio registro, que sigue corriendo. Sondear con un bind lo daba
	// por ocupado y elegía el siguiente, así que reinstalar movía el servicio
	// del 8010 al 8011 y dejaba el proxy inverso apuntando al vacío. Sin error,
	// sin aviso: el resumen imprimía el puerto nuevo como si fuera lo normal.
	//
	// Si de verdad se lo hubiera llevado otro proceso, el restart falla y se ve,
	// con las líneas del diario incluidas. Un error a la vista es mejor que un
	// puerto movido a espaldas de quien configuró el proxy.
	switch {
	case pedido != 0 && yaInstalado && pedido == previa.PuertoRegistro:
		c.PuertoRegistro = pedido
	case pedido != 0:
		p, err := setup.PuertoLibre(setup.RangoInternoDesde, setup.RangoInternoHasta, pedido)
		if err != nil {
			return c, err
		}
		if p != pedido {
			return c, fmt.Errorf("port %d is taken", pedido)
		}
		c.PuertoRegistro = p
	case yaInstalado && previa.PuertoRegistro != 0:
		c.PuertoRegistro = previa.PuertoRegistro
	default:
		p, err := setup.PuertoLibre(setup.RangoInternoDesde, setup.RangoInternoHasta, setup.PuertoRegPorDefecto)
		if err != nil {
			return c, err
		}
		c.PuertoRegistro = p
	}

	// El del RPC va por el mismo criterio, y por el mismo motivo: lo tiene
	// tomado el motor, que también sigue vivo mientras init corre.
	if yaInstalado && previa.PuertoRPC != 0 {
		c.PuertoRPC = previa.PuertoRPC
	} else {
		rpc, err := setup.PuertoLibre(setup.RangoRPCDesde, setup.RangoRPCHasta, setup.PuertoRPCPorDefecto)
		if err != nil {
			return c, err
		}
		c.PuertoRPC = rpc
	}

	motor := setup.PuertoMotorPorDefecto
	switch {
	case pedidoMotor != 0:
		motor = pedidoMotor
	case yaInstalado:
		motor = previa.PuertoMotor
	}
	c.PuertoMotor = motor

	// El puerto del motor solo se comprueba si está libre AHORA. Si el propio
	// seed ya lo tiene tomado, ocupado es lo correcto y no un problema.
	if !yaInstalado && !setup.PuertoPublicoLibre(motor) {
		aviso("port %d is already taken on this machine", motor)
		tenue("  clients carry that port compiled in, so moving it forces publishing a")
		tenue("  new version of the client. Check what is holding it.")
		if !confirmar("Carry on anyway?", false) {
			return c, fmt.Errorf("cancelled: the engine port %d is taken", motor)
		}
	}
	return c, nil
}

// instalarPagina copia index.html junto a los binarios.
//
// Se busca al lado del binario que se está ejecutando, que es donde la deja el
// paquete de instalación, y si no aparece se pide la ruta. Sin página no hay
// nada que servir, así que esto no puede fallar en silencio.
func instalarPagina(indicada string) error {
	destino := filepath.Join(setup.DirLib, "index.html")

	candidatas := []string{}
	if indicada != "" {
		candidatas = append(candidatas, indicada)
	}
	if propio, err := os.Executable(); err == nil {
		dir := filepath.Dir(propio)
		candidatas = append(candidatas,
			filepath.Join(dir, "index.html"),
			filepath.Join(dir, "invite", "index.html"),
		)
	}
	candidatas = append(candidatas, destino)

	for _, c := range candidatas {
		if _, err := os.Stat(c); err != nil {
			continue
		}
		if c == destino {
			return nil
		}
		return copiarArchivo(c, destino, 0o644)
	}
	return fmt.Errorf("index.html not found. Point at it with --page /path/to/index.html")
}

// instalarseAsiMismo copia el binario a /usr/local/bin si se está ejecutando
// desde otro sitio, que es el caso de un `./kanpseed init` recién bajado.
// La unit apunta a la ruta fija, así que el binario tiene que estar ahí.
func instalarseAsiMismo() error {
	propio, err := os.Executable()
	if err != nil {
		return err
	}
	destino := filepath.Join(setup.DirBin, setup.Binario)
	if propio == destino {
		return nil
	}
	if err := os.MkdirAll(setup.DirBin, 0o755); err != nil {
		return err
	}
	if err := copiarArchivo(propio, destino, 0o755); err != nil {
		return err
	}
	ok("binary installed in %s", destino)
	return nil
}

func copiarArchivo(origen, destino string, modo os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(destino), 0o755); err != nil {
		return err
	}
	in, err := os.Open(origen)
	if err != nil {
		return err
	}
	defer in.Close()

	// Temporal y rename: reemplazar un binario en uso falla o deja basura, y
	// renombrar encima es atómico.
	tmp := destino + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, modo)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, destino)
}

func imprimirResumen(c setup.Config) {
	seccion("Done")
	fmt.Println()
	fmt.Println(cCaja.Render(
		cTitulo.Render("INTERNAL REGISTRY PORT: "+strconv.Itoa(c.PuertoRegistro)) + "\n" +
			cTenue.Render("This is what goes in the reverse proxy")))
	fmt.Println()
	fmt.Print(setup.BloqueDeProxy(c))
	seccion("Next")
	codigo(
		"kanpseed doctor      checks that everything is fine",
		"kanpseed nginx       reprints the block above",
		"journalctl -u "+setup.UnitReg+" -f",
	)
	fmt.Println()
}
