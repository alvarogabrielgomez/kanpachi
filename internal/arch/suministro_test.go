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
