package main

// La tabla de comandos, que es el espejo de la lista cerrada de métodos.
//
// Espejo y no envoltorio automático: hay métodos del protocolo que no son un
// comando y no deberían serlo. `show_ui` y `pending_invite` son de la interfaz
// de escritorio; `progress` y `cancel` se piden por una conexión aparte
// mientras otra espera, que es una forma de uso y no una orden que alguien
// escriba. Lo que queda es lo que una persona quiere hacerle a su sala.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/accentiostudios/kanpachi/core/timing"
	"github.com/accentiostudios/kanpachi/daemon/transport/client"
	"github.com/accentiostudios/kanpachi/daemon/transport/protocol"
)

type comando struct {
	// args es cómo se escribe lo que va después del nombre, para la ayuda.
	args  string
	breve string
	// correr recibe el contexto para poder cortar por Ctrl+C durante una
	// operación larga. Crear una sala tarda cerca de un minuto.
	correr func(ctx context.Context, op opciones, args []string) error
}

// grupos ordena la ayuda por lo que se hace primero, y no alfabéticamente.
//
// **Un comando que no está acá no sale en `kanpachi help`**, aunque exista y
// funcione. Le pasó a `quarantine`, que se publicó en 0.5.0 con sus tres bocas
// y sin una sola línea que dijera que existe. Al agregar un comando a la tabla
// de abajo hay que agregarlo también acá, y no hay nada que lo compruebe: es la
// única lista del binario que puede quedarse corta en silencio.
var grupos = []struct {
	titulo  string
	nombres []string
}{
	{"The room:", []string{"status", "watch", "host", "join", "leave", "link", "rotate", "rename"}},
	{"Who is in:", []string{"members", "kick"}},
	{"The game:", []string{"games", "game"}},
	{"Checking:", []string{"exposure", "diag", "probe", "protect"}},
	{"What was left from before:", []string{"pending", "resume", "discard", "last"}},
	{"The system:", []string{"name", "seed", "quarantine", "password", "doctor", "upgrade"}},
	{"Other:", []string{"version", "help"}},
}

// comandos es la tabla. Se rellena en `init` y no en la declaración porque
// varias entradas se nombran entre ellas —`help` recorre `grupos`, el asistente
// llama a las mismas funciones— y Go no admite ciclos de inicialización.
var comandos map[string]comando

func init() {
	comandos = map[string]comando{
		"status": {breve: "what there is right now: room, members, network and protection",
			correr: cmdStatus},
		"watch": {breve: "the same, redrawn until you press Ctrl+C",
			correr: cmdWatch},
		"host": {args: "[name]", breve: "open a room and be its host",
			correr: cmdHost},
		"join": {args: "<code|link>", breve: "enter someone else's room",
			correr: cmdJoin},
		"leave": {breve: "close your room, leave someone else's, or stop going back to the last one",
			correr: cmdLeave},
		"link": {breve: "the invite link, to copy and hand out",
			correr: cmdLink},
		"rotate": {breve: "renew the code: the links you handed out stop working",
			correr: cmdRotate},
		"rename": {args: "<name>", breve: "rename the room",
			correr: cmdRename},
		"members": {breve: "who is in, by which path, and with what latency",
			correr: cmdMembers},
		"kick": {args: "<name|ip>", breve: "kick someone out of the room",
			correr: cmdKick},
		"games": {breve: "the game catalog, and which ones are installed",
			correr: cmdGames},
		"game": {args: "[id]", breve: "activate a game profile; with no id, close the ports",
			correr: cmdGame},
		"exposure": {breve: "what Kanpachi has open, and toward whom",
			correr: cmdExposure},
		"diag": {breve: "the network as the engine sees it: NAT, UDP and MTU",
			correr: cmdDiag},
		"probe": {breve: "probe this machine FROM another one in the room",
			correr: cmdProbe},
		"protect": {breve: "put Kanpachi Protection back. It is idempotent",
			correr: cmdProtect},
		"quarantine": {args: "[on|off]",
			breve:  "close this PC's risky server ports on every network; bare, it tells the state",
			correr: cmdQuarantine},
		"pending": {breve: "whether a room was left open from the previous start",
			correr: cmdPending},
		"resume": {breve: "reopen that room with the same code",
			correr: cmdResume},
		"discard": {breve: "forget it",
			correr: cmdDiscard},
		"last": {breve: "the last room you entered as a guest",
			correr: cmdLast},
		"name": {args: "[name]", breve: "the name rooms show you by, shared with the window; bare, it shows it",
			correr: cmdName},
		"seed": {args: "[host]", breve: "the registry this machine opens rooms on; with no host, shows it",
			correr: cmdSeed},
		"password": {breve: "the password of a registry that asks for one to host. Never on the command line",
			correr: cmdPassword},
		"doctor": {args: "[--fix]", breve: "what this needs to work, and what is broken",
			correr: cmdDoctor},
		"upgrade": {args: "[--check] [--version v] [--force] [--yes]",
			breve:  "fetch the new version. Restarts the service, so the room drops",
			correr: cmdUpgrade},
		"version": {breve: "which version this is",
			correr: cmdVersion},
		"help": {breve: "this",
			correr: func(context.Context, opciones, []string) error { ayuda(os.Stdout); return nil }},
	}
}

