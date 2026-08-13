package main

// La confirmación de confianza en un registro, que es el mismo acto en los dos
// momentos donde aparece.
//
// # Por qué hace falta preguntar, desde que no hay seed por defecto
//
// Porque el registro dejó de ser una constante compilada y pasa a ser de
// cualquiera. Al CREAR sale de lo que esta máquina tenga configurado; al ENTRAR
// lo elige quien escribió el código que te pegaron. En los dos casos es una
// máquina de un tercero con la que este proceso va a hablar, y que ve tu IP
// pública, así que la decisión es de la persona y no del programa.
//
// # Por qué vive en el CLIENTE y no en el daemon
//
// Porque el daemon no tiene a quién preguntarle. Es un servicio, y la
// invariante del producto es que nada de fuera surte efecto sin confirmación
// DENTRO de la app: el sitio donde eso se cumple es cada cara, la ventana y esta
// terminal. Las dos enseñan lo mismo, y ninguna guarda la respuesta.

import (
	"encoding/json"
	"fmt"

	"github.com/AlecAivazis/survey/v2"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/daemon/transport/protocol"
)

// sacarSí saca `--yes` de los argumentos y dice si estaba.
//
// Se saca y no se lee al pasar porque `host` junta lo que queda como NOMBRE de
// la sala: sin esto, `kanpachi host mi partida --yes` abre una sala llamada
// "mi partida --yes".
func sacarSí(args []string) ([]string, bool) {
	resto := make([]string, 0, len(args))
	sí := false
	for _, a := range args {
		switch a {
		case "--yes", "-yes", "-y":
			sí = true
		default:
			resto = append(resto, a)
		}
	}
	return resto, sí
}

// confiarEnElRegistro pregunta antes de hablar con un registro, y se niega
// cuando no hay a quién preguntarle.
//
// # Las tres salidas, y por qué la del medio es de seguridad
//
//  1. Con `--yes`, se sigue. Quien lo escribió ya dijo que sí.
//  2. **Sin terminal y sin `--yes`, se RECHAZA.** Es el caso que se pasa por
//     alto: dentro de un script no hay dónde confirmar, y resolver esa ausencia
//     como un sí haría desaparecer la confirmación justo donde nadie mira. Es la
//     forma que toma en Linux la invariante de que nada de fuera surte efecto
//     sin confirmación dentro.
//  3. Con terminal, se pregunta, y el valor por omisión es NO.
//
// La negativa sale por el flujo de errores, así que con `--json` la salida
// estándar sigue limpia: lo que un script consume es stdout, y esta explicación
// va a stderr con su código de salida.
func confiarEnElRegistro(op opciones, seed, momento string, sinPreguntar bool) error {
	if sinPreguntar {
		// Se dice igual, y no es redundante: quien automatiza también quiere ver
		// en el log a qué máquina habló su script. Con `--json` se calla, porque
		// ahí lo que hay del otro lado es un parser.
		if !op.json {
			fmt.Printf("Using registry %s.\n", seed)
		}
		return nil
	}
	if !hayTerminal() {
		return negativa("this needs you to confirm the registry, and there is no terminal to ask in.\n"+
			"  %s would talk to %s.\n"+
			"  If that is what you want, say so on purpose:  --yes", momento, seed)
	}

	fmt.Println()
	fmt.Printf("  %s would use the registry %s\n", momento, seed)
	fmt.Println()
	fmt.Println("  That machine is the meeting point: it sees your public IP and the")
	fmt.Println("  invite ID, and it is what everyone in the room connects through.")
	fmt.Println("  Only go on if you trust whoever runs it.")
	fmt.Println()

	var sí bool
	if err := preguntar(&survey.Confirm{
		Message: fmt.Sprintf("Trust %s?", seed),
		Default: false,
	}, &sí); err != nil {
		return err
	}
	if !sí {
		return negativa("nothing was done, because you did not trust %s.\n"+
			"  To point this machine somewhere else:  kanpachi seed <host>", seed)
	}
	return nil
}

// registroDeEstaMáquina lee el registro configurado, para poder enseñarlo antes
// de crear.
//
// Vacío no es un fallo del daemon: es una instalación que todavía no eligió
// ninguno, y lo que hace falta ahí es decir cómo se elige.
func registroDeEstaMáquina(op opciones) (string, error) {
	c, err := abrir(op)
	if err != nil {
		return "", err
	}
	defer func() { _ = c.Close() }()

	var v struct {
		Seed      string `json:"seed"`
		Suggested string `json:"suggested"`
	}
	raw, err := c.Call(protocol.MethodOwnSeed, nil)
	if err != nil {
		return "", err
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", fmt.Errorf("reading the configured registry: %w", err)
	}
	if v.Seed != "" {
		return v.Seed, nil
	}
	if v.Suggested != "" {
		return "", negativa("this machine has no registry configured, so there is nowhere to open a room.\n"+
			"  The last room you entered was on %s.\n"+
			"  To use that one:  kanpachi seed %s", v.Suggested, v.Suggested)
	}
	return "", negativa("this machine has no registry configured, so there is nowhere to open a room.\n" +
		"  To set one:  kanpachi seed seed.example.com")
}

// registroDelCódigo saca el registro de lo que pegaron, SOLO para enseñarlo.
//
// El texto sigue viajando tal cual al daemon, que es la frontera de entrada
// hostil y el único sitio donde se decide si un código es un código. Acá se
// mira para poder nombrar la máquina en la pregunta, y un texto que no parsea
// devuelve vacío en vez de un error: quien tiene que explicar qué le falta a ese
// código es el daemon, con su mensaje, y no esta función.
func registroDelCódigo(texto string) string {
	room, err := domain.ParseRoom(texto)
	if err != nil {
		return ""
	}
	return room.Seed
}
