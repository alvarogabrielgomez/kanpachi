package usecase

import (
	"fmt"
	"sync"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/port"
)

// maxSteps es cuántos steps se guardan de una operación.
//
// Crear una sala da del orden de quince. Sesenta y cuatro deja sitio de sobra
// para reintentos y para operaciones que crezcan, y pone un techo a un bucle
// que emita sin parar. Al pasarse se tiran los del MEDIO y no los primeros:
// dónde empezó y dónde se atascó son las dos puntas que importan.
const maxSteps = 64

// Journal es el diario de la operación larga que esté en curso.
//
// # Por qué tiene su PROPIO candado
//
// Porque se lee mientras la sesión está ocupada. `CreateRoom` toma el candado
// de la sesión durante toda su ejecución, que son decenas de segundos, y este
// diario existe justamente para poder mirar dentro de esa espera. Colgándolo
// del candado de la sesión, la pantalla se quedaría esperando exactamente lo
// mismo que quiere observar.
//
// # Una operación a la vez, y eso ya es cierto
//
// No hay dos operaciones largas concurrentes: el candado de la sesión las
// serializa. Así que [Journal.Begin] pisa el diario anterior en vez de
// acumular, y lo que queda al terminar es la última, que es la que alguien va a
// mirar.
type Journal struct {
	clock port.Clock

	mu      sync.Mutex
	op      string
	steps   []domain.ProgressStep
	started time.Time
	ended   time.Time
	// running es si la operación sigue en curso. Se distingue de un `ended`
	// cero porque un diario recién creado tampoco tiene fin y no está
	// corriendo.
	running bool
	failure string
	// dropped cuenta los steps del medio que se tiraron por el tope, para poder
	// decirlo en vez de mentir por omisión.
	dropped int
}

// NewJournal arma el diario.
func NewJournal(clock port.Clock) *Journal { return &Journal{clock: clock} }

// Begin abre una operación y descarta la anterior.
func (j *Journal) Begin(op string) {
	if j == nil {
		return
	}
	now := j.clock.Now()
	j.mu.Lock()
	defer j.mu.Unlock()
	j.op = op
	j.steps = nil
	j.started = now
	j.ended = time.Time{}
	j.running = true
	j.failure = ""
	j.dropped = 0
}

// Step anota un paso. Cumple [port.ProgressSink].
//
// Si no hay operación abierta se ignora. No es defensivo de más: los
// adaptadores emiten steps siempre, y muchos de sus caminos corren fuera de una
// operación larga —el arranque del daemon, un reinicio del motor por su cuenta—
// donde no hay nada a lo que pertenecer.
func (j *Journal) Step(scope domain.ProgressScope, text string) {
	if j == nil {
		return
	}
	now := j.clock.Now()
	j.mu.Lock()
	defer j.mu.Unlock()
	if !j.running {
		return
	}
	if len(j.steps) >= maxSteps {
		// Se tira uno del medio. Los primeros dicen cómo empezó y los últimos
		// dónde está now; el medio de una operación que se fue de madre es lo
		// único prescindible.
		half := len(j.steps) / 2
		j.steps = append(j.steps[:half], j.steps[half+1:]...)
		j.dropped++
	}
	j.steps = append(j.steps, domain.ProgressStep{
		At:    now,
		Scope: scope,
		Text:  text,
		Since: now.Sub(j.started),
	})
}

// Stepf es [Journal.Step] con formato.
func (j *Journal) Stepf(scope domain.ProgressScope, formato string, args ...any) {
	j.Step(scope, fmt.Sprintf(formato, args...))
}

// End cierra la operación. Un error no nil la marca fallida.
//
// El diario NO se borra al terminar, y eso es la half de para qué sirve: lo
// que la pantalla enseña dentro de "ver detalles" de un error son justo los
// steps de la operación que acaba de fallar.
func (j *Journal) End(err error) {
	if j == nil {
		return
	}
	now := j.clock.Now()
	j.mu.Lock()
	defer j.mu.Unlock()
	if !j.running {
		return
	}
	j.running = false
	j.ended = now
	if err != nil {
		j.failure = err.Error()
	}
}

// Report devuelve una copia.
//
// Los steps se COPIAN: quien la reciba puede quedársela sin que la operación
// siguiente se la cambie debajo mientras la está serializando.
func (j *Journal) Report() domain.Progress {
	if j == nil {
		return domain.Progress{}
	}
	j.mu.Lock()
	defer j.mu.Unlock()

	fin := j.ended
	if j.running {
		fin = j.clock.Now()
	}
	var elapsed time.Duration
	if !j.started.IsZero() {
		elapsed = fin.Sub(j.started)
	}

	steps := make([]domain.ProgressStep, len(j.steps))
	copy(steps, j.steps)
	return domain.Progress{
		Op:      j.op,
		Running: j.running,
		Failure: j.failure,
		Elapsed: elapsed,
		Steps:   steps,
		Dropped: j.dropped,
	}
}

// Progress es lo que la API expone. No toma el candado de la sesión.
func (s *Session) Progress() domain.Progress { return s.deps.Progress.Report() }

// Compilación: el diario es el sumidero que los adaptadores reciben.
var _ port.ProgressSink = (*Journal)(nil)