// ─── Pedirle cosas al daemon ─────────────────────────────────────────────────

// pedir hace la llamada y, con `--json`, imprime el crudo y avisa de que ya
// imprimió.
//
// El crudo sale ANTES de interpretarlo, y a propósito: lo que hace guionable a
// esto es la forma de cable, que es un contrato con candados, y no lo pintado,
// que cambia cuando alguien mejora una pantalla. Sale incluso cuando el daemon
// contestó con error, porque la expulsión a medias trae resultado y error a la
// vez y el script necesita las dos cosas.
func pedir[T any](c *client.Client, op opciones, m protocol.Method, params any) (T, bool, error) {
	var out T
	raw, err := c.Call(m, params)
	if op.json && len(raw) > 0 {
		var indentado bytes.Buffer
		if e := json.Indent(&indentado, raw, "", "  "); e == nil {
			fmt.Println(indentado.String())
		} else {
			// Sin indentar antes que nada: lo que importa es que el crudo salga.
			fmt.Println(string(raw))
		}
		return out, true, err
	}
	if len(raw) > 0 {
		if e := json.Unmarshal(raw, &out); e != nil && err == nil {
			return out, false, fmt.Errorf("parsing the answer to %s: %w", m, e)
		}
	}
	return out, false, err
}

// conSala es el molde de casi todos: abrir, pedir, pintar la sala.
func conSala(op opciones, m protocol.Method, params any) error {
	c, err := abrir(op)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	st, hecho, err := pedir[protocol.RoomView](c, op, m, params)
	if hecho {
		return err
	}
	// La sala se pinta AUNQUE haya error, y ese orden importa: la expulsión a
	// medias contesta con la lista ya sin el expulsado más el aviso de lo que no
	// se pudo cerrar, y quien mire solo el error tira el estado que acaba de
	// pedir. Ver [protocol.Response].
	if st.Conn != "" {
		pintarSala(os.Stdout, st, catalogForAddress(c, st))
	}
	return err
}

// catalogForAddress fetches the catalog only when there is an address to build.
//
// The connect address needs the active profile's port, and the port does not
// travel in the room view: it travels in the catalog, the same place the window
// reads it from. With no game active there is nothing to build, so nothing is
// asked for and the common case costs no extra round trip.
//
// A failure here returns nil and stays quiet. Not being able to name the port
// is not a reason to withhold the room: without it the address line is skipped
// and everything else prints as it did.
func catalogForAddress(c *client.Client, st protocol.RoomView) []protocol.GameView {
	if st.Game == "" {
		return nil
	}
	catalog, err := client.Ask[[]protocol.GameView](c, protocol.MethodListGames, nil)
	if err != nil {
		return nil
	}
	return catalog
}

// ─── La sala ─────────────────────────────────────────────────────────────────

func cmdStatus(_ context.Context, op opciones, _ []string) error {
	return conSala(op, protocol.MethodStatus, nil)
}

