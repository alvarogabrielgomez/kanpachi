package arch

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/internal/selfupdate"
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

func leerPagina(t *testing.T) string {
	t.Helper()
	crudo, err := os.ReadFile(invitePage)
	if err != nil {
		t.Fatalf("no se pudo leer %s: %v", invitePage, err)
	}
	return string(crudo)
}

// TestPaginaDeInvitacionCoincideConElDominio evita una desincronización
// silenciosa entre dos implementaciones del mismo formato.
//
// La página valida el invite ID en JavaScript porque tiene que decidir qué
// pantalla mostrar antes de hablar con nadie. Eso significa que el alfabeto y
// la longitud viven duplicados, en Go y en JS. Si se separan, la página
// aceptaría IDs que la app rechaza, y el usuario concluiría que la app está
// rota. Este test los ata.
func TestPaginaDeInvitacionCoincideConElDominio(t *testing.T) {
	html := leerPagina(t)

	t.Run("alfabeto", func(t *testing.T) {
		re := regexp.MustCompile(`var ALPHABET = "([^"]*)"`)
		m := re.FindStringSubmatch(html)
		if m == nil {
			t.Fatal("no se encontró ALPHABET en la página: ¿se renombró la constante?")
		}
		if m[1] != domain.Alphabet {
			t.Errorf("el alfabeto de la página es %q\n  y el del dominio es %q\n"+
				"  con alfabetos distintos, la página acepta IDs que la app rechaza", m[1], domain.Alphabet)
		}
	})

	t.Run("longitud", func(t *testing.T) {
		re := regexp.MustCompile(`var CODE_LEN = (\d+)`)
		m := re.FindStringSubmatch(html)
		if m == nil {
			t.Fatal("no se encontró CODE_LEN en la página")
		}
		if m[1] != "8" || domain.InviteIDLen != 8 {
			t.Errorf("CODE_LEN de la página es %s y el del dominio es %d", m[1], domain.InviteIDLen)
		}
	})

	// La página NO puede llevar un seed compilado, y este caso cambió de sentido
	// cuando el proyecto se abrió.
	//
	// Antes comprobaba que el `DEFAULT_SEED` de la página coincidiera con el del
	// dominio, o sea que los dos dijeran `kanpachi.accentio.dev`. Ahora no hay
	// seed por defecto en ningún lado, y lo que hay que vigilar es lo contrario:
	// que nadie vuelva a escribir un host ahí.
	//
	// **Es lo que hace que el registro de cualquiera funcione sin editar nada.**
	// La página saca su seed de `window.location.hostname`, así que servida desde
	// `seed.midominio.com` reparte códigos que apuntan a `seed.midominio.com`. Un
	// literal compilado la haría repartir códigos del servidor de otro, con IDs
	// que ahí no existen.
	t.Run("la pagina no lleva ningun seed compilado", func(t *testing.T) {
		if strings.Contains(html, "var DEFAULT_SEED") {
			t.Error("la página volvió a declarar DEFAULT_SEED: su seed sale del origen que la sirve")
		}
		if !strings.Contains(html, "window.location.hostname") {
			t.Error("la página no lee su propio host, así que no puede saber qué seed reparte")
		}
		// El dominio de Accentio no puede aparecer en el CÓDIGO de la página. Se
		// mira solo dentro del `<script>`: en la prosa de arriba y en los
		// comentarios se nombra a propósito, para contar qué se quitó.
		i := strings.Index(html, "<script>")
		if i < 0 {
			t.Fatal("no se encontró el bloque de script de la página")
		}
		for _, línea := range strings.Split(html[i:], "\n") {
			limpia := strings.TrimSpace(línea)
			if strings.HasPrefix(limpia, "//") || strings.HasPrefix(limpia, "*") {
				continue
			}
			if strings.Contains(limpia, "kanpachi.accentio.dev") {
				t.Errorf("la página lleva un seed escrito a mano: %q", limpia)
			}
		}
	})

	t.Run("la URL que genera la app abre esta pagina", func(t *testing.T) {
		// La app genera `seed/A7K2-M9QX` y la página lee el invite ID de la
		// ruta. Si una de las dos cambia de forma, el enlace deja de abrir nada
		// y el síntoma aparece en producción, no acá. Este caso ata las dos.
		r, err := domain.ParseRoom("A7K2M9QX@seed.midominio.com")
		if err != nil {
			t.Fatalf("setup: %v", err)
		}
		url := r.InviteURL()
		if strings.Contains(url, "#") {
			t.Errorf("InviteURL() = %q: el invite ID va en la ruta, el fragmento es para la clave de tarjeta", url)
		}
		if !strings.Contains(html, "window.location.pathname") {
			t.Error("la página no lee la ruta: el invite ID viaja ahí desde la decisión 17")
		}
	})
}

