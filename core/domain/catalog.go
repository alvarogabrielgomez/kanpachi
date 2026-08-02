package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// CatalogEnvelope es la versión del formato de intercambio, el archivo que
// alguien manda por Telegram. Es distinta de [SchemaVersion], que versiona un
// perfil suelto: el sobre puede cambiar sin que cambien los perfiles.
const CatalogEnvelope = 1

// RejectedProfile es un perfil que no entró, con el motivo en lenguaje humano.
//
// Existe como valor y no como error suelto porque la carga NO se aborta cuando
// un perfil es inválido: los demás entran, y la UI marca este como no
// disponible con su razón. Abortar la carga entera dejaría a alguien sin
// catálogo por un archivo compartido que vino mal.
type RejectedProfile struct {
	ID     string
	Name   string
	Origin Origin
	Reason string

	// Raw son los bytes tal como venían, y solo se rellenan en la capa local.
	//
	// Existen para NO BORRARLE EL ARCHIVO AL USUARIO. Guardar el catálogo lo
	// reescribe entero desde los perfiles válidos, así que sin esto el primer
	// alta manual después de un rechazo se llevaba por delante el perfil
	// rechazado: el usuario perdía un archivo suyo por un error que la UI le
	// estaba mostrando como corregible. Ningún documento dice que se borre.
	//
	// No se reinterpretan jamás: se copian de vuelta al archivo tal cual.
	Raw []byte
}

// Catalog es la lista efectiva de perfiles, ya con la precedencia aplicada.
//
// Se construye con [BuildCatalog] a partir de las tres capas. El cero es un
// catálogo vacío y válido: es lo que queda si builtin y local fallan los dos,
// y la app tiene que seguir funcionando en ese caso.
type Catalog struct {
	profiles []GameProfile     // ordenados por nombre, precedencia ya resuelta
	rejected []RejectedProfile // lo que no entró, con motivo
	shadowed []GameProfile     // perfiles tapados por otra capa de más rango
}

// BuildCatalog aplica la precedencia entre capas: mine sobre imported sobre
// builtin.
//
// Recibe perfiles YA validados, o sea que quien los cargó del disco ya llamó a
// [ParseGameProfile] y separó los rechazos. Esta función solo resuelve
// colisiones de id, que es lógica de dominio pura.
//
// Un perfil tapado no desaparece: queda en [Catalog.Shadowed] para que la UI
// pueda decir "tu perfil de Valheim está oculto por el builtin" en vez de que
// el usuario vea que su archivo no hizo nada.
func BuildCatalog(profiles []GameProfile, rejected []RejectedProfile) Catalog {
	best := make(map[string]GameProfile, len(profiles))
	var shadowed []GameProfile

	for _, p := range profiles {
		prev, seen := best[p.ID]
		if !seen {
			best[p.ID] = p
			continue
		}
		// A igual precedencia gana el primero que llegó. Empatar es que la
		// misma capa trae el id dos veces, que es un archivo mal formado, y
		// quedarse con el primero es tan arbitrario como quedarse con el
		// último, con la ventaja de ser estable entre ejecuciones.
		if p.Origin.precedence() > prev.Origin.precedence() {
			best[p.ID] = p
			shadowed = append(shadowed, prev)
			continue
		}
		shadowed = append(shadowed, p)
	}

	out := make([]GameProfile, 0, len(best))
	for _, p := range best {
		out = append(out, p)
	}
	sortProfiles(out)
	sortProfiles(shadowed)

	return Catalog{profiles: out, rejected: rejected, shadowed: shadowed}
}

// sortProfiles ordena por nombre y desempata por id.
//
// El orden se fija acá y no en la UI porque el recorrido de un map de Go es
// aleatorio a propósito: sin este paso, la lista de juegos saldría en un orden
// distinto en cada arranque y nadie encontraría nada dos veces en el mismo
// sitio. El desempate por id existe para que dos juegos con el mismo nombre no
// se intercambien entre arranques.
func sortProfiles(ps []GameProfile) {
	sort.Slice(ps, func(i, j int) bool {
		li, lj := strings.ToLower(ps[i].Name), strings.ToLower(ps[j].Name)
		if li != lj {
			return li < lj
		}
		return ps[i].ID < ps[j].ID
	})
}

// Profiles devuelve la lista efectiva. La copia es deliberada: un Catalog es
// un valor que se publica por la API y no puede mutar desde fuera.
func (c Catalog) Profiles() []GameProfile {
	return append([]GameProfile(nil), c.profiles...)
}

