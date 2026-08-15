package registry

import (
	"context"
	"sync"
	"time"

	"github.com/accentiostudios/kanpachi/registry/selfupdate"
)

// Cadencia es cada cuánto se le vuelve a preguntar a GitHub por el release.
//
// Un día. Publicar una versión pasa unas pocas veces al mes, así que preguntar
// veinticuatro veces diarias es gastar cuota para enterarse antes de algo que no
// cambia. Era una hora, y su único consumidor ahora es la página: el cliente
// dejó de preguntarle al registro por la versión, y `/api/version` se borró al
// quedarse sin llamadores.
//
// La espera no se nota al publicar: `kanpseed upgrade` reinicia el servicio, y
// un proceso nuevo arranca sin nada cacheado. Para recargar sin reiniciar está
// [Release.Olvidar], colgado del SIGHUP que ya relee la página.
const Cadencia = 24 * time.Hour

// CadenciaTrasFallo es cuándo se reintenta cuando GitHub no contestó.
//
// # Por qué hace falta un plazo aparte, y no lo hacía antes
//
// Porque [Release.refrescar] toca el sello de tiempo también cuando falla, a
// propósito: sin eso, un GitHub caído dispararía una consulta nueva con cada
// visita a la página. Con la cadencia en una hora, un fallo costaba una hora de
// desfase. Con un día costaría un día, o sea que una caída de cinco minutos
// dejaría la página sin versión hasta mañana.
const CadenciaTrasFallo = 10 * time.Minute

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
// Preguntando acá, la consulta sale una vez al día desde el droplet, para todos
// los visitantes, y la página sigue hablando solo con su propio origen. Ver
// [Cadencia].
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
	// falló dice si la última consulta salió mal, y con eso se elige el plazo
	// del siguiente intento. Ver [CadenciaTrasFallo].
	falló bool
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

	if !r.buscando && time.Since(r.visto) >= r.esperaActual() {
		r.buscando = true
		go r.refrescar()
	}
	return r.tag
}

// esperaActual es cuánto hay que esperar desde la última consulta, y depende de
// si aquélla salió bien. Asume el candado tomado.
func (r *Release) esperaActual() time.Duration {
	if r.falló {
		return CadenciaTrasFallo
	}
	return r.cadencia
}

// Olvidar hace que la próxima visita vuelva a preguntar, sin esperar el día.
//
// # Por qué una señal y no pasarle el tag ya sabido
//
// Porque quien acaba de publicar es OTRO PROCESO. `kanpseed upgrade` corre en la
// terminal y el servidor es un servicio: la CLI no puede escribirle nada en
// memoria, así que "dejarle el tag que acaba de recibir" no tiene por dónde
// llegar. Lo que sí llega es un SIGHUP, que ya existe para releer la página, y
// eso alcanza: olvidar lo cacheado hace que la visita siguiente consulte.
//
// **Actualizar el seed no la necesita**, y por eso esto no es el camino normal:
// `kanpseed upgrade` reinicia el servicio, y un proceso nuevo arranca sin nada
// cacheado. Esto es para el otro caso, el operador que recarga sin reiniciar.
//
// No borra el tag conocido: mientras la consulta nueva no conteste, la página
// sigue diciendo lo último cierto, que es lo mismo que hace ante un fallo.
func (r *Release) Olvidar() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.visto = time.Time{}
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
// con cada visita a la página. Lo que SÍ cambia entre los dos casos es cuándo
// se vuelve a intentar, ver [CadenciaTrasFallo].
func (r *Release) refrescar() {
	ctx, cancel := context.WithTimeout(context.Background(), PlazoRelease)
	defer cancel()

	tag, err := r.preguntar(ctx)

	r.mu.Lock()
	defer r.mu.Unlock()
	r.buscando = false
	r.visto = time.Now()
	r.falló = err != nil || tag == ""
	if !r.falló {
		r.tag = tag
	}
}