// TestPaginaDeInvitacionRespetaSusInvariantes comprueba en el propio archivo
// las reglas de las decisiones 17 y 25. Son reglas que se rompen sin querer al
// editar, y ninguna deja rastro visible cuando se rompe: la página sigue
// pareciendo correcta.
func TestPaginaDeInvitacionRespetaSusInvariantes(t *testing.T) {
	crudo := leerPagina(t)
	// Se revisa el código, no los comentarios. Sin esto, un comentario que
	// explica "acá jamás se usa innerHTML" hace fallar el test que comprueba
	// que no se usa innerHTML, y la única salida sería dejar de documentar el
	// motivo. Los comentarios de esa página son parte de su valor.
	html := sinComentarios(crudo)

	t.Run("sin recursos externos ni telemetria", func(t *testing.T) {
		// Cero telemetría, y además la deja servible desde cualquier sitio sin
		// depender de nadie, que es lo que necesita un self-hoster.
		prohibidos := map[string]string{
			"<script src":               "script externo",
			"<link":                     "hoja de estilos o fuente externa",
			"@import":                   "CSS importado de fuera",
			"XMLHttpRequ":               "petición fuera del único fetch permitido",
			"navigator.s" + "endBeacon": "telemetría",
			"googleapis":                "recurso de Google",
			"gtag":                      "Google Analytics",
			"analytics":                 "analítica",
			"document.coo" + "kie":      "cookies",
		}
		for patron, queEs := range prohibidos {
			if strings.Contains(html, patron) {
				t.Errorf("la página contiene %q (%s): debe ser autocontenida, sin telemetría y sin cookies", patron, queEs)
			}
		}

		// GitHub es el único origen absoluto admitido, y siempre como un href
		// sobre el que el usuario decide, jamás una carga automática. Los logos
		// van EN LÍNEA justamente para no necesitar ninguna otra.
		for _, u := range regexp.MustCompile(`https?://[^"'\s)]+`).FindAllString(html, -1) {
			if !strings.HasPrefix(u, "https://github.com/") {
				t.Errorf("URL absoluta inesperada en la página: %q", u)
			}
		}

		// Y el REPOSITORIO no puede estar escrito acá.
		//
		// Este guardián decía lo contrario: exigía que las URL empezaran por
		// `https://github.com/alvarogabrielgomez/kanpachi`, o sea que el nombre
		// del repositorio estuviera dentro del HTML. Eso era una segunda copia
		// de `internal/selfupdate.Repo`, y con ella un fork que cambiara la
		// constante de Go seguía repartiendo una página que apunta al
		// repositorio original. Ahora el nombre lo sirve el servidor en el hueco
		// de estado, y lo que este guardián protege es que no vuelva a copiarse.
		if strings.Contains(html, selfupdate.Repo) {
			t.Errorf("la página lleva %q escrito: el repositorio viaja en el hueco de estado, "+
				"para que un fork cambie una sola constante", selfupdate.Repo)
		}
	})

	t.Run("la unica peticion es al mismo origen", func(t *testing.T) {
		// La decisión 24 autoriza exactamente una: el registro de salas. Tiene
		// que ser relativa, o sea al mismo host que sirvió la página. Una
		// absoluta filtraría a un tercero qué sala está mirando el visitante,
		// que es justo lo que la decisión acotó.
		llamadas := regexp.MustCompile(`fetch\(([^,)]*)`).FindAllStringSubmatch(html, -1)
		if len(llamadas) == 0 {
			t.Fatalf("la página no hace ninguna llamada a fetch, se esperaba al menos el registro")
		}
		for _, llamada := range llamadas {
			destino := strings.TrimSpace(llamada[1])
			if !strings.HasPrefix(destino, `"/`) {
				t.Errorf("fetch a %s: tiene que ser una ruta relativa al mismo origen", destino)
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

	t.Run("el intent lleva el ID pelado, sin guion", func(t *testing.T) {
		// El guion es presentación: hace que ocho caracteres se puedan leer en
		// voz alta y comparar de un vistazo, y no forma parte del
		// identificador. Mandarlo por el intent obliga a normalizar a todo lo
		// que lo reciba, y basta con que una pieza no lo haga para que la sala
		// "no exista" sin nada que lo explique. Lo que se ve y se copia lleva
		// guion; lo que viaja, no.
		if strings.Contains(html, `"kanpachi://" + pretty`) {
			t.Error("el intent manda el código con guion: quien lo reciba tiene que limpiarlo para que funcione")
		}
		if !strings.Contains(html, `"kanpachi://" + id`) {
			t.Error("el intent no se construye con el ID pelado")
		}
	})

	t.Run("nada de la URL se inyecta como HTML", func(t *testing.T) {
		// La ruta, el fragmento y la tarjeta los controla quien manda el link.
		if strings.Contains(html, "inner"+"HTML") || strings.Contains(html, "document.wr"+"ite") {
			t.Error("la página construye HTML con entrada de fuera: solo textContent")
		}
		if strings.Contains(html, "insertAdjacentHTML") || strings.Contains(html, "outer"+"HTML") {
			t.Error("la página construye HTML con entrada de fuera: solo textContent")
		}
	})

	t.Run("no intenta detectar si la app esta instalada", func(t *testing.T) {
		// El truco del temporizador falla distinto en cada navegador, y
		// adivinar mal es peor que preguntar.
		if strings.Contains(html, "blur") || strings.Contains(html, "visibilitychange") {
			t.Error("la página parece intentar detectar la instalación: no hay forma confiable, ver decisión 17")
		}
	})

	t.Run("no promete que el codigo no llega al servidor", func(t *testing.T) {
		// Fue cierto hasta la decisión 17 y dejó de serlo: el invite ID va en la
		// ruta y el servidor lo recibe. La frase sobrevivió en el diseño de la
		// portada y hubo que quitarla. Este caso existe para que no vuelva,
		// porque es de las que se copian de un mockup viejo sin releer.
		falsas := []string{
			"nunca sale de tu navegador",
			"jamás lo recibe",
			"jamas lo recibe",
			"viaja en el fragmento del enlace",
		}
		for _, f := range falsas {
			if strings.Contains(crudo, f) {
				t.Errorf("la página dice %q: el invite ID viaja en la RUTA desde la decisión 17, "+
					"así que el servidor sí lo recibe. Esa frase es falsa", f)
			}
		}
	})

	t.Run("no afirma identidad", func(t *testing.T) {
		// Decisión 25: el nickname no se verifica, así que la página no puede
		// decir quién te invitó. Este test cuida una frase, y esa frase es la
		// diferencia entre informar y mentir.
		if strings.Contains(html, "te invitó a su sala") {
			t.Error(`la página dice "te invitó a su sala": el nickname no se verifica, ` +
				`debe decir "se identifica como". Ver decisión 25`)
		}
		if !strings.Contains(html, "Se identifica como") {
			t.Error(`falta la fórmula "Se identifica como": es lo que evita afirmar una identidad que nadie verificó`)
		}
	})

	t.Run("la entrada de fuera tiene tope antes de mirarse", func(t *testing.T) {
		// Mismo principio que domain.MaxInputLen: el tope va antes de tocar el
		// contenido, para que una entrada de megabytes no llegue a los bucles.
		for _, c := range []string{"MAX_INPUT", "MAX_KEY", "MAX_NAME"} {
			if !strings.Contains(html, "var "+c+" =") {
				t.Errorf("falta el tope %s: ruta, fragmento y tarjeta son entrada hostil", c)
			}
		}
	})

	t.Run("el campo del código lleva máscara", func(t *testing.T) {
		// El guion tiene que aparecer solo mientras se escribe. Sin eso, el
		// placeholder "A7K2-M9QX" promete una forma que el campo no aplica, y
		// quien escribe los ocho seguidos no sabe si le falta algo.
		if !strings.Contains(html, "enmascarar(campo)") {
			t.Error("el campo no aplica la máscara: el guion del placeholder sería una promesa vacía")
		}

		// Y la máscara tiene que descartar el enlace antes de filtrar, no
		// después. Pegar "kanpachi.accentio.dev/A7K2-M9QX" y quedarse con los
		// caracteres del alfabeto produce KANPACHIA: ocho de forma impecable y
		// un código que no existe. Un código falso que parece válido manda a
		// buscar el fallo al otro lado.
		if !strings.Contains(html, "codigoDeLoPegado") {
			t.Error("la máscara no aísla el último tramo del enlace: pegar la URL produciría un código inventado con forma válida")
		}

		// El tope del campo tiene que dejar entrar un enlace entero, o el
		// navegador lo recorta antes de que la máscara pueda verlo.
		if !strings.Contains(html, `maxlength="64"`) {
			t.Error("el maxlength del campo no admite un enlace completo, así que pegarlo se recortaría a medias")
		}
	})

	t.Run("sin bytes de control en el archivo", func(t *testing.T) {
		// Un carácter de control invisible dentro de un literal de JavaScript
		// no se ve al revisar el diff y puede cambiar lo que hace el código.
		for i, r := range crudo {
			if r < 32 && r != '\n' && r != '\r' && r != '\t' {
				t.Fatalf("byte de control %#x en la posición %d del archivo", r, i)
			}
		}
	})
}
