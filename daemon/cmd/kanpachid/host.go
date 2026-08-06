package main

import (
	"fmt"
	"sync"

	"github.com/accentiostudios/kanpachi/core/port"
	"github.com/accentiostudios/kanpachi/daemon/adapter/uihost"
	"github.com/accentiostudios/kanpachi/daemon/transport/protocol"
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