// cmdWatch redibuja el estado hasta que lo corten.
//
// # Reusa la conexión, y eso no es un ahorro cosmético
//
// Abrir una por fotograma haría un saludo por segundo y ocuparía una plaza del
// oyente cada vez. Hay ocho plazas y el daemon las cuenta.
func cmdWatch(ctx context.Context, op opciones, _ []string) error {
	if op.json {
		return uso("watch paints a screen, so it has no --json form.\n" +
			"  For a script, `kanpachi status --json` inside a loop")
	}
	c, err := abrir(op)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	// Once, outside the loop. The catalog is what the address line needs and it
	// does not change while this runs, so asking per frame would be a request a
	// second for an answer that is already in hand. Which profile is active can
	// change mid-loop, and that is read from the room view every frame.
	catalog, err := client.Ask[[]protocol.GameView](c, protocol.MethodListGames, nil)
	if err != nil {
		catalog = nil
	}

	for {
		st, err := client.Ask[protocol.RoomView](c, protocol.MethodStatus, nil)
		if err != nil {
			return err
		}
		limpiarPantalla(os.Stdout)
		pintarSala(os.Stdout, st, catalog)
		fmt.Println("  [Ctrl+C] to leave. This does NOT close the room: the room lives in the daemon.")

		select {
		case <-ctx.Done():
			return errInterrumpido
		case <-time.After(timing.LiveViewRefresh):
		}
	}
}

func cmdHost(_ context.Context, op opciones, args []string) error {
	args, sinPreguntar := sacarSí(args)
	args, cuarentena, err := sacarCuarentena(args)
	if err != nil {
		return err
	}
	nick, err := apodo(op)
	if err != nil {
		return err
	}
	// Abrir una sala desplaza lo mismo que entrar a una ajena, y pasa por la
	// misma compuerta: quien ya hospeda una la estaría cerrando.
	replace, err := confirmarDesplazamiento(op, sinPreguntar)
	if err != nil {
		return err
	}
	// El registro se lee y se confirma ANTES de anunciar el minuto de espera.
	// Preguntar después de "esto tarda un minuto" haría que la pregunta pareciera
	// parte de la espera, y lo que se está decidiendo es si empezarla.
	seed, err := registroDeEstaMáquina(op)
	if err != nil {
		return err
	}
	if err := confiarEnElRegistro(op, seed, "Opening a room", sinPreguntar); err != nil {
		return err
	}
	nombre := strings.Join(args, " ")
	if nombre == "" {
		// El nombre viaja DENTRO de la tarjeta cifrada y el seed no lo conoce,
		// así que ponerle el del equipo no filtra nada y ahorra la pregunta en el
		// caso normal de un servidor.
		nombre = nombreDelEquipo()
	}
	if !op.json {
		fmt.Println("Opening the room. This takes about a minute: two adapters have to")
		fmt.Println("come up, the credential has to be exchanged, and the MTU measured.")
	}
	return conSalaConsintiendo(op, protocol.MethodCreateRoom, sinPreguntar, cuarentena,
		func(allowFirewall bool, quarantine string) any {
			return struct {
				Nickname      string `json:"nickname"`
				Name          string `json:"name"`
				Replace       bool   `json:"replace,omitempty"`
				AllowFirewall bool   `json:"allow_firewall,omitempty"`
				Quarantine    string `json:"quarantine,omitempty"`
			}{nick, nombre, replace, allowFirewall, quarantine}
		})
}

