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

	"github.com/accentiostudios/kanpachi/core/domain"
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
var grupos = []struct {
	titulo  string
	nombres []string
}{
	{"The room:", []string{"status", "watch", "host", "join", "leave", "link", "rotate", "rename"}},
	{"Who is in:", []string{"members", "kick"}},
	{"The game:", []string{"games", "game"}},
	{"Checking:", []string{"exposure", "diag", "probe", "protect"}},
	{"What was left from before:", []string{"pending", "resume", "discard", "last"}},
	{"The system:", []string{"doctor", "upgrade"}},
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
		"leave": {breve: "close the room if you are the host, leave if you are a guest",
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
		"pending": {breve: "whether a room was left open from the previous start",
			correr: cmdPending},
		"resume": {breve: "reopen that room with the same code",
			correr: cmdResume},
		"discard": {breve: "forget it",
			correr: cmdDiscard},
		"last": {breve: "the last room you entered as a guest",
			correr: cmdLast},
		"doctor": {args: "[--fix]", breve: "what this needs to work, and what is broken",
			correr: cmdDoctor},
		"upgrade": {args: "[--check] [--version v] [--yes]",
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
		pintarSala(os.Stdout, st)
	}
	return err
}

// ─── La sala ─────────────────────────────────────────────────────────────────

func cmdStatus(_ context.Context, op opciones, _ []string) error {
	return conSala(op, protocol.MethodStatus, nil)
}

// refrescoDeWatch es cada cuánto se redibuja.
//
// Un segundo y no menos: lo que se mira ahí son plazos de minutos, y redibujar
// más rápido solo consigue que la consola parpadee. Va muy por debajo del latido
// del supervisor a propósito: esto enseña el estado ya cambiado, no compite con
// quien lo cambia.
const refrescoDeWatch = 1 * time.Second

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

	for {
		st, err := client.Ask[protocol.RoomView](c, protocol.MethodStatus, nil)
		if err != nil {
			return err
		}
		limpiarPantalla(os.Stdout)
		pintarSala(os.Stdout, st)
		fmt.Println("  [Ctrl+C] to leave. This does NOT close the room: the room lives in the daemon.")

		select {
		case <-ctx.Done():
			return errInterrumpido
		case <-time.After(refrescoDeWatch):
		}
	}
}

func cmdHost(_ context.Context, op opciones, args []string) error {
	nick, err := apodo(op)
	if err != nil {
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
	return conSala(op, protocol.MethodCreateRoom, struct {
		Nickname string `json:"nickname"`
		Name     string `json:"name"`
	}{nick, nombre})
}

// cmdJoin pasa el texto TAL CUAL lo pegó la persona.
//
// Nada de quitarle los guiones ni de anteponerle el host del seed:
// `domain.ParseRoom` acepta las seis formas documentadas y es la frontera de
// entrada hostil del producto. Normalizar antes de llamarla es probar otra cosa
// distinta de la que se quiere probar.
func cmdJoin(_ context.Context, op opciones, args []string) error {
	if len(args) == 0 {
		return uso("join needs the code or the link.\n" +
			"  All six forms work: VA3BSF5L, va3b-sf5l, kanpachi://VA3BSF5L,\n" +
			"  VA3BSF5L@another-seed.com, kanpachi.accentio.dev/VA3BSF5L and https://...")
	}
	nick, err := apodo(op)
	if err != nil {
		return err
	}
	if !op.json {
		fmt.Println("Entering...")
	}
	return conSala(op, protocol.MethodJoinRoom, struct {
		Code     string `json:"code"`
		Nickname string `json:"nickname"`
	}{args[0], nick})
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
		Found bool                 `json:"found"`
		Room  protocol.PendingView `json:"room"`
	}](c, op, protocol.MethodPendingRoom, nil)
	if hecho || err != nil {
		return err
	}
	if !v.Found {
		fmt.Println("No room was left from the previous start.")
		return nil
	}
	fmt.Printf("  Pending room    %s\n", v.Room.Name)
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

	_, hecho, err := pedir[struct{}](c, op, protocol.MethodDiscardPendingRoom, nil)
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
	fmt.Printf("\n  To go back:  kanpachi join %s@%s\n", v.Room.Code, v.Room.Seed)
	return nil
}

// ─── El apodo ────────────────────────────────────────────────────────────────

// apodo decide con qué nombre se entra, y lo recuerda.
//
// El orden es: lo que se pidió con `--nick`, lo que se guardó la vez anterior,
// y el nombre del equipo. Los tres pasan por [domain.ParseNickname], que es la
// misma validación que aplica el daemon: rechazar acá no protege al daemon, solo
// hace que el mensaje llegue antes de esperar un minuto por una sala.
func apodo(op opciones) (string, error) {
	if op.nick != "" {
		n, err := domain.ParseNickname(op.nick)
		if err != nil {
			return "", uso("--nick %q is not valid: %v", op.nick, err)
		}
		guardarApodo(op.datos, n.String())
		return n.String(), nil
	}
	if guardado := leerApodo(op.datos); guardado != "" {
		if n, err := domain.ParseNickname(guardado); err == nil {
			return n.String(), nil
		}
		// Uno guardado que ya no vale se ignora en silencio y se recalcula: pudo
		// escribirlo una versión con otras reglas, y morirse por eso dejaría a
		// alguien sin poder abrir una sala hasta que encontrara el fichero.
	}
	n, err := domain.ParseNickname(nombreDeApodoPorDefecto())
	if err != nil {
		return "", fmt.Errorf("could not derive a name from this machine: %w.\n"+
			"  Pass one by hand with --nick", err)
	}
	guardarApodo(op.datos, n.String())
	return n.String(), nil
}
