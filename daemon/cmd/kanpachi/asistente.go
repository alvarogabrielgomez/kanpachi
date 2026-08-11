package main

// El asistente: lo que sale al escribir `kanpachi` a secas.
//
// # Para qué, si ya hay subcomandos
//
// Porque el caso normal de este binario es un servidor recién instalado y una
// persona que acaba de leer una línea del README. Los subcomandos son para el
// script y para la segunda vez; esto es para la primera, y para no tener que
// recordar que renovar el código se llama `rotate`.
//
// # Llama a las MISMAS funciones que los subcomandos
//
// Nada acá reimplementa una orden. Un asistente con su propio camino al daemon
// es un segundo sitio donde arreglar cada cosa, y el que se olvida es siempre el
// que menos se usa.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/AlecAivazis/survey/v2"
	"github.com/AlecAivazis/survey/v2/terminal"
	"golang.org/x/term"

	"github.com/accentiostudios/kanpachi/daemon/transport/client"
	"github.com/accentiostudios/kanpachi/daemon/transport/protocol"
)

func asistente(ctx context.Context, op opciones) error {
	// Sin terminal no hay flechas que pulsar. Se dice y se enseña la ayuda, en
	// vez de fallar con lo que survey diría, que habla de descriptores de
	// fichero y no de lo que le pasa a quien lo está corriendo.
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprintln(os.Stderr, "kanpachi: sin terminal no hay asistente que enseñar.")
		fmt.Fprintln(os.Stderr, "  Dentro de un script, un subcomando:")
		fmt.Fprintln(os.Stderr)
		ayuda(os.Stderr)
		return uso("hace falta un subcomando")
	}
	// El asistente nunca imprime JSON: es una pantalla. Que `--json` lo apagara
	// en silencio sería peor, así que se dice.
	if op.json {
		return uso("--json es para los subcomandos, y el asistente es una pantalla")
	}

	for {
		if ctx.Err() != nil {
			return errInterrumpido
		}
		st, err := estadoParaElMenú(op)
		if err != nil {
			return err
		}
		var siguiente error
		if st.Conn == "idle" || st.Conn == "" {
			siguiente = menuSinSala(ctx, op)
		} else {
			siguiente = menuConSala(ctx, op, st)
		}
		if errors.Is(siguiente, errSalir) {
			return nil
		}
		if siguiente != nil {
			return siguiente
		}
	}
}

// errSalir es haber elegido «Salir» en el menú, que no es un fallo.
var errSalir = errors.New("salir")

// estadoParaElMenú abre, pregunta y cierra.
//
// Una conexión por vuelta de menú y no una para todo el asistente, a propósito:
// entre dos vueltas puede haber pasado un minuto con alguien leyendo el menú, y
// una conexión ociosa ocupa una de las ocho plazas del oyente todo ese rato.
func estadoParaElMenú(op opciones) (protocol.RoomView, error) {
	c, err := abrir(op)
	if err != nil {
		return protocol.RoomView{}, err
	}
	defer func() { _ = c.Close() }()
	return client.Ask[protocol.RoomView](c, protocol.MethodStatus, nil)
}

// ─── Sin sala ────────────────────────────────────────────────────────────────

func menuSinSala(ctx context.Context, op opciones) error {
	limpiarPantalla(os.Stdout)
	fmt.Println(raya)
	fmt.Printf("  KANPACHI %-20s canal: %s\n", Version, op.canal)
	fmt.Println(raya)
	fmt.Println()

	const (
		abrir     = "Abrir una sala"
		entrar    = "Entrar a la sala de otro"
		volver    = "Volver a la última sala a la que entré"
		reanudar  = "Reabrir la sala que quedó del arranque anterior"
		descartar = "Olvidar esa sala pendiente"
		juegos    = "Ver el catálogo de juegos"
		comprobar = "Comprobar el sistema"
		nombre    = "Cambiar mi nombre"
		salir     = "Salir"
	)

	opciones := []string{abrir, entrar}
	if hay, _ := haySalaPendiente(op); hay {
		opciones = append(opciones, reanudar, descartar)
	}
	if hay, _ := hayÚltimaSala(op); hay {
		opciones = append(opciones, volver)
	}
	opciones = append(opciones, juegos, comprobar, nombre, salir)

	sel, err := elegir("Qué hacemos:", opciones)
	if err != nil {
		return err
	}
	switch sel {
	case abrir:
		nombreSala, err := texto("Nombre de la sala:",
			"Viaja dentro de la tarjeta cifrada. El registro no lo conoce.", nombreDelEquipo())
		if err != nil {
			return err
		}
		return conAviso(cmdHost(ctx, op, strings.Fields(nombreSala)))
	case entrar:
		pegado, err := texto("Pega el enlace o el código tal como te llegó:",
			"Valen las seis formas: VA3BSF5L, va3b-sf5l, kanpachi://VA3BSF5L,\n"+
				"VA3BSF5L@otro-seed.com, kanpachi.accentio.dev/VA3BSF5L y https://...", "")
		if err != nil {
			return err
		}
		return conAviso(cmdJoin(ctx, op, []string{pegado}))
	case volver:
		return conAviso(volverALaÚltima(ctx, op))
	case reanudar:
		return conAviso(cmdResume(ctx, op, nil))
	case descartar:
		return conAviso(cmdDiscard(ctx, op, nil))
	case juegos:
		return conAviso(cmdGames(ctx, op, nil))
	case comprobar:
		return menuDeComprobaciones(ctx, op)
	case nombre:
		return cambiarNombre(op)
	case salir:
		return errSalir
	}
	return nil
}

