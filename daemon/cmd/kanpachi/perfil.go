package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/daemon/transport/protocol"
)

// El alta de un perfil desde la terminal.
//
// # Por qué existe, si esto ya estaba escrito
//
// Porque estaba escrito en una sola boca. `save_profile` vive en el protocolo
// desde que hay creador de perfiles, y la única cara que lo llama es la ventana
// de Flutter, o sea Windows. En un host headless no había forma de describir un
// juego que el catálogo no trae, y describirlo a mano en `local.json` es
// escribir un fichero que el daemon lee con un decodificador estricto.
//
// Este comando no reimplementa nada: arma el JSON del perfil y se lo pasa al
// mismo método, que vuelve a decodificarlo con [domain.ParseGameProfile] y le
// aplica las invariantes enteras. Lo que se gana es la boca, no la lógica.
//
// # Por qué re-ejecutarlo es seguro
//
// El guardado de core pisa por id, así que correrlo otra vez con otros puertos
// ACTUALIZA el mismo perfil en vez de crear un segundo. Es lo que permite que
// un contenedor lo llame en cada arranque.

// perfilJSON es lo que viaja como `profile`, y lleva solo los campos que un
// alta desde la terminal puede rellenar.
//
// **Los campos que no están se omiten a propósito, y no vacíos.** El daemon
// decodifica con los campos desconocidos prohibidos, así que agregar una clave
// de más rechaza el perfil entero; las que faltan toman su cero, que es lo que
// corresponde a un perfil descrito por sus puertos y nada más. `origin` no
// viaja: lo fija el daemon en "mine" y mandarlo sería pedirle que confíe en lo
// que diga quien escribe.
type perfilJSON struct {
	ID          string       `json:"id"`
	Schema      int          `json:"schema"`
	Name        string       `json:"name"`
	HostPorts   []rangoJSON  `json:"host_ports"`
	ConnectHint conexiónJSON `json:"connect_hint"`
}

type rangoJSON struct {
	Proto string `json:"proto"`
	Range string `json:"range"`
}

// conexiónJSON es obligatorio aunque acá siempre valga lo mismo: el esquema
// exige `kind`, y de los tres valores admitidos el único que describe a un
// servidor dedicado es la IP directa.
type conexiónJSON struct {
	Kind string `json:"kind"`
}

func cmdProfile(_ context.Context, op opciones, args []string) error {
	id := ""
	nombre := ""
	tcp := ""
	udp := ""
	reemplazar := false

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--replace", a == "-replace":
			reemplazar = true
		case a == "--name", a == "-name":
			var err error
			if nombre, err = valorDe(args, &i, "--name"); err != nil {
				return err
			}
		case a == "--tcp", a == "-tcp":
			var err error
			if tcp, err = valorDe(args, &i, "--tcp"); err != nil {
				return err
			}
		case a == "--udp", a == "-udp":
			var err error
			if udp, err = valorDe(args, &i, "--udp"); err != nil {
				return err
			}
		case strings.HasPrefix(a, "--name="):
			nombre = strings.TrimPrefix(a, "--name=")
		case strings.HasPrefix(a, "--tcp="):
			tcp = strings.TrimPrefix(a, "--tcp=")
		case strings.HasPrefix(a, "--udp="):
			udp = strings.TrimPrefix(a, "--udp=")
		case strings.HasPrefix(a, "-"):
			return uso("profile does not understand %q. It takes --name, --tcp, --udp and --replace", a)
		case id == "":
			id = a
		default:
			return uso("profile takes one id, and it got %q as well", a)
		}
	}

	if id == "" {
		return uso("profile needs an id: kanpachi profile my-server --name \"My server\" --tcp 25565")
	}
	if strings.TrimSpace(nombre) == "" {
		return uso("profile needs --name, which is how this machine lists the game")
	}
	if tcp == "" && udp == "" {
		return uso("profile needs --tcp or --udp, or it describes nothing")
	}

	// Los puertos se validan ACÁ además de en el daemon, y no es duplicar la
	// invariante: es que el error salga con el rango que la persona escribió.
	// La que manda sigue siendo la del daemon, que corre sobre el JSON ya
	// armado y es la que puede decir que no.
	rangos := make([]rangoJSON, 0, domain.MaxPortRanges)
	for _, par := range []struct {
		proto domain.Proto
		lista string
	}{
		{domain.ProtoTCP, tcp},
		{domain.ProtoUDP, udp},
	} {
		if par.lista == "" {
			continue
		}
		leídos, err := domain.ParsePortRanges(par.proto, par.lista)
		if err != nil {
			return uso("--%s: %v", par.proto, err)
		}
		for _, r := range leídos {
			rangos = append(rangos, rangoJSON{Proto: r.Proto.String(), Range: r.Spec()})
		}
	}

	c, err := abrir(op)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	guardado, hecho, err := pedir[protocol.GameView](c, op, protocol.MethodSaveProfile, struct {
		Profile perfilJSON `json:"profile"`
		Replace bool       `json:"replace"`
	}{
		Profile: perfilJSON{
			ID:          id,
			Schema:      domain.SchemaVersion,
			Name:        nombre,
			HostPorts:   rangos,
			ConnectHint: conexiónJSON{Kind: "direct_ip"},
		},
		Replace: reemplazar,
	})
	if hecho || err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "Saved %s (%s).\n", guardado.ID, guardado.Name)
	for _, r := range guardado.HostPorts {
		if r.From == r.To {
			fmt.Fprintf(os.Stdout, "  %d/%s\n", r.From, r.Proto)
			continue
		}
		fmt.Fprintf(os.Stdout, "  %d-%d/%s\n", r.From, r.To, r.Proto)
	}
	fmt.Fprintf(os.Stdout, "\n  Activate it with: kanpachi game %s\n", guardado.ID)
	return nil
}

// valorDe saca el valor que sigue a una bandera y adelanta el índice.
func valorDe(args []string, i *int, bandera string) (string, error) {
	if *i+1 >= len(args) {
		return "", uso("%s is missing its value", bandera)
	}
	*i++
	return args[*i], nil
}