// cmdJoin pasa el texto TAL CUAL lo pegó la persona.
//
// Nada de quitarle los guiones ni de anteponerle el host del seed:
// `domain.ParseRoom` acepta las seis formas documentadas y es la frontera de
// entrada hostil del producto. Normalizar antes de llamarla es probar otra cosa
// distinta de la que se quiere probar.
func cmdJoin(_ context.Context, op opciones, args []string) error {
	args, sinPreguntar := sacarSí(args)
	args, cuarentena, err := sacarCuarentena(args)
	if err != nil {
		return err
	}
	if len(args) == 0 {
		return uso("join needs the code or the link.\n" +
			"  Every form works as long as it carries the registry:\n" +
			"  VA3BSF5L@seed.example.com, seed.example.com/VA3BSF5L,\n" +
			"  https://seed.example.com/VA3BSF5L#key and kanpachi://VA3BSF5L@seed.example.com")
	}
	nick, err := apodo(op)
	if err != nil {
		return err
	}
	// Vacío cuando el texto no parsea, y ahí NO se pregunta: quien tiene que
	// explicar qué le falta a ese código es el daemon, que es la frontera de
	// entrada hostil. Preguntar por un registro que no se pudo leer sería
	// nombrar una máquina inventada.
	// Lo que estorba se pregunta ANTES que el registro, y el orden importa: si
	// alguien va a decir que no, que lo diga antes de haber leído un párrafo
	// sobre una máquina que ya no va a usar.
	replace, err := confirmarDesplazamiento(op, sinPreguntar)
	if err != nil {
		return err
	}
	if seed := registroDelCódigo(args[0]); seed != "" {
		if err := confiarEnElRegistro(op, seed, "Entering that room", sinPreguntar); err != nil {
			return err
		}
	}
	if !op.json {
		fmt.Println("Entering...")
	}
	return conSalaConsintiendo(op, protocol.MethodJoinRoom, sinPreguntar, cuarentena,
		func(allowFirewall bool, quarantine string) any {
			return struct {
				Code          string `json:"code"`
				Nickname      string `json:"nickname"`
				Replace       bool   `json:"replace,omitempty"`
				AllowFirewall bool   `json:"allow_firewall,omitempty"`
				Quarantine    string `json:"quarantine,omitempty"`
			}{args[0], nick, replace, allowFirewall, quarantine}
		})
}

func cmdLeave(_ context.Context, op opciones, _ []string) error {
	return conSala(op, protocol.MethodLeaveRoom, nil)
}

func cmdLink(_ context.Context, op opciones, _ []string) error {
	c, err := abrir(op)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	v, hecho, err := pedir[struct {
		Link string `json:"link"`
	}](c, op, protocol.MethodInviteLink, nil)
	if hecho || err != nil {
		return err
	}
	if v.Link == "" {
		return negativa("no room is open, so there is no link to hand out")
	}
	// Pelado y sin adornos: esto se usa dentro de un `$(...)`.
	fmt.Println(v.Link)
	return nil
}

func cmdRotate(_ context.Context, op opciones, _ []string) error {
	if !op.json {
		fmt.Println("Renewing the code. The links already handed out stop working;")
		fmt.Println("whoever is inside stays inside.")
	}
	return conSala(op, protocol.MethodRotateInviteCode, nil)
}

func cmdRename(_ context.Context, op opciones, args []string) error {
	if len(args) == 0 {
		return uso("rename needs the new name")
	}
	return conSala(op, protocol.MethodRenameRoom, struct {
		Name string `json:"name"`
	}{strings.Join(args, " ")})
}

// ─── Quién está dentro ───────────────────────────────────────────────────────

func cmdMembers(_ context.Context, op opciones, _ []string) error {
	c, err := abrir(op)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	st, hecho, err := pedir[protocol.RoomView](c, op, protocol.MethodStatus, nil)
	if hecho || err != nil {
		return err
	}
	pintarMiembros(os.Stdout, st)
	return nil
}

// cmdKick acepta el nombre además de la IP, y por eso pregunta el estado antes.
//
// El protocolo solo entiende la dirección, que es lo correcto: un nombre no es
// único en una sala y expulsar al que no era no tiene deshacer. Resolverlo acá
// deja que quien escribe use lo que ve en pantalla, y **se niega si el nombre
// aparece dos veces** en vez de elegir uno.
func cmdKick(_ context.Context, op opciones, args []string) error {
	if len(args) == 0 {
		return uso("kick needs who: their name in the room or their virtual IP")
	}
	c, err := abrir(op)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	st, err := client.Ask[protocol.RoomView](c, protocol.MethodStatus, nil)
	if err != nil {
		return err
	}
	ip, err := resolverMiembro(st, args[0])
	if err != nil {
		return err
	}

	v, hecho, err := pedir[protocol.RoomView](c, op, protocol.MethodKickMember, struct {
		IP string `json:"ip"`
	}{ip})
	if hecho {
		return err
	}
	if v.Conn != "" {
		pintarMiembros(os.Stdout, v)
	}
	// La expulsión a medias trae las dos cosas y el error no se traga: significa
	// que la persona salió de la sala y algún puerto suyo se quedó abierto.
	return err
}

