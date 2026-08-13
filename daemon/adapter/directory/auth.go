package directory

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/port"
)

// Hospedar en un seed que pide password.
//
// # Lo que este fichero NO toca, y es la mitad del diseño
//
// El password. No llega acá, no se guarda y no se manda: lo que viaja es
// [domain.SeedAuthProof], que es un hash con el host del seed dentro, y lo que
// queda en disco es el refresh token y nada más. Un disco robado entrega una
// credencial que caduca y que el operador revoca cambiando el password.
//
// Y entrar a una sala. Ninguna de estas rutas está en el camino de un invitado:
// `Lookup` no manda bearer porque el registro nunca se lo pide.

// TokenStore es lo poco que este adaptador necesita del almacén de estado.
//
// Se declara acá con los nombres de [port.StateStore] a propósito, para que la
// implementación real lo cumpla sin envoltorio y para que este paquete no tenga
// que conocer el puerto entero. Nil significa no recordar nada entre arranques,
// que es lo que usan los tests y las herramientas.
type TokenStore interface {
	LoadSeedToken() ([]byte, error)
	SaveSeedToken([]byte) error
	ClearSeedToken() error
}

// Authenticate cambia un proof por un par de tokens, y guarda el refresh.
//
// Lo llama un solo camino: alguien acaba de escribir el password. El resto del
// tiempo la credencial se renueva sola con [Directory.refreshAccess], que es lo que
// hace que la pantalla de password aparezca cuando el refresh muere y no antes.
func (d *Directory) Authenticate(ctx context.Context, proof string) error {
	if len(proof) != domain.SeedAuthProofLen {
		return fmt.Errorf("%w: el proof no tiene la forma que produce domain.SeedAuthProof",
			domain.ErrInputShape)
	}
	var out tokenBody
	err := d.attempt(ctx, http.MethodPost, "/api/auth/token",
		authBody{Proof: proof}, &out, http.StatusOK)
	if err != nil {
		return err
	}
	if out.Access == "" || out.Refresh == "" {
		return errors.New("el registro contestó sin tokens")
	}

	d.authMu.Lock()
	d.access = out.Access
	d.refresh = out.Refresh
	d.loaded = true
	d.authMu.Unlock()

	return d.saveRefresh(out.Refresh)
}

// accessToken es lo que se manda como bearer, o vacío.
//
// **Nunca toma el lock del refresco**, y esa separación es lo que evita el
// abrazo mortal: [Directory.refreshAccess] hace una petición HTTP, y esa petición
// pasa por acá a preguntar qué bearer poner.
func (d *Directory) accessToken() string {
	d.authMu.Lock()
	defer d.authMu.Unlock()
	return d.access
}

// refreshAccess cambia el refresh token por un access nuevo.
//
// `stale` es el bearer que se acaba de comer el 401. Si para cuando se toma el
// turno el access ya es otro, es que otra llamada refrescó mientras esta
// esperaba, y entonces no hay nada que hacer salvo reintentar: sin esa
// comprobación, dos operaciones que fallan a la vez gastan dos refrescos y la
// segunda invalida el trabajo de la primera.
func (d *Directory) refreshAccess(ctx context.Context, stale string) error {
	d.refreshMu.Lock()
	defer d.refreshMu.Unlock()

	if current := d.accessToken(); current != "" && current != stale {
		return nil
	}

	refresh, err := d.refreshToken()
	if err != nil {
		return err
	}

	var out tokenBody
	err = d.attempt(ctx, http.MethodPost, "/api/auth/refresh",
		refreshRequest{Refresh: refresh}, &out, http.StatusOK)
	if err != nil {
		// Un refresco que falla no se reintenta y no se conserva. El registro se
		// niega a decir si venció o si el operador cambió el password, y las dos
		// llevan al mismo sitio: alguien tiene que escribirlo otra vez. Dejar el
		// fichero puesto haría que cada operación pagara un viaje inútil hasta
		// que alguien lo notara.
		d.forget()
		if errors.Is(err, port.ErrSeedPassword) {
			return err
		}
		return fmt.Errorf("%w: %v", port.ErrSeedPassword, err)
	}
	if out.Access == "" {
		d.forget()
		return fmt.Errorf("%w: el registro refrescó sin dar token", port.ErrSeedPassword)
	}

	d.authMu.Lock()
	d.access = out.Access
	d.authMu.Unlock()
	return nil
}

// refreshToken devuelve el refresh, leyéndolo del disco la primera vez.
//
// Perezoso y no en `New` por lo mismo que la llave de la instalación: una
// máquina que solo entra a salas jamás hospeda, y abrir el sello del estado en
// el arranque sería trabajo por una credencial que no se va a usar.
func (d *Directory) refreshToken() (string, error) {
	d.authMu.Lock()
	defer d.authMu.Unlock()

	if !d.loaded {
		d.loaded = true
		d.refresh = d.readFromDisk()
	}
	if d.refresh == "" {
		return "", port.ErrSeedPassword
	}
	return d.refresh, nil
}

// readFromDisk lee el token guardado, o devuelve vacío.
//
// Nada de lo que salga mal acá es un error que suba: un fichero que no está es
// el caso normal, y uno que no se sostiene se descarta igual que se descarta una
// sala guardada que no decodifica. Lo que sigue es pedir el password, que es lo
// mismo que habría que hacer con el fichero ausente.
//
// **El seed guardado tiene que ser ESTE.** Un token solo vale en el registro que
// lo emitió, y mandar el de otro pondría la credencial de un operador delante de
// otro distinto. Pasa de verdad en cuanto alguien cambia su registro.
func (d *Directory) readFromDisk() string {
	if d.deps.Tokens == nil {
		return ""
	}
	raw, err := d.deps.Tokens.LoadSeedToken()
	if err != nil {
		return ""
	}
	tok, err := domain.DecodeSeedToken(raw)
	if err != nil {
		d.deps.Log.Warn("el token guardado del registro no se sostiene, se pide el password otra vez",
			"error", err)
		return ""
	}
	if tok.Seed != d.deps.Seed {
		d.deps.Log.Info("el token guardado es de otro registro, así que no se manda",
			"guardado", tok.Seed, "current", d.deps.Seed)
		return ""
	}
	return tok.Refresh
}

func (d *Directory) saveRefresh(refresh string) error {
	if d.deps.Tokens == nil {
		return nil
	}
	raw, err := domain.EncodeSeedToken(domain.SeedToken{Seed: d.deps.Seed, Refresh: refresh})
	if err != nil {
		return err
	}
	if err := d.deps.Tokens.SaveSeedToken(raw); err != nil {
		return fmt.Errorf("no se pudo guardar la credencial del registro: %w", err)
	}
	return nil
}

// forget tira lo que hay, en memoria y en disco.
func (d *Directory) forget() {
	d.authMu.Lock()
	d.access = ""
	d.refresh = ""
	d.loaded = true
	d.authMu.Unlock()

	if d.deps.Tokens == nil {
		return
	}
	if err := d.deps.Tokens.ClearSeedToken(); err != nil {
		d.deps.Log.Warn("no se pudo borrar la credencial caducada del registro", "error", err)
	}
}
