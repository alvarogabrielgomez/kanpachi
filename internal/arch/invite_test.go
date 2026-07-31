package arch

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/accentiostudios/kanpachi/core/domain"
)

const invitePage = "../../invite/index.html"

var (
	comentarioHTML   = regexp.MustCompile(`(?s)<!--.*?-->`)
	comentarioBloque = regexp.MustCompile(`(?s)/\*.*?\*/`)
)

// sinComentarios quita los comentarios HTML, los de bloque, y las líneas que
// empiezan con //. Las líneas se filtran por su inicio y no por contener //,
// para no destrozar una URL con esquema dentro de una cadena.
func sinComentarios(s string) string {
	s = comentarioHTML.ReplaceAllString(s, "")
	s = comentarioBloque.ReplaceAllString(s, "")
	var b strings.Builder
	for _, linea := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(linea), "//") {
			continue
		}
		b.WriteString(linea)
		b.WriteByte('\n')
	}
	return b.String()
}

// TestPaginaDeInvitacionCoincideConElDominio evita una desincronización
// silenciosa entre dos implementaciones del mismo formato.
//
// La página valida el código en JavaScript porque el fragmento nunca llega al
// servidor y no hay backend que pueda validarlo. Eso significa que el alfabeto
// y la longitud viven duplicados, en Go y en JS. Si se separan, la página
// aceptaría códigos que la app rechaza, y el usuario concluiría que la app
// está rota. Este test los ata.
func TestPaginaDeInvitacionCoincideConElDominio(t *testing.T) {
	crudo, err := os.ReadFile(invitePage)
	if err != nil {
		t.Fatalf("no se pudo leer %s: %v", invitePage, err)
	}
	html := string(crudo)

	t.Run("alfabeto", func(t *testing.T) {
		re := regexp.MustCompile(`var ALPHABET = "([^"]*)"`)
		m := re.FindStringSubmatch(html)
		if m == nil {
			t.Fatal("no se encontró ALPHABET en la página: ¿se renombró la constante?")
		}
		if m[1] != domain.Alphabet {
			t.Errorf("el alfabeto de la página es %q\n  y el del dominio es %q\n"+
				"  con alfabetos distintos, la página acepta códigos que la app rechaza", m[1], domain.Alphabet)
		}
	})

	t.Run("longitud", func(t *testing.T) {
		re := regexp.MustCompile(`var CODE_LEN = (\d+)`)
		m := re.FindStringSubmatch(html)
		if m == nil {
			t.Fatal("no se encontró CODE_LEN en la página")
		}
		if m[1] != "12" || domain.CodeLen != 12 {
			t.Errorf("CODE_LEN de la página es %s y el del dominio es %d", m[1], domain.CodeLen)
		}
	})

	t.Run("seed por defecto", func(t *testing.T) {
		re := regexp.MustCompile(`var DEFAULT_SEED = "([^"]*)"`)
		m := re.FindStringSubmatch(html)
		if m == nil {
			t.Fatal("no se encontró DEFAULT_SEED en la página")
		}
		if m[1] != domain.DefaultSeedHost {
			t.Errorf("el seed por defecto de la página es %q y el del dominio es %q", m[1], domain.DefaultSeedHost)
		}
	})
}

// TestPaginaDeInvitacionRespetaSusInvariantes comprueba en el propio archivo
// las tres reglas de la decisión 17. Son reglas que se rompen sin querer al
// editar, y ninguna deja rastro visible cuando se rompe: la página sigue
// pareciendo correcta.
func TestPaginaDeInvitacionRespetaSusInvariantes(t *testing.T) {
	crudo, err := os.ReadFile(invitePage)
	if err != nil {
		t.Fatalf("no se pudo leer %s: %v", invitePage, err)
	}
	// Se revisa el código, no los comentarios. Sin esto, un comentario que
	// explica "acá jamás se usa innerHTML" hace fallar el test que comprueba
	// que no se usa innerHTML, y la única salida sería dejar de documentar el
	// motivo. Los comentarios de esa página son parte de su valor.
	html := sinComentarios(string(crudo))

	t.Run("sin peticiones ni recursos externos", func(t *testing.T) {
		// Cero telemetría, y además la deja servible desde cualquier sitio sin
		// depender de nadie, que es lo que necesita un self-hoster.
		prohibidos := map[string]string{
			"<script src":  "script externo",
			"<link":        "hoja de estilos o fuente externa",
			"@import":      "CSS importado de fuera",
			"fetch(":       "petición saliente",
			"XMLHttpRequ":  "petición saliente",
			"navigator.se": "sendBeacon, o sea telemetría",
			"googleapis":   "recurso de Google",
			"gtag":         "Google Analytics",
			"analytics":    "analítica",
			"document.coo": "cookies",
		}
		for patron, queEs := range prohibidos {
			if strings.Contains(html, patron) {
				t.Errorf("la página contiene %q (%s): debe ser autocontenida, sin peticiones salientes y sin cookies", patron, queEs)
			}
		}

		// El enlace al repositorio es la única URL absoluta admitida, y es un
		// href sobre el que el usuario decide, no una carga automática.
		for _, u := range regexp.MustCompile(`https?://[^"'\s)]+`).FindAllString(html, -1) {
			if !strings.HasPrefix(u, "https://github.com/accentiostudios/") {
				t.Errorf("URL absoluta inesperada en la página: %q", u)
			}
		}
	})

	t.Run("el intent no se dispara al cargar", func(t *testing.T) {
		// Solo puede haber una asignación de location.href, y tiene que estar
		// dentro del manejador del click.
		if strings.Count(html, "window.location.href") != 1 {
			t.Error("hay más de una navegación en la página: el intent debe dispararse solo con el click")
		}
		idx := strings.Index(html, `getElementById("open").addEventListener`)
		nav := strings.Index(html, "window.location.href =")
		if idx == -1 || nav == -1 || nav < idx {
			t.Error("la navegación a kanpachi:// no está dentro del manejador del click")
		}
	})

	t.Run("el codigo nunca se inyecta como HTML", func(t *testing.T) {
		// El fragmento lo controla quien manda el enlace.
		if strings.Contains(html, "innerHTML") || strings.Contains(html, "document.write") {
			t.Error("la página usa innerHTML o document.write: el fragmento es entrada hostil, solo textContent")
		}
	})

	t.Run("no intenta detectar si la app esta instalada", func(t *testing.T) {
		// El truco del temporizador falla distinto en cada navegador, y
		// adivinar mal es peor que preguntar.
		if strings.Contains(html, "blur") || strings.Contains(html, "visibilitychange") {
			t.Error("la página parece intentar detectar la instalación: no hay forma confiable, ver decisión 17")
		}
	})
}