// resolverMiembro traduce lo que escribió una persona a una IP virtual.
func resolverMiembro(st protocol.RoomView, quién string) (string, error) {
	var encontrados []protocol.PeerView
	for _, p := range st.Peers {
		if p.IP == quién || strings.EqualFold(p.Name, quién) {
			encontrados = append(encontrados, p)
		}
	}
	switch len(encontrados) {
	case 0:
		return "", uso("there is no %q in the room.\n  `kanpachi members` says who is", quién)
	case 1:
		if encontrados[0].Self {
			return "", uso("that is you. To leave the room it is `kanpachi leave`")
		}
		return encontrados[0].IP, nil
	default:
		var ips []string
		for _, p := range encontrados {
			ips = append(ips, p.IP)
		}
		return "", uso("there are %d members called %q: %s.\n"+
			"  Write the IP so you do not kick the wrong one",
			len(encontrados), quién, strings.Join(ips, ", "))
	}
}

// ─── El juego ────────────────────────────────────────────────────────────────

func cmdGames(_ context.Context, op opciones, _ []string) error {
	c, err := abrir(op)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	juegos, hecho, err := pedir[[]protocol.GameView](c, op, protocol.MethodListGames, nil)
	if hecho || err != nil {
		return err
	}
	pintarJuegos(os.Stdout, juegos)
	return nil
}

// cmdGame activa un perfil, y sin id los cierra todos.
//
// El id vacío es legal en el protocolo y significa exactamente eso, así que no
// se rechaza acá: quien escribe `kanpachi game` a secas está diciendo "deja de
// tener puertos abiertos", que es una orden con sentido.
func cmdGame(_ context.Context, op opciones, args []string) error {
	id := ""
	if len(args) > 0 {
		id = args[0]
	}
	return conSala(op, protocol.MethodActivateProfile, struct {
		Game string `json:"game"`
	}{id})
}

// ─── Comprobar ───────────────────────────────────────────────────────────────

func cmdExposure(_ context.Context, op opciones, _ []string) error {
	c, err := abrir(op)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	v, hecho, err := pedir[protocol.ExposureView](c, op, protocol.MethodExposure, nil)
	if hecho || err != nil {
		return err
	}
	pintarExposicion(os.Stdout, v)
	return nil
}

func cmdDiag(_ context.Context, op opciones, _ []string) error {
	c, err := abrir(op)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	if !op.json {
		fmt.Println("Measuring. This goes out to the network, so it takes a few seconds.")
	}
	v, hecho, err := pedir[protocol.NetView](c, op, protocol.MethodDiagReport, nil)
	if hecho || err != nil {
		return err
	}
	pintarRed(os.Stdout, v)
	return nil
}

func cmdProbe(_ context.Context, op opciones, _ []string) error {
	c, err := abrir(op)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	if !op.json {
		fmt.Println("Asking another machine in the room to probe this one.")
	}
	v, hecho, err := pedir[protocol.ProbeView](c, op, protocol.MethodProbeHost, nil)
	if hecho || err != nil {
		return err
	}
	pintarSondeo(os.Stdout, v)
	return nil
}

func cmdProtect(_ context.Context, op opciones, _ []string) error {
	return conSala(op, protocol.MethodReapplyProtection, nil)
}

// ─── Lo que quedó de antes ───────────────────────────────────────────────────

