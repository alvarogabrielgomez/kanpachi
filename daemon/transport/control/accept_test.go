package control

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"
)

// oyenteQueFalla contesta lo que el test le ponga, en orden.
type oyenteQueFalla struct {
	mu        sync.Mutex
	respuesta []error
	llamadas  int
	fin       chan struct{}
	unaVez    sync.Once
}

func (o *oyenteQueFalla) Accept() (net.Conn, error) {
	o.mu.Lock()
	i := o.llamadas
	o.llamadas++
	var err error
	if i < len(o.respuesta) {
		err = o.respuesta[i]
	} else {
		err = net.ErrClosed
	}
	o.mu.Unlock()
	if errors.Is(err, net.ErrClosed) {
		o.unaVez.Do(func() { close(o.fin) })
	}
	return nil, err
}

func (o *oyenteQueFalla) Close() error   { return nil }
func (o *oyenteQueFalla) Addr() net.Addr { return &net.TCPAddr{} }

func (o *oyenteQueFalla) veces() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.llamadas
}

// TestUnFalloPasajeroDeAcceptNoMataElOyente.
//
// El bucle salía con CUALQUIER error, y `relisten` solo recrea un oyente cuya
// dirección cambió. O sea que un error pasajero —descriptores agotados, una
// conexión abortada entre el SYN y el accept— dejaba al host sin aceptar nada
// para siempre, con la sala en pie, el pod en verde y ni una línea que lo
// dijera. Es la misma forma del fallo de treinta y tres horas: algo se apaga y
// todo lo demás sigue afirmando que está bien.
//
// Salir sigue siendo lo correcto cuando el oyente se cerró a propósito, que es
// la forma normal de terminar.
func TestUnFalloPasajeroDeAcceptNoMataElOyente(t *testing.T) {
	ln := &oyenteQueFalla{
		respuesta: []error{
			errors.New("too many open files"),
			errors.New("connection aborted"),
			errors.New("too many open files"),
		},
		fin: make(chan struct{}),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &server{ch: New(Deps{Clock: &relojFalso{}, Log: logMudo{}}), ctx: ctx}

	hecho := make(chan struct{})
	go func() { s.accept(ln, false); close(hecho) }()

	select {
	case <-ln.fin:
	case <-time.After(5 * time.Second):
		t.Fatalf("el bucle murió con el primer error pasajero: %d llamadas", ln.veces())
	}
	select {
	case <-hecho:
	case <-time.After(2 * time.Second):
		t.Fatal("el oyente cerrado a propósito no terminó el bucle")
	}

	if n := ln.veces(); n < 4 {
		t.Fatalf("Accept se llamó %d veces: el bucle no sobrevivió a los tres errores pasajeros", n)
	}
}
