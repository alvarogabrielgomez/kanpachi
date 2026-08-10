package arch

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Los guardianes de la cadena de suministro.
//
// Salen de la auditoría de la Parte 5 del plan, y cubren dos cosas que este
// proyecto CONSUME y no escribe: los binarios del motor y las skills de terceros
// que influyen en lo que se escribe acá.
//
// **Los dos directorios están en .gitignore**, así que en CI no existen y estos
// tests se saltan. Eso no los hace decorativos: el riesgo de los dos vive en la
// máquina donde se ejecuta y donde se programa, no en el runner. Un binario
// cambiado lo ejecuta el daemon como SYSTEM; una skill cambiada le dicta a un
// agente qué escribir.
//
// Saltarse no es aprobar. Si el directorio está, se verifica entero.

// TestLosBinariosDelMotorSonLosQueSeProbaron.
//
// El daemon ejecuta easytier-core.exe como proceso hijo con los privilegios del
// servicio. Sin esto no hay nada en el repo que diga que el binario de esta
// máquina es el del release que se probó, y la promesa de "v2.6.4 fijada" de la
// decisión 1 sería una frase sin nada detrás.
func TestLosBinariosDelMotorSonLosQueSeProbaron(t *testing.T) {
	verificaManifiesto(t, "easytier.sums", "../../third_party/easytier")
}

// TestLasSkillsDeTercerosNoCambiaronSolas.
//
// Cuarenta y seis skills de un tercero, instaladas por skills-lock.json. Ese
// archivo guarda un hash por skill y **nada lo comprueba**, así que era
// inventario y no control: cualquier edición posterior a la instalación pasaba
// sin que nadie se enterara.
//
// Este manifiesto se calcula acá, con un esquema que este repo puede reproducir,
// que es la diferencia entre verificar y confiar.
func TestLasSkillsDeTercerosNoCambiaronSolas(t *testing.T) {
	verificaManifiesto(t, "skills.sums", "../../.agents/skills")
}

// verificaManifiesto compara un directorio entero contra su lista de sumas.
//
// Falla por las tres vías, y las tres importan: un archivo que cambió, uno que
// falta, y uno que APARECIÓ. La tercera es la que atrapa el caso interesante, que
// es contenido nuevo colado en un directorio que nadie vuelve a mirar.
func verificaManifiesto(t *testing.T, manifiesto, raíz string) {
	t.Helper()

	if _, err := os.Stat(raíz); os.IsNotExist(err) {
		t.Skipf("%s no está en esta máquina, no hay nada que verificar", raíz)
	}
	quiero := leeSumas(t, manifiesto)
	tengo := sumaDirectorio(t, raíz)

	for ruta, esperado := range quiero {
		actual, ok := tengo[ruta]
		switch {
		case !ok:
			t.Errorf("falta %s, que el manifiesto sí lista", ruta)
		case actual != esperado:
			t.Errorf("%s cambió\n  manifiesto: %s\n  en disco:   %s", ruta, esperado, actual)
		}
	}
	for ruta := range tengo {
		if _, ok := quiero[ruta]; !ok {
			t.Errorf("apareció %s, que no está en el manifiesto", ruta)
		}
	}
	if len(quiero) == 0 {
		t.Fatalf("%s no tiene ninguna entrada, así que este test no probaría nada", manifiesto)
	}
}