func cmdPending(_ context.Context, op opciones, _ []string) error {
	c, err := abrir(op)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	v, hecho, err := pedir[struct {
		Found bool                   `json:"found"`
		Room  protocol.SavedRoomView `json:"room"`
	}](c, op, protocol.MethodSavedRoom, nil)
	if hecho || err != nil {
		return err
	}
	if !v.Found {
		fmt.Println("No room was left from the previous start.")
		return nil
	}
	fmt.Printf("  Saved room      %s\n", v.Room.Name)
	fmt.Printf("  Code            %s@%s\n", v.Room.Code, v.Room.Seed)
	if v.Room.Game != "" {
		fmt.Printf("  Game            %s\n", v.Room.Game)
	}
	if v.Room.SavedAt != "" {
		fmt.Printf("  Saved           %s\n", v.Room.SavedAt)
	}
	fmt.Println("\n  `kanpachi resume` reopens it with the same code. `kanpachi discard` forgets it.")
	return nil
}

func cmdResume(_ context.Context, op opciones, _ []string) error {
	if !op.json {
		fmt.Println("Reopening the previous room with its same code.")
	}
	return conSala(op, protocol.MethodResumeRoom, nil)
}

func cmdDiscard(_ context.Context, op opciones, _ []string) error {
	c, err := abrir(op)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	_, hecho, err := pedir[struct{}](c, op, protocol.MethodDiscardSavedRoom, nil)
	if hecho || err != nil {
		return err
	}
	fmt.Println("Forgotten.")
	return nil
}

// cmdLast enseña la última sala a la que se entró COMO INVITADO.
//
// No es lo mismo que `pending`, y confundirlas es el error que esta separación
// existe para no cometer: aquella es del host y sigue siendo suya, esta es un
// código guardado con el que hay que volver a entrar, con canje de credencial y
// con el host viendo entrar a quien entra.
func cmdLast(_ context.Context, op opciones, _ []string) error {
	c, err := abrir(op)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	v, hecho, err := pedir[struct {
		Found bool                  `json:"found"`
		Room  protocol.LastRoomView `json:"room"`
	}](c, op, protocol.MethodLastRoom, nil)
	if hecho || err != nil {
		return err
	}
	if !v.Found {
		fmt.Println("There is no previous room saved.")
		return nil
	}
	fmt.Printf("  %s  %s@%s  (as %s)\n", v.Room.Name, v.Room.Code, v.Room.Seed, v.Room.Nick)
	// Whether it comes back on its own is the one thing worth knowing about a
	// saved room, and the file's presence does not say it: it outlives leaving
	// and being kicked alike. Only the flag does.
	if v.Room.AutoReturn {
		fmt.Println("\n  Kanpachi is going back to it by itself. `kanpachi leave` stops that.")
		return nil
	}
	fmt.Printf("\n  It does not go back on its own. To go back:  kanpachi join %s@%s\n",
		v.Room.Code, v.Room.Seed)
	return nil
}

// ─── El registro de esta máquina ─────────────────────────────────────────────

// cmdQuarantine es el interruptor de la cuarentena de base, y su lectura.
//
// `on` y `off` SON la decisión, en la dirección que digan, y las dos son
// idempotentes: encender con todo puesto repara lo que falte, apagar sin nada
// puesto ya está cumplido. A secas cuenta el estado con el puente
// síntoma→causa, que es lo que va a leer quien llegue con "no puedo compartir
// una carpeta" sin saber la palabra cuarentena. Con `--json`, la tercera boca
// imprime el estado entero.
//
// Apagar no pregunta: el comando escrito ES la intención, igual que `rotate`.
// Lo que sí hace es decir la verdad de una línea sobre lo que acaba de pasar.
func cmdQuarantine(_ context.Context, op opciones, args []string) error {
	set := ""
	if len(args) > 0 {
		switch args[0] {
		case "on", "off":
			set = args[0]
		default:
			return uso("quarantine takes on, off, or nothing to show the state")
		}
	}

	c, err := abrir(op)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	var params any
	if set != "" {
		params = struct {
			Set string `json:"set"`
		}{set}
	}
	v, hecho, err := pedir[protocol.RoomView](c, op, protocol.MethodQuarantine, params)
	if hecho || err != nil {
		return err
	}

	switch set {
	case "on":
		fmt.Println("Done. Those ports are closed on every network of this machine, until")
		fmt.Println("you turn it off. Your rooms and games are not affected.")
	case "off":
		fmt.Println("Done. This PC answers file sharing and Remote Desktop again, on all")
		fmt.Println("its networks. Your Kanpachi room does not change.")
	}
	pintarCuarentena(os.Stdout, v.Quarantine)
	return nil
}

