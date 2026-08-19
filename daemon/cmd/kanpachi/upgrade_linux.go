//go:build linux

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

// aptInstall le entrega el paquete a apt.
//
// # Las tres variables de entorno no son decoración
//
// `DEBIAN_FRONTEND=noninteractive` impide que dpkg abra un diálogo de pantalla
// completa preguntando por un fichero de configuración. Ese diálogo, dentro de
// `kanpachi upgrade`, deja la terminal ocupada esperando una tecla que quien
// escribió el comando no espera tener que pulsar, y por `ssh` con la sesión a
// medias se convierte en un paquete a medio configurar.
//
// Las dos de dpkg contestan por adelantado la pregunta que sí importa: qué hacer
// con un fichero de `/etc` que el operador editó. `confold` conserva el suyo, que
// es lo correcto para `/etc/kanpachi/quarantine.nft`: ese fichero lo escribe el
// daemon con la cuarentena de ESTA máquina, y dejar que el paquete lo pise
// significaría cambiar qué puertos están cerrados sin que nadie lo pidiera.
//
// La salida va a la terminal en directo, sin capturar: apt tarda, y un comando
// que se queda mudo medio minuto parece colgado. Además, cuando falla, lo que
// hay que leer es lo que dice apt.
// # Y la cuarta opción apaga un sandbox que acá no protege de nada
//
// Sin ella, cada `kanpachi upgrade` en Linux termina con este párrafo pegado al
// final, medido en el droplet el 2026-08-18 sobre la 0.6.0:
//
//	N: Download is performed unsandboxed as root as file
//	'/var/lib/kanpachi/kanpachi-amd64.deb' couldn't be accessed by user '_apt'
//
// Es cierto y no es un fallo. El paquete se escribe en el directorio de estado,
// que es 0700 de root a propósito —en `/tmp` escribe cualquiera, y esto se
// instala como root un segundo después—, así que el usuario `_apt` con el que
// apt baja cosas no puede leerlo, y apt renuncia a su sandbox y lo cuenta.
//
// Lo que hay que arreglar no es el permiso, es la sorpresa: acá no se BAJA
// nada. El fichero ya está en disco y su SHA-256 ya se comprobó contra el
// manifiesto del release, así que el sandbox de descarga no tiene nada que
// aislar. Decírselo por adelantado es la diferencia entre una decisión escrita
// y un aviso que aparece cada vez y que alguien va a tener que ir a investigar.
func aptInstall(ctx context.Context, ruta string) error {
	entorno := append(os.Environ(),
		"DEBIAN_FRONTEND=noninteractive",
		"DPKG_FORCE=confold",
	)
	return ejecutarVisible(ctx, entorno, "apt-get", "install", "-y",
		"-o", "Dpkg::Options::=--force-confold",
		"-o", "APT::Sandbox::User=root", ruta)
}

// ejecutarVisible corre un comando dejando que hable por la terminal.
//
// Es lo contrario de [ejecutar], que captura la salida para meterla dentro de un
// error, y las dos formas se justifican por cuánto tardan: los arreglos de
// `doctor` son instantáneos y lo que importa de ellos es el mensaje final; una
// instalación con apt tarda, y verla avanzar es la diferencia entre esperar y
// pensar que se colgó.
func ejecutarVisible(ctx context.Context, entorno []string, nombre string, args ...string) error {
	cmd := exec.CommandContext(ctx, nombre, args...)
	cmd.Env = entorno
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s failed: %w", nombre, err)
	}
	return nil
}