func leeSumas(t *testing.T, nombre string) map[string]string {
	t.Helper()
	f, err := os.Open(nombre)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		línea := strings.TrimSpace(sc.Text())
		if línea == "" || strings.HasPrefix(línea, "#") {
			continue
		}
		suma, ruta, ok := strings.Cut(línea, "  ")
		if !ok {
			t.Fatalf("%s: línea sin separar: %q", nombre, línea)
		}
		out[ruta] = suma
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func sumaDirectorio(t *testing.T, raíz string) map[string]string {
	t.Helper()
	out := map[string]string{}

	err := filepath.WalkDir(raíz, func(ruta string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := os.ReadFile(ruta)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(raíz, ruta)
		if err != nil {
			return err
		}
		suma := sha256.Sum256(raw)
		out[filepath.ToSlash(rel)] = hex.EncodeToString(suma[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestElPinDelMotorEstáTambiénEnLosDocs.
//
// El .gitignore promete que "el pin y el hash viven en docs/02, decisión 1", y
// hasta la auditoría esa frase era falsa: no había ningún hash en los siete
// documentos. Esto la vuelve cierta y la mantiene cierta.
func TestElPinDelMotorEstáTambiénEnLosDocs(t *testing.T) {
	docs, err := os.ReadFile("../../docs/02-decisiones-de-diseno.md")
	if err != nil {
		t.Fatal(err)
	}
	texto := string(docs)

	sumas := leeSumas(t, "easytier.sums")
	for ruta, suma := range sumas {
		if !strings.Contains(texto, suma) {
			t.Errorf("la suma de %s no está en docs/02: %s", ruta, suma)
		}
	}
}

// TestElInstaladorActualizaUnServicioQueYaExiste.
//
// `sc create` no actualiza nada: si el servicio quedó de una prueba anterior,
// devuelve que ya existe y conserva su binPath y su start type. Fue exactamente
// como un instalador correcto terminó lanzando `C:\kt\carga\kanpachid.exe` en
// vez del binario recién copiado a Program Files.
func TestElInstaladorActualizaUnServicioQueYaExiste(t *testing.T) {
	raw, err := os.ReadFile("../../installer/kanpachi.iss")
	if err != nil {
		t.Fatal(err)
	}
	iss := string(raw)

	for _, obligatorio := range []string{
		"function PrepareToInstall",
		"config {#ServiceName} binPath=",
		"start= delayed-auto",
		"obj= LocalSystem",
	} {
		if !strings.Contains(iss, obligatorio) {
			t.Errorf("el instalador no contiene %q", obligatorio)
		}
	}
}

// TestElDesinstaladorBorraLasPreferenciasDeLaUIInstalada.
//
// Flutter guarda fuera de Program Files: shared_preferences_windows usa el
// Application Support de Windows, que path_provider resuelve a Roaming
// AppData\CompanyName\ProductName. Sin una entrada explícita, reinstalar
// conserva onboarding, nickname y ajustes aunque todos los binarios se hayan
// eliminado correctamente.
func TestElDesinstaladorBorraLasPreferenciasDeLaUIInstalada(t *testing.T) {
	raw, err := os.ReadFile("../../installer/kanpachi.iss")
	if err != nil {
		t.Fatal(err)
	}
	iss := string(raw)

	for _, obligatorio := range []string{
		"procedure BorrarPreferenciasDeLaUI",
		"ProfileListKey",
		"shared_preferences.json",
		"DeleteFile(Preferencias)",
		"CurUninstallStep = usPostUninstall",
	} {
		if !strings.Contains(iss, obligatorio) {
			t.Errorf("la limpieza de preferencias no contiene %q", obligatorio)
		}
	}
	if strings.Contains(iss, "{userappdata}") {
		t.Error("un instalador por máquina no debe confundir el perfil que autorizó UAC con quien ejecutó la UI")
	}
	if strings.Contains(iss, "DelTree(DirectorioPublisher") {
		t.Error("el desinstalador borra todo Accentio Studios en vez del fichero exacto de Kanpachi")
	}
}

// TestLosTresCanalesCoincidenEnGoYDart.
//
// Son dos ejecutables y dos lenguajes: un typo compila perfecto y se ve como
// "no hay servicio" aunque ambos procesos estén sanos. Los tres nombres son
// distintos para que instalado, portable y consola puedan coexistir.
//
// Lee el fichero de WINDOWS del paquete y no `pipe.go`, que es donde estaban
// antes de que el canal tuviera dos sistemas. Es lo correcto y no un ajuste al
// refactor: la interfaz de Flutter es solo de Windows, así que este par lo forman
// el Dart y el lado de Windows del canal. En Linux quien habla es el CLI, que
// está escrito en Go y usa la constante directamente.
func TestLosTresCanalesCoincidenEnGoYDart(t *testing.T) {
	goSource, err := os.ReadFile("../../daemon/transport/pipe/pipe_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	dartSource, err := os.ReadFile("../../ui/lib/features/session/infra/daemon/pipe/pipe_names.dart")
	if err != nil {
		t.Fatal(err)
	}

	goText, dartText := string(goSource), string(dartSource)
	for _, nombre := range []string{
		`\\.\pipe\ProtectedPrefix\Administrators\kanpachi-installed`,
		`\\.\pipe\ProtectedPrefix\Administrators\kanpachi-portable`,
		`\\.\pipe\ProtectedPrefix\Administrators\kanpachi-console`,
	} {
		if !strings.Contains(goText, nombre) {
			t.Errorf("Go no declara el canal %q", nombre)
		}
		if !strings.Contains(dartText, nombre) {
			t.Errorf("Dart no declara el canal %q", nombre)
		}
	}
}
