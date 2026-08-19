package main

import (
	"fmt"
	"sync"

	"github.com/accentiostudios/kanpachi/core/port"
	"github.com/accentiostudios/kanpachi/daemon/adapter/uihost"
	"github.com/accentiostudios/kanpachi/daemon/transport/protocol"
	"github.com/accentiostudios/kanpachi/internal/engineid"
)

// La comprobación de que esto sigue encajando en el puerto del transporte. Si
// la interfaz de `protocol` cambia, el error sale acá y no donde se cablea.
var _ protocol.Host = (*procesoHost)(nil)

// procesoHost es lo que el pipe puede pedirle al PROCESO, no a la sala.
//
// Vive acá y no en `daemon/transport` porque las tres cosas que hace —lanzar la
// interfaz, apagar todo, y cambiar el tipo de arranque del servicio— solo las
// sabe montar el `main`. Es la misma frontera de siempre: el transporte declara
// qué necesita y este binario decide con qué.
type procesoHost struct {
	// ui es nil en modo consola, y no es un hueco: en consola no hay interfaz
	// que hospedar. Cada método lo comprueba y lo dice.
	ui *uihost.Host
	// apagar es el apagado coordinado del daemon entero.
	apagar func()
	log    port.Logger

	mu sync.Mutex
	// invitación es el `kanpachi://` que trajo el navegador y que la interfaz
	// todavía no recogió.
	//
	// **Vive acá y no en la sesión**, y no es organización: es un recado entre
	// PROCESOS —el que abrió Windows por el enlace y el que tiene la ventana—,
	// no un estado de la sala. La sesión no cambia hasta que alguien confirme.
	//
	// Uno solo, y el último gana. Dos clics seguidos en dos enlaces distintos
	// son alguien que se equivocó y volvió a pulsar: acumularlos daría una cola
	// de pantallas de confirmación que hay que despachar una por una.
	invitación string

	// motorRuta es dónde vive el motor que este daemon lanza; la pone el main,
	// que es quien lo sabe. motorUnaVez amortiza la lectura: los centinelas no
	// cambian sin reemplazar el fichero, y reemplazarlo pasa por reiniciar.
	motorRuta   string
	motorUnaVez sync.Once
	motorID     engineid.Identity
}

// EngineInfo lee los centinelas del motor, una vez, y contesta siempre lo
// mismo: los valores del FICHERO. Un fallo de lectura contesta vacío, porque
// esto alimenta una línea de la pantalla de Configuración y no una condición.
func (h *procesoHost) EngineInfo() (string, string) {
	h.motorUnaVez.Do(func() {
		if h.motorRuta == "" {
			return
		}
		id, err := engineid.Scan(h.motorRuta)
		if err != nil {
			h.log.Warn("no se pudo leer la identidad del motor", "error", err)
			return
		}
		h.motorID = id
	})
	return h.motorID.Build, h.motorID.Lib
}

// ShowUI guarda el enlace, si vino, y enseña la ventana.
//
// **El orden importa y es este.** La interfaz pregunta por el enlace en cuanto
// aparece, así que guardarlo después de mostrarla es una carrera que se pierde
// en la máquina rápida: la ventana se abre, pregunta, no hay nada, y el enlace
// llega a un buzón que nadie va a volver a mirar hasta el latido siguiente.
func (h *procesoHost) ShowUI(link string) error {
	h.setInvitación(link)
	if h.ui == nil {
		return fmt.Errorf("este daemon no hospeda la interfaz")
	}
	return h.ui.Show()
}

// setInvitación guarda un enlace pendiente. El vacío no borra el que hubiera:
// un `show_ui` a secas es el doble clic del acceso directo, y no tiene por qué
// tirar un enlace que llegó un instante antes.
func (h *procesoHost) setInvitación(link string) {
	if link == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.invitación = link
}

func (h *procesoHost) TakePendingInvite() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	link := h.invitación
	h.invitación = ""
	return link
}

// Shutdown apaga todo, y NO bloquea.
//
// El apagado se lleva por delante a la interfaz que lo pidió, así que esperar a
// terminarlo aquí dejaría al servidor del pipe intentando contestarle a un
// proceso que ya no está, sobre una conexión que se va a cortar de todos modos.
// Se lanza y se devuelve; el servidor contesta primero y el apagado ocurre
// detrás.
func (h *procesoHost) Shutdown() {
	h.log.Info("apagado pedido desde la interfaz")
	go h.apagar()
}

// fatalDeMáquina devuelve qué hacer cuando esta MÁQUINA no puede con Kanpachi.
//
// # Qué cuenta como esto, y qué no
//
// Solo lo que no depende de nosotros y no cambia reintentando: hoy, que el
// driver de red virtual no se pueda instalar. Ver `ErrPreflight` en el
// adaptador del motor. Una sala que sale mal NO pasa por acá: vuelve por donde
// vino y la enseña quien la pidió, porque la sala siguiente puede ir bien.
//
// # Por qué se apaga en vez de dejarlo abierto
//
// Porque lo que queda si no es una ventana que acepta clics y falla en todos
// igual, cada vez con treinta segundos de espera. La primera versión de este
// fallo, medida en la máquina de un invitado el 2026-08-11, fueron dos días de
// intentos: no había forma de distinguir «esta máquina no puede» de «esta sala
// no anda», y sin esa distinción lo razonable es seguir probando.
//
// # Por qué nil sin ventana
//
// Sin interfaz que hospedar no hay a quién enseñárselo, y apagar un servicio
// por un error que su operador va a leer en el canal sería quitarle la máquina
// de debajo. En consola y en la variante headless, el error vuelve y ya está.
func fatalDeMáquina(ui *uihost.Host, host *procesoHost, log port.Logger) func(error) {
	if ui == nil {
		return nil
	}
	return func(err error) {
		log.Error("esta máquina no puede crear un adaptador virtual, apagando", "error", err)
		// Bloquea hasta que alguien pulse Aceptar, con plazo. Es lo que hace que
		// el mensaje se lea ANTES de que todo se cierre: al revés, una ventana
		// que aparece en el mismo instante en que Kanpachi desaparece se lee
		// como que se rompió, no como una explicación.
		uihost.Stop(err.Error())
		if host.apagar == nil {
			// Se apunta y no se calla: sin esto el proceso se queda vivo
			// después de haber dicho que se iba, que es la forma más rara de
			// las dos de fallar acá.
			log.Error("no hay apagado cableado todavía, así que Kanpachi se queda abierto")
			return
		}
		host.apagar()
	}
}
