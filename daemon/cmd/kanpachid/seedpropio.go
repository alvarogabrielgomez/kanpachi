package main

// De dónde sale el registro en el que ESTA máquina abre sus salas.

import (
	"errors"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/port"
	statestore "github.com/accentiostudios/kanpachi/daemon/adapter/state/jsonfile"
)

// seedPropio lee el registro guardado, y NUNCA falla el arranque por él.
//
// # Por qué vacío no es un error
//
// Porque es el estado normal de una instalación que todavía no hospedó ni entró
// a ninguna sala. El registro se aprende de una de esas dos cosas, y hasta que
// pase una, no haberlo es correcto. Quien intente crear una sala se topa con
// `port.ErrNoOwnSeed`, que lleva a configurarlo.
//
// # Por qué un fichero ilegible tampoco corta el arranque
//
// Por lo mismo que un fichero de salas ilegible no impide arrancar el registro
// del seed: un daemon que no arranca es peor que uno sin registro configurado,
// porque con él se van la ventana, `doctor` y el canal por el que se explica qué
// pasa. Se dice fuerte en el log y se sigue.
//
// **Se pasa por el parser del dominio y no por un TrimSpace.** De este valor
// salen una URL a la que este proceso marca y los `--peers` del motor, y este
// proceso corre como SYSTEM o como root. En Windows el fichero vive además en un
// directorio que todos los usuarios de la máquina pueden leer, así que merece la
// misma desconfianza que un enlace pegado. Ver [domain.ParseOwnSeed].
func seedPropio(almacén *statestore.Store, log port.Logger) string {
	raw, err := almacén.LoadSeed()
	if err != nil {
		if !errors.Is(err, statestore.ErrNoState) {
			log.Warn("no se pudo leer el registro guardado, se arranca sin ninguno", "error", err)
		}
		return ""
	}
	seed, err := domain.ParseOwnSeed(raw)
	if err != nil {
		log.Error("el registro guardado no es un nombre válido, se ignora", "error", err)
		return ""
	}
	return seed
}