// cmdSeed enseña o cambia el registro en el que esta máquina abre sus salas.
//
// # Por qué hace falta un comando, si antes no hacía falta nada
//
// Porque desapareció el seed por defecto. Hasta el 2026-08-12 un código pelado
// significaba una máquina concreta, compilada dentro; ahora el registro viaja en
// cada código, y al CREAR no hay código todavía, porque el código es justo lo que
// el registro emite. Así que crear necesita saber a quién preguntarle, y esto es
// dónde se contesta desde una terminal.
//
// # Por qué NO se adopta solo el de la sala a la que entraste
//
// Porque la siguiente sala que abras se hospedaría en el servidor de un
// desconocido sin que nadie lo haya decidido. Lo que ese registro sí hace es
// aparecer acá como sugerencia, para no tener que ir a buscarlo a un chat.
// cmdName lee o cambia el nombre con el que esta máquina entra a las salas.
//
// Molde de [cmdSeed], que es el comando hermano: cero o un argumento, y con el
// valor vacío se imprime la sugerencia como un comando listo para pegar en vez
// de aplicarla sola. Aplicarla sola sería exactamente el defecto que este
// cambio arregla, con el nombre del equipo ascendido a elección de nadie.
func cmdName(_ context.Context, op opciones, args []string) error {
	nuevo := ""
	switch len(args) {
	case 0:
	case 1:
		nuevo = args[0]
		if strings.HasPrefix(nuevo, "-") {
			return uso("name takes a name, not a flag: kanpachi name alvaro")
		}
	default:
		return uso("name takes at most one name, and a name has no spaces")
	}

	c, err := abrir(op)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	var params any
	if nuevo != "" {
		params = struct {
			Nickname string `json:"nickname"`
		}{nuevo}
	}
	v, hecho, err := pedir[struct {
		Nickname  string `json:"nickname"`
		Suggested string `json:"suggested"`
	}](c, op, protocol.MethodNickname, params)
	if hecho || err != nil {
		return err
	}

	if v.Nickname == "" {
		fmt.Println("Nobody has chosen a name on this machine yet.")
		fmt.Printf("\n  Rooms would show you as %s, taken from this machine's name.\n", v.Suggested)
		fmt.Println("  To choose one:  kanpachi name alvaro")
		return nil
	}
	fmt.Printf("Rooms show you as %s.\n", v.Nickname)
	fmt.Println("\n  It is the same name in the window, the wizard and here.")
	return nil
}

func cmdSeed(_ context.Context, op opciones, args []string) error {
	nuevo := ""
	switch len(args) {
	case 0:
	case 1:
		nuevo = args[0]
		if strings.HasPrefix(nuevo, "-") {
			return uso("seed takes a host name, not a flag: kanpachi seed seed.example.com")
		}
	default:
		return uso("seed takes at most one host name")
	}

	c, err := abrir(op)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	var params any
	if nuevo != "" {
		params = struct {
			Seed string `json:"seed"`
		}{nuevo}
	}
	v, hecho, err := pedir[struct {
		Seed      string `json:"seed"`
		Suggested string `json:"suggested"`
	}](c, op, protocol.MethodOwnSeed, params)
	if hecho || err != nil {
		return err
	}

	if v.Seed == "" {
		fmt.Println("This machine has no registry configured, so it cannot open rooms yet.")
		// La sugerencia se ofrece con el comando ya escrito, y no se aplica sola:
		// entrar a la sala de alguien no elige dónde vas a hospedar tú.
		if v.Suggested != "" {
			fmt.Printf("\n  The last room you entered was on %s.\n", v.Suggested)
			fmt.Printf("  To use that one:  kanpachi seed %s\n", v.Suggested)
		} else {
			fmt.Println("\n  To set one:  kanpachi seed seed.example.com")
		}
		return nil
	}
	fmt.Printf("Rooms opened from this machine live on %s.\n", v.Seed)
	if v.Suggested != "" && v.Suggested != v.Seed {
		fmt.Printf("\n  The last room you entered was on %s, which is a different one.\n", v.Suggested)
	}
	return nil
}

