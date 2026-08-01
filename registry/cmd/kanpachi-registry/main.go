// kanpachi-registry: el registro de salas del seed.
//
// Corre al lado de easytier-core dentro de la misma imagen. Lee su portal RPC
// por loopback para contar miembros, guarda tarjetas cifradas que no puede
// leer, y sirve la página de invitación. Ver la decisión 24.
//
// Escucha en loopback por defecto y espera un proxy inverso delante. Eso no es
// una comodidad: el límite de tasa lee X-Forwarded-For, y publicar este
// proceso directo a internet permitiría falsificar esa cabecera y anularlo.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/accentiostudios/kanpachi/registry"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8010", "dirección donde escuchar. Loopback a propósito, ver el comentario de arriba")
	pagina := flag.String("page", "index.html", "ruta a la página de invitación")
	cli := flag.String("easytier-cli", "easytier-cli", "ruta al binario easytier-cli")
	portal := flag.String("rpc-portal", "127.0.0.1:15888", "portal RPC de easytier-core")
	cada := flag.Duration("poll", 3*time.Second, "cada cuánto se refresca el contador de miembros")
	flag.Parse()

	page, err := registry.NewPage(*pagina)
	if err != nil {
		log.Fatalf("kanpachi-registry: %v", err)
	}

	store := registry.NewStore(nil, nil)
	counter := registry.NewCounter(*cli, *portal)
	srv := &http.Server{
		Addr:              *addr,
		Handler:           registry.NewServer(store, counter, page).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, parar := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer parar()

	go counter.Run(ctx, *cada)
	go barrer(ctx, store)

	go func() {
		log.Printf("kanpachi-registry escuchando en %s, página %s, rpc %s", *addr, *pagina, *portal)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("kanpachi-registry: %v", err)
		}
	}()

	<-ctx.Done()
	log.Print("kanpachi-registry: cerrando")
	cierre, cancelar := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelar()
	if err := srv.Shutdown(cierre); err != nil {
		log.Printf("kanpachi-registry: cierre sucio: %v", err)
	}
}

// barrer descarta salas cuyo fijado ya expiró. El intervalo es largo porque el
// vencimiento se comprueba también al leer, así que esto solo libera memoria y
// nunca decide qué se ve.
func barrer(ctx context.Context, s *registry.Store) {
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if n := s.Sweep(); n > 0 {
				log.Printf("kanpachi-registry: %d salas descartadas, quedan %d", n, s.Len())
			}
		}
	}
}
