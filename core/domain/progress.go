package domain

import "time"

// ProgressScope es quién dio un paso de una operación larga.
//
// Lista cerrada, y corta a propósito: es lo que la pantalla usa para agrupar y
// para colorear, no una taxonomía del sistema.
type ProgressScope string

const (
	// ScopeDaemon es el propio daemon: decisiones, cálculos, estado.
	ScopeDaemon ProgressScope = "daemon"
	// ScopeSeed es el registro del vestíbulo, que está en otra máquina y es lo
	// único de esta lista que depende de internet.
	ScopeSeed ProgressScope = "seed"
	// ScopeEngine es kanpachi-engine.exe, que es otro proceso.
	ScopeEngine ProgressScope = "engine"
	// ScopeNetwork es el adaptador virtual: direcciones, MTU, métricas.
	ScopeNetwork ProgressScope = "red"
	// ScopeFirewall son las dos capas de contención.
	ScopeFirewall ProgressScope = "firewall"
)

// ProgressStep es un paso que ya ocurrió.
type ProgressStep struct {
	At    time.Time
	Scope ProgressScope
	Text  string
	// Since es lo que llevaba la operación cuando ocurrió.
	//
	// Va calculado de este lado y no derivado en la pantalla porque el reloj de
	// acá es el que manda: entre el daemon y la interfaz hay un pipe, no un
	// reloj compartido, y restar dos marcas de relojes distintos da números que
	// mienten.
	Since time.Duration
}

// Progress es la foto de la operación larga en curso, o de la última.
type Progress struct {
	// Op es qué se está haciendo, en castellano. Vacío significa que no ha
	// pasado nada todavía.
	Op string
	// Running es si sigue en curso.
	Running bool
	// Failure es el error con que terminó. Vacío si fue bien o sigue viva.
	Failure string
	// Elapsed es lo que lleva, o lo que duró.
	Elapsed time.Duration
	Steps   []ProgressStep
	// Dropped son los pasos del medio que se tiraron por el tope.
	//
	// Se cuenta para poder DECIRLO. Una lista recortada en silencio se lee como
	// una lista completa, y entonces el hueco donde estaba el problema parece
	// que nunca ocurrió.
	Dropped int
}

// Done dice si terminó bien.
func (p Progress) Done() bool { return p.Op != "" && !p.Running && p.Failure == "" }

// Failed dice si terminó mal.
func (p Progress) Failed() bool { return p.Failure != "" }
