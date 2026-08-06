package main

import (
	"fmt"

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
}

func (h *procesoHost) ShowUI() error {
	if h.ui == nil {
		return fmt.Errorf("este daemon no hospeda la interfaz")
	}
	return h.ui.Show()
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