func (c Catalog) Rejected() []RejectedProfile {
	return append([]RejectedProfile(nil), c.rejected...)
}

func (c Catalog) Shadowed() []GameProfile {
	return append([]GameProfile(nil), c.shadowed...)
}

// Find busca por id. El segundo valor distingue "no está" de "está y es el
// cero", que sin él serían la misma respuesta.
func (c Catalog) Find(id string) (GameProfile, bool) {
	for _, p := range c.profiles {
		if p.ID == id {
			return p, true
		}
	}
	return GameProfile{}, false
}

// Local devuelve los perfiles que viven en local.json, o sea los propios y los
// importados. Es lo que se vuelve a escribir tras crear o importar uno.
//
// Incluye los tapados: un perfil propio que hoy está oculto por un builtin
// sigue siendo del usuario, y perderlo al guardar sería borrarle un archivo
// por un efecto de precedencia que él no pidió.
func (c Catalog) Local() []GameProfile {
	var out []GameProfile
	for _, p := range append(c.Profiles(), c.shadowed...) {
		if p.Origin == OriginMine || p.Origin == OriginImported {
			out = append(out, p)
		}
	}
	sortProfiles(out)
	return out
}

// catalogFileJSON es el formato de intercambio de 06-catalogo.md.
//
// Los perfiles quedan SIN DECODIFICAR, en bytes crudos, y eso resuelve dos
// cosas de una:
//
//  1. Cada uno se decodifica después con [decodeProfileStrict], que es el único
//     sitio con DisallowUnknownFields. Con `[]profileJSON` acá, la invariante 8
//     no corría en ningún camino real de carga.
//  2. Un perfil mal escrito no se lleva a los demás por delante. Con la lista
//     tipada, un `"broadcast_route": "sí"` en el tercer perfil abortaba el
//     Unmarshal del archivo entero, así que el usuario perdía los dos buenos
//     al importar y se quedaba sin catálogo local al arrancar.
type catalogFileJSON struct {
	Envelope   int               `json:"kanpachi_catalog"`
	ExportedAt string            `json:"exported_at,omitempty"`
	ExportedBy string            `json:"exported_by,omitempty"`
	AppVersion string            `json:"app_version,omitempty"`
	Profiles   []json.RawMessage `json:"profiles"`
}

// ParseCatalogLayer lee uno de los dos archivos del disco, builtin.json o
// local.json, y separa lo que entró de lo que no.
//
// No devuelve error por un perfil malo: devuelve el perfil en la lista de
// rechazados con su motivo y sigue con el resto. Solo un archivo entero
// ilegible es un error, y quien llama trata ese caso ignorando la capa
// completa. Un local.json corrupto se ignora entero, con aviso en la UI, y
// Kanpachi sigue funcionando con los builtin: nunca se queda sin catálogo.
//
// `layer` fija el origen de todo el archivo. La excepción es local.json, que
// mezcla propios con importados: ahí cada perfil puede llevar su `origin`, y
// lo que no lo lleve se toma como propio. En un archivo de intercambio ese
// campo se ignora, porque la capa la decide quien carga.
func ParseCatalogLayer(raw []byte, layer Origin) ([]GameProfile, []RejectedProfile, error) {
	var f catalogFileJSON
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrCatalogEnvelope, err)
	}
	if f.Envelope != CatalogEnvelope {
		return nil, nil, fmt.Errorf("%w: declara kanpachi_catalog %d y esta versión entiende %d",
			ErrCatalogEnvelope, f.Envelope, CatalogEnvelope)
	}

	var (
		ok  []GameProfile
		bad []RejectedProfile
	)
	for _, crudo := range f.Profiles {
		j, err := decodeProfileStrict(crudo)
		if err != nil {
			id, name := identifyProfile(crudo)
			bad = append(bad, RejectedProfile{
				ID: id, Name: name, Origin: layer, Reason: humanReason(err),
				Raw: append([]byte(nil), crudo...),
			})
			continue
		}

		origin := layer
		if layer != OriginBuiltin && j.Origin == OriginImported.String() {
			origin = OriginImported
		}
		p, err := j.toDomain(origin)
		if err != nil {
			bad = append(bad, RejectedProfile{
				ID: j.ID, Name: j.Name, Origin: origin, Reason: humanReason(err),
				Raw: append([]byte(nil), crudo...),
			})
			continue
		}
		ok = append(ok, p)
	}
	return ok, bad, nil
}