// volverALaÚltima es el camino del INVITADO, y no es `resume`.
//
// Confundirlas es el error que esta separación existe para no cometer. `resume`
// es del HOST y retoma una sala que sigue siendo suya. Esto es el mismo ingreso
// de la primera vez con el código guardado: hay canje de credencial y el host ve
// entrar a quien entra, que es lo que mantiene con sentido a la revocación.
//
// Se compone `CÓDIGO@seed` y no el código pelado: la última sala guarda su seed,
// y un código pelado siempre resuelve al seed por omisión.
func volverALaÚltima(ctx context.Context, op opciones) error {
	c, err := abrir(op)
	if err != nil {
		return err
	}
	v, err := client.Ask[struct {
		Found bool                  `json:"found"`
		Room  protocol.LastRoomView `json:"room"`
	}](c, protocol.MethodLastRoom, nil)
	_ = c.Close()
	if err != nil {
		return err
	}
	if !v.Found {
		return errors.New("no hay ninguna sala anterior guardada")
	}
	return cmdJoin(ctx, op, []string{v.Room.Code + "@" + v.Room.Seed})
}

func haySalaPendiente(op opciones) (bool, error) {
	c, err := abrir(op)
	if err != nil {
		return false, err
	}
	defer func() { _ = c.Close() }()
	v, err := client.Ask[struct {
		Found bool `json:"found"`
	}](c, protocol.MethodPendingRoom, nil)
	return v.Found, err
}

func hayÚltimaSala(op opciones) (bool, error) {
	c, err := abrir(op)
	if err != nil {
		return false, err
	}
	defer func() { _ = c.Close() }()
	v, err := client.Ask[struct {
		Found bool `json:"found"`
	}](c, protocol.MethodLastRoom, nil)
	return v.Found, err
}

// ─── Con sala ────────────────────────────────────────────────────────────────

func menuConSala(ctx context.Context, op opciones, st protocol.RoomView) error {
	limpiarPantalla(os.Stdout)
	pintarSala(os.Stdout, st)
	fmt.Println()

	const (
		vigilar   = "Ver la sala en vivo"
		copiar    = "Enseñar el enlace para repartir"
		rotar     = "Renovar el código (los enlaces repartidos dejan de valer)"
		juego     = "Activar el perfil de un juego"
		cerrarJue = "Cerrar los puertos del juego"
		comprobar = "Comprobar el sistema"
		cerrar    = "Cerrar la sala"
		salirSala = "Salir de la sala"
		salir     = "Dejar el asistente (la sala sigue abierta)"
	)

	opciones := []string{vigilar, copiar}
	expulsar := map[string]string{}
	if st.Role == "host" {
		opciones = append(opciones, rotar, juego)
		if st.Game != "" {
			opciones = append(opciones, cerrarJue)
		}
		for _, p := range st.Peers {
			if p.Self {
				continue
			}
			etiqueta := fmt.Sprintf("Expulsar a %s (%s)", p.Name, p.IP)
			expulsar[etiqueta] = p.IP
			opciones = append(opciones, etiqueta)
		}
	}
	opciones = append(opciones, comprobar)
	if st.Role == "host" {
		opciones = append(opciones, cerrar)
	} else {
		opciones = append(opciones, salirSala)
	}
	opciones = append(opciones, salir)

	sel, err := elegir("Acción:", opciones)
	if err != nil {
		return err
	}
	if ip, esExpulsión := expulsar[sel]; esExpulsión {
		return conAviso(cmdKick(ctx, op, []string{ip}))
	}
	switch sel {
	case vigilar:
		// Ctrl+C sale del vivo y vuelve al menú, no del programa: quien lo pulsa
		// ahí quiere dejar de mirar, no cerrar nada.
		err := cmdWatch(ctx, op, nil)
		if errors.Is(err, errInterrumpido) {
			return nil
		}
		return conAviso(err)
	case copiar:
		return conAviso(cmdLink(ctx, op, nil))
	case rotar:
		ok, err := confirmar("Los enlaces que ya repartiste dejarán de valer. ¿Seguimos?")
		if err != nil || !ok {
			return err
		}
		return conAviso(cmdRotate(ctx, op, nil))
	case juego:
		return conAviso(elegirJuego(ctx, op))
	case cerrarJue:
		return conAviso(cmdGame(ctx, op, nil))
	case comprobar:
		return menuDeComprobaciones(ctx, op)
	case cerrar, salirSala:
		return conAviso(cmdLeave(ctx, op, nil))
	case salir:
		return errSalir
	}
	return nil
}

