package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/accentiostudios/kanpachi/core/port"
)

// LogFile es dónde queda todo, JUNTO AL EJECUTABLE.
//
// Junto al ejecutable y no dentro de `-data`, que es donde lo pone el daemon.
// La diferencia importa: el directorio de datos del daemon lo crea el
// instalador con su ACL y dura lo que dure la instalación, y el de esta
// herramienta es un `data` de pruebas que se borra entre corridas. Lo que tiene
// que sobrevivir a borrar `data` es justamente el registro de por qué hubo que
// borrarlo.
const LogFile = "roomprobe.log"

// maxLogBytes son dos megas, igual que en el daemon y por lo mismo: es lo que
// abre el Bloc de notas sin pensarlo y lo que aguantan varias sesiones de una
// herramienta que habla mucho a propósito.
const maxLogBytes = 2 << 20

// bom es la marca de UTF-8 y hace falta de verdad. Sin ella, el `Get-Content`
// de PowerShell 5.1 lee el fichero como ANSI y convierte "credencial emitida"
// en algo que no se puede pegar en un chat. Medido en el daemon.
var bom = []byte{0xEF, 0xBB, 0xBF}

// logArchivo escribe a disco, con rotación por tamaño.
//
// Es una COPIA de `daemon/cmd/kanpachid/log.go`, no una importación: aquel vive
// en un `package main` y no se puede importar desde acá. Quien arregle un fallo
// allá tiene un gemelo en este fichero.
type logArchivo struct {
	ruta string

	mu       sync.Mutex
	f        *os.File
	escritos int64
}

// nuevoLogArchivo abre el fichero. NUNCA falla hacia arriba: una herramienta
// que no arranca porque no pudo abrir su log es peor que una que corre sin él.
func nuevoLogArchivo(dir string) *logArchivo {
	l := &logArchivo{ruta: filepath.Join(dir, LogFile)}
	l.abrir()
	return l
}

func (l *logArchivo) abrir() {
	nuevo := false
	if st, err := os.Stat(l.ruta); err != nil {
		nuevo = true
	} else {
		l.escritos = st.Size()
	}

	f, err := os.OpenFile(l.ruta, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		// Se sigue sin log. `escribir` comprueba el nil y no hace nada.
		return
	}
	l.f = f
	if nuevo {
		if n, err := f.Write(bom); err == nil {
			l.escritos += int64(n)
		}
	}
}

func (l *logArchivo) escribir(nivel, msg string, kv []any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return
	}

	linea := fmt.Sprintf("%s %s %s %v\n",
		time.Now().Format("2006-01-02 15:04:05.000"), nivel, msg, kv)
	n, err := l.f.WriteString(linea)
	if err != nil {
		return
	}
	l.escritos += int64(n)
	if l.escritos >= maxLogBytes {
		l.rotar()
	}
}

// rotar deja UNA copia y empieza de cero. Sin carrusel numerado: dos ficheros
// se entienden sin explicar nada, y lo que se pide para diagnosticar es
// siempre el último tramo.
func (l *logArchivo) rotar() {
	_ = l.f.Close()
	l.f = nil
	_ = os.Remove(l.ruta + ".1")
	_ = os.Rename(l.ruta, l.ruta+".1")
	l.escritos = 0
	l.abrir()
}

func (l *logArchivo) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return nil
	}
	err := l.f.Close()
	l.f = nil
	return err
}

// linea es una entrada ya formada, para la vista.
type linea struct {
	At    time.Time
	Nivel string // "info " / "aviso" / "error", del mismo ancho para que la columna cuadre
	Msg   string
	KV    []any
}

// anilloMax son doscientas líneas. La vista pinta las últimas doce; el resto
// está para la opción "ver el log" del menú, que existe para no tener que abrir
// el fichero en otra ventana con la sala viva.
const anilloMax = 200

// anillo son las últimas líneas, en memoria, para poder pintarlas.
//
// Tiene su PROPIO candado y no comparte el del fichero. Lo escriben tres
// goroutines —el supervisor, el lector del motor y el canal de la sala— y lo
// lee el bucle que redibuja la pantalla. Compartir el candado del fichero haría
// que un fotograma esperase a un `fsync`.
type anillo struct {
	mu  sync.Mutex
	buf []linea
	pos int
	n   int
	seq uint64
}

func nuevoAnillo() *anillo { return &anillo{buf: make([]linea, anilloMax)} }

func (a *anillo) anotar(nivel, msg string, kv []any) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.buf[a.pos] = linea{At: time.Now(), Nivel: nivel, Msg: msg, KV: kv}
	a.pos = (a.pos + 1) % len(a.buf)
	if a.n < len(a.buf) {
		a.n++
	}
	a.seq++
}

// ultimas devuelve hasta n líneas, de la más vieja a la más nueva.
func (a *anillo) ultimas(n int) []linea {
	a.mu.Lock()
	defer a.mu.Unlock()
	if n > a.n {
		n = a.n
	}
	out := make([]linea, 0, n)
	for i := n; i > 0; i-- {
		idx := (a.pos - i + len(a.buf)) % len(a.buf)
		out = append(out, a.buf[idx])
	}
	return out
}

// Seq sube con cada línea. La vista lo mira para saber si hay algo nuevo sin
// copiar el anillo entero en cada fotograma.
func (a *anillo) Seq() uint64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.seq
}

// logRoomprobe es el único [port.Logger] de este binario.
//
// **NUNCA escribe a la consola**, y esa ausencia es lo que hace posible la
// vista en vivo. La consola tiene un dueño en cada momento —survey mientras
// pregunta, el bucle de la vista mientras dibuja— y una línea de log colándose
// por stdout parte el redibujado por la mitad. Es exactamente el fallo que hoy
// hace parecer que borrar la pantalla borra también el log.
type logRoomprobe struct {
	archivo *logArchivo
	anillo  *anillo
}

func nuevoLog(dir string) *logRoomprobe {
	return &logRoomprobe{archivo: nuevoLogArchivo(dir), anillo: nuevoAnillo()}
}

func (l *logRoomprobe) reparte(nivel, msg string, kv []any) {
	l.archivo.escribir(nivel, msg, kv)
	l.anillo.anotar(nivel, msg, kv)
}

// Los tres niveles van con el mismo ancho para que las columnas cuadren al
// leer el fichero con el ojo, que es como se lee de verdad.
func (l *logRoomprobe) Info(msg string, kv ...any)  { l.reparte("info ", msg, kv) }
func (l *logRoomprobe) Warn(msg string, kv ...any)  { l.reparte("aviso", msg, kv) }
func (l *logRoomprobe) Error(msg string, kv ...any) { l.reparte("error", msg, kv) }

func (l *logRoomprobe) Close() error { return l.archivo.Close() }

var _ port.Logger = (*logRoomprobe)(nil)