// EncodeLocalCatalog escribe local.json, que es la única capa escribible.
//
// Marca el origen perfil por perfil porque el archivo mezcla los propios con
// los importados, y esa distinción es lo que la UI muestra como "tuyo" o
// "compartido por alguien". Perderla al guardar convertiría todo lo importado
// en propio en el siguiente arranque, que es justo la confianza que
// 06-catalogo.md dice que no se hereda.
// RejectedLocal son los rechazos que vinieron de local.json, con sus bytes.
// Se los pasa a [EncodeLocalCatalog] para que guardar no borre lo que la UI
// está mostrando como corregible.
func (c Catalog) RejectedLocal() []RejectedProfile {
	var out []RejectedProfile
	for _, r := range c.rejected {
		if r.Origin != OriginBuiltin && len(r.Raw) > 0 {
			out = append(out, r)
		}
	}
	return out
}

func EncodeLocalCatalog(profiles []GameProfile, rejected []RejectedProfile, savedAt, appVersion string) ([]byte, error) {
	ordered := append([]GameProfile(nil), profiles...)
	sortProfiles(ordered)

	f := catalogFileJSON{
		Envelope:   CatalogEnvelope,
		ExportedAt: savedAt,
		AppVersion: appVersion,
		Profiles:   make([]json.RawMessage, 0, len(ordered)),
	}
	for _, p := range ordered {
		j := p.toJSON()
		j.Origin = p.Origin.String()
		crudo, err := json.Marshal(j)
		if err != nil {
			return nil, fmt.Errorf("serializando el perfil %s: %w", p.ID, err)
		}
		f.Profiles = append(f.Profiles, crudo)
	}
	// Los rechazados vuelven al archivo tal como estaban. No se reinterpretan y
	// no se corrigen: lo que se conserva es el archivo del usuario, y quien lo
	// arregla es él desde la pantalla que le dice qué tiene mal.
	for _, r := range rejected {
		if len(r.Raw) > 0 {
			f.Profiles = append(f.Profiles, append(json.RawMessage(nil), r.Raw...))
		}
	}
	return json.MarshalIndent(f, "", "  ")
}

// ImportCandidate es un perfil del archivo con el veredicto ya calculado.
//
// La decisión de importarlo o no la toma el usuario en la pantalla, así que
// esta función no escribe nada: produce la lista que la UI pinta con sus
// casillas. Un rechazado no lleva casilla, porque la invariante 1 dice que un
// perfil que viola las reglas no se puede forzar.
type ImportCandidate struct {
	Profile GameProfile

	// Rejected y Reason: la invariante lo tumbó. No hay forma de importarlo.
	Rejected bool
	Reason   string

	// Collides dice que ya existe un perfil con ese id en el catálogo actual.
	Collides bool
	// Existing es con qué colisiona, para que la pantalla diga "el tuyo es
	// verificado" en vez de solo "ya lo tienes".
	Existing GameProfile
	// Suggested es la casilla que viene marcada por defecto. Nunca se
	// sobreescribe en silencio: sin colisión viene marcada, con colisión viene
	// desmarcada si lo que ya hay está verificado.
	Suggested bool
}

