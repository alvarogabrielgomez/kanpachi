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

func assistant(ctx context.Context, op opciones) error {
	// Sin terminal no hay flechas que pulsar. Se dice y se enseña la ayuda, en
	// vez de fallar con lo que survey diría, que habla de descriptores de
	// fichero y no de lo que le pasa a quien lo está corriendo.
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprintln(os.Stderr, "kanpachi: with no terminal there is no wizard to show.")
		fmt.Fprintln(os.Stderr, "  Inside a script, use a subcommand:")
		fmt.Fprintln(os.Stderr)
		ayuda(os.Stderr)
		return uso("a subcommand is needed")
	}
	// El asistente nunca imprime JSON: es una pantalla. Que `--json` lo apagara
	// en silencio sería peor, así que se dice.
	if op.json {
		return uso("--json is for the subcommands, and the wizard is a screen")
	}

	for {
		if ctx.Err() != nil {
			return errInterrumpido
		}
		st, err := currentMenuStatus(op)
		if err != nil {
			return err
		}
		var nextError error
		if st.Conn == "idle" || st.Conn == "" {
			nextError = presentNoRoomMenu(ctx, op, st)
		} else {
			nextError = roomMenu(ctx, op, st)
		}
		if errors.Is(nextError, errExit) {
			return nil
		}
		if nextError != nil {
			return nextError
		}
	}
}

// errExit es haber elegido «Salir» en el menú, que no es un fallo.
var errExit = errors.New("salir")

// estadoParaElMenú abre, pregunta y cierra.
//
// Una conexión por vuelta de menú y no una para todo el asistente, a propósito:
// entre dos vueltas puede haber pasado un minuto con alguien leyendo el menú, y
// una conexión ociosa ocupa una de las ocho plazas del oyente todo ese rato.
func currentMenuStatus(op opciones) (protocol.RoomView, error) {
	c, err := abrir(op)
	if err != nil {
		return protocol.RoomView{}, err
	}
	defer func() { _ = c.Close() }()
	return client.Ask[protocol.RoomView](c, protocol.MethodStatus, nil)
}

// ─── Sin sala ────────────────────────────────────────────────────────────────

func presentNoRoomMenu(ctx context.Context, op opciones, st protocol.RoomView) error {
	limpiarPantalla(os.Stdout)
	fmt.Println(raya)
	fmt.Printf("  KANPACHI %-20s channel: %s\n", Version, op.canal)
	fmt.Println(raya)
	fmt.Println()

	// El estado se PINTA, y hasta ahora esta rama era la única boca que no lo
	// hacía: el asistente enseñaba el menú sin decir por qué terminó lo anterior
	// ni que se está volviendo a una sala. Es el mismo renderizador que usan
	// `status` y `watch`, no una segunda versión.
	pintarSala(os.Stdout, st)
	fmt.Println()

	const (
		abrir     = "Open a room"
		entrar    = "Enter someone else's room"
		volver    = "Go back to the last room I entered"
		ahora     = "Try going back now"
		dejarlo   = "Stop going back to that room"
		reanudar  = "Reopen the room left from the previous start"
		descartar = "Forget that pending room"
		juegos    = "See the game catalog"
		comprobar = "Check the system"
		actualiza = "Look for a new version"
		nombre    = "Change my name"
		salir     = "Quit"
	)

	// Volviendo, las dos entradas que importan van PRIMERO, porque es lo que
	// está pasando ahora mismo. Y `volver` desaparece: ofrecer «vuelve» a quien
	// ya está volviendo es ofrecer lo que ya tiene.
	volviendo := st.Returning != nil
	var opciones []string
	if volviendo {
		opciones = append(opciones, ahora, dejarlo)
	}
	opciones = append(opciones, abrir, entrar)
	if hay, _ := haySalaGuardada(op); hay {
		opciones = append(opciones, reanudar, descartar)
	}
	// La pregunta solo se hace si puede cambiar algo. Este menú abre una conexión
	// por cada una de estas comprobaciones, de las ocho que hay, y volviendo la
	// respuesta ya la trajo el estado.
	if !volviendo {
		if hay, _ := hayÚltimaSala(op); hay {
			opciones = append(opciones, volver)
		}
	}
	opciones = append(opciones, juegos, comprobar, actualiza, nombre, salir)

	sel, err := elegir("What do we do:", opciones)
	if err != nil {
		return err
	}
	switch sel {
	case abrir:
		nombreSala, err := texto("Room name:",
			"It travels inside the encrypted card. The registry never sees it.", nombreDelEquipo())
		if err != nil {
			return err
		}
		return conAviso(ctx, op, cmdHost(ctx, op, strings.Fields(nombreSala)))
	case entrar:
		pegado, err := texto("Paste the link or the code exactly as it reached you:",
			"All six forms work: VA3BSF5L, va3b-sf5l, kanpachi://VA3BSF5L,\n"+
				"VA3BSF5L@another-seed.com, kanpachi.accentio.dev/VA3BSF5L and https://...", "")
		if err != nil {
			return err
		}
		return conAviso(ctx, op, cmdJoin(ctx, op, []string{pegado}))
	case volver, ahora:
		// El mismo camino para las dos, y no es pereza: «volver» y «probar ahora»
		// son literalmente lo mismo, entrar con el código guardado. Lo único que
		// cambia es que en el segundo caso ya había un reloj contándolo. La
		// compuerta no pregunta nada, porque volver a donde ya se está volviendo
		// no desplaza nada.
		return conAviso(ctx, op, volverALaÚltima(ctx, op))
	case dejarlo:
		return conAviso(ctx, op, cmdLeave(ctx, op, nil))
	case reanudar:
		return conAviso(ctx, op, cmdResume(ctx, op, nil))
	case descartar:
		return conAviso(ctx, op, cmdDiscard(ctx, op, nil))
	case juegos:
		return conAviso(ctx, op, cmdGames(ctx, op, nil))
	case comprobar:
		return menuDeComprobaciones(ctx, op)
	case actualiza:
		// `--check` y no la actualización entera, a propósito: desde el menú se
		// MIRA, y actualizar reinicia el servicio. El comando que lo hace se
		// escribe a mano, que es un paso que ahí sí vale la pena.
		return conAviso(ctx, op, cmdUpgrade(ctx, op, []string{"--check"}))
	case nombre:
		return cambiarNombre(ctx, op)
	case salir:
		return errExit
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
		return errors.New("there is no previous room saved")
	}
	return cmdJoin(ctx, op, []string{v.Room.Code + "@" + v.Room.Seed})
}

