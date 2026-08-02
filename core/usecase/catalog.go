package usecase

import (
	"context"
	"fmt"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// ListGames devuelve el catálogo efectivo con los instalados arriba.
//
// El orden es un atajo, jamás una puerta: la biblioteca completa sigue
// accesible y elegir un juego que la detección no vio funciona exactamente
// igual, sin advertencias. Que la detección falle es normal y tiene motivos
// que no son errores: el juego está fuera de Steam, vive en Epic o en GOG, o
// la unidad no se pudo leer.
func (s *Session) ListGames() []domain.GameProfile {
	s.mu.Lock()
	defer s.mu.Unlock()
	return domain.SortByInstalled(s.catalog.Profiles(), s.installed)
}

// RejectedGames son los perfiles que no entraron, con su motivo. La UI los
// marca como no disponibles en vez de ocultarlos: un juego que desaparece sin
// explicación manda a alguien a buscar en el sitio equivocado.
func (s *Session) RejectedGames() []domain.RejectedProfile {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.catalog.Rejected()
}

// SaveProfile da de alta un juego que no estaba, o corrige uno propio.
//
// El perfil nace SIN VERIFICAR y no hay forma de marcarlo a mano. La única vía
// es la pregunta que aparece al salir de una sala donde ese juego estuvo
// activo y hubo dos personas o más, que es lo que hace que "verificado"
// signifique algo.
// `replace` es la respuesta del usuario a la colisión con un builtin, y solo
// tiene efecto en ese caso. Sin ella, guardar con el id de un builtin falla con
// [ErrShadowsBuiltin] y la UI pregunta.
func (s *Session) SaveProfile(ctx context.Context, p domain.GameProfile, replace bool) (domain.GameProfile, error) {
	p.Origin = domain.OriginMine
	// Verified se descarta venga como venga. Es la puerta que impide marcar un
	// perfil como probado escribiéndolo en la petición, que sería la forma
	// obvia de saltarse la única regla que sostiene la etiqueta.
	p.Verified = nil

	saved, err := domain.NewGameProfile(p)
	if err != nil {
		return domain.GameProfile{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Un perfil local NUNCA reemplaza a uno builtin en silencio. El camino de
	// importación ya modela la colisión con una pantalla donde el usuario
	// elige; el alta manual no la tenía, así que crear un perfil con el id de
	// un builtin lo tapaba para siempre y el juego dejaba de recibir las
	// correcciones que trae cada versión de la app.
	if existente, hay := s.catalog.Find(saved.ID); hay && existente.Origin == domain.OriginBuiltin {
		if !replace {
			return domain.GameProfile{}, fmt.Errorf("%w: %q", ErrShadowsBuiltin, saved.ID)
		}
		s.deps.Log.Warn("un perfil propio tapa a uno que vino con la app", "juego", saved.ID)
	}

	local := replaceByID(s.catalog.Local(), saved)
	if err := s.writeLocalLocked(ctx, local); err != nil {
		return domain.GameProfile{}, err
	}
	s.deps.Log.Info("perfil guardado", "juego", saved.ID)
	return saved, nil
}

// ImportCatalog aplica una selección de un archivo compartido.
//
// `chosen` son los ids que el usuario marcó en la pantalla. Nada se
// sobreescribe en silencio: un id que colisiona solo entra si viene en esta
// lista, y la pantalla lo trae desmarcado cuando lo que ya hay está
// verificado. La confianza no se hereda, así que un perfil importado queda
// como "verificado por Humberto", jamás como verificado por ti.
func (s *Session) ImportCatalog(ctx context.Context, raw []byte, chosen []string) ([]domain.ImportCandidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	candidates, err := domain.ParseCatalogFile(raw, s.catalog)
	if err != nil {
		return nil, err
	}

	want := make(map[string]bool, len(chosen))
	for _, id := range chosen {
		want[id] = true
	}

	local := s.catalog.Local()
	var applied int
	for _, c := range candidates {
		// Las invariantes corren primero y un rechazado no se puede forzar,
		// esté o no en la selección. Si estuviera, sería porque la UI se
		// desincronizó del daemon, y en esa duda gana el daemon.
		if c.Rejected || !want[c.Profile.ID] {
			continue
		}
		local = replaceByID(local, c.Profile)
		applied++
	}
	if applied == 0 {
		return candidates, nil
	}
	if err := s.writeLocalLocked(ctx, local); err != nil {
		return nil, err
	}
	s.deps.Log.Info("catálogo importado", "perfiles", applied)
	return candidates, nil
}

// ExportCatalog arma el archivo para compartir.
//
// Por defecto solo los propios, que es lo que tiene sentido mandar: los
// builtin ya los tiene el otro. `all` incluye todo, para el caso de querer
// pasarle a alguien el catálogo entero de una máquina.
func (s *Session) ExportCatalog(at, by, version string, all bool) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	src := s.catalog.Profiles()
	if !all {
		src = nil
		for _, p := range s.catalog.Local() {
			if p.Origin == domain.OriginMine {
				src = append(src, p)
			}
		}
	}
	return domain.ExportCatalog(src, at, by, version)
}

// MarkVerified rellena la constancia tras una partida real.
//
// Lo dispara la pregunta que aparece al SALIR de la sala, que es una acción
// del usuario. Kanpachi no sabe si de verdad jugaron, y por eso pregunta en
// vez de suponer: no observa procesos y no detecta que abriste un juego.
func (s *Session) MarkVerified(ctx context.Context, gameID string, v domain.Verified) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Las dos condiciones de 06-catalogo.md, comprobadas acá y no confiadas a
	// la UI. "No se puede marcar a mano" no significa nada si la API acepta la
	// marca de quien la pida: sin esto, cualquier proceso del usuario puede
	// escribirle "verificado en partida real, 4 jugadores" a un perfil que
	// nadie probó, y la etiqueta que sostiene la confianza del catálogo
	// compartido deja de significar algo.
	testigo, ok := s.verificables[gameID]
	if !ok {
		return fmt.Errorf("%w: %q", ErrNotPlayed, gameID)
	}
	delete(s.verificables, gameID)
	v.Date = testigo

	p, ok := s.catalog.Find(gameID)
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownGame, gameID)
	}
	if p.Origin == domain.OriginBuiltin {
		// Un builtin ya viene verificado por el autor y vive en Program Files,
		// que es de solo lectura. Escribir una copia local para cambiarle la
		// constancia crearía un perfil "mine" que tapa al builtin por
		// precedencia, y a partir de ahí las actualizaciones de la app
		// dejarían de llegarle a ese juego.
		return nil
	}
	p.Verified = &v

	local := replaceByID(s.catalog.Local(), p)
	if err := s.writeLocalLocked(ctx, local); err != nil {
		return err
	}
	s.deps.Log.Info("perfil verificado en partida real", "juego", gameID, "por", v.By)
	return nil
}

