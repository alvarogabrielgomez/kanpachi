package usecase

// El registro de esta máquina: leerlo y cambiarlo.
//
// # Por qué es del daemon y no de cada cara
//
// Porque lo usa la fábrica de registros, que vive acá dentro, y porque las dos
// caras tienen que ver el mismo valor. Es la diferencia con el apodo, que sí es
// del cliente: aquél viaja como parámetro en cada orden, así que dos caras con
// dos apodos distintos son dos personas distintas y eso es correcto. Un daemon
// con dos registros distintos según quién pregunte no es nada.

import (
	"context"
	"fmt"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/timing"
)

// OwnSeed contesta a qué registro abre salas esta máquina, o "" si todavía no
// hay ninguno.
//
// Vacío es una respuesta legítima y no un fallo: es el estado de quien nunca
// hospedó. La pantalla que lo pregunta es la que lo convierte en algo.
func (s *Session) OwnSeed() string {
	raw, err := s.deps.State.LoadSeed()
	if err != nil {
		return ""
	}
	seed, err := domain.ParseOwnSeed(raw)
	if err != nil {
		// El fichero está y no sirve. No es lo mismo que no estar, así que se
		// dice, y se contesta lo único honesto: no hay registro utilizable.
		s.deps.Log.Warn("el registro guardado no es un nombre válido", "error", err)
		return ""
	}
	return seed
}

// SuggestedSeed es con qué se prellena la pantalla de configuración, o "".
//
// Sale de la última sala a la que se entró, y **no manda en nada**: no lo lee
// ninguna decisión de a dónde conectarse. Existe para que quien entró con el
// código de un amigo y hoy quiere hospedar no tenga que ir a buscar el nombre
// del servidor a un chat, y para que el diálogo de creación pueda decir de dónde
// salió eso que está ofreciendo.
//
// Que sea la última sala y no un valor guardado aparte es lo que mantiene los
// dos significados separados sin tener que explicarlos: uno es "el registro de
// esta máquina" y este es literalmente "la última sala a la que entraste".
func (s *Session) SuggestedSeed() string {
	last, hay := s.LastRoom()
	if !hay {
		return ""
	}
	return last.Room.Seed
}

// SetOwnSeed fija el registro de esta máquina y devuelve el que quedó puesto.
//
// # El orden importa
//
// Valida, comprueba que el registro CONTESTE, escribe en disco, y solo entonces
// cambia la memoria. Al revés, un fallo del disco dejaría esta sesión hablando
// con un registro que el próximo arranque no va a recordar, que es la clase de
// estado partido que nadie diagnostica: funciona hoy y deja de funcionar mañana
// sin que nadie toque nada.
//
// # Por qué se comprueba antes de guardar, y qué cuesta
//
// Porque el momento de descubrir un nombre mal escrito o un servidor caído es
// cuando alguien lo está mirando, no la próxima vez que intente abrir una sala.
// Guardarlo sin comprobar diferiría el fallo al peor momento posible: en mitad
// de crear, con el nombre ya fuera de la pantalla y la sospecha apuntando a
// cualquier otra cosa.
//
// Lo que cuesta, dicho entero: guardar el registro ahora exige red, así que no
// se puede configurar sin conexión ni con el servidor caído. Es el intercambio
// que se quiere, porque un registro que no contesta tampoco sirve para nada de
// lo que se guarda para hacer.
//
// El fallo sale como [ErrNoRegistry], que es el mismo centinela de crear y
// entrar cuando el registro no contesta: la cara ya sabe qué decir de él.
//
// # Se puede cambiar con una sala abierta
//
// Y no la toca. Publicar la tarjeta, renovar el código y reabrir van al registro
// de ESA sala, que viaja dentro de ella. Lo único que cambia es dónde se abre la
// siguiente.
func (s *Session) SetOwnSeed(ctx context.Context, seed string) (string, error) {
	limpio, err := domain.ParseOwnSeed([]byte(seed))
	if err != nil {
		return "", err
	}
	if limpio == "" {
		return "", fmt.Errorf("%w: hace falta el nombre del registro", domain.ErrInputShape)
	}
	// Por la fábrica y no por `Own()`: el que hay que sondear es el que se está
	// GUARDANDO, y el propio todavía es el anterior, o ninguno.
	dir, err := s.deps.Directories.For(limpio)
	if err != nil {
		return "", err
	}
	sondeo, cancel := context.WithTimeout(ctx, timing.SeedCheckTimeout)
	defer cancel()
	if err := dir.Reachable(sondeo); err != nil {
		return "", fmt.Errorf("%w: %s no contestó: %v", ErrNoRegistry, limpio, err)
	}
	if err := s.deps.State.SaveSeed([]byte(limpio)); err != nil {
		return "", fmt.Errorf("no se pudo guardar el registro: %w", err)
	}
	s.deps.Directories.SetOwn(limpio)
	s.deps.Log.Info("registro de esta máquina", "seed", limpio)
	return limpio, nil
}
