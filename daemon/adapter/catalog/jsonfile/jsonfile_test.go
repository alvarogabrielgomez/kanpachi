package jsonfile

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/port"
)

const catálogoDePrueba = `{"kanpachi_catalog":1,"profiles":[
  {"id":"project-zomboid","schema":2,"name":"Project Zomboid",
   "detect":{"steam_appid":108600},
   "host_ports":[{"proto":"udp","range":"16261-16262"}],"client_ports":[],
   "connect_hint":{"kind":"direct_ip","text_es":"Join, escribe la IP del host"}}
]}`

func TestElAlmacénSatisfaceElPuerto(t *testing.T) {
	var _ port.CatalogStore = (*Store)(nil)
}

// TestQueNoHayaCatálogoLocalNoEsUnFallo: es una instalación nueva.
func TestQueNoHayaCatálogoLocalNoEsUnFallo(t *testing.T) {
	s := New(t.TempDir(), t.TempDir(), logMudo{})

	if _, err := s.LoadLocal(); !errors.Is(err, ErrNoCatalog) {
		t.Fatalf("LoadLocal sin archivo = %v", err)
	}
	if _, err := s.LoadBuiltin(); !errors.Is(err, ErrNoCatalog) {
		t.Fatalf("LoadBuiltin sin archivo = %v", err)
	}
}

// TestElCatálogoVaYVuelvePorElDecodificadorDelDominio.
func TestElCatálogoVaYVuelvePorElDecodificadorDelDominio(t *testing.T) {
	builtin, local := t.TempDir(), t.TempDir()
	escribe(t, filepath.Join(builtin, BuiltinFile), catálogoDePrueba)
	s := New(builtin, local, logMudo{})

	raw, err := s.LoadBuiltin()
	if err != nil {
		t.Fatal(err)
	}
	perfiles, rechazados, err := domain.ParseCatalogLayer(raw, domain.OriginBuiltin)
	if err != nil {
		t.Fatalf("lo que se leyó no se pudo interpretar: %v", err)
	}
	if len(perfiles) != 1 || len(rechazados) != 0 {
		t.Fatalf("perfiles = %d, rechazados = %d", len(perfiles), len(rechazados))
	}
}

// TestGuardarRespaldaLaEscrituraAnterior.
//
// El respaldo existe acá y no en el estado de sala porque lo que se puede
// perder es distinto: un catálogo son horas de alguien creando perfiles a mano.
func TestGuardarRespaldaLaEscrituraAnterior(t *testing.T) {
	local := t.TempDir()
	s := New(t.TempDir(), local, logMudo{})

	if err := s.SaveLocal([]byte("primero")); err != nil {
		t.Fatal(err)
	}
	// La primera escritura no tiene qué respaldar, y eso no es un error.
	if _, err := os.Stat(filepath.Join(local, LocalFile+backupSuffix)); !os.IsNotExist(err) {
		t.Fatalf("apareció un respaldo de la nada: %v", err)
	}

	if err := s.SaveLocal([]byte("segundo")); err != nil {
		t.Fatal(err)
	}
	respaldo, err := os.ReadFile(filepath.Join(local, LocalFile+backupSuffix))
	if err != nil {
		t.Fatalf("no se respaldó la escritura anterior: %v", err)
	}
	if string(respaldo) != "primero" {
		t.Fatalf("el respaldo no es lo anterior: %q", respaldo)
	}

	actual, err := s.LoadLocal()
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != "segundo" {
		t.Fatalf("el catálogo actual = %q", actual)
	}
}

// TestElRespaldoEsUnoSoloYNoUnHistorial: es una copia, la anterior, y su único
// trabajo es que un archivo corrupto tenga de dónde volver a mano.
func TestElRespaldoEsUnoSoloYNoUnHistorial(t *testing.T) {
	local := t.TempDir()
	s := New(t.TempDir(), local, logMudo{})

	for _, texto := range []string{"uno", "dos", "tres"} {
		if err := s.SaveLocal([]byte(texto)); err != nil {
			t.Fatal(err)
		}
	}
	entradas, err := os.ReadDir(local)
	if err != nil {
		t.Fatal(err)
	}
	if len(entradas) != 2 {
		nombres := make([]string, 0, len(entradas))
		for _, e := range entradas {
			nombres = append(nombres, e.Name())
		}
		t.Fatalf("archivos = %v, se esperaban el catálogo y un respaldo", nombres)
	}
	respaldo, err := os.ReadFile(filepath.Join(local, LocalFile+backupSuffix))
	if err != nil {
		t.Fatal(err)
	}
	if string(respaldo) != "dos" {
		t.Fatalf("el respaldo no es la escritura inmediatamente anterior: %q", respaldo)
	}
}

// TestElBuiltinNoSeEscribeJamás.
//
// Es del instalador y se actualiza con la app, que es lo que hace que cada
// versión pueda traer las correcciones de perfiles que hagan falta. Un daemon
// que lo escribiera se llevaría esas correcciones por delante.
func TestElBuiltinNoSeEscribeJamás(t *testing.T) {
	builtin, local := t.TempDir(), t.TempDir()
	ruta := filepath.Join(builtin, BuiltinFile)
	escribe(t, ruta, catálogoDePrueba)
	s := New(builtin, local, logMudo{})

	if err := s.SaveLocal([]byte("algo del usuario")); err != nil {
		t.Fatal(err)
	}
	tras, err := os.ReadFile(ruta)
	if err != nil {
		t.Fatal(err)
	}
	if string(tras) != catálogoDePrueba {
		t.Fatal("guardar el catálogo del usuario tocó el que vino con la app")
	}
	entradas, err := os.ReadDir(builtin)
	if err != nil {
		t.Fatal(err)
	}
	if len(entradas) != 1 {
		t.Fatalf("aparecieron archivos en el directorio del builtin: %d", len(entradas))
	}
}

func escribe(t *testing.T, ruta, contenido string) {
	t.Helper()
	if err := os.WriteFile(ruta, []byte(contenido), 0o644); err != nil {
		t.Fatal(err)
	}
}

type logMudo struct{}

func (logMudo) Info(string, ...any)  {}
func (logMudo) Warn(string, ...any)  {}
func (logMudo) Error(string, ...any) {}
