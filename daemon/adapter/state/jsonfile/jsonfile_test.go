package jsonfile

import (
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/port"
)

// TestElAlmacénSatisfaceElPuerto: la comprobación que evita que el adaptador y
// el puerto se separen sin que nadie lo note hasta cablear el servicio.
func TestElAlmacénSatisfaceElPuerto(t *testing.T) {
	var _ port.StateStore = (*Store)(nil)
}

// TestQueNoHayaSalaGuardadaNoEsUnFallo.
//
// It is the normal case while nobody has hosted, and after the room is closed.
// The absence says nothing beyond that: shutting down cleanly keeps the file.
func TestQueNoHayaSalaGuardadaNoEsUnFallo(t *testing.T) {
	s := New(t.TempDir())

	if _, err := s.LoadRoom(); !errors.Is(err, ErrNoState) {
		t.Fatalf("LoadRoom sin archivo = %v", err)
	}
	if _, err := s.LoadLast(); !errors.Is(err, ErrNoState) {
		t.Fatalf("LoadLast sin archivo = %v", err)
	}
}

// TestLaSalaGuardadaVaYVuelvePorElDecodificadorDelDominio.
//
// El adaptador mueve bytes. Quien decide si son una sala válida es el dominio,
// y este test lo recorre entero para que el contrato del puerto quede probado
// de punta a punta.
func TestLaSalaGuardadaVaYVuelvePorElDecodificadorDelDominio(t *testing.T) {
	s := New(t.TempDir())
	quiero := salaDePrueba(t)

	raw, err := quiero.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveRoom(raw); err != nil {
		t.Fatal(err)
	}
	leído, err := s.LoadRoom()
	if err != nil {
		t.Fatal(err)
	}
	tengo, err := domain.DecodeHostedRoom(leído)
	if err != nil {
		t.Fatalf("lo que se guardó no se pudo releer: %v", err)
	}
	// DeepEqual y no `!=`: la sala guardada lleva la tarjeta sellada, que es un
	// slice, así que el struct dejó de ser comparable con el operador.
	if !reflect.DeepEqual(tengo, quiero) {
		t.Fatalf("la vuelta por disco cambió la sala:\n%+v\n%+v", tengo, quiero)
	}
}

// TestBorrarDosVecesNoEsUnError.
//
// Es lo que pasa cuando se sale de la sala y después se apaga el servicio, y
// las dos veces la intención se cumplió.
func TestBorrarDosVecesNoEsUnError(t *testing.T) {
	s := New(t.TempDir())
	raw, _ := salaDePrueba(t).Encode()
	if err := s.SaveRoom(raw); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := s.ClearRoom(); err != nil {
			t.Fatalf("borrado %d: %v", i+1, err)
		}
	}
	if _, err := s.LoadRoom(); !errors.Is(err, ErrNoState) {
		t.Fatalf("quedó algo tras borrar: %v", err)
	}
}

// TestGuardarNoDejaTemporalesSueltos.
//
// La escritura pasa por un temporal en el mismo directorio. Uno que sobreviva
// es un archivo que el instalador no borra al desinstalar y que nadie mira.
func TestGuardarNoDejaTemporalesSueltos(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	raw, _ := salaDePrueba(t).Encode()

	for i := 0; i < 3; i++ {
		if err := s.SaveRoom(raw); err != nil {
			t.Fatal(err)
		}
	}
	entradas, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entradas {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Errorf("quedó un temporal: %s", e.Name())
		}
	}
	if len(entradas) != 1 {
		t.Fatalf("archivos en el directorio = %d, se esperaba solo %s", len(entradas), HostedRoomFile)
	}
}

// TestGuardarReemplazaEnUnSoloPaso: nadie puede leer un archivo a medio
// escribir, así que reescribir con menos contenido no deja la cola del
// anterior.
func TestGuardarReemplazaEnUnSoloPaso(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	if err := s.SaveRoom([]byte(strings.Repeat("x", 4096))); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveRoom([]byte("corto")); err != nil {
		t.Fatal(err)
	}
	raw, err := s.LoadRoom()
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "corto" {
		t.Fatalf("quedó cola de la escritura anterior: %q", raw)
	}
}

// TestUnArchivoCortadoSeLeeYLoRechazaElDominio.
//
// El adaptador no interpreta: devuelve los bytes y el dominio decide. Es la
// división que hace que un archivo manipulado se rechace con las mismas reglas
// venga de donde venga.
func TestUnArchivoCortadoSeLeeYLoRechazaElDominio(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	if err := os.WriteFile(filepath.Join(dir, HostedRoomFile), []byte(`{"invite_id":"A7K2M9QX",`), 0o600); err != nil {
		t.Fatal(err)
	}

	raw, err := s.LoadRoom()
	if err != nil {
		t.Fatalf("el adaptador se negó a leer un archivo cortado: %v", err)
	}
	if _, err := domain.DecodeHostedRoom(raw); !errors.Is(err, domain.ErrPersistedShape) {
		t.Fatalf("el dominio aceptó un archivo cortado: %v", err)
	}
}

// TestLaSalaYLaÚltimaSalaSonArchivosDistintos: uno es del host y el otro del
// invitado, y confundirlos haría que salir de una sala borrara la otra.
func TestLaSalaYLaÚltimaSalaSonArchivosDistintos(t *testing.T) {
	s := New(t.TempDir())

	if err := s.SaveRoom([]byte("sala")); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveLast([]byte("última")); err != nil {
		t.Fatal(err)
	}
	if err := s.ClearRoom(); err != nil {
		t.Fatal(err)
	}

	última, err := s.LoadLast()
	if err != nil {
		t.Fatalf("borrar la sala se llevó la última: %v", err)
	}
	if string(última) != "última" {
		t.Fatalf("la última sala cambió: %q", última)
	}
}

func salaDePrueba(t *testing.T) domain.HostedRoom {
	t.Helper()
	room, err := domain.ParseRoom("A7K2M9QX@seed.midominio.com")
	if err != nil {
		t.Fatal(err)
	}
	host, err := domain.ParseNickname("alvaro")
	if err != nil {
		t.Fatal(err)
	}
	p := domain.HostedRoom{
		Room:    room,
		Name:    "Los panas",
		Host:    host,
		Subnet:  netip.MustParsePrefix("100.87.3.0/24"),
		GameID:  "project-zomboid",
		SavedAt: time.Date(2026, 8, 2, 20, 0, 0, 0, time.UTC),
	}
	for i := range p.NetworkID {
		p.NetworkID[i] = byte(i)
	}
	for i := range p.NetworkSecret {
		p.NetworkSecret[i] = byte(255 - i)
	}
	return p
}
