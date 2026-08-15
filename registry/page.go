package registry

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
)

// Marca es el hueco que la página deja para el estado resuelto en el servidor.
//
// El contenido por defecto es `null`, así que el archivo abierto directamente
// desde el disco funciona igual: la página ve null y cae al fetch. Eso mantiene
// una sola fuente para el HTML, sin una copia "de servidor" que se desincronice
// de la que se prueba en local.
const (
	MarcaAbre   = `<script type="application/json" id="ssr">`
	MarcaCierra = `</script>`
)

var ErrSinMarca = errors.New("la página no tiene el hueco de estado del servidor")

// Page sirve invite/index.html con el estado ya resuelto incrustado.
//
// Es lo que hace cierta la promesa de la decisión 17: la página llega armada,
// sin parpadeo y sin depender de que corra JavaScript para saber a qué sala
// mira. El fetch del cliente sigue existiendo como respaldo, para cuando el
// archivo se sirve estático o el estado no se pudo resolver.
type Page struct {
	ruta string

	mu      sync.RWMutex
	antes   []byte
	despues []byte
}

// NewPage carga la página y verifica que tenga el hueco. Falla temprano y
// ruidosamente: un binario que arranca sirviendo una página sin hueco parece
// sano y calla un bug que solo se nota mirando el HTML.
func NewPage(ruta string) (*Page, error) {
	p := &Page{ruta: ruta}
	if err := p.cargar(); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *Page) cargar() error {
	crudo, err := os.ReadFile(p.ruta)
	if err != nil {
		return fmt.Errorf("leyendo la página: %w", err)
	}
	i := bytes.Index(crudo, []byte(MarcaAbre))
	if i < 0 {
		return fmt.Errorf("%w: falta %q en %s", ErrSinMarca, MarcaAbre, p.ruta)
	}
	inicio := i + len(MarcaAbre)
	j := bytes.Index(crudo[inicio:], []byte(MarcaCierra))
	if j < 0 {
		return fmt.Errorf("%w: el hueco no cierra en %s", ErrSinMarca, p.ruta)
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	p.antes = crudo[:inicio]
	p.despues = crudo[inicio+j:]
	return nil
}

// Reload vuelve a leer el archivo. Sirve para actualizar la página en el
// droplet sin reiniciar el proceso, o sea sin cortar salas vivas del registro.
func (p *Page) Reload() error { return p.cargar() }

// Render devuelve el HTML con estado incrustado. Un estado nil deja el hueco
// en `null`, que es lo que la página interpreta como "resuélvelo tú".
func (p *Page) Render(estado any) ([]byte, error) {
	dato := []byte("null")
	if estado != nil {
		b, err := json.Marshal(estado)
		if err != nil {
			return nil, err
		}
		dato = escapar(b)
	}

	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]byte, 0, len(p.antes)+len(dato)+len(p.despues))
	out = append(out, p.antes...)
	out = append(out, dato...)
	out = append(out, p.despues...)
	return out, nil
}

// escapar impide que el contenido rompa fuera del bloque.
//
// Lo que se incrusta viene del registro, y la tarjeta la escribió un
// desconocido. Sin esto, una tarjeta que contuviera `</script>` cerraría el
// bloque y lo que siguiera se interpretaría como marcado. Se escapan también
// los separadores de línea de Unicode, que JSON admite crudos y JavaScript no.
func escapar(b []byte) []byte {
	r := strings.NewReplacer(
		"<", `\u003c`,
		">", `\u003e`,
		"&", `\u0026`,
		"\u2028", `\u2028`,
		"\u2029", `\u2029`,
	)
	return []byte(r.Replace(string(b)))
}