func haySalaGuardada(op opciones) (bool, error) {
	c, err := abrir(op)
	if err != nil {
		return false, err
	}
	defer func() { _ = c.Close() }()
	v, err := client.Ask[struct {
		Found bool `json:"found"`
	}](c, protocol.MethodSavedRoom, nil)
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

func roomMenu(ctx context.Context, op opciones, st protocol.RoomView) error {
	limpiarPantalla(os.Stdout)
	pintarSala(os.Stdout, st)
	fmt.Println()

	const (
		vigilar   = "Watch the room live"
		copiar    = "Show the link to hand out"
		rotar     = "Renew the code (the links handed out stop working)"
		juego     = "Activate a game profile"
		cerrarJue = "Close the game ports"
		comprobar = "Check the system"
		otraSala  = "Enter another room"
		cerrar    = "Close the room"
		salirSala = "Leave the room"
		salir     = "Quit the wizard (the room stays open)"
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
			etiqueta := fmt.Sprintf("Kick %s (%s)", p.Name, p.IP)
			expulsar[etiqueta] = p.IP
			opciones = append(opciones, etiqueta)
		}
	}
	opciones = append(opciones, comprobar)
	// Cambiar de sala con una abierta es el caso que en la ventana NO existe,
	// porque allá estar dentro te lleva a la pantalla de la sala y no hay campo
	// de código. Acá la terminal es la interfaz, y lo que se puede escribir como
	// `kanpachi join <otro>` tiene que poder elegirse. La confirmación la pone la
	// compuerta, con la frase que le toque según se salga o se cierre.
	opciones = append(opciones, otraSala)
	if st.Role == "host" {
		opciones = append(opciones, cerrar)
	} else {
		opciones = append(opciones, salirSala)
	}
	opciones = append(opciones, salir)

	sel, err := elegir("Action:", opciones)
	if err != nil {
		return err
	}
	if ip, esExpulsión := expulsar[sel]; esExpulsión {
		return conAviso(ctx, op, cmdKick(ctx, op, []string{ip}))
	}
	switch sel {
	case vigilar:
		// Ctrl+C sale del vivo y vuelve al menú, no del programa: quien lo pulsa
		// ahí quiere dejar de mirar, no cerrar nada.
		err := cmdWatch(ctx, op, nil)
		if errors.Is(err, errInterrumpido) {
			return nil
		}
		return conAviso(ctx, op, err)
	case copiar:
		return conAviso(ctx, op, cmdLink(ctx, op, nil))
	case rotar:
		ok, err := confirmar("The links you already handed out will stop working. Go on?")
		if err != nil || !ok {
			return err
		}
		return conAviso(ctx, op, cmdRotate(ctx, op, nil))
	case juego:
		return conAviso(ctx, op, elegirJuego(ctx, op))
	case cerrarJue:
		return conAviso(ctx, op, cmdGame(ctx, op, nil))
	case comprobar:
		return menuDeComprobaciones(ctx, op)
	case otraSala:
		pegado, err := texto("Paste the link or the code of the room you want to enter:",
			"Entering it means leaving this one. You get asked before anything happens.", "")
		if err != nil {
			return err
		}
		return conAviso(ctx, op, cmdJoin(ctx, op, []string{pegado}))
	case cerrar, salirSala:
		return conAviso(ctx, op, cmdLeave(ctx, op, nil))
	case salir:
		return errExit
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
		return errors.New("the catalog is empty")
	}

	etiquetas := make([]string, 0, len(juegos))
	porEtiqueta := map[string]string{}
	for _, g := range juegos {
		e := g.Name
		if g.Installed {
			e += "  (installed)"
		}
		etiquetas = append(etiquetas, e)
		porEtiqueta[e] = g.ID
	}
	sel, err := elegir("Which game:", etiquetas)
	if err != nil {
		return err
	}
	return cmdGame(ctx, op, []string{porEtiqueta[sel]})
}

