//go:build windows

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/usecase"
)

// vistaRefresco es cada cuánto se redibuja la vista en vivo.
//
// Un segundo y no menos: lo que se mira ahí son plazos de minutos, y redibujar
// más rápido solo consigue que la consola parpadee. Va muy por debajo del
// latido de quince segundos del supervisor a propósito: la vista enseña el
// estado ya cambiado, no compite con quien lo cambia.
const vistaRefresco = 1 * time.Second

func menuPrincipal(ctx context.Context, e entorno) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if e.s.Status().Conn.InRoom() {
			if err := enSala(ctx, e); err != nil {
				return err
			}
			continue
		}
		if err := menuSinSala(ctx, e); err != nil {
			return err
		}
	}
}

// ─── Sin sala ────────────────────────────────────────────────────────────────

func menuSinSala(ctx context.Context, e entorno) error {
	e.c.cocido()
	e.c.limpiar()
	fmt.Println(raya)
	fmt.Printf("  ROOMPROBE            Yo: %s\n", e.op.nick)
	fmt.Printf("  Registro: %-24s Log: %s\n", e.op.seed, LogFile)
	fmt.Println(raya)
	fmt.Println()

	const (
		crear      = "1. Crear sala"
		entrar     = "2. Entrar a sala"
		ultima     = "3. Volver a la ultima sala (invitado)"
		reanudar   = "4. Reanudar la sala pendiente (host, tras un corte sucio)"
		descartar  = "5. Descartar la sala pendiente"
		verificar  = "6. Verificaciones del sistema"
		renombrar  = "7. Cambiar nombre"
		salirDeTod = "8. Salir"
	)

	opciones := []string{crear, entrar}
	if _, hay := e.s.LastRoom(); hay {
		opciones = append(opciones, ultima)
	}
	if _, hay := e.s.PendingRoom(); hay {
		opciones = append(opciones, reanudar, descartar)
	}
	opciones = append(opciones, verificar, renombrar, salirDeTod)

	sel, err := elegir("Que hacemos:", opciones)
	if err != nil {
		return err
	}

	switch sel {
	case crear:
		return conAviso(e, crearSala(ctx, e))
	case entrar:
		return conAviso(e, entrarASala(ctx, e))
	case ultima:
		return conAviso(e, volverALaUltima(ctx, e))
	case reanudar:
		fmt.Println("Reabriendo la sala anterior...")
		_, err := e.s.ResumeRoom(ctx)
		return conAviso(e, err)
	case descartar:
		return conAviso(e, e.s.DiscardPendingRoom(ctx))
	case verificar:
		return menuVerificaciones(ctx, e)
	case renombrar:
		cambiarNombre(e)
		return nil
	case salirDeTod:
		e.apagar()
		return errInterrumpido
	}
	return nil
}

func crearSala(ctx context.Context, e entorno) error {
	nick, err := domain.ParseNickname(e.op.nick)
	if err != nil {
		return err
	}
	nombre, err := texto("Nombre de la sala (se ve en la invitacion):",
		"Viaja dentro de la tarjeta cifrada. El seed no lo conoce.", "Prueba")
	if err != nil {
		return err
	}
	fmt.Println("\nCreando la sala. Esto tarda; los pasos van al log y a la vista.")
	_, err = e.s.CreateRoom(ctx, nick, nombre)
	if err == nil {
		e.log.Info("sala creada desde roomprobe", "enlace", e.s.InviteLink())
		fmt.Println("\n  ENLACE PARA COMPARTIR:")
		fmt.Println("  " + e.s.InviteLink())
		esperarEnter()
	}
	return err
}

// entrarASala pasa el texto TAL CUAL lo pegó la persona.
//
// Nada de quitarle los guiones ni de anteponerle el host del seed, que es lo
// que hacía antes: `domain.ParseRoom` acepta las seis formas documentadas y es
// la frontera de entrada hostil del producto. Normalizar antes de llamarla es
// probar otra cosa distinta de la que se quiere probar.
func entrarASala(ctx context.Context, e entorno) error {
	nick, err := domain.ParseNickname(e.op.nick)
	if err != nil {
		return err
	}
	pegado, err := texto("Pega el enlace o el codigo tal como te llego:",
		"Valen las seis formas: VA3BSF5L, va3b-sf5l, kanpachi://VA3BSF5L,\n"+
			"VA3BSF5L@otro-seed.com, kanpachi.accentio.dev/VA3BSF5L y https://...\n"+
			"Se pasa TAL CUAL a domain.ParseRoom.", "")
	if err != nil {
		return err
	}
	// Lo que la ventana enseña antes de que alguien pulse: a qué sala, quién
	// dice hospedarla, y qué sabe la libreta de su llave. Acá se imprime en vez
	// de pintarse, y NO detiene nada, igual que en el producto.
	mirarAntesDeEntrar(ctx, e, pegado)
	fmt.Println("\nEntrando. Los pasos van al log y a la vista.")
	_, err = e.s.JoinRoom(ctx, pegado, nick)
	if err == nil {
		tras := e.s.KnownHosts()
		e.log.Info("libreta después de entrar", "hosts", len(tras.Hosts))
	}
	return err
}

