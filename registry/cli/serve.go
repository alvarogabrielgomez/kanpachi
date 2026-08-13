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

// recargarConHUP relee la página y olvida la versión cacheada, con cada SIGHUP.
//
// Un fallo al releer NO tumba nada y deja servida la página anterior: el
// fichero puede estar a medio copiar cuando llega la señal, y quedarse con la
// versión buena es mejor que dejar de servir.
//
// Las TRES cosas con la misma señal porque son el mismo hecho: alguien acaba de
// tocar lo que este proceso sirve sin reiniciarlo. Preguntarle a GitHub una vez
// al día ahorra cuota y cuesta un día de desfase justo el día que se publica, y
// esta señal es lo que lo salda. La credencial la escribe `kanpseed password`,
// que es otro proceso. Ver [registry.Release.Olvidar] y [registry.Auth.Reload].
//
// Recargar la página y recargar la credencial se dicen por separado a
// propósito: que la página siga siendo la de antes es cosmético, y que el
// password siga siendo el de antes es que el operador cree cerrada una puerta
// que sigue como estaba.
func recargarConHUP(ctx context.Context, page *registry.Page, srv *registry.Server) {
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	defer signal.Stop(hup)
	for {
		select {
		case <-ctx.Done():
			return
		case <-hup:
			if err := page.Reload(); err != nil {
				log.Printf("kanpseed: the page could not be reloaded, carrying on with the previous one: %v", err)
			}
			if err := srv.OlvidarVersión(); err != nil {
				log.Printf("kanpseed: THE CREDENTIAL COULD NOT BE RELOADED, the seed is still on the previous one: %v", err)
				continue
			}
			log.Print("kanpseed: reloaded: page, version and credential")
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
		log.Print("kanpseed: no state directory, the room registry does NOT survive a restart")
		return registry.NewStore(nil, nil), nil
	}

	avisar := func(err error) {
		log.Printf("kanpseed: the room registry could not be written to disk: %v", err)
	}
	store, cargadas, descartadas, err := registry.OpenStore(dir, avisar)
	if err != nil {
		log.Printf("kanpseed: the saved registry could not be read, starting empty "+
			"and open rooms will stop resolving: %v", err)
		return store, nil
	}
	if descartadas > 0 {
		log.Printf("kanpseed: %d entries of the saved registry were discarded for not holding up", descartadas)
	}
	log.Printf("kanpseed: room registry loaded from %s, %d rooms", dir, cargadas)
	return store, nil
}

// openCredential lee el password del operador, si es que hay uno.
//
// # Por qué esto SÍ impide arrancar, y el registro de salas no
//
// Son fallos opuestos. Un fichero de salas ilegible degrada: se pierden salas,
// y lo peor que pasa es que alguien tenga que renovar su código. Una credencial
// ilegible **abre un seed que el operador cerró**, y nadie se entera: el
// servicio arranca, contesta, y acepta que cualquiera hospede en él.
//
// Sin directorio de estado el seed no se puede cerrar, y se dice al arrancar en
// vez de dejar que se descubra el día que alguien intente ponerle password.
func openCredential(dir string) (*registry.Auth, error) {
	auth, err := registry.OpenAuth(dir)
	if err != nil {
		return nil, fmt.Errorf("the seed credential could not be read: %w", err)
	}
	switch {
	case dir == "":
		log.Print("kanpseed: no state directory, this seed cannot be closed with a password")
	case auth.Closed():
		log.Print("kanpseed: seed CLOSED, hosting asks for a password")
	default:
		log.Print("kanpseed: seed open, anyone can host. To close it: kanpseed password")
	}
	return auth, nil
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
	addr := fs.String("addr", fmt.Sprintf("127.0.0.1:%d", cfg.PuertoRegistro), "address to listen on")
	pagina := fs.String("page", setup.DirLib+"/index.html", "path to the invitation page")
	cli := fs.String("easytier-cli", setup.DirLib+"/easytier-cli", "path to the easytier-cli binary")
	portal := fs.String("rpc-portal", fmt.Sprintf("127.0.0.1:%d", cfg.PuertoRPC), "RPC portal of easytier-core")
	cada := fs.Duration("poll", 3*time.Second, "how often the member counter is refreshed")
	// Vacío significa registro volátil, que es el comportamiento de antes. La
	// unidad lo llena con `StateDirectory=`, y systemd exporta esa ruta en
	// `STATE_DIRECTORY`, así que no hay una ruta escrita en dos sitios que se
	// puedan desincronizar.
	estado := fs.String("state-dir", os.Getenv("STATE_DIRECTORY"),
		"directory where the room registry survives restarts")
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
	auth, err := openCredential(*estado)
	if err != nil {
		return err
	}
	counter := registry.NewCounter(*cli, *portal)
	// Se nombra para poder pasárselo al SIGHUP. Antes se construía en línea
	// dentro del `http.Server`, y así no había forma de decirle nada después.
	registro := registry.NewServer(store, counter, page, auth)
	srv := &http.Server{
		Addr:              *addr,
		Handler:           registro.Handler(),
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
	go recargarConHUP(ctx, page, registro)

	go counter.Run(ctx, *cada)
	go barrer(ctx, store)

	// Escuchar ANTES de decir READY. Con Type=notify systemd no considera
	// arrancado el servicio hasta ese aviso, así que lo que dependa de él
	// espera de verdad en vez de adivinar con un sleep.
	lis, err := listenar(srv.Addr)
	if err != nil {
		return err
	}
	log.Printf("kanpseed listening on %s, page %s, rpc %s", srv.Addr, *pagina, *portal)
	registry.Listo("listening on " + srv.Addr)

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
	log.Print("kanpseed: shutting down")
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
				log.Printf("kanpseed: %d rooms discarded, %d left", n, s.Len())
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
	return fmt.Errorf("the registry did not answer within %s: %w", plazo, ultimo)
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
