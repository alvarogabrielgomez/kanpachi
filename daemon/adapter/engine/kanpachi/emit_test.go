package kanpachi

import (
	"strings"
	"sync"
	"testing"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// logQueAnota guarda los mensajes para poder afirmar que algo se DIJO.
type logQueAnota struct {
	mu     sync.Mutex
	líneas []string
}

func (l *logQueAnota) anota(msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.líneas = append(l.líneas, msg)
}

func (l *logQueAnota) Debug(msg string, _ ...any) { l.anota(msg) }
func (l *logQueAnota) Info(msg string, _ ...any)  { l.anota(msg) }
func (l *logQueAnota) Warn(msg string, _ ...any)  { l.anota(msg) }
func (l *logQueAnota) Error(msg string, _ ...any) { l.anota(msg) }

func (l *logQueAnota) dijoAlgoCon(s string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, x := range l.líneas {
		if strings.Contains(x, s) {
			return true
		}
	}
	return false
}

// TestUnEventoDelMotorDescartadoDejaRastro.
//
// Es el único sitio del árbol donde un evento del motor desaparece sin dejar
// nada: `select` con `default` pelado, sin contador y sin línea, en un camino
// que este mismo código llama ráfagas. Con eso, la pregunta «¿el motor lo
// emitió?» no tenía respuesta local, y el 2026-08-25 se contestó que no
// mirando un fichero de log que no podía contenerlo.
func TestUnEventoDelMotorDescartadoDejaRastro(t *testing.T) {
	log := &logQueAnota{}
	e := &Engine{events: make(chan domain.EngineEvent, 1)}
	e.deps.Log = log

	e.emitLocked(domain.EngineEvent{Kind: domain.EngineConnected})
	e.emitLocked(domain.EngineEvent{Kind: domain.EnginePeersChanged})

	if e.descartados != 1 {
		t.Fatalf("descartados = %d, se esperaba 1", e.descartados)
	}
	if !log.dijoAlgoCon("descartado") {
		t.Fatalf("el evento se tiró en silencio: %v", log.líneas)
	}
}