// mirarAntesDeEntrar es la pantalla de confirmación de la app, en consola.
//
// Todo lo que sale de aquí es de la MISMA consulta que hace el producto,
// `PeekInvite`, así que lo que se lee es lo que un usuario vería. Un fallo no
// detiene el ingreso: esta función informa, y quien decide es quien pulsa.
func mirarAntesDeEntrar(ctx context.Context, e entorno, pegado string) {
	previa, err := e.s.PeekInvite(ctx, pegado)
	if err != nil {
		e.log.Warn("no se pudo mirar el enlace antes de entrar", "error", err)
		fmt.Println("  (no se pudo previsualizar:", err, ")")
		return
	}
	fmt.Println()
	fmt.Println("  Sala:      ", oVacio(previa.Card.Room, "(no se pudo abrir la tarjeta)"))
	fmt.Println("  Se ident.: ", oVacio(previa.Card.Host.String(), "(sin nick)"))
	fmt.Println("  Tarjeta:   ", nombreDeConfianza(previa.Trust))
	fmt.Println("  Huella:    ", oVacio(previa.Fingerprint, "(sin firma verificada)"))
	fmt.Println("  Libreta:   ", fraseDeVeredicto(previa))
	if previa.Verdict == domain.HostKeyChanged {
		fmt.Println()
		fmt.Println("  *** OJO: la huella cambió ***")
		fmt.Println("      ANTES:", domain.Fingerprint(previa.Known.Key))
		fmt.Println("      AHORA:", previa.Fingerprint)
		fmt.Println("      Se entra igual, que es lo que hace el producto. El aviso avisa.")
	}
	e.log.Info("previsualización del enlace antes de entrar",
		"sala", previa.Card.Room, "nick", previa.Card.Host.String(),
		"tarjeta", nombreDeConfianza(previa.Trust),
		"huella", previa.Fingerprint,
		"veredicto", previa.Verdict.String(),
		"huella-recordada", domain.Fingerprint(previa.Known.Key),
		"salas-con-ese-host", previa.Known.Rooms)
}

func nombreDeConfianza(t domain.CardTrust) string {
	switch t {
	case domain.CardSigned:
		return "firmada por la llave que el registro fijó"
	case domain.CardForged:
		return "NO valida contra la llave fijada: ese registro sirve algo que su propia llave no respalda"
	default:
		return "sin firma que comprobar (registro viejo, o sin llave fijada)"
	}
}

func fraseDeVeredicto(p usecase.InvitePreview) string {
	switch p.Verdict {
	case domain.HostNew:
		return "es la primera vez que ves esta llave"
	case domain.HostKnown:
		return fmt.Sprintf("ya jugaste con %s, en %d sala(s)", p.Known.Nick, p.Known.Rooms)
	case domain.HostRenamed:
		return fmt.Sprintf("es la llave de %s, que ahora se identifica como %s",
			p.Known.Nick, p.Card.Host.String())
	case domain.HostKeyChanged:
		return fmt.Sprintf("CONOCES a %s con OTRA llave", p.Known.Nick)
	default:
		return "no hay nada que juzgar: sin firma verificada no se consulta"
	}
}

func oVacio(v, siVacio string) string {
	if v == "" {
		return siVacio
	}
	return v
}

// volverALaUltima es el camino del INVITADO, y no es `ResumeRoom`.
//
// Confundirlas es el error que esta opción existe para no cometer. `ResumeRoom`
// es del HOST y retoma una sala que sigue siendo suya. Esto es el mismo ingreso
// de la primera vez con el código guardado: hay canje de credencial y el host
// ve entrar a quien entra, que es lo que mantiene con sentido a la revocación.
func volverALaUltima(ctx context.Context, e entorno) error {
	last, ok := e.s.LastRoom()
	if !ok {
		return errors.New("no hay ninguna sala anterior guardada")
	}
	nick, err := domain.ParseNickname(e.op.nick)
	if err != nil {
		return err
	}
	// Se compone `CODIGO@seed` y no el código pelado: la última sala guarda su
	// seed, y un código pelado siempre resuelve al seed por defecto.
	enlace := last.Room.InviteID.String() + "@" + last.Room.Seed
	fmt.Printf("\nVolviendo a %s...\n", enlace)
	e.log.Info("volviendo a la última sala", "enlace", enlace,
		"guardada", last.SavedAt.Format(time.RFC3339))
	_, err = e.s.JoinRoom(ctx, enlace, nick)
	return err
}

// ─── Dentro de la sala ───────────────────────────────────────────────────────

func enSala(ctx context.Context, e entorno) error {
	tecla, err := enVivo(ctx, e)
	if err != nil {
		return err
	}
	switch tecla {
	case 3: // Ctrl+C, que aquí llega como byte y no como señal
		e.apagar()
		return errInterrumpido
	case 'm', 'M':
		return menuDeSala(ctx, e)
	}
	return nil
}