// ParseCatalogFile lee un archivo de intercambio y devuelve los candidatos.
//
// No escribe nada y no decide nada. Devuelve error solo si el archivo entero
// es ilegible: un perfil malo dentro de un archivo bueno es un candidato
// rechazado, no un archivo rechazado, porque el usuario tiene que poder
// importar los otros dos que sí venían bien.
func ParseCatalogFile(raw []byte, current Catalog) ([]ImportCandidate, error) {
	// El sobre se decodifica SIN DisallowUnknownFields, al revés que un
	// perfil. Son dos cosas distintas: un campo desconocido en un perfil pide
	// una capacidad que no entendemos y es peligroso, mientras que en el sobre
	// es metadato de quien exportó, no llega a ninguna regla de firewall, y
	// rechazar el archivo entero por él impediría abrir catálogos escritos por
	// una versión más nueva.
	var f catalogFileJSON
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCatalogEnvelope, err)
	}
	if f.Envelope != CatalogEnvelope {
		return nil, fmt.Errorf("%w: declara kanpachi_catalog %d y esta versión entiende %d",
			ErrCatalogEnvelope, f.Envelope, CatalogEnvelope)
	}

	seen := make(map[string]bool, len(f.Profiles))
	out := make([]ImportCandidate, 0, len(f.Profiles))

	for _, crudo := range f.Profiles {
		j, err := decodeProfileStrict(crudo)
		if err != nil {
			id, name := identifyProfile(crudo)
			out = append(out, ImportCandidate{
				Profile:  GameProfile{ID: id, Name: name, Origin: OriginImported},
				Rejected: true,
				Reason:   humanReason(err),
			})
			continue
		}
		p, err := j.toDomain(OriginImported)
		if err != nil {
			out = append(out, ImportCandidate{
				Profile:  GameProfile{ID: j.ID, Name: j.Name, Origin: OriginImported},
				Rejected: true,
				Reason:   humanReason(err),
			})
			continue
		}
		if seen[p.ID] {
			out = append(out, ImportCandidate{
				Profile:  p,
				Rejected: true,
				Reason:   "el archivo trae este perfil dos veces",
			})
			continue
		}
		seen[p.ID] = true

		c := ImportCandidate{Profile: p, Suggested: true}
		if existing, ok := current.Find(p.ID); ok {
			c.Collides = true
			c.Existing = existing
			// Marcado por defecto solo si lo que ya hay NO está verificado y
			// NO es builtin. Los dos casos son la misma idea dicha de dos
			// formas: un perfil probado en partida real y uno que vino
			// revisado en el instalador valen más que uno que llegó por
			// Telegram, así que el default es conservar. La confianza no se
			// hereda y tampoco se pisa sin querer.
			c.Suggested = existing.Verified == nil && existing.Origin != OriginBuiltin
		}
		out = append(out, c)
	}
	return out, nil
}

// humanReason recorta el error a algo que quepa en una línea de la pantalla de
// importación. Sin esto la UI mostraría la cadena envuelta entera, con el id
// repetido tres veces y el nombre del campo de Go dentro.
func humanReason(err error) string {
	switch {
	case errors.Is(err, ErrPortForbidden):
		return "pide un puerto que no se permite"
	case errors.Is(err, ErrTooManyRanges):
		return fmt.Sprintf("pide más de %d rangos de puertos", MaxPortRanges)
	case errors.Is(err, ErrProfileSchemaNewer):
		return "es de una versión más nueva de Kanpachi, actualiza la app"
	case errors.Is(err, ErrProfileSchemaOlder):
		return "es de un formato viejo que esta versión ya no lee"
	case errors.Is(err, ErrUnknownField):
		return "pide algo que esta versión no entiende"
	case errors.Is(err, ErrPortRangeShape), errors.Is(err, ErrPortRangeOrder), errors.Is(err, ErrPortZero):
		return "tiene un rango de puertos mal escrito"
	case errors.Is(err, ErrProto):
		return "declara un protocolo que no existe"
	case errors.Is(err, ErrConnectHintKind):
		return "no dice cómo se conecta la gente"
	case errors.Is(err, ErrProfileNoPorts):
		return "no abre ningún puerto"
	case errors.Is(err, ErrProfileID), errors.Is(err, ErrProfileName):
		return "está mal identificado"
	default:
		return "no se pudo leer"
	}
}

// ExportCatalog arma el archivo de intercambio.
//
// Sin firma criptográfica, deliberadamente: firmar daría una sensación de
// autoridad que no corresponde. Lo que protege a quien importa no es la firma,
// son las invariantes, que corren de todos modos. Ver 06-catalogo.md.
func ExportCatalog(profiles []GameProfile, exportedAt, exportedBy, appVersion string) ([]byte, error) {
	ordered := append([]GameProfile(nil), profiles...)
	sortProfiles(ordered)

	f := catalogFileJSON{
		Envelope:   CatalogEnvelope,
		ExportedAt: exportedAt,
		ExportedBy: exportedBy,
		AppVersion: appVersion,
		Profiles:   make([]json.RawMessage, 0, len(ordered)),
	}
	for _, p := range ordered {
		crudo, err := json.Marshal(p.toJSON())
		if err != nil {
			return nil, fmt.Errorf("serializando el perfil %s: %w", p.ID, err)
		}
		f.Profiles = append(f.Profiles, crudo)
	}
	// Con sangría porque el archivo se manda por Telegram y alguien lo va a
	// abrir para mirar qué puertos pide antes de importarlo. Que se pueda leer
	// sin herramientas es parte de la promesa de "un archivo plano, legible".
	return json.MarshalIndent(f, "", "  ")
}
