//go:build linux

package sysevents

import (
	"errors"
	"net"
	"syscall"

	"github.com/mdlayher/netlink"
)

// Las suscripciones en Linux: una de las tres es real y las otras dos no
// existen en este sistema.
//
// # Cuál sí, y por qué las otras no son un pendiente
//
//   - `changed`, los cambios de dirección, SÍ. Es netlink, es el mismo aviso
//     que en Windows da `NotifyAddrChange`, y sirve para lo mismo: el adaptador
//     de la sala toma o pierde su dirección y el supervisor se entera en el
//     momento en vez de esperar al siguiente latido.
//   - `identified`, la identificación de red, NO existe. Es un concepto de
//     Windows: NLA clasifica cada red como Privada o Pública y revierte
//     ajustes al hacerlo. Linux no clasifica redes ni revierte nada, así que no
//     hay evento que escuchar porque no hay nada que reponer después de él.
//   - `resumed`, la vuelta de la suspensión, NO. Existe el aviso, lo emite
//     logind por D-Bus, y no se suscribe: haría falta hablar D-Bus para avisar
//     de algo que en el destino de este producto, un servidor que no se
//     suspende, no pasa. Si además pasara, lo que se ve es que las direcciones
//     cambian, y de eso avisa el canal que sí está.
//
// Un canal mudo no afirma nada falso, y el supervisor reaplica igual cada
// tantos latidos: esto adelanta ese respaldo, no lo reemplaza. Ver la cabecera
// del paquete.

// subscribe abre la única suscripción que este sistema tiene.
func (e *Events) subscribe() {
	// Los tres grupos, no solo el de IPv4. Una interfaz que aparece o
	// desaparece es tan relevante como una dirección que cambia: el adaptador de
	// la sala lo crea y lo destruye el motor, así que su alta y su baja son
	// justo los dos momentos en que hay algo que reponer.
	conn, err := netlink.Dial(syscall.NETLINK_ROUTE, &netlink.Config{Groups: rtnlGroups()})
	if err != nil {
		// No es fatal y no aborta nada: sin esta suscripción el supervisor
		// sigue reaplicando por latido, que es más lento y no está roto.
		e.log.Warn("no se pudo escuchar los cambios de red del sistema",
			"error", err,
			"detalle", "el supervisor los va a notar igual en el siguiente latido")
		return
	}

	e.undo = append(e.undo, func() { _ = conn.Close() })

	// **El socket se cierra al ver `stop`, y NO solo desde `undo`.**
	//
	// Sin esto el apagado se cuelga, y el orden de [Events.Close] explica por
	// qué: cierra `stop`, después espera a las goroutines con `waiting.Wait()`,
	// y RECIÉN DESPUÉS corre `undo`. En Windows funciona porque cada suscripción
	// espera un handle de evento que `stop` despierta; acá la espera es un
	// `Receive` sobre un socket, que solo termina cuando el socket se cierra, y
	// el cierre estaba detrás de la espera. Los dos esperándose.
	//
	// Medido con el paquete instalado el 2026-08-10: el daemon anotaba su
	// arranque fallido y no salía nunca. systemd esperó los noventa segundos del
	// plazo de arranque, mandó SIGTERM, esperó otros cincuenta, y lo mató con
	// SIGKILL. En modo consola no se veía: ahí Ctrl+C mata el proceso entero sin
	// pasar por este cierre.
	//
	// Cerrar dos veces no es un problema: la segunda devuelve un error que
	// `undo` ya descarta.
	go func() {
		<-e.stop
		_ = conn.Close()
	}()

	e.waiting.Add(1)
	go func() {
		defer e.waiting.Done()
		for {
			// Receive se desbloquea cuando Close cierra el socket, que es lo que
			// hace [Events.Close] a través de `undo`. Es la razón de usar esta
			// librería en vez de un socket a pelo: con `syscall.Recvfrom` la
			// única forma de despertar la espera es cerrar el descriptor por
			// debajo, y eso es una carrera con la lectura en curso.
			if _, err := conn.Receive(); err != nil {
				select {
				case <-e.stop:
					// Salida normal: el apagado cerró el socket.
				default:
					if !errors.Is(err, net.ErrClosed) {
						e.log.Warn("se cortó la escucha de cambios de red", "error", err)
					}
				}
				return
			}
			// QUÉ cambió no se mira, y es deliberado: quien lo consume reaplica
			// el estado entero del adaptador, que es idempotente. Interpretar el
			// mensaje para decidir si vale la pena sería adivinar en el sitio
			// donde equivocarse cuesta que no se reponga nada.
			select {
			case e.changed <- struct{}{}:
			case <-e.stop:
				return
			default:
				// El canal tiene hueco de sobra y quien lo lee reaplica todo, así
				// que un aviso perdido mientras otro está en curso no pierde
				// información: el que está en curso ya va a ver el estado nuevo.
			}
		}
	}()

	e.log.Info("escuchando los cambios de red del sistema",
		"detalle", "la identificación de red y la vuelta de la suspensión son de Windows "+
			"y acá no tienen equivalente")
}

// rtnlGroups son los grupos multicast de rtnetlink, como MÁSCARA DE BITS.
//
// La trampa está acá y vale la línea: las constantes `RTNLGRP_*` son NÚMEROS DE
// GRUPO, o sea 1, 5 y 9, y el campo `Groups` de la suscripción es una máscara de
// bits. Pasar los números sumados suscribe a grupos que no son los que uno cree,
// y el síntoma es que llegan avisos de otra cosa o no llega ninguno. La
// conversión es `1 << (grupo - 1)`, que es lo que hace `RTMGRP_*` en las
// cabeceras de C.
func rtnlGroups() uint32 {
	grupo := func(n uint32) uint32 { return 1 << (n - 1) }
	return grupo(syscall.RTNLGRP_LINK) |
		grupo(syscall.RTNLGRP_IPV4_IFADDR) |
		grupo(syscall.RTNLGRP_IPV6_IFADDR)
}