// enVivo dibuja la sala y no se va hasta que alguien pulsa una tecla, o hasta
// que la sala deja de existir.
//
// # Por qué no llama a nada caro
//
// Solo usa `Status()`, `Progress()` e `InviteLink()`, y las tres son SIN el
// candado de la sesión. Es lo que permite que la pantalla siga viva mientras
// `CreateRoom` lo sostiene un minuto entero. `Exposure`, `Diagnose` y
// `RunCanaryRound` enumeran reglas de Windows y salen a la red: llamarlas desde
// aquí convertiría cada fotograma en algo que tarda segundos. Van en el menú de
// verificaciones, a mano.
func enVivo(ctx context.Context, e entorno) (byte, error) {
	e.c.crudo()
	defer e.c.cocido()
	e.c.limpiar()

	for {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		st := e.s.Status()
		if !st.Conn.InRoom() {
			e.c.cocido()
			fmt.Println("\n  Ya no estas en la sala. Motivo:", st.LastExit.String())
			esperarEnter()
			return 0, nil
		}

		var buf bytes.Buffer
		pintarSala(&buf, st, e.s.Progress(), e.log.anillo.ultimas(eventosEnPantalla),
			e.op.nick, e.s.InviteLink(), time.Now())
		fmt.Fprintln(&buf, "  [m] menu    [d] volcar diagnostico al log    [Ctrl+C] cerrar y salir")

		e.c.alOrigen()
		_, _ = os.Stdout.Write(buf.Bytes())
		e.c.borrarElResto()

		if t, hubo := e.c.esperarTecla(vistaRefresco); hubo {
			if t == 'd' || t == 'D' {
				volcarDiagnostico(ctx, e.s, e.log, e.op.seed)
				continue
			}
			return t, nil
		}
	}
}

func menuDeSala(ctx context.Context, e entorno) error {
	e.c.cocido()
	st := e.s.Status()

	const (
		volver    = "1. << Volver a la vista en vivo"
		rotar     = "2. Cambiar el codigo de la sala"
		cerrar    = "3. Cerrar la sala"
		salirSala = "3. Salir de la sala"
		verificar = "5. Verificaciones del sistema"
		volcar    = "6. Volcar el diagnostico al log"
		renombrar = "7. Cambiar nombre"
	)

	opciones := []string{volver}
	expulsables := map[string]domain.Peer{}
	if st.Role == domain.RoleHost {
		opciones = append(opciones, rotar)
		for _, p := range st.Peers {
			if p.Self {
				continue
			}
			etiqueta := fmt.Sprintf("4. Expulsar a %s (%s)", p.Name.String(), p.VirtualIP.String())
			expulsables[etiqueta] = p
			opciones = append(opciones, etiqueta)
		}
		opciones = append(opciones, cerrar)
	} else {
		opciones = append(opciones, salirSala)
	}
	opciones = append(opciones, verificar, volcar, renombrar)

	sel, err := elegir("Accion:", opciones)
	if err != nil {
		return err
	}

	switch {
	case sel == volver:
		return nil
	case sel == rotar:
		_, err := e.s.RotateInviteCode(ctx)
		return conAviso(e, err)
	case sel == cerrar, sel == salirSala:
		e.s.LeaveRoom(ctx)
		return nil
	case sel == verificar:
		return menuVerificaciones(ctx, e)
	case sel == volcar:
		volcarDiagnostico(ctx, e.s, e.log, e.op.seed)
		fmt.Println("Volcado a", LogFile)
		esperarEnter()
		return nil
	case sel == renombrar:
		cambiarNombre(e)
		return nil
	case strings.HasPrefix(sel, "4. Expulsar"):
		p := expulsables[sel]
		_, err := e.s.KickMember(ctx, p.VirtualIP)
		// Una expulsión a medias devuelve estado válido Y error: se dice, y no
		// se trata como si no hubiera pasado nada.
		if errors.Is(err, usecase.ErrKickPartial) {
			e.log.Error("expulsión a medias", "ip", p.VirtualIP.String(), "error", err)
			*e.fallos++
		}
		return conAviso(e, err)
	}
	return nil
}

// ─── Comunes ─────────────────────────────────────────────────────────────────

// conAviso enseña el error y espera, en vez de tragárselo o de subirlo.
//
// Los errores de estos caminos son respuestas del producto —"ese código no
// existe", "el host no respondió"— y lo que hay que hacer con ellos es leerlos.
// La interrupción sí sube, porque es lo único que tiene que llegar al apagado.
func conAviso(e entorno, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, errInterrumpido) || errors.Is(err, context.Canceled) {
		return err
	}
	e.log.Error("la operación terminó mal", "error", err)
	fmt.Println("\n  MAL:", err)
	fmt.Println("\n  El detalle paso a paso está en", LogFile)
	esperarEnter()
	return nil
}

func cambiarNombre(e entorno) {
	nuevo := pedirApodo(e.op.nick)
	if _, err := domain.ParseNickname(nuevo); err != nil {
		fmt.Println("  Ese nombre no vale:", err)
		esperarEnter()
		return
	}
	e.op.nick = nuevo
	guardarApodo(e.op.datos, nuevo)
}

func esperarEnter() {
	fmt.Println("\n  Pulsa Enter para continuar...")
	var nada string
	_, _ = fmt.Scanln(&nada)
}
