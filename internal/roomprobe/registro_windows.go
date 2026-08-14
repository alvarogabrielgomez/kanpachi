//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/port"
)

// El registro y su password, que son los dos pasos que le faltaban a esta
// herramienta para poder abrir una sala.
//
// # Qué pasaba sin ellos
//
// Que crear sala moría en "esta máquina no tiene registro configurado" y no
// había nada que hacer desde dentro: la única forma de configurarlo era volver
// a arrancar con `-seed`, y el password de un registro cerrado no tenía por
// dónde entrar. La ventana tiene las dos pantallas —la de servidor y la de
// password— y la sonda que existe para medir el mismo camino no las tenía.

// registroActual es el registro donde ESTA máquina abre salas, leído de donde
// manda: el estado de la sesión.
//
// `-seed` solo lo siembra al arrancar. Preguntarle a la sesión y no al flag es
// lo que hace que cambiarlo desde el menú se vea en la cabecera y en el resto
// de las medidas sin tener que reiniciar nada.
func registroActual(e entorno) string {
	if s := e.s.OwnSeed(); s != "" {
		return s
	}
	return ""
}

// asegurarRegistro deja un registro configurado, preguntándolo si hace falta.
//
// Devuelve el que quedó. Cadena vacía significa que no hay, y entonces quien
// llama tiene que abandonar la operación: crear una sala sin registro no falla
// a medias, no arranca.
func asegurarRegistro(ctx context.Context, e entorno) (string, error) {
	if s := registroActual(e); s != "" {
		return s, nil
	}
	fmt.Println("\n  Esta máquina todavía no tiene registro donde abrir salas.")
	return pedirRegistro(ctx, e)
}

// pedirRegistro pregunta el nombre y lo guarda.
//
// Va por `SetOwnSeed`, que es lo que hace la ventana: valida el nombre, SONDEA
// que conteste y solo entonces lo guarda. Escribirlo a mano en `seed.txt`
// habría sido más corto y habría medido otra cosa.
func pedirRegistro(ctx context.Context, e entorno) (string, error) {
	valor, err := texto("Host del registro (seed):",
		"Solo el nombre, sin https:// y sin puerto. Ejemplo: kanpachi.accentio.dev\n"+
			"Es donde ESTA máquina abre sus salas. Para entrar a una ajena no hace falta:\n"+
			"ese sale del código pegado.", registroActual(e))
	if err != nil {
		return "", err
	}
	return guardarRegistro(ctx, e, valor)
}

// guardarRegistro es el paso común del menú y de la bandera `-seed`.
func guardarRegistro(ctx context.Context, e entorno, valor string) (string, error) {
	valor = strings.TrimSpace(valor)
	if valor == "" {
		return "", nil
	}
	fmt.Printf("  Comprobando que %s conteste...\n", valor)
	limpio, err := e.s.SetOwnSeed(ctx, valor)
	if err != nil {
		e.log.Error("no se pudo configurar el registro", "seed", valor, "error", err)
		fmt.Println("  MAL:", err)
		return "", nil
	}
	e.op.seed = limpio
	e.log.Info("registro configurado", "seed", limpio)
	fmt.Println("  OK, registro:", limpio)
	return limpio, nil
}

// conPasswordSiHaceFalta corre una operación y, si el registro pide password
// para hospedar, lo pregunta y la repite UNA vez.
//
// # Por qué un reintento y no un bucle
//
// Porque el segundo fallo ya no es "faltaba el password": es que el password no
// vale, y volver a preguntarlo sin decirlo convierte un error claro en una
// pantalla que se repite sola. Se sale con el error de verdad y quien lo lee
// decide.
//
// El password se pregunta OCULTO y no se escribe en el log ni en el error, por
// lo mismo que el caso de uso lo saca de sus mensajes: nadie mira lo que pega
// en un chat.
func conPasswordSiHaceFalta(ctx context.Context, e entorno, op func() error) error {
	err := op()
	if !errors.Is(err, port.ErrSeedPassword) {
		return err
	}
	fmt.Println("\n  Ese registro pide password para hospedar.")
	if e.op.password == "" {
		clave, errP := secreto("Password para hospedar:",
			"No es para entrar a salas: entrar nunca lo pide.\n"+
				"Se guarda solo el token que devuelve, jamás el password.")
		if errP != nil {
			return errP
		}
		e.op.password = clave
	}
	if err := e.s.SeedPassword(ctx, e.op.password); err != nil {
		// El password no sirvió, así que no se guarda para el intento
		// siguiente: dejarlo puesto haría que el próximo también fallara sin
		// volver a preguntar.
		e.op.password = ""
		e.log.Error("el registro no aceptó el password", "error", err)
		return err
	}
	e.log.Info("credencial aceptada, se repite la operación")
	fmt.Println("  Credencial aceptada. Reintentando...")
	return op()
}

// autenticarSiHaceFalta usa el password que vino por bandera ANTES de intentar
// nada, para que una corrida desatendida no dependa de tropezar primero.
//
// Sin registro no hay contra quién autenticarse, así que no es un error: se
// calla y sigue, y el camino interactivo lo pedirá cuando toque.
func autenticarSiHaceFalta(ctx context.Context, e entorno) {
	if e.op.password == "" || registroActual(e) == "" {
		return
	}
	if err := e.s.SeedPassword(ctx, e.op.password); err != nil {
		e.log.Warn("el password de la bandera no fue aceptado", "error", err)
		fmt.Println("  El password que pasaste no fue aceptado:", err)
		e.op.password = ""
		return
	}
	e.log.Info("credencial aceptada desde la bandera -seed-password")
}

// completarConRegistro le pega el registro configurado a un código que vino sin
// seed.
//
// `domain.ParseRoom` rechaza `A7K2M9QX` a secas a propósito: un invite ID solo
// significa algo en el registro que lo emitió. Acá no se adivina en silencio, se
// PREGUNTA, que es lo que hace que la sonda siga midiendo el camino real.
func completarConRegistro(ctx context.Context, e entorno, pegado string, err error) (string, error) {
	if !errors.Is(err, domain.ErrSeedMissing) {
		return "", nil
	}
	seed := registroActual(e)
	fmt.Println("\n  Ese código no dice de qué registro es.")
	if seed == "" {
		fmt.Println("  Y esta máquina no tiene ninguno configurado.")
		nuevo, errR := pedirRegistro(ctx, e)
		if errR != nil || nuevo == "" {
			return "", errR
		}
		seed = nuevo
	}
	sel, errS := elegir(fmt.Sprintf("¿Lo probamos contra %s?", seed), []string{
		"1. Si, usar " + seed,
		"2. No, cancelar",
	})
	if errS != nil || strings.HasPrefix(sel, "2.") {
		return "", errS
	}
	return strings.TrimSpace(pegado) + "@" + seed, nil
}
