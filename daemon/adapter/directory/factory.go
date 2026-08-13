package directory

// La fábrica de clientes del registro, uno por seed.
//
// # Por qué dejó de ser uno solo
//
// Porque desapareció el seed por defecto. Hasta el 2026-08-12 el daemon
// construía UN cliente contra una constante del dominio, y de ahí salían dos
// consecuencias que se veían como fallos sueltos:
//
//   - entrar con un código de otro registro **se saltaba la comprobación
//     entera**, porque preguntarle al nuestro por un ID que emitió otro contesta
//     "no existe" sobre una sala que existe. Se entraba a ciegas y sin tarjeta.
//   - y quien se autohospedaba **no podía crear una sala en su propio
//     registro**, porque `Open` iba siempre al compilado.
//
// Ahora el registro sale de dónde viene la pregunta: del código al entrar, y del
// que esta máquina tiene configurado al crear.

import (
	"fmt"
	"sync"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/port"
)

// maxCached es cuántos registros distintos se recuerdan a la vez.
//
// # Por qué hay tope, si construir uno es barato
//
// Porque el nombre del registro **lo elige quien manda el código**, y este
// proceso corre como SYSTEM en Windows y como root en Linux. Sin tope, pegar
// códigos con hosts distintos hace crecer un mapa dentro de ese proceso sin
// nada que lo pare: no es una fuga de memoria peligrosa, es una que un
// desconocido controla, y esas se acotan.
//
// Ocho porque el caso real es uno o dos: el registro propio y el del amigo que
// hospeda. Ocho ya cubre a un grupo con varios servidores y sigue siendo un
// número que cabe en la cabeza.
const maxCached = 8

// Factory implementa [port.RoomDirectories].
//
// Guarda las dependencias comunes UNA vez y solo cambia el seed, que es lo que
// hace que un cliente nuevo no pueda nacer con otro protector de ficheros ni
// otro resolvedor que el resto.
type Factory struct {
	base Deps
	// own es el registro de esta máquina, o vacío si todavía no hay ninguno.
	// Ver [Factory.SetOwn].
	mu    sync.Mutex
	own   string
	cache map[string]*Directory
	// orden recuerda en qué secuencia entraron, para saber a quién echar. Un
	// slice y no un LRU de verdad: con ocho entradas, recorrer es más barato que
	// mantener una lista enlazada, y mucho más fácil de leer.
	orden []string
}

var _ port.RoomDirectories = (*Factory)(nil)

// NewFactory recibe las dependencias SIN seed: ese es el único campo que cambia
// entre un cliente y otro.
//
// El `own` inicial sale del estado guardado y puede venir vacío, que es lo
// normal en una instalación que todavía no hospedó ni entró a ninguna sala.
func NewFactory(base Deps, own string) *Factory {
	base.Seed = ""
	return &Factory{base: base, own: own, cache: map[string]*Directory{}}
}

// SetOwn cambia el registro de esta máquina.
//
// Lo llama un solo camino, y que sea uno solo es la decisión: la pantalla de
// configuración, cuando alguien lo escribe. Entrar a una sala **no** lo toca,
// aunque no haya ninguno guardado. Ver [port.StateStore.LoadSeed].
//
// **No vacía la caché.** Los clientes que hay dentro están indexados por su
// propio host y siguen siendo correctos: lo que cambia es a cuál apunta [Own],
// no qué es cada uno.
func (f *Factory) SetOwn(seed string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.own = seed
}

// OwnSeed contesta el nombre, para quien necesite decirlo sin abrir un cliente.
func (f *Factory) OwnSeed() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.own
}

// Own es el registro donde esta máquina abre salas.
func (f *Factory) Own() (port.RoomDirectory, error) {
	f.mu.Lock()
	own := f.own
	f.mu.Unlock()
	if own == "" {
		return nil, port.ErrNoOwnSeed
	}
	return f.For(own)
}

// For entrega el cliente de un seed, construyéndolo la primera vez.
//
// **Vuelve a validar el nombre**, aunque venga de `domain.ParseRoom`. No es
// desconfianza del parser: es que esta función también la llama [Own] con lo que
// había guardado en disco, y ese fichero vive en un directorio que en Windows
// todos los usuarios de la máquina pueden leer. Una comprobación en la frontera
// del adaptador cubre las dos entradas de una vez.
func (f *Factory) For(seed string) (port.RoomDirectory, error) {
	limpio, err := domain.ParseOwnSeed([]byte(seed))
	if err != nil {
		return nil, fmt.Errorf("el registro %q no es un nombre válido: %w", seed, err)
	}
	if limpio == "" {
		return nil, port.ErrNoOwnSeed
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	if d, hay := f.cache[limpio]; hay {
		return d, nil
	}

	deps := f.base
	deps.Seed = limpio
	d, err := New(deps)
	if err != nil {
		return nil, err
	}

	// Se echa ANTES de meter, para que el mapa no llegue nunca a tener uno de
	// más. El propio no se echa jamás: es el que se va a volver a pedir seguro,
	// y echarlo por una ráfaga de códigos ajenos sería dejar que un desconocido
	// decida qué se recuerda.
	for len(f.orden) >= maxCached {
		víctima := f.orden[0]
		f.orden = f.orden[1:]
		if víctima == f.own {
			// Se salta y se manda al final, para no quedarse dando vueltas
			// mirando siempre al mismo.
			f.orden = append(f.orden, víctima)
			continue
		}
		delete(f.cache, víctima)
	}
	f.cache[limpio] = d
	f.orden = append(f.orden, limpio)
	return d, nil
}
