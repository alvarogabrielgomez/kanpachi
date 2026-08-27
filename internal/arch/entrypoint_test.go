package arch

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// El guardián del arranque en contenedor.
//
// # Por qué un test de Go vigila un fichero de shell
//
// Porque ese fichero es PID 1 de la imagen y no lo prueba nadie más. Corre bajo
// `dash`, no bajo bash, y `dash` calla ante cosas que bash perdona: un fallo de
// sintaxis o un nombre de variable inválido no se ven al escribirlos, se ven en
// el clúster de otra persona, en forma de contenedor que se reinicia. Acá cuesta
// milisegundos y se ejecuta en la misma corrida que el resto de guardianes.
//
// # Lo medido, el 2026-08-26
//
// El sidecar de un servidor de Project Zomboid se reinició siete veces seguidas
// sin llegar a levantar la sala. La línea que le llegó a quien lo miraba fue:
//
//	entrypoint: 175: qué=the saved room could not be reopened: not found
//
// Eso no es el daemon hablando. Es `dash` diciendo que `qué="$2"` no es una
// asignación —su nombre tiene un carácter fuera de [A-Za-z_][A-Za-z0-9_]*— así
// que lo tomó por un COMANDO, no lo encontró, y salió con 127. Con `set -e`, eso
// mata el contenedor.
//
// El agravante es lo que esa línea escondía: `explain` es la función que traduce
// el código de error del daemon a qué tocar en el compose, y **cada camino de
// error del script terminaba ahí**. O sea que ninguna de sus explicaciones se
// había impreso nunca, y el fallo real —una sala que el daemon ya había
// reabierto, con la puerta del CLI negándose por falta de `--yes`— quedó tapado
// bajo un mensaje que no nombra ni la sala ni el motivo.
func TestElArranqueEnContenedorSoloUsaNombresQueDashEntiende(t *testing.T) {
	guion := leerEntrypoint(t)

	// Una asignación al principio de línea, con o sin sangría. No busca
	// asignaciones en medio de una tubería porque el script no tiene ninguna, y
	// una expresión más amplia empezaría a casar texto de los comentarios.
	asignación := regexp.MustCompile(`^\s*([^\s=]+)=`)

	for n, línea := range strings.Split(guion, "\n") {
		recortada := strings.TrimSpace(línea)
		if recortada == "" || strings.HasPrefix(recortada, "#") {
			continue
		}
		m := asignación.FindStringSubmatch(línea)
		if m == nil {
			continue
		}
		nombre := m[1]
		if !nombreDeVariableValido(nombre) {
			t.Errorf("docker/entrypoint.sh:%d asigna a %q, que dash no lee como una asignación.\n"+
				"  Su nombre tiene que caber en [A-Za-z_][A-Za-z0-9_]*, y con cualquier otra cosa\n"+
				"  dash ejecuta la línea como un comando, no lo encuentra, y sale con 127.",
				n+1, nombre)
		}
	}
}

// TestElArranqueEnContenedorContestaLaPuertaDelDesplazamiento.
//
// Reabrir una sala pasa por la misma puerta que `host` y que `join`: la que
// pregunta antes de que entrar cueste otra cosa. **Sin terminal esa puerta se
// NIEGA**, a propósito, porque leer la ausencia de una respuesta como un sí es
// como una máquina abandona la sala en la que estaba sin que nadie se lo pida.
//
// Un contenedor no tiene terminal, así que todo comando que la cruce tiene que
// llevar `--yes` escrito. Lo que había en el medio el 2026-08-26 era esto: el
// daemon reabría la sala guardada solo, al arrancar; el script pedía `resume`
// sin `--yes`; la puerta se negaba porque lo que estorbaba era la sala que el
// propio daemon acababa de levantar; y el contenedor moría sobre su propio
// éxito. Desplazar no puede: el caso de uso contesta `busy` antes de mirar
// siquiera el `replace`.
func TestElArranqueEnContenedorContestaLaPuertaDelDesplazamiento(t *testing.T) {
	// Por línea LÓGICA: una orden partida al final de línea es una sola, y su
	// `--yes` suele viajar en la continuación.
	for _, línea := range líneasLógicas(leerEntrypoint(t)) {
		recortada := strings.TrimSpace(línea)
		if strings.HasPrefix(recortada, "#") {
			continue
		}
		if !strings.Contains(recortada, "kanpachi ") && !strings.Contains(recortada, "kanpachi --") {
			continue
		}
		for _, orden := range []string{" resume", " host ", " join "} {
			if strings.Contains(recortada, orden) && !strings.Contains(recortada, "--yes") {
				t.Errorf("docker/entrypoint.sh llama a%s sin --yes:\n  %s\n"+
					"  Esa orden cruza la puerta del desplazamiento, y sin terminal la puerta\n"+
					"  se niega en vez de suponer. En un contenedor eso es el arranque muerto.",
					orden, recortada)
			}
		}
	}
}

func leerEntrypoint(t *testing.T) string {
	t.Helper()
	// Relativo, como el resto de guardianes de este paquete: el test corre
	// con su directorio en internal/arch.
	crudo, err := os.ReadFile(filepath.Join("..", "..", "docker", "entrypoint.sh"))
	if err != nil {
		t.Fatalf("leyendo el arranque del contenedor: %v", err)
	}
	return string(crudo)
}

func nombreDeVariableValido(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r == '_':
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

// líneasLógicas junta lo que el shell junta: una línea continuada y la
// siguiente son una sola orden.
func líneasLógicas(guion string) []string {
	var out []string
	var actual string
	for _, l := range strings.Split(guion, "\n") {
		l = strings.TrimRight(l, "\r")
		sinCola := strings.TrimRight(l, " \t")
		if strings.HasSuffix(sinCola, `\`) {
			actual += strings.TrimSuffix(sinCola, `\`) + " "
			continue
		}
		out = append(out, actual+l)
		actual = ""
	}
	if actual != "" {
		out = append(out, actual)
	}
	return out
}