// elegirJuego enseña el catálogo y activa el que se elija.
func elegirJuego(ctx context.Context, op opciones) error {
	c, err := abrir(op)
	if err != nil {
		return err
	}
	juegos, err := client.Ask[[]protocol.GameView](c, protocol.MethodListGames, nil)
	_ = c.Close()
	if err != nil {
		return err
	}
	if len(juegos) == 0 {
		return errors.New("el catálogo está vacío")
	}

	etiquetas := make([]string, 0, len(juegos))
	porEtiqueta := map[string]string{}
	for _, g := range juegos {
		e := g.Name
		if g.Installed {
			e += "  (instalado)"
		}
		etiquetas = append(etiquetas, e)
		porEtiqueta[e] = g.ID
	}
	sel, err := elegir("Qué juego:", etiquetas)
	if err != nil {
		return err
	}
	return cmdGame(ctx, op, []string{porEtiqueta[sel]})
}

// ─── Comprobaciones ──────────────────────────────────────────────────────────

func menuDeComprobaciones(ctx context.Context, op opciones) error {
	const (
		exposicion = "Qué tengo abierto, y hacia quién"
		red        = "Cómo se ve mi red desde el motor"
		sondeo     = "Sondearme desde otra máquina de la sala"
		reponer    = "Reponer la Protección Kanpachi"
		volver     = "<< Volver"
	)
	sel, err := elegir("Qué comprobamos:", []string{exposicion, red, sondeo, reponer, volver})
	if err != nil {
		return err
	}
	switch sel {
	case exposicion:
		return conAviso(cmdExposure(ctx, op, nil))
	case red:
		return conAviso(cmdDiag(ctx, op, nil))
	case sondeo:
		return conAviso(cmdProbe(ctx, op, nil))
	case reponer:
		return conAviso(cmdProtect(ctx, op, nil))
	}
	return nil
}

func cambiarNombre(op opciones) error {
	actual := leerApodo(op.datos)
	nuevo, err := texto("Tu nombre:", "Lo ven los demás miembros de la sala. "+
		"Solo letras y dígitos, hasta 12.", actual)
	if err != nil {
		return err
	}
	// Se valida por el mismo camino que usan los subcomandos, y ahí es donde se
	// guarda: así no hay dos sitios que decidan qué nombre vale.
	if _, err := apodo(opciones{datos: op.datos, nick: nuevo}); err != nil {
		return conAviso(err)
	}
	return nil
}

// ─── Preguntar ───────────────────────────────────────────────────────────────

// preguntar envuelve a survey para que la interrupción no se escape.
//
// **Ningún sitio de este binario llama a `survey.AskOne` directamente.** Dentro
// de una pregunta, survey limpia `ENABLE_PROCESSED_INPUT` de la consola, así que
// Windows NO genera CTRL_C_EVENT y `signal.NotifyContext` no se entera de nada:
// lo único que ocurre es que `AskOne` devuelve `terminal.InterruptErr`.
func preguntar(p survey.Prompt, destino any, opts ...survey.AskOpt) error {
	err := survey.AskOne(p, destino, opts...)
	if errors.Is(err, terminal.InterruptErr) {
		return errInterrumpido
	}
	return err
}

func elegir(mensaje string, opciones []string) (string, error) {
	var sel string
	err := preguntar(&survey.Select{Message: mensaje, Options: opciones, PageSize: 15}, &sel)
	return sel, err
}

func texto(mensaje, ayuda, porDefecto string) (string, error) {
	var v string
	err := preguntar(&survey.Input{Message: mensaje, Help: ayuda, Default: porDefecto}, &v,
		survey.WithValidator(survey.Required))
	return strings.TrimSpace(v), err
}

func confirmar(mensaje string) (bool, error) {
	var v bool
	err := preguntar(&survey.Confirm{Message: mensaje, Default: false}, &v)
	return v, err
}

// conAviso enseña el error y espera, en vez de tragárselo o de subirlo.
//
// Los errores de estos caminos son respuestas del producto —"ese código no
// existe", "el host no contestó"— y lo que hay que hacer con ellos es leerlos.
// La interrupción sí sube, porque es lo único que tiene que llegar al final.
func conAviso(err error) error {
	if err == nil {
		esperarEnter()
		return nil
	}
	if errors.Is(err, errInterrumpido) || errors.Is(err, context.Canceled) {
		return err
	}
	fmt.Println("\n  MAL:", err)
	esperarEnter()
	return nil
}

func esperarEnter() {
	fmt.Println("\n  Pulsa Enter para continuar...")
	var nada string
	_, _ = fmt.Scanln(&nada)
}
