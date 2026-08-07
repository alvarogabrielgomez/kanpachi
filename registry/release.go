package registry

import (
	"context"
	"sync"
	"time"

	"github.com/accentiostudios/kanpachi/registry/selfupdate"
)

// Cadencia es cada cuánto se le vuelve a preguntar a GitHub por el release.
//
// Una hora. Publicar una versión pasa unas pocas veces al mes, así que
// preguntar más seguido es gastar la cuota de la API para enterarse antes de
// algo que no cambia. La cuota sin autenticar son 60 peticiones por hora y por
// IP: con esto se usa UNA, y le sobra sitio a `kanpseed upgrade --check`, que
// pregunta por su cuenta desde la misma máquina.
const Cadencia = time.Hour

// PlazoRelease acota la consulta. GitHub colgado no puede colgar la página.
const PlazoRelease = 5 * time.Second

// Release recuerda cuál es la última versión publicada.
//
// # Por qué lo pregunta el SEED y no el navegador
//
// La página podría llamar a `api.github.com` ella misma, y sale más barato de
// escribir. Cuesta tres cosas que no se ven: manda la IP de quien mira la
// página a un tercero, gasta la cuota de 60 por hora DE ESA PERSONA —que la
// comparte con todo lo que haya en su red—, y obliga a abrir `connect-src` a
// un origen ajeno en la CSP, que hoy está cerrada a `'self'` a propósito.
//
// Preguntando acá, la consulta sale una vez por hora desde el droplet, para
// todos los visitantes, y la página sigue hablando solo con su propio origen.
//
// # Por qué nunca bloquea
//
// [Release.Latest] contesta AL INSTANTE con lo último que se supo, y si eso
// está viejo dispara la consulta por detrás. La primera visita tras arrancar
// se lleva una respuesta vacía y la página se queda como estaba, que es
// exactamente lo que hace hoy. Una versión que aparece un segundo tarde no le
// cuesta nada a nadie; una página que tarda cinco segundos en pintar, sí.
type Release struct {
	// preguntar está inyectado para poder ejercitar la caché sin salir a
	// internet. En producción es [selfupdate.Latest].
	preguntar func(context.Context) (string, error)
	cadencia  time.Duration

	mu       sync.Mutex
	tag      string
	visto    time.Time
	buscando bool
}

func NewRelease() *Release {
	return &Release{preguntar: selfupdate.Latest, cadencia: Cadencia}
}

// Latest devuelve el tag conocido, o "" si todavía no se sabe ninguno.
//
// El vacío es una respuesta legítima y hay que tratarlo como tal: significa "no
// lo sé", y lo que se hace con eso es NO decir nada. Inventar un número acá
// sería anunciar una versión que a lo mejor no existe, en la única página
// desde la que la gente descarga.
func (r *Release) Latest() string {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.buscando && time.Since(r.visto) >= r.cadencia {
		r.buscando = true
		go r.refrescar()
	}
	return r.tag
}

// refrescar consulta y guarda. Corre fuera del hilo que sirve la página.
//
// Un fallo NO borra lo que ya se sabía. GitHub caído un rato no puede hacer
// que la página deje de decir la versión que dijo hace diez minutos, y esa
// versión sigue siendo cierta: el release no desaparece porque su API no
// conteste.
//
// El sello de tiempo se toca en los dos casos, y eso es lo que impide el otro
// fallo: sin él, un GitHub que contesta error dispararía una consulta nueva
// con cada visita a la página.
func (r *Release) refrescar() {
	ctx, cancel := context.WithTimeout(context.Background(), PlazoRelease)
	defer cancel()

	tag, err := r.preguntar(ctx)

	r.mu.Lock()
	defer r.mu.Unlock()
	r.buscando = false
	r.visto = time.Now()
	if err == nil && tag != "" {
		r.tag = tag
	}
}
