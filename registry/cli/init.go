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

	"github.com/accentiostudios/kanpachi/registry/setup"
)

func cmdInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	dominio := fs.String("domain", "", "dominio por el que se sirve la página, solo para imprimir el bloque del proxy")
	puerto := fs.Int("port", 0, "puerto interno del registro. Por defecto, el primero libre desde 8010")
	puertoMotor := fs.Int("engine-port", 0, "puerto público del motor. Por defecto 11010")
	pagina := fs.String("page", "", "ruta a index.html. Por defecto, el que venga junto al binario")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if err := requiereLinux(); err != nil {
		return err
	}
	if err := requiereRoot("init"); err != nil {
		return err
	}

	banner("Kanpachi seed", "una ejecución configura todo")

	// Si ya está instalado, se respeta lo que hay. Reinstalar no puede mover
	// el puerto en silencio: el proxy inverso de la máquina apunta a él.
	previa, err := setup.Cargar()
	yaInstalado := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if yaInstalado {
		aviso("ya hay un seed instalado, escuchando en el puerto %d", previa.PuertoRegistro)
		tenue("  se conservan los puertos actuales. Para cambiarlos: kanpseed config")
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
		cfg.Dominio = preguntar("¿Por qué dominio se va a servir la página?", "kanpachi.accentio.dev")
	}

	seccion("Instalando")

	descargado, err := setup.InstalarEasyTier(setup.DirLib, func(m string) { tenue("  %s", m) })
	if err != nil {
		return err
	}
	if descargado {
		ok("EasyTier %s instalado en %s", setup.VersionEasyTier, setup.DirLib)
	} else {
		ok("EasyTier %s ya estaba instalado", setup.VersionEasyTier)
	}

	if err := instalarPagina(*pagina); err != nil {
		return err
	}
	ok("página de invitación en %s", filepath.Join(setup.DirLib, "index.html"))

	if err := instalarseAsiMismo(); err != nil {
		return err
	}

	if err := cfg.Guardar(); err != nil {
		return err
	}
	ok("configuración en %s", setup.RutaConfig())

	if _, err := setup.EscribirUnits(cfg); err != nil {
		return err
	}
	ok("servicios escritos en %s", setup.DirUnits)

	seccion("Arrancando")
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
		return fmt.Errorf("%w\n\nÚltimas líneas del diario:\n%s", err, setup.LogsDeUnit(setup.UnitReg, 15))
	}

	if err := esperarSalud(cfg, 20*time.Second); err != nil {
		return fmt.Errorf("%w\n\nÚltimas líneas del diario:\n%s", err, setup.LogsDeUnit(setup.UnitReg, 15))
	}
	ok("el registro responde en 127.0.0.1:%d", cfg.PuertoRegistro)
	ok("el motor escucha en el %d, TCP y UDP", cfg.PuertoMotor)

	imprimirResumen(cfg)
	return nil
}

// decidirPuertos elige puertos libres, respetando lo ya instalado y lo pedido.
//
// El del motor se avisa fuerte si hay que moverlo: los clientes lo llevan
// compilado, así que cambiarlo obliga a releasear el cliente.
func decidirPuertos(previa setup.Config, yaInstalado bool, pedido, pedidoMotor int) (setup.Config, error) {
	c := setup.Config{}

	preferido := setup.PuertoRegPorDefecto
	switch {
	case pedido != 0:
		preferido = pedido
	case yaInstalado:
		preferido = previa.PuertoRegistro
	}
	p, err := setup.PuertoLibre(setup.RangoInternoDesde, setup.RangoInternoHasta, preferido)
	if err != nil {
		return c, err
	}
	if p != preferido && pedido != 0 {
		return c, fmt.Errorf("el puerto %d está ocupado", pedido)
	}
	c.PuertoRegistro = p

	rpcPreferido := setup.PuertoRPCPorDefecto
	if yaInstalado {
		rpcPreferido = previa.PuertoRPC
	}
	rpc, err := setup.PuertoLibre(setup.RangoRPCDesde, setup.RangoRPCHasta, rpcPreferido)
	if err != nil {
		return c, err
	}
	c.PuertoRPC = rpc

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
		aviso("el puerto %d ya está ocupado en esta máquina", motor)
		tenue("  los clientes llevan ese puerto compilado, así que moverlo obliga a")
		tenue("  publicar una versión nueva del cliente. Revisa qué lo tiene tomado.")
		if !confirmar("¿Seguir de todas formas?", false) {
			return c, fmt.Errorf("cancelado: el puerto %d del motor está ocupado", motor)
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
	return fmt.Errorf("no encuentro index.html. Indícalo con --page /ruta/a/index.html")
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
	ok("binario instalado en %s", destino)
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
	seccion("Listo")
	fmt.Println()
	fmt.Println(cCaja.Render(
		cTitulo.Render("PUERTO INTERNO DEL REGISTRO: "+strconv.Itoa(c.PuertoRegistro)) + "\n" +
			cTenue.Render("Es el que hay que poner en el proxy inverso")))
	fmt.Println()
	fmt.Print(setup.BloqueDeProxy(c))
	seccion("Después")
	codigo(
		"kanpseed doctor      revisa que todo esté bien",
		"kanpseed nginx       vuelve a imprimir el bloque de arriba",
		"journalctl -u "+setup.UnitReg+" -f",
	)
	fmt.Println()
}
