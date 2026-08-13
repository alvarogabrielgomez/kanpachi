package main

// `kanpachi password`: the password of the registry this machine hosts on.
//
// # Why there is no --password flag, and never will be
//
// On Linux any user reads /proc/<pid>/cmdline, and the shell keeps a history. A
// flag would put the password of somebody else's seed in two places that outlive
// the command. It is typed, masked, or it does not get sent.
//
// Reading it from stdin is allowed, and that is the door for a script: a file
// with mode 0600 piped in never appears in an argument list.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/AlecAivazis/survey/v2"
	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/daemon/transport/protocol"
)

func cmdPassword(ctx context.Context, op opciones, args []string) error {
	if len(args) > 0 {
		return uso("password takes no arguments. It is typed, never passed: " +
			"the argv of a process is world readable")
	}

	pw, err := leerPassword(op)
	if err != nil {
		return err
	}

	c, err := abrir(op)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	// La respuesta está vacía a propósito, así que no hay nada que imprimir
	// salvo que salió bien. Ver [protocol.MethodSeedPassword].
	_, hecho, err := pedir[struct{}](c, op, protocol.MethodSeedPassword, struct {
		Password string `json:"password"`
	}{pw})
	if hecho || err != nil {
		return err
	}
	fmt.Println("The registry accepted it. This machine can host on it now.")
	return nil
}

// leerPassword lo toma de la terminal, o de la entrada si la redirigieron.
//
// # Sin terminal se lee de stdin y no se rechaza
//
// Es lo contrario de lo que hace la confirmación de confianza, y por un motivo
// concreto: allá la ausencia de terminal significa que NADIE confirmó, y dar por
// dado un sí sería quitar la decisión. Acá la entrada redirigida trae el dato en
// sí, así que no hay nada que dar por supuesto. Un fichero 0600 por la tubería
// es justamente la forma correcta de automatizar esto.
func leerPassword(op opciones) (string, error) {
	if !hayTerminal() {
		raw, err := io.ReadAll(io.LimitReader(os.Stdin, 4096))
		if err != nil {
			return "", fmt.Errorf("could not read the password from standard input: %w", err)
		}
		pw := strings.TrimRight(string(raw), "\r\n")
		if pw == "" {
			return "", errors.New("there is no terminal and standard input was empty.\n" +
				"  To automate it:  sudo kanpachi password < /path/to/secret")
		}
		return pw, domain.ValidateSeedPassword(pw)
	}
	if op.json {
		// Con `--json` la salida es de una máquina, y una máquina no teclea. Se
		// dice acá en vez de dejar que un prompt se pinte encima del JSON.
		return "", errors.New("with --json the password comes from standard input")
	}

	var pw string
	pregunta := &survey.Password{
		Message: fmt.Sprintf("Password of the registry (%d-%d characters):",
			domain.MinSeedPasswordLen, domain.MaxSeedPasswordLen),
	}
	if err := survey.AskOne(pregunta, &pw); err != nil {
		return "", err
	}
	// La regla se comprueba ACÁ además de en el daemon, y no es redundante: sin
	// esto, un password demasiado corto viaja por el pipe antes de que nadie lo
	// mire, y volver a pedirlo cuesta un viaje y un mensaje que llega en el
	// idioma del daemon.
	return pw, domain.ValidateSeedPassword(pw)
}
