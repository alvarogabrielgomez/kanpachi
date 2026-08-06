package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// LogDir y LogFile son dónde queda escrito, dentro del directorio de datos.
//
// En un subdirectorio y no sueltos: el directorio de datos ya tiene una docena
// de archivos de estado, y el log más su copia anterior son ruido ahí. **El
// subdirectorio sí lo crea el daemon**, a diferencia del directorio de datos, y
// la diferencia importa: lo que protege esos archivos es la ACL del padre, y un
// subdirectorio la HEREDA. El que no se puede crear es el de arriba, que no
// tiene de quién heredarla.
const (
	LogDir  = "logs"
	LogFile = "kanpachid.log"
)

// maxLogBytes es cuándo se rota.
//
// Dos megas es lo que aguanta el Bloc de notas sin quejarse y varios días de un
// daemon que habla poco. El corte no es por tiempo porque el tiempo no acota
// nada: un daemon en bucle de reintentos llena un disco en horas.
const maxLogBytes = 2 << 20

// logConsola escribe por la salida estándar. Es el del modo consola.
type logConsola struct{}

func (logConsola) Info(msg string, kv ...any)  { fmt.Println("info ", msg, kv) }
func (logConsola) Warn(msg string, kv ...any)  { fmt.Println("aviso", msg, kv) }
func (logConsola) Error(msg string, kv ...any) { fmt.Println("error", msg, kv) }

// logArchivo escribe en `ProgramData\Kanpachi\kanpachid.log`.
//
// # Por qué existe
//
// Porque un servicio NO TIENE salida estándar, y este binario está enlazado con
// `-H windowsgui`, así que ni siquiera tiene consola a la que reengancharse.
// Escribiendo por `fmt.Println` como servicio, todo lo que el daemon dice se
// pierde: un arranque que falla queda como un servicio que se detuvo solo, sin
// una línea que diga por qué, ni en pantalla ni en disco.
//
// Es la clase de agujero que solo se nota cuando ya hace falta.
//
// # Por qué un archivo y no el registro de eventos de Windows
//
// El Visor de eventos sería lo idiomático y cuesta un paso más en el
// instalador: una fuente registrada en el registro, que si falta convierte cada
// línea en "no se encuentra la descripción del ID de evento". Un archivo de
// texto al lado de los otros datos lo abre cualquiera, se puede pegar en un
// reporte de fallo, y ya está protegido por la ACL que el instalador le puso al
// directorio.
//
// # Por qué no aborta si no puede escribir
//
// Porque quedarse sin daemon por no poder abrir el log sería cambiar un
// problema de diagnóstico por uno de producto. Si el archivo no se puede abrir,
// las líneas se tiran y el daemon sigue.
type logArchivo struct {
	ruta string

	mu sync.Mutex
	f  *os.File
	// escritos son los bytes que lleva el archivo abierto. Se cuenta en vez de
	// preguntarle al sistema en cada línea: `Stat` por línea es una syscall por
	// línea para responder algo que ya sabemos.
	escritos int64
}

// nuevoLogArchivo abre el log. Nunca devuelve error: ver el tipo.
func nuevoLogArchivo(dir string) *logArchivo {
	carpeta := filepath.Join(dir, LogDir)
	// Si no se puede crear, `abrir` va a fallar y las líneas se van a tirar.
	// Quedarse sin daemon por no poder escribir un log sería cambiar un
	// problema de diagnóstico por uno de producto.
	_ = os.MkdirAll(carpeta, 0o700)
	l := &logArchivo{ruta: filepath.Join(carpeta, LogFile)}
	l.abrir()
	return l
}

// bom es la marca de orden de bytes de UTF-8.
//
// **Se escribe al crear el archivo, y hace falta de verdad.** Go escribe UTF-8
// y las herramientas de Windows leen ANSI por defecto: sin la marca, el
// `Get-Content` de PowerShell 5.1 convierte "catálogo" en "catÃ¡logo", y lo
// mismo hacen la mitad de los editores. Medido en la primera corrida del
// servicio. Este archivo existe para que alguien lo lea y lo pegue en un
// reporte de fallo, así que tiene que llegar legible sin que nadie sepa pasarle
// una bandera de codificación.
var bom = []byte{0xEF, 0xBB, 0xBF}

// abrir deja `f` listo, o en nil si no se pudo. Con el candado tomado, o antes
// de que nadie más lo vea.
func (l *logArchivo) abrir() {
	f, err := os.OpenFile(l.ruta, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	if info, err := f.Stat(); err == nil {
		l.escritos = info.Size()
	}
	// Solo en un archivo recién creado: repetirla al reabrir para agregar
	// dejaría la marca en medio del texto, que es peor que no tenerla.
	if l.escritos == 0 {
		if n, err := f.Write(bom); err == nil {
			l.escritos = int64(n)
		}
	}
	l.f = f
}

func (l *logArchivo) Info(msg string, kv ...any)  { l.escribir("info ", msg, kv) }
func (l *logArchivo) Warn(msg string, kv ...any)  { l.escribir("aviso", msg, kv) }
func (l *logArchivo) Error(msg string, kv ...any) { l.escribir("error", msg, kv) }

func (l *logArchivo) escribir(nivel, msg string, kv []any) {
	// La hora va en local y no en UTC: quien lee esto está mirando el reloj de
	// la máquina donde falló.
	linea := fmt.Sprintf("%s %s %s %v\n", time.Now().Format("2006-01-02 15:04:05.000"), nivel, msg, kv)

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return
	}
	n, err := l.f.WriteString(linea)
	if err != nil {
		return
	}
	l.escritos += int64(n)
	if l.escritos >= maxLogBytes {
		l.rotar()
	}
}

// rotar deja el archivo en `.1` y empieza uno nuevo. Con el candado tomado.
//
// UNA copia y no un carrusel numerado: lo que sirve para diagnosticar es lo que
// pasó hace un rato, y guardar cinco generaciones de un log que nadie va a leer
// es ocupar disco de otro.
func (l *logArchivo) rotar() {
	_ = l.f.Close()
	l.f = nil
	l.escritos = 0
	// Si el rename falla, el archivo se reabre igual en modo append y lo único
	// que se pierde es el corte: mejor un log grande que ninguno.
	_ = os.Remove(l.ruta + ".1")
	_ = os.Rename(l.ruta, l.ruta+".1")
	l.abrir()
}

// Close cierra el archivo. Idempotente.
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