// ─── Comprobaciones ──────────────────────────────────────────────────────────

func menuDeComprobaciones(ctx context.Context, op opciones) error {
	const (
		exposicion = "What I have open, and toward whom"
		red        = "How my network looks from the engine"
		sondeo     = "Probe me from another machine in the room"
		reponer    = "Put Kanpachi Protection back"
		volver     = "<< Back"
	)
	sel, err := elegir("What do we check:", []string{exposicion, red, sondeo, reponer, volver})
	if err != nil {
		return err
	}
	switch sel {
	case exposicion:
		return conAviso(ctx, op, cmdExposure(ctx, op, nil))
	case red:
		return conAviso(ctx, op, cmdDiag(ctx, op, nil))
	case sondeo:
		return conAviso(ctx, op, cmdProbe(ctx, op, nil))
	case reponer:
		return conAviso(ctx, op, cmdProtect(ctx, op, nil))
	}
	return nil
}

func cambiarNombre(ctx context.Context, op opciones) error {
	actual := leerApodo(op.datos)
	nuevo, err := texto("Your name:", "The other members of the room see it. "+
		"Letters and digits only, up to 12.", actual)
	if err != nil {
		return err
	}
	// Se valida por el mismo camino que usan los subcomandos, y ahí es donde se
	// guarda: así no hay dos sitios que decidan qué nombre vale.
	if _, err := apodo(opciones{datos: op.datos, nick: nuevo}); err != nil {
		return conAviso(ctx, op, err)
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
func conAviso(ctx context.Context, op opciones, err error) error {
	if err == nil {
		esperarEnter()
		return nil
	}
	if errors.Is(err, errInterrumpido) || errors.Is(err, context.Canceled) {
		return err
	}
	fmt.Println("\n  BAD:", err)
	if seArreglaAquíMismo(ctx, op, err) {
		return nil
	}
	esperarEnter()
	return nil
}

// seArreglaAquíMismo ofrece resolver en el acto lo que tiene arreglo, y dice si
// llegó a hacerlo.
//
// # Por qué acá y no dejándolo en un consejo
//
// Porque el asistente existe para quien no leyó nada, y mandarlo a escribir
// `kanpachi seed <host>` es mandarlo a salir del asistente para volver a entrar.
// El subcomando sí se queda con el consejo, que es lo correcto ahí: quien
// escribió un comando está en una terminal y puede escribir otro.
//
// **No reintenta la operación sola.** Configurar el registro y abrir una sala
// son dos decisiones, y encadenarlas haría que quien solo quería arreglar la
// configuración se encontrara con una sala abierta.
func seArreglaAquíMismo(ctx context.Context, op opciones, err error) bool {
	switch client.Code(err) {
	case protocol.CodeNoOwnSeed:
		fmt.Println("\n  This machine has no registry to open rooms on yet.")
		host, e := texto("Registry host:",
			"The name of the server, with no https:// and no slashes. It travels\n"+
				"inside every code you hand out.", "")
		if e != nil || strings.TrimSpace(host) == "" {
			return false
		}
		return conAviso(ctx, op, cmdSeed(ctx, op, []string{strings.TrimSpace(host)})) == nil
	case protocol.CodeSeedPassword:
		fmt.Println("\n  That registry asks for a password to host on it.")
		fmt.Println("  Entering a room never asks for one.")
		return conAviso(ctx, op, cmdPassword(ctx, op, nil)) == nil
	}
	return false
}

func esperarEnter() {
	fmt.Println("\n  Press Enter to continue...")
	var nada string
	_, _ = fmt.Scanln(&nada)
}
