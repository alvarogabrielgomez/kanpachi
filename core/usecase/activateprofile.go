package usecase

import (
	"context"
	"fmt"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// ActivateProfile abre los puertos de un juego, o los cierra todos.
//
// SOLO el host. Los puertos se abren únicamente en su máquina, y eso no es una
// limitación: es lo que significa ser host. Un invitado que llame a esto
// recibe [ErrNotHost] por la API.
//
// `gameID` vacío cierra todo y deja la sala sin juego, que es un estado válido
// y el que trae una sala recién creada: red cifrada, cero puertos abiertos.
//
// Cambiar de juego NO toca la sala. Se recalcula el RuleSet, se aplica la
// diferencia, y nadie se reconecta ni vuelve a pegar un código. Es la misma
// operación que ejecuta un cambio de miembros.
func (s *Session) ActivateProfile(ctx context.Context, gameID string) (domain.RoomState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.requireHost(); err != nil {
		return domain.RoomState{}, err
	}

	previous := s.state.Game

	if gameID == "" {
		s.state.Game = domain.GameProfile{}
	} else {
		// La API solo puede aplicar perfiles del catálogo, y esta línea es esa
		// frontera. Es la mitigación principal de la superficie del named
		// pipe: un proceso malicioso corriendo como el usuario puede, como
		// máximo, activar el perfil de un juego que ya está en el catálogo, y
		// jamás abrir 445 ni nada fuera de él.
		p, ok := s.catalog.Find(gameID)
		if !ok {
			return domain.RoomState{}, fmt.Errorf("%w: %q", ErrUnknownGame, gameID)
		}
		s.state.Game = p
	}

	if err := s.applyPolicy(ctx); err != nil {
		// El estado vuelve atrás. Si el firewall rechazó el conjunto nuevo, lo
		// que sigue aplicado es el viejo, y dejar el juego nuevo en el estado
		// haría que la UI mostrara puertos abiertos que no lo están.
		s.state.Game = previous
		return domain.RoomState{}, err
	}

	// Y se apaga el aviso de la sala que volvió sin juego, que es pegajoso a
	// propósito: nada del barrido lo vuelve a medir, así que lo tiene que quitar
	// justo la acción que arregla el caso. Se quita también al dejar la sala sin
	// juego a propósito, porque ahí la ausencia ya no es una pérdida, es una
	// decisión. Ver [domain.AlertGameLost].
	s.state.DropAlerts(domain.AlertGameLost)

	// Los ajustes del adaptador salen del perfil, así que cambiar de juego los
	// cambia. Sin juego, AdapterStateFor los devuelve todos apagados, que es
	// lo que hace que quitar el juego revierta las rutas sin que nadie tenga
	// que deshacerlas una por una.
	s.configureAdapter(ctx)
	s.announceLocked(ctx)
	// El juego activo se guarda con la sala, para poder reponerlo si la máquina
	// se apaga de golpe. Va el ID y jamás el perfil, igual que en el anuncio.
	s.saveRoomLocked()

	s.deps.Log.Info("juego activo", "juego", s.state.Game.ID, "anterior", previous.ID)
	return s.snapshot(), nil
}

// ForeignRulesFor busca reglas ajenas que dejan la máquina alcanzable sin pasar
// por Kanpachi.
//
// Se consulta y se MUESTRA. Nunca se desactiva sola: suspender una regla del
// firewall del usuario es una decisión suya, y hacerlo automáticamente sería
// tocarle la máquina por algo que él permitió alguna vez a propósito.
//
// La consulta va contra el almacén de reglas del firewall, buscando por ruta
// de ejecutable. No enumera procesos y no le importa si el programa está
// corriendo: la regla existe en disco haya o no partida.
//
// **Busca dos cosas, no una.** El ejecutable del juego, y las herramientas de
// control remoto. Ver [domain.RemoteAccessExecutables]: la cuarentena tapa el
// escritorio remoto estándar por puerto, y Parsec o Sunshine escuchan donde el
// usuario les diga, así que solo el ejecutable las identifica. Las de control
// remoto vuelven marcadas como [domain.ClassRemoteControl], que la UI trata
// como bloqueante.
func (s *Session) ForeignRulesFor(ctx context.Context, gameID string) ([]domain.ForeignRule, error) {
	s.mu.Lock()
	p, ok := s.catalog.Find(gameID)
	s.mu.Unlock()

	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownGame, gameID)
	}
	return s.deps.Firewall.AuditForeign(ctx, p)
}

// BlockingForeignRules son las que hay que resolver ANTES de abrir la sala.
//
// Hoy es una sola clase, el control remoto, y tiene un motivo que ninguna otra
// comparte: entrega teclado, pantalla y sistema de archivos. Despacharlo como
// una fila más de una lista es tratarlo como si fuera una regla de más para el
// juego, y no lo es.
//
// Devuelve lista vacía sin juego activo, y eso no significa que no haya nada:
// significa que todavía no se preguntó. Quien decide cuándo preguntar es la UI.
func (s *Session) BlockingForeignRules(ctx context.Context, gameID string) ([]domain.ForeignRule, error) {
	rules, err := s.ForeignRulesFor(ctx, gameID)
	if err != nil {
		return nil, err
	}
	return domain.BlockingForeign(rules), nil
}

// SuspendForeignRules desactiva las reglas ajenas que el usuario aceptó
// desactivar.
//
// Nunca se borran, solo se desactivan, y el estado previo se persiste antes de
// tocar nada. Se restauran al salir de la sala y también al arrancar el
// servicio, por si una salida sucia dejó algo suspendido.
func (s *Session) SuspendForeignRules(ctx context.Context, rules []domain.ForeignRule) error {
	if len(rules) == 0 {
		return nil
	}
	s.mu.Lock()
	err := s.requireHost()
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if err := s.deps.Firewall.SuspendForeign(ctx, rules); err != nil {
		return fmt.Errorf("desactivando las reglas del juego: %w", err)
	}
	s.deps.Log.Info("reglas ajenas desactivadas mientras dure la sala", "cuántas", len(rules))
	return nil
}
