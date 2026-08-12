package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/accentiostudios/kanpachi/registry"
	"github.com/accentiostudios/kanpachi/registry/setup"
)

// recargarConHUP vuelve a leer la página cada vez que llega un SIGHUP.
//
// Un fallo al releer NO tumba nada y deja servida la página anterior: el
// fichero puede estar a medio copiar cuando llega la señal, y quedarse con la
// versión buena es mejor que dejar de servir.
func recargarConHUP(ctx context.Context, page *registry.Page) {
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	defer signal.Stop(hup)
	for {
		select {
		case <-ctx.Done():
			return
		case <-hup:
			if err := page.Reload(); err != nil {
				log.Printf("kanpseed: la página no se pudo recargar, se sigue con la de antes: %v", err)
				continue
			}
			log.Print("kanpseed: página recargada")
		}
	}
}

// abrirRegistro construye el almacén, con respaldo en disco si hay dónde.
//
// # Por qué un fichero ilegible no impide arrancar
//
// Porque un registro que no arranca es peor que uno con una sala menos. Sin
// registro no se resuelve ningún código, no se sirve la página y el motor se
// para con él por el `BindsTo` de la unidad. Con el fichero descartado se
// pierden las salas guardadas, que es exactamente el estado que había antes de
// que esto existiera.
//
// Se dice fuerte igualmente: es la única señal de que alguien va a llamar
// diciendo que su código dejó de valer.
func abrirRegistro(dir string) (*registry.Store, error) {
	if dir == "" {
		log.Print("kanpseed: sin directorio de estado, el registro de salas NO sobrevive a un reinicio")
		return registry.NewStore(nil, nil), nil
	}

	avisar := func(err error) {
		log.Printf("kanpseed: no se pudo respaldar el registro de salas: %v", err)
	}
	store, cargadas, descartadas, err := registry.OpenStore(dir, avisar)
	if err != nil {
		log.Printf("kanpseed: el registro guardado no se pudo leer, se arranca vacío "+
			"y las salas abiertas van a dejar de resolver: %v", err)
		return store, nil
	}
	if descartadas > 0 {
		log.Printf("kanpseed: %d entradas del registro guardado se descartaron por no sostenerse", descartadas)
	}
	log.Printf("kanpseed: registro de salas cargado desde %s, %d salas", dir, cargadas)
	return store, nil
}