// writeLocalLocked serializa, escribe y recarga. Asume el candado tomado.
//
// Recarga en vez de mutar el catálogo en memoria porque la precedencia entre
// capas es lo que decide qué perfil gana, y aplicarla a mano acá sería
// escribir esa regla dos veces. Releer del disco tiene además la propiedad de
// que lo que la app muestra es lo que quedó guardado, no lo que se pretendía
// guardar.
func (s *Session) writeLocalLocked(ctx context.Context, local []domain.GameProfile) error {
	// Los rechazados de la capa local viajan con sus bytes originales. Sin eso,
	// el primer guardado después de un rechazo le borra del disco un perfil que
	// la UI le está mostrando al usuario como corregible.
	raw, err := domain.EncodeLocalCatalog(
		local, s.catalog.RejectedLocal(), s.deps.Clock.Now().UTC().Format(timeLayout), "")
	if err != nil {
		return fmt.Errorf("serializando el catálogo local: %w", err)
	}
	if err := s.deps.Store.SaveLocal(raw); err != nil {
		return fmt.Errorf("guardando el catálogo local: %w", err)
	}
	s.reloadCatalog(ctx)
	return nil
}

// timeLayout es RFC3339 en UTC, que es lo que lleva `exported_at` en el
// formato de intercambio.
const timeLayout = "2006-01-02T15:04:05Z"

// replaceByID sustituye el perfil con ese id, o lo agrega si no estaba.
func replaceByID(in []domain.GameProfile, p domain.GameProfile) []domain.GameProfile {
	out := make([]domain.GameProfile, 0, len(in)+1)
	var replaced bool
	for _, existing := range in {
		if existing.ID == p.ID {
			out = append(out, p)
			replaced = true
			continue
		}
		out = append(out, existing)
	}
	if !replaced {
		out = append(out, p)
	}
	return out
}
