package identity

import (
	"crypto/ed25519"
	"os"
	"path/filepath"
	"testing"
)

// protectorEspía anota lo único que hay que comprobar de la protección: CUÁNDO
// se llamó, medido en bytes que ya había dentro del fichero.
type protectorEspía struct {
	llamadas []string
	tamaños  []int64
	falla    error
}

func (p *protectorEspía) proteger(ruta string) error {
	p.llamadas = append(p.llamadas, ruta)
	// El tamaño EN EL MOMENTO de la llamada. Es todo el test: si acá hay bytes,
	// la semilla estuvo en disco sin permisos propios.
	if fi, err := os.Stat(ruta); err == nil {
		p.tamaños = append(p.tamaños, fi.Size())
	} else {
		p.tamaños = append(p.tamaños, -1)
	}
	return p.falla
}

// La llave nace una vez y la segunda carga devuelve la misma.
//
// Es la propiedad de la que cuelga todo lo demás: el registro fija la primera
// llave pública que ve para un invite ID y rechaza cualquier actualización
// firmada por otra, durante veintiún días. Una llave que cambia sola es un
// equipo que pierde las salas que tenía reservadas.
func TestLaLlaveNaceUnaVezYLaSegundaCargaEsLaMisma(t *testing.T) {
	dir := t.TempDir()
	espía := &protectorEspía{}

	primera, err := LoadOrCreate(dir, espía.proteger)
	if err != nil {
		t.Fatal(err)
	}
	segunda, err := LoadOrCreate(dir, espía.proteger)
	if err != nil {
		t.Fatal(err)
	}
	if !primera.Equal(segunda) {
		t.Error("la segunda carga devolvió otra llave")
	}
	if len(espía.llamadas) != 1 {
		t.Errorf("se protegió %d vez/veces, y solo se escribe una", len(espía.llamadas))
	}
	if _, err := os.Stat(filepath.Join(dir, IdentityFile)); err != nil {
		t.Errorf("la llave no quedó en disco: %v", err)
	}
}

// Los permisos se ponen con el fichero VACÍO.
//
// # La ventana que esto cierra
//
// Escribir la semilla y arreglar los permisos después deja un instante con la
// llave en disco heredando la ACL de ProgramData, que da lectura a todos los
// usuarios de la máquina. Es el único fichero cuyo robo ES la suplantación, así
// que un instante sigue siendo un instante.
//
// Por eso el espía mide el TAMAÑO en el momento de la llamada, y no que la
// llamada exista: un test que solo comprobara que se protegió pasaría en verde
// sobre el orden equivocado.
func TestLosPermisosSePonenConElFicheroTodavíaVacío(t *testing.T) {
	dir := t.TempDir()
	espía := &protectorEspía{}

	if _, err := LoadOrCreate(dir, espía.proteger); err != nil {
		t.Fatal(err)
	}
	if len(espía.tamaños) != 1 {
		t.Fatalf("se protegió %d vez/veces", len(espía.tamaños))
	}
	if espía.tamaños[0] != 0 {
		t.Errorf("cuando se pusieron los permisos el fichero ya tenía %d bytes dentro.\n"+
			"  La semilla estuvo en disco legible para cualquier usuario de la máquina.",
			espía.tamaños[0])
	}
	// Y se protegió el TEMPORAL, no el nombre final: la ACL viaja con el
	// rename, así que el nombre bueno nunca existe sin ella.
	if espía.llamadas[0] == filepath.Join(dir, IdentityFile) {
		t.Error("se protegió el nombre final, o sea que existió un instante sin permisos propios")
	}
}

// Una llave presente e ilegible es ERROR, jamás una llave nueva.
func TestUnaLlaveCorruptaNoSeRegeneraEnSilencio(t *testing.T) {
	dir := t.TempDir()
	ruta := filepath.Join(dir, IdentityFile)
	rota := []byte("esto no mide treinta y dos bytes")[:31]
	if err := os.WriteFile(ruta, rota, 0o600); err != nil {
		t.Fatal(err)
	}

	espía := &protectorEspía{}
	if _, err := LoadOrCreate(dir, espía.proteger); err == nil {
		t.Fatal("una llave corrupta se regeneró sola, y con eso se pierden las salas reservadas")
	}
	quedó, err := os.ReadFile(ruta)
	if err != nil {
		t.Fatal(err)
	}
	if len(quedó) != len(rota) {
		t.Errorf("el fichero cambió: medía %d y ahora mide %d", len(rota), len(quedó))
	}
	if len(espía.llamadas) != 0 {
		t.Error("se escribió algo pese a que la llave existía")
	}
}

// Sin protector no se escribe. Es un parámetro y no un valor por defecto para
// que nadie lo pueda olvidar por omisión.
func TestSinProtectorNoSeEscribeLaLlave(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadOrCreate(dir, nil); err == nil {
		t.Fatal("se escribió la llave sin nadie que le pusiera permisos")
	}
	if _, err := os.Stat(filepath.Join(dir, IdentityFile)); !os.IsNotExist(err) {
		t.Error("quedó una llave en disco")
	}
}

// Si la protección falla, no queda ni la llave ni el temporal. Media llave sin
// permisos es peor que ninguna.
func TestSiLaProtecciónFallaNoQuedaNada(t *testing.T) {
	dir := t.TempDir()
	espía := &protectorEspía{falla: os.ErrPermission}

	if _, err := LoadOrCreate(dir, espía.proteger); err == nil {
		t.Fatal("se escribió la llave con la protección fallando")
	}
	entradas, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entradas) != 0 {
		t.Errorf("quedaron %d fichero(s) sueltos: %v", len(entradas), entradas[0].Name())
	}
}

// La semilla en disco es cruda y mide exactamente lo que Ed25519 pide: cero
// superficie de parseo.
func TestLaSemillaEnDiscoEsCrudaYDeTreintaYDosBytes(t *testing.T) {
	dir := t.TempDir()
	espía := &protectorEspía{}
	priv, err := LoadOrCreate(dir, espía.proteger)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, IdentityFile))
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != ed25519.SeedSize {
		t.Fatalf("la semilla mide %d bytes y tiene que medir %d", len(raw), ed25519.SeedSize)
	}
	if !ed25519.NewKeyFromSeed(raw).Equal(priv) {
		t.Error("la semilla del disco no reconstruye la llave que se devolvió")
	}
}
