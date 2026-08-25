package main

import (
	"strings"
	"sync"
	"testing"
)

// logDePrueba anota lo que le llega.
type logDePrueba struct {
	mu     sync.Mutex
	líneas []string
}

func (l *logDePrueba) anota(s string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.líneas = append(l.líneas, s)
}

func (l *logDePrueba) Info(msg string, _ ...any)  { l.anota("info " + msg) }
func (l *logDePrueba) Warn(msg string, _ ...any)  { l.anota("aviso " + msg) }
func (l *logDePrueba) Error(msg string, _ ...any) { l.anota("error " + msg) }

func (l *logDePrueba) todo() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.líneas, "\n")
}

// TestEnContenedorCadaLíneaVaALosDosSitios.
//
// [logArchivo] existe porque un servicio de Windows NO TIENE salida estándar.
// Un contenedor es el caso opuesto y simétrico: su salida estándar es el único
// log que alguien lee, porque `docker logs` y `kubectl logs` leen eso y nada
// más.
//
// Medido el 2026-08-25: un host en Kubernetes pasó treinta y tres horas sin
// poder recibir a nadie, escribiendo cumplidamente en un fichero dentro de su
// volumen, y `kubectl logs` no mostró una sola línea en toda la ventana. Para
// leer el diagnóstico había que entrar al pod con `exec`.
//
// Los DOS y no solo stdout: el fichero sobrevive al reinicio del contenedor y
// stdout no.
func TestEnContenedorCadaLíneaVaALosDosSitios(t *testing.T) {
	a, b := &logDePrueba{}, &logDePrueba{}
	l := logDoble{a: a, b: b}

	l.Info("la sala cambió de estado", "de", "sin sala", "a", "resolviendo")
	l.Warn("la malla se quedó vacía")
	l.Error("no se pudo aplicar la compuerta")

	for nombre, d := range map[string]*logDePrueba{"stdout": a, "fichero": b} {
		for _, q := range []string{
			"info la sala cambió de estado",
			"aviso la malla se quedó vacía",
			"error no se pudo aplicar la compuerta",
		} {
			if !strings.Contains(d.todo(), q) {
				t.Fatalf("a %s no le llegó %q. Lo que hay:\n%s", nombre, q, d.todo())
			}
		}
	}
}
