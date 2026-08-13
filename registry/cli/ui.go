// Package cli es la interfaz de línea de comandos del seed.
//
// Un solo binario con subcomandos: `serve` es lo que arranca systemd, y
// `init`, `doctor`, `config` y `nginx` son lo que ejecuta una persona. Van
// juntos a propósito, porque separarlos permite que la herramienta que
// configura y el servicio que corre queden en versiones distintas, y ese fallo
// se manifiesta como "configuré el puerto y no cambió nada".
package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

var (
	cTitulo = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63"))
	cOK     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42"))
	cAviso  = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	cError  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196"))
	cTenue  = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	cCodigo = lipgloss.NewStyle().Foreground(lipgloss.Color("81"))
	cCaja   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("63")).Padding(0, 1)
)

// interactivo informa si se puede guiar al usuario con preguntas. Con la
// entrada redirigida no hay a quién preguntar, y el comando tiene que
// comportarse con los valores por defecto en vez de quedarse esperando.
func interactivo() bool { return term.IsTerminal(int(os.Stdin.Fd())) }

func banner(titulo, subtitulo string) {
	fmt.Println()
	fmt.Println(cCaja.Render(cTitulo.Render(titulo) + "\n" + cTenue.Render(subtitulo)))
	fmt.Println()
}

func ok(formato string, a ...any)    { fmt.Println(cOK.Render("✓ ") + fmt.Sprintf(formato, a...)) }
func aviso(formato string, a ...any) { fmt.Println(cAviso.Render("! ") + fmt.Sprintf(formato, a...)) }
func fallo(formato string, a ...any) { fmt.Println(cError.Render("✗ ") + fmt.Sprintf(formato, a...)) }
func tenue(formato string, a ...any) { fmt.Println(cTenue.Render(fmt.Sprintf(formato, a...))) }
func seccion(s string)               { fmt.Println(); fmt.Println(cTitulo.Render(s)) }
func codigo(lineas ...string) {
	for _, l := range lineas {
		fmt.Println("  " + cCodigo.Render(l))
	}
}

// preguntar pide un valor con un valor por defecto visible. Sin terminal
// devuelve el defecto sin bloquearse.
func preguntar(pregunta, porDefecto string) string {
	if !interactivo() {
		return porDefecto
	}
	if porDefecto != "" {
		fmt.Printf("%s %s ", pregunta, cTenue.Render("["+porDefecto+"]"))
	} else {
		fmt.Printf("%s ", pregunta)
	}
	linea, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return porDefecto
	}
	linea = strings.TrimSpace(linea)
	if linea == "" {
		return porDefecto
	}
	return linea
}

// confirmar pide un sí o un no. Sin terminal devuelve el defecto, para que el
// instalador funcione igual desde un script.
func confirmar(pregunta string, porDefecto bool) bool {
	opciones := "y/N"
	if porDefecto {
		opciones = "Y/n"
	}
	r := strings.ToLower(preguntar(pregunta+" "+cTenue.Render("("+opciones+")"), ""))
	switch r {
	case "":
		return porDefecto
	case "s", "si", "sí", "y", "yes":
		return true
	default:
		return false
	}
}