// ─── El apodo ────────────────────────────────────────────────────────────────

// apodo decide con qué nombre se entra, y ya NO lo recuerda: lo recuerda el
// daemon, que es la única pieza que las tres caras comparten.
//
// # Qué arregla que el fichero ya no sea de acá
//
// Que esta máquina tenga un nombre y no dos. La terminal guardaba el suyo en
// `nickname.txt` y la ventana el suyo en `ui-prefs.json`, en la misma carpeta,
// y la sala enseñaba el de la cara que hubiera entrado: medido el 2026-08-18,
// una ventana que decía «Alvaro» y una sala que enseñaba «AlvaroGDeskt».
//
// # Y lo derivado NUNCA se guarda
//
// Esa es la mitad del arreglo, más que la unificación. La rama de abajo escribía
// en disco el nombre del equipo ya limpio, y una vez escrito dejaba de
// distinguirse de un nombre elegido: por eso le ganaba al de verdad. Hoy el
// daemon lo SUGIERE, la terminal lo usa y lo dice por stderr, y nadie lo
// persiste. Con `--nick` sí se guarda, porque eso lo tecleó una persona.
//
// La derivación tampoco vive acá: vive en el daemon, que corre en la misma
// máquina, así que el nombre del equipo es el mismo y una copia menos es una
// copia que no se puede separar.
func apodo(op opciones) (string, error) {
	c, err := abrir(op)
	if err != nil {
		return "", err
	}
	defer func() { _ = c.Close() }()
	return apodoCon(c, op)
}

// apodoCon es lo mismo sobre una conexión ya abierta.
//
// No pasa por `pedir` a propósito: `pedir` imprime el JSON crudo con `--json`, y
// esto es un paso interno de crear o entrar, no la respuesta al comando.
func apodoCon(c *client.Client, op opciones) (string, error) {
	elegido, sugerido, err := pedirApodo(c, op.nick)
	if err != nil {
		return "", err
	}
	if elegido != "" {
		return elegido, nil
	}
	if sugerido == "" {
		return "", fmt.Errorf("this machine has no name yet and none could be derived.\n" +
			"  Choose one:  kanpachi name <yours>")
	}
	// Por stderr y en una línea: lo que se está haciendo es correcto y no hay
	// que pararlo —el droplet headless y cualquier script dependen de que un
	// primer arranque sin nada configurado funcione— y a la vez nadie eligió
	// este nombre, así que se dice y se dice cómo cambiarlo.
	fmt.Fprintf(os.Stderr, "kanpachi: using %s, derived from this machine's name."+
		" `kanpachi name <yours>` to change it.\n", sugerido)
	return sugerido, nil
}

// pedirApodo es la llamada pelada: con un nombre lo fija, sin él solo lee, y en
// los dos casos devuelve el elegido y el sugerido.
//
// No pasa por `pedir` a propósito: `pedir` imprime el JSON crudo con `--json`,
// y esto es un paso interno de crear, entrar o del asistente, no la respuesta al
// comando que alguien escribió. El que sí es esa respuesta es [cmdName].
func pedirApodo(c *client.Client, nuevo string) (elegido, sugerido string, err error) {
	var params any
	if nuevo != "" {
		params = struct {
			Nickname string `json:"nickname"`
		}{nuevo}
	}
	raw, err := c.Call(protocol.MethodNickname, params)
	if err != nil {
		return "", "", err
	}
	var v struct {
		Nickname  string `json:"nickname"`
		Suggested string `json:"suggested"`
	}
	if e := json.Unmarshal(raw, &v); e != nil {
		return "", "", fmt.Errorf("parsing the answer to %s: %w", protocol.MethodNickname, e)
	}
	return v.Nickname, v.Suggested, nil
}