// cmdServe es lo que arranca systemd. No está pensado para invocarse a mano,
// y por eso sus valores por defecto salen de la configuración instalada en vez
// de ser constantes: así `kanpseed serve` a secas hace lo correcto en una
// máquina ya instalada, que es lo que uno intenta al depurar.
func cmdServe(args []string) error {
	cfg, _ := setup.Cargar()
	if cfg.PuertoRegistro == 0 {
		cfg.PuertoRegistro = setup.PuertoRegPorDefecto
	}
	if cfg.PuertoRPC == 0 {
		cfg.PuertoRPC = setup.PuertoRPCPorDefecto
	}

	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	addr := fs.String("addr", fmt.Sprintf("127.0.0.1:%d", cfg.PuertoRegistro), "dirección donde escuchar")
	pagina := fs.String("page", setup.DirLib+"/index.html", "ruta a la página de invitación")
	cli := fs.String("easytier-cli", setup.DirLib+"/easytier-cli", "ruta al binario easytier-cli")
	portal := fs.String("rpc-portal", fmt.Sprintf("127.0.0.1:%d", cfg.PuertoRPC), "portal RPC de easytier-core")
	cada := fs.Duration("poll", 3*time.Second, "cada cuánto se refresca el contador de miembros")
	// Vacío significa registro volátil, que es el comportamiento de antes. La
	// unidad lo llena con `StateDirectory=`, y systemd exporta esa ruta en
	// `STATE_DIRECTORY`, así que no hay una ruta escrita en dos sitios que se
	// puedan desincronizar.
	estado := fs.String("state-dir", os.Getenv("STATE_DIRECTORY"),
		"directorio donde el registro de salas sobrevive a los reinicios")
	if err := fs.Parse(args); err != nil {
		return err
	}

	page, err := registry.NewPage(*pagina)
	if err != nil {
		return err
	}

	store, err := abrirRegistro(*estado)
	if err != nil {
		return err
	}
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

	// **SIGHUP vuelve a leer la página, sin reiniciar el proceso.**
	//
	// Existe por una razón concreta y no por parecerse a otros daemons: el
	// registro de salas vive EN MEMORIA. Reiniciar para publicar un cambio de
	// la página tira todas las salas registradas, y quien tuviera un enlace
	// repartido se queda con un código que el servidor ya no conoce.
	//
	// `Page.Reload` estaba escrito, documentado con este motivo exacto, y no lo
	// llamaba nadie. Es la cuarta vez que pasa en este repositorio, después de
	// `control.Attach`, `firewall.SetScope` y el modo servicio.
	go recargarConHUP(ctx, page)

	go counter.Run(ctx, *cada)
	go barrer(ctx, store)

	// Escuchar ANTES de decir READY. Con Type=notify systemd no considera
	// arrancado el servicio hasta ese aviso, así que lo que dependa de él
	// espera de verdad en vez de adivinar con un sleep.
	lis, err := listenar(srv.Addr)
	if err != nil {
		return err
	}
	log.Printf("kanpseed escuchando en %s, página %s, rpc %s", srv.Addr, *pagina, *portal)
	registry.Listo("escuchando en " + srv.Addr)

	// El latido se calla si el registro deja de responderse a sí mismo. Es la
	// diferencia entre "el proceso existe" y "el proceso sirve": la primera ya
	// la cubre Restart=always, la segunda no la cubre nada más.
	go registry.Latido(ctx, func() bool { return sano(srv.Addr) })

	go func() {
		if err := srv.Serve(lis); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("kanpseed: %v", err)
		}
	}()

	<-ctx.Done()
	registry.Parando()
	log.Print("kanpseed: cerrando")
	cierre, cancelar := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelar()
	return srv.Shutdown(cierre)
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
				log.Printf("kanpseed: %d salas descartadas, quedan %d", n, s.Len())
			}
		}
	}
}

// sano comprueba el servicio desde fuera, por su propio puerto. Preguntarle a
// una variable en memoria diría que sí aunque el servidor estuviera atascado,
// que es justo el caso que el watchdog existe para detectar.
func sano(addr string) bool {
	cliente := &http.Client{Timeout: 5 * time.Second}
	res, err := cliente.Get("http://" + addr + "/healthz")
	if err != nil {
		return false
	}
	defer res.Body.Close()
	// Un 503 significa que EasyTier no contesta, y eso NO es motivo para
	// reiniciar el registro: sigue resolviendo invite IDs y sirviendo la
	// página, solo sin contador. Reiniciarlo por eso empeoraría las cosas.
	return res.StatusCode < 500 || res.StatusCode == http.StatusServiceUnavailable
}

func esperarSalud(cfg setup.Config, plazo time.Duration) error {
	addr := fmt.Sprintf("127.0.0.1:%d", cfg.PuertoRegistro)
	limite := time.Now().Add(plazo)
	var ultimo error
	for time.Now().Before(limite) {
		cliente := &http.Client{Timeout: 2 * time.Second}
		res, err := cliente.Get("http://" + addr + "/healthz")
		if err == nil {
			res.Body.Close()
			return nil
		}
		ultimo = err
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("el registro no respondió en %s: %w", plazo, ultimo)
}

// consultarSalud devuelve lo que /healthz reporta, para el doctor.
func consultarSalud(cfg setup.Config) (map[string]any, error) {
	cliente := &http.Client{Timeout: 3 * time.Second}
	res, err := cliente.Get(fmt.Sprintf("http://127.0.0.1:%d/healthz", cfg.PuertoRegistro))
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var cuerpo map[string]any
	if err := json.NewDecoder(res.Body).Decode(&cuerpo); err != nil {
		return nil, err
	}
	return cuerpo, nil
}

// listenar abre el socket antes de avisar READY. Está aparte para que la
// intención se lea: primero se escucha, luego se dice que se está listo.
func listenar(addr string) (net.Listener, error) {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("no se pudo escuchar en %s: %w", addr, err)
	}
	return l, nil
}
