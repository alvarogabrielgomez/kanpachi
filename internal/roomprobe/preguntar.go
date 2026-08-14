package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/AlecAivazis/survey/v2"
	"github.com/AlecAivazis/survey/v2/terminal"
)

// errInterrumpido es que alguien pulsó Ctrl+C dentro de una pregunta.
//
// Es una RESPUESTA, no un error, y por eso tiene centinela propio. Sin esto, la
// interrupción sube por el mismo camino que un fallo de verdad hasta `main`,
// que imprime y sale: la sala se queda abierta, `kanpachi0` arriba y las reglas
// de firewall puestas. Es el fallo que tenía esta herramienta.
var errInterrumpido = errors.New("interrumpido")

// preguntar envuelve a survey para que la interrupción no se escape.
//
// **Ningún sitio de este binario llama a `survey.AskOne` directamente.** Dentro
// de una pregunta, survey limpia `ENABLE_PROCESSED_INPUT` de la consola, así
// que Windows NO genera CTRL_C_EVENT y `signal.NotifyContext` no se entera de
// nada: lo único que ocurre es que `AskOne` devuelve `terminal.InterruptErr`.
func preguntar(p survey.Prompt, destino any, opts ...survey.AskOpt) error {
	err := survey.AskOne(p, destino, opts...)
	if errors.Is(err, terminal.InterruptErr) {
		return errInterrumpido
	}
	return err
}

// elegir es la pregunta de menú, que es la única forma de pregunta que usa esta
// herramienta aparte del texto libre.
func elegir(mensaje string, opciones []string) (string, error) {
	var sel string
	err := preguntar(&survey.Select{Message: mensaje, Options: opciones, PageSize: 15}, &sel)
	return sel, err
}

// texto pide una línea.
func texto(mensaje, ayuda, porDefecto string) (string, error) {
	var v string
	err := preguntar(&survey.Input{Message: mensaje, Help: ayuda, Default: porDefecto}, &v,
		survey.WithValidator(survey.Required))
	return strings.TrimSpace(v), err
}

// secreto pide una línea SIN pintarla, para el password de hospedar.
//
// Sin `survey.Required`: un password vacío es una respuesta legítima —«mejor no,
// cancela»— y forzarlo dejaría a alguien atrapado en la pregunta.
func secreto(mensaje, ayuda string) (string, error) {
	var v string
	err := preguntar(&survey.Password{Message: mensaje, Help: ayuda}, &v)
	return strings.TrimSpace(v), err
}

func pedirApodo(actual string) string {
	msg := "Tu nombre:"
	if actual != "" {
		msg = fmt.Sprintf("Tu nombre (actual: %s):", actual)
	}
	v, err := texto(msg, "Lo ven los demás miembros de la sala.", actual)
	if err != nil {
		return actual
	}
	return v
}
