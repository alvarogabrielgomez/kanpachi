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
	"fmt"

	"github.com/accentiostudios/kanpachi/core/domain"
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
// Valida, escribe en disco, y solo entonces cambia la memoria. Al revés, un
// fallo del disco dejaría esta sesión hablando con un registro que el próximo
// arranque no va a recordar, que es la clase de estado partido que nadie
// diagnostica: funciona hoy y deja de funcionar mañana sin que nadie toque nada.
//
// # Se puede cambiar con una sala abierta
//
// Y no la toca. Publicar la tarjeta, renovar el código y reabrir van al registro
// de ESA sala, que viaja dentro de ella. Lo único que cambia es dónde se abre la
// siguiente.
func (s *Session) SetOwnSeed(seed string) (string, error) {
	limpio, err := domain.ParseOwnSeed([]byte(seed))
	if err != nil {
		return "", err
	}
	if limpio == "" {
		return "", fmt.Errorf("%w: hace falta el nombre del registro", domain.ErrInputShape)
	}
	if err := s.deps.State.SaveSeed([]byte(limpio)); err != nil {
		return "", fmt.Errorf("no se pudo guardar el registro: %w", err)
	}
	s.deps.Directories.SetOwn(limpio)
	s.deps.Log.Info("registro de esta máquina", "seed", limpio)
	return limpio, nil
}
