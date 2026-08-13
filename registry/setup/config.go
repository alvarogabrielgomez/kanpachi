// Package setup instala y diagnostica el seed sobre systemd.
//
// Existe para que "una ejecución configure todo": elegir puertos libres,
// colocar los binarios, escribir las units, arrancar, y decir exactamente qué
// falta pegar en el proxy inverso.
//
// Vive aparte del servidor HTTP a propósito. Lo de aquí toca disco, red y el
// gestor de servicios de la máquina; el registro en sí no toca nada de eso y
// sus tests corren en cualquier sistema.
package setup

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
)

// Rutas fijas. Se eligen una vez y no se preguntan: la regla del producto es
// que si algo puede decidirse, no se configura.
const (
	DirConfig  = "/etc/kanpachi"
	DirLib     = "/usr/local/lib/kanpachi"
	DirBin     = "/usr/local/bin"
	DirUnits   = "/etc/systemd/system"
	Binario    = "kanpseed"
	UnitMotor  = "kanpseed-engine.service"
	UnitReg    = "kanpseed-registry.service"
	ArchivoCfg = "seed.json"

	// StateDirName is what goes in `StateDirectory=`, and DirState is where
	// systemd puts it. The service reads the path out of STATE_DIRECTORY and
	// never needs this constant; `kanpseed password` does, because it is a
	// different process and gets no such variable.
	StateDirName = "kanpseed"
	DirState     = "/var/lib/" + StateDirName

	// DirStatePrivado is where DynamicUser=yes actually puts it. DirState is a
	// symlink to this, and removing a symlink removes the symlink, so uninstall
	// has to name both or it leaves the operator's credential behind.
	DirStatePrivado = "/var/lib/private/" + StateDirName
)

// Puertos por defecto, y el rango donde buscar si alguno está tomado.
//
// El 11010 no se mueve salvo que esté ocupado: es el que los clientes llevan
// compilado, así que cambiarlo obliga a releasear el cliente. El interno sí se
// mueve con libertad, porque solo lo conoce el proxy inverso de la máquina.
const (
	PuertoMotorPorDefecto = 11010
	PuertoRegPorDefecto   = 8010
	PuertoRPCPorDefecto   = 15888

	RangoInternoDesde = 8010
	RangoInternoHasta = 8099
	RangoRPCDesde     = 15888
	RangoRPCHasta     = 15949
)

// Config es lo que el instalador decidió, escrito para que `doctor` y `config`
// puedan leerlo después. Es la única fuente de verdad sobre qué puerto quedó.
type Config struct {
	// PuertoRegistro es el puerto INTERNO donde escucha la página y la API.
	// Es el que hay que poner en el proxy inverso.
	PuertoRegistro int `json:"puerto_registro"`
	// PuertoMotor es el público, el que ven los clientes. TCP y UDP.
	PuertoMotor int `json:"puerto_motor"`
	// PuertoRPC es el panel de control del motor. Solo loopback, jamás sale.
	PuertoRPC int `json:"puerto_rpc"`
	// Dominio es el nombre por el que se sirve la página. Solo se usa para
	// imprimir el bloque del proxy, el registro no lo necesita.
	Dominio string `json:"dominio"`
}

func RutaConfig() string { return filepath.Join(DirConfig, ArchivoCfg) }

// Cargar lee la configuración existente. Devuelve os.ErrNotExist si el seed no
// está instalado, que es un caso normal y no un fallo.
func Cargar() (Config, error) {
	crudo, err := os.ReadFile(RutaConfig())
	if err != nil {
		return Config{}, err
	}
	var c Config
	if err := json.Unmarshal(crudo, &c); err != nil {
		return Config{}, fmt.Errorf("%s is corrupt: %w", RutaConfig(), err)
	}
	return c, nil
}

// Guardar escribe la configuración de forma atómica.
//
// Atómica porque si el proceso muere a mitad de escritura, un JSON truncado
// deja el seed sin saber en qué puerto vive, y el síntoma sería que el proxy
// apunta a un sitio y el servicio a otro.
func (c Config) Guardar() error {
	if err := os.MkdirAll(DirConfig, 0o755); err != nil {
		return err
	}
	crudo, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	crudo = append(crudo, '\n')

	tmp := RutaConfig() + ".tmp"
	if err := os.WriteFile(tmp, crudo, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, RutaConfig())
}

// PuertoLibre busca el primer puerto sin ocupar del rango, en loopback.
//
// Comprueba intentando escuchar de verdad, no leyendo una tabla: es la única
// forma que no miente. Ojo con lo que NO puede saber: un puerto libre ahora
// mismo puede estar reservado en la configuración de otro servicio que esté
// apagado. Por eso el instalador lo IMPRIME en vez de darlo por obvio.
func PuertoLibre(desde, hasta int, preferido int) (int, error) {
	if preferido != 0 && libre(preferido) {
		return preferido, nil
	}
	for p := desde; p <= hasta; p++ {
		if libre(p) {
			return p, nil
		}
	}
	return 0, fmt.Errorf("there is no free port between %d and %d", desde, hasta)
}

func libre(puerto int) bool {
	l, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(puerto)))
	if err != nil {
		return false
	}
	l.Close()
	return true
}

// PuertoPublicoLibre comprueba el puerto del motor, que tiene que estar libre
// en TCP y en UDP a la vez y en todas las interfaces, no solo en loopback.
func PuertoPublicoLibre(puerto int) bool {
	dir := net.JoinHostPort("0.0.0.0", strconv.Itoa(puerto))
	l, err := net.Listen("tcp", dir)
	if err != nil {
		return false
	}
	l.Close()
	u, err := net.ListenPacket("udp", dir)
	if err != nil {
		return false
	}
	u.Close()
	return true
}
