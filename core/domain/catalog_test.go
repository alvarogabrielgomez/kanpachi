package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

func perfil(id, nombre string, origen Origin) GameProfile {
	p := perfilValido()
	p.ID = id
	p.Name = nombre
	p.Origin = origen
	out, err := NewGameProfile(p)
	if err != nil {
		panic(err)
	}
	return out
}

// TestPrecedenciaMineSobreImportedSobreBuiltin es la tabla de 06-catalogo.md.
func TestPrecedenciaMineSobreImportedSobreBuiltin(t *testing.T) {
	c := BuildCatalog([]GameProfile{
		perfil("valheim", "Valheim builtin", OriginBuiltin),
		perfil("valheim", "Valheim importado", OriginImported),
		perfil("valheim", "Valheim mío", OriginMine),
	}, nil)

	got, ok := c.Find("valheim")
	if !ok {
		t.Fatal("valheim desapareció")
	}
	if got.Origin != OriginMine {
		t.Fatalf("ganó %s, tenía que ganar mine", got.Origin)
	}
	if len(c.Shadowed()) != 2 {
		t.Fatalf("los tapados son %d, tenían que ser 2", len(c.Shadowed()))
	}
}

// TestUnPerfilTapadoNoDesaparece: sin esto, el usuario ve que su archivo no
// hizo nada y no hay forma de que la UI le diga por qué.
func TestUnPerfilTapadoNoDesaparece(t *testing.T) {
	c := BuildCatalog([]GameProfile{
		perfil("valheim", "Valheim builtin", OriginBuiltin),
		perfil("valheim", "Valheim mío", OriginMine),
	}, nil)

	if len(c.Profiles()) != 1 {
		t.Fatalf("la lista efectiva tiene %d perfiles", len(c.Profiles()))
	}
	if len(c.Shadowed()) != 1 || c.Shadowed()[0].Origin != OriginBuiltin {
		t.Fatalf("el builtin tapado no quedó registrado: %+v", c.Shadowed())
	}
}

// TestGuardarLocalNoPierdeUnPerfilPropioTapado.
//
// Un perfil propio que hoy está oculto por un builtin sigue siendo del
// usuario, y dejarlo fuera al guardar sería borrarle un archivo por un efecto
// de precedencia que él no pidió.
func TestGuardarLocalNoPierdeUnPerfilPropioTapado(t *testing.T) {
	c := BuildCatalog([]GameProfile{
		perfil("valheim", "Valheim", OriginBuiltin),
		perfil("valheim", "Valheim mío", OriginMine),
		perfil("terraria", "Terraria", OriginImported),
	}, nil)

	local := c.Local()
	if len(local) != 2 {
		t.Fatalf("Local() devolvió %d perfiles: %+v", len(local), local)
	}
	var vio bool
	for _, p := range local {
		if p.ID == "valheim" && p.Origin == OriginMine {
			vio = true
		}
	}
	if !vio {
		t.Fatal("el perfil propio tapado por el builtin se habría perdido al guardar")
	}
}

// TestElOrdenNoDependeDelRecorridoDelMapa: sin ordenar, la lista de juegos
// saldría distinta en cada arranque y nadie encontraría nada dos veces en el
// mismo sitio.
func TestElOrdenNoDependeDelRecorridoDelMapa(t *testing.T) {
	entrada := []GameProfile{
		perfil("zomboid", "Project Zomboid", OriginBuiltin),
		perfil("valheim", "Valheim", OriginBuiltin),
		perfil("terraria", "Terraria", OriginBuiltin),
		perfil("minecraft", "Minecraft", OriginBuiltin),
	}
	primero := BuildCatalog(entrada, nil).Profiles()
	for i := 0; i < 20; i++ {
		otro := BuildCatalog(entrada, nil).Profiles()
		for j := range primero {
			if primero[j].ID != otro[j].ID {
				t.Fatalf("el orden cambió entre dos construcciones: %s contra %s", primero[j].ID, otro[j].ID)
			}
		}
	}
	if primero[0].Name != "Minecraft" || primero[3].Name != "Valheim" {
		t.Fatalf("no está ordenado por nombre: %s ... %s", primero[0].Name, primero[3].Name)
	}
}

func archivoDeIntercambio(t *testing.T, perfiles ...string) []byte {
	t.Helper()
	return []byte(`{"kanpachi_catalog":1,"exported_by":"humberto","profiles":[` +
		strings.Join(perfiles, ",") + `]}`)
}

func perfilCrudo(id, nombre, rango string) string {
	return `{"id":"` + id + `","schema":2,"name":"` + nombre + `",
	  "host_ports":[{"proto":"udp","range":"` + rango + `"}],"client_ports":[],
	  "connect_hint":{"kind":"direct_ip","text_es":"x"}}`
}

// TestUnPerfilMaloNoTumbaElArchivo: el usuario tiene que poder importar los
// dos que sí venían bien.
func TestUnPerfilMaloNoTumbaElArchivo(t *testing.T) {
	raw := archivoDeIntercambio(t,
		perfilCrudo("valheim", "Valheim", "2456-2458"),
		perfilCrudo("rust", "Rust", "445"),
		perfilCrudo("terraria", "Terraria", "7777"),
	)
	cands, err := ParseCatalogFile(raw, Catalog{})
	if err != nil {
		t.Fatalf("el archivo entero se rechazó por un perfil malo: %v", err)
	}
	if len(cands) != 3 {
		t.Fatalf("se esperaban 3 candidatos, hay %d", len(cands))
	}
	var rechazados int
	for _, c := range cands {
		if c.Rejected {
			rechazados++
			if c.Profile.ID != "rust" {
				t.Errorf("se rechazó el perfil equivocado: %s", c.Profile.ID)
			}
			if !strings.Contains(c.Reason, "puerto") {
				t.Errorf("el motivo no menciona el puerto: %q", c.Reason)
			}
		}
	}
	if rechazados != 1 {
		t.Fatalf("rechazados = %d", rechazados)
	}
}

// TestUnRechazadoNoSePuedeForzar: las invariantes corren primero y no hay
// casilla que las salte.
func TestUnRechazadoNoSePuedeForzar(t *testing.T) {
	raw := archivoDeIntercambio(t, perfilCrudo("rust", "Rust", "3389"))
	cands, err := ParseCatalogFile(raw, Catalog{})
	if err != nil {
		t.Fatal(err)
	}
	if !cands[0].Rejected || cands[0].Suggested {
		t.Fatalf("un perfil rechazado vino marcado para importar: %+v", cands[0])
	}
}

// TestNadaSeSobreescribeEnSilencio, y con un verificado enfrente la casilla
// viene desmarcada: la confianza no se hereda ni se pisa sin querer.
func TestNadaSeSobreescribeEnSilencio(t *testing.T) {
	mio := perfil("valheim", "Valheim", OriginMine)
	mio.Verified = &Verified{Date: "2026-07-31", By: "alvaro", Method: "partida real"}
	actual := BuildCatalog([]GameProfile{mio, perfil("terraria", "Terraria", OriginMine)}, nil)

	raw := archivoDeIntercambio(t,
		perfilCrudo("valheim", "Valheim de Humberto", "2456-2458"),
		perfilCrudo("minecraft", "Minecraft", "25565"),
	)
	cands, err := ParseCatalogFile(raw, actual)
	if err != nil {
		t.Fatal(err)
	}

	for _, c := range cands {
		switch c.Profile.ID {
		case "valheim":
			if !c.Collides {
				t.Error("no detectó la colisión con el propio")
			}
			if c.Suggested {
				t.Error("propuso pisar un perfil verificado")
			}
			if c.Existing.Verified == nil {
				t.Error("no dice contra qué colisiona, la pantalla no puede explicar por qué")
			}
		case "minecraft":
			if c.Collides || !c.Suggested {
				t.Error("un perfil nuevo tenía que venir marcado")
			}
		}
	}
}

// TestColisiónContraUnoSinVerificarSíVieneMarcada: el default conserva lo
// probado, no lo viejo.
func TestColisiónContraUnoSinVerificarSíVieneMarcada(t *testing.T) {
	actual := BuildCatalog([]GameProfile{perfil("valheim", "Valheim", OriginMine)}, nil)
	raw := archivoDeIntercambio(t, perfilCrudo("valheim", "Valheim de Humberto", "2456-2458"))

	cands, err := ParseCatalogFile(raw, actual)
	if err != nil {
		t.Fatal(err)
	}
	if !cands[0].Collides || !cands[0].Suggested {
		t.Fatalf("una colisión contra uno sin verificar tenía que venir marcada: %+v", cands[0])
	}
}

func TestUnIdRepetidoDentroDelMismoArchivo(t *testing.T) {
	raw := archivoDeIntercambio(t,
		perfilCrudo("valheim", "Valheim", "2456"),
		perfilCrudo("valheim", "Valheim otra vez", "2457"),
	)
	cands, err := ParseCatalogFile(raw, Catalog{})
	if err != nil {
		t.Fatal(err)
	}
	if cands[0].Rejected || !cands[1].Rejected {
		t.Fatalf("el duplicado no se marcó: %+v", cands)
	}
}

func TestUnSobreDeOtraVersiónSeRechaza(t *testing.T) {
	if _, err := ParseCatalogFile([]byte(`{"kanpachi_catalog":9,"profiles":[]}`), Catalog{}); err == nil {
		t.Fatal("se aceptó un sobre de otra versión")
	}
	if _, err := ParseCatalogFile([]byte(`{"perfiles":[]}`), Catalog{}); err == nil {
		t.Fatal("se aceptó un archivo que no es un catálogo")
	}
}

// TestElSobreSíToleraCamposDesconocidos, al revés que un perfil. Son dos cosas
// distintas: un campo raro en el sobre es metadato de quien exportó y no llega
// a ninguna regla de firewall.
func TestElSobreSíToleraCamposDesconocidos(t *testing.T) {
	raw := []byte(`{"kanpachi_catalog":1,"exported_from":"una versión futura","profiles":[` +
		perfilCrudo("valheim", "Valheim", "2456") + `]}`)
	cands, err := ParseCatalogFile(raw, Catalog{})
	if err != nil {
		t.Fatalf("un campo nuevo en el sobre tumbó el archivo: %v", err)
	}
	if len(cands) != 1 || cands[0].Rejected {
		t.Fatalf("el perfil no entró: %+v", cands)
	}
}

// TestElArchivoLocalConservaElOrigenDeCadaPerfil: perderlo convertiría todo lo
// importado en propio en el siguiente arranque.
func TestElArchivoLocalConservaElOrigenDeCadaPerfil(t *testing.T) {
	raw, err := EncodeLocalCatalog([]GameProfile{
		perfil("valheim", "Valheim", OriginMine),
		perfil("terraria", "Terraria", OriginImported),
	}, nil, "2026-08-02T00:00:00Z", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}

	ps, bad, err := ParseCatalogLayer(raw, OriginMine)
	if err != nil {
		t.Fatal(err)
	}
	if len(bad) != 0 {
		t.Fatalf("rechazos al releer lo que se acaba de escribir: %+v", bad)
	}
	origenes := map[string]Origin{}
	for _, p := range ps {
		origenes[p.ID] = p.Origin
	}
	if origenes["valheim"] != OriginMine || origenes["terraria"] != OriginImported {
		t.Fatalf("los orígenes no sobrevivieron al guardado: %+v", origenes)
	}
}

// TestElBuiltinFuerzaSuCapaAunqueElArchivoDigaOtraCosa. Program Files es de
// solo lectura, pero si alguien lo edita no puede ascender un perfil a "mine".
func TestElBuiltinFuerzaSuCapaAunqueElArchivoDigaOtraCosa(t *testing.T) {
	raw := []byte(`{"kanpachi_catalog":1,"profiles":[{"id":"valheim","schema":2,"name":"Valheim",
	  "origin":"imported","host_ports":[{"proto":"udp","range":"2456"}],"client_ports":[],
	  "connect_hint":{"kind":"direct_ip","text_es":"x"}}]}`)
	ps, _, err := ParseCatalogLayer(raw, OriginBuiltin)
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 1 || ps[0].Origin != OriginBuiltin {
		t.Fatalf("el archivo cambió su propia capa: %+v", ps)
	}
}

// TestExportarNoLlevaElOrigen: quien reciba el archivo lo importa como
// "imported" sí o sí, y un campo que diga lo contrario sería una vía para
// colarse en la capa que gana.
func TestExportarNoLlevaElOrigen(t *testing.T) {
	raw, err := ExportCatalog([]GameProfile{perfil("valheim", "Valheim", OriginMine)},
		"2026-08-02T00:00:00Z", "alvaro", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"origin"`) {
		t.Fatalf("el archivo exportado lleva el origen dentro:\n%s", raw)
	}

	var f map[string]any
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	if f["exported_by"] != "alvaro" {
		t.Errorf("falta quién exportó: %v", f["exported_by"])
	}
}

// TestExportarYVolverAImportarCierraElCírculo, que es el flujo real de mandar
// el .json por Telegram.
func TestExportarYVolverAImportarCierraElCírculo(t *testing.T) {
	original := perfil("valheim", "Valheim", OriginMine)
	original.Verified = &Verified{Date: "2026-07-31", By: "alvaro", Method: "partida real"}
	original.Tweaks = SystemTweaks{MulticastRoute: true}
	original.LANDiscovery = true

	raw, err := ExportCatalog([]GameProfile{original}, "2026-08-02T00:00:00Z", "alvaro", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	cands, err := ParseCatalogFile(raw, Catalog{})
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 1 || cands[0].Rejected {
		t.Fatalf("no volvió: %+v", cands)
	}

	vuelto := cands[0].Profile
	if vuelto.Origin != OriginImported {
		t.Errorf("el que importa lo recibe como %s", vuelto.Origin)
	}
	// La confianza NO se hereda: el perfil queda como verificado por Humberto,
	// jamás como verificado por ti. Se conserva el autor, no se borra ni se
	// reescribe.
	if vuelto.Verified == nil || vuelto.Verified.By != "alvaro" {
		t.Errorf("se perdió quién verificó: %+v", vuelto.Verified)
	}
	if !vuelto.Tweaks.MulticastRoute || !vuelto.LANDiscovery {
		t.Errorf("se perdieron ajustes en el viaje: %+v", vuelto)
	}
}

// TestClientPortsSeEscribeAunqueEstéVacío: es el campo que declara la
// topología, y su ausencia se lee distinto que su vacío por quien revisa el
// archivo a ojo antes de importarlo.
func TestClientPortsSeEscribeAunqueEstéVacío(t *testing.T) {
	raw, err := ExportCatalog([]GameProfile{perfil("valheim", "Valheim", OriginMine)}, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"client_ports": []`) {
		t.Fatalf("client_ports no aparece vacío y explícito:\n%s", raw)
	}
}

// TestLaInvarianteOchoCorreEnLosCAMINOSREALESDeCarga.
//
// Es el hallazgo más caro de la auditoría. Hubo una versión en que el sobre
// decodificaba su lista de perfiles de una sola pasada, así que
// DisallowUnknownFields vivía en una función que ningún camino de producción
// llamaba: un perfil con `"run_command"` entraba por builtin.json, por
// local.json y por un archivo compartido, y el único sitio que lo rechazaba
// eran los tests.
func TestLaInvarianteOchoCorreEnLosCAMINOSREALESDeCarga(t *testing.T) {
	colado := `{"id":"colado","schema":2,"name":"Colado","run_command":"cmd.exe /c calc",
	  "host_ports":[{"proto":"udp","range":"1234"}],"client_ports":[],
	  "connect_hint":{"kind":"direct_ip","text_es":"x"}}`

	t.Run("al cargar una capa del disco", func(t *testing.T) {
		raw := archivoDeIntercambio(t, colado, perfilCrudo("valheim", "Valheim", "2456"))
		ok, bad, err := ParseCatalogLayer(raw, OriginBuiltin)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range ok {
			if p.ID == "colado" {
				t.Fatal("un perfil con un campo inventado entró al catálogo")
			}
		}
		if len(bad) != 1 || bad[0].ID != "colado" {
			t.Fatalf("rechazados = %+v", bad)
		}
		if len(ok) != 1 {
			t.Fatalf("el perfil bueno del mismo archivo no entró: %+v", ok)
		}
	})

	t.Run("al importar un archivo compartido", func(t *testing.T) {
		raw := archivoDeIntercambio(t, colado)
		cands, err := ParseCatalogFile(raw, Catalog{})
		if err != nil {
			t.Fatal(err)
		}
		if len(cands) != 1 || !cands[0].Rejected {
			t.Fatalf("se ofreció importar un perfil con un campo inventado: %+v", cands)
		}
		if cands[0].Profile.ID != "colado" {
			t.Errorf("el rechazo no dice de qué perfil es: %+v", cands[0])
		}
	})
}

// TestUnPerfilConUnTipoMalNoSeLlevaALosDemásPorDelante.
//
// Con la lista tipada en el sobre, un `"broadcast_route": "sí"` en el tercer
// perfil abortaba el Unmarshal del archivo entero: el usuario perdía los dos
// buenos al importar, y al arrancar se quedaba sin catálogo local.
func TestUnPerfilConUnTipoMalNoSeLlevaALosDemásPorDelante(t *testing.T) {
	roto := `{"id":"roto","schema":2,"name":"Roto",
	  "system_tweaks":{"broadcast_route":"sí"},
	  "host_ports":[{"proto":"udp","range":"1234"}],"client_ports":[],
	  "connect_hint":{"kind":"direct_ip","text_es":"x"}}`
	raw := archivoDeIntercambio(t,
		perfilCrudo("valheim", "Valheim", "2456"),
		roto,
		perfilCrudo("terraria", "Terraria", "7777"),
	)

	ok, bad, err := ParseCatalogLayer(raw, OriginMine)
	if err != nil {
		t.Fatalf("un tipo mal en un perfil tumbó el archivo entero: %v", err)
	}
	if len(ok) != 2 {
		t.Fatalf("entraron %d perfiles buenos de 2: %+v", len(ok), ok)
	}
	if len(bad) != 1 || bad[0].ID != "roto" {
		t.Fatalf("rechazados = %+v", bad)
	}
	if bad[0].Name != "Roto" {
		t.Errorf("el rechazo no se puede etiquetar en pantalla: %+v", bad[0])
	}
}

// TestElArchivoExportadoSigueSiendoLegible: se manda por Telegram y alguien lo
// abre para mirar qué puertos pide antes de importarlo.
func TestElArchivoExportadoSigueSiendoLegible(t *testing.T) {
	raw, err := ExportCatalog([]GameProfile{perfil("valheim", "Valheim", OriginMine)}, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	txt := string(raw)
	if !strings.Contains(txt, "\n      \"id\": \"valheim\"") {
		t.Fatalf("los perfiles salieron sin sangrar, en una sola línea:\n%s", txt)
	}
}

// TestGuardarNoLeBorraAlUsuarioUnPerfilRechazado.
//
// El catálogo se reescribe entero desde los perfiles válidos, así que sin
// conservar los bytes crudos el primer alta manual después de un rechazo se
// llevaba por delante el perfil rechazado. El usuario perdía un archivo suyo
// por un error que la pantalla le estaba mostrando como corregible.
func TestGuardarNoLeBorraAlUsuarioUnPerfilRechazado(t *testing.T) {
	roto := `{"id":"roto","schema":2,"name":"Roto","run_command":"calc",
	  "host_ports":[{"proto":"udp","range":"1234"}],"client_ports":[],
	  "connect_hint":{"kind":"direct_ip","text_es":"x"}}`
	original := archivoDeIntercambio(t, perfilCrudo("valheim", "Valheim", "2456"), roto)

	ok, bad, err := ParseCatalogLayer(original, OriginMine)
	if err != nil {
		t.Fatal(err)
	}
	c := BuildCatalog(ok, bad)
	if len(c.RejectedLocal()) != 1 {
		t.Fatalf("el rechazo local no conservó sus bytes: %+v", c.Rejected())
	}

	guardado, err := EncodeLocalCatalog(c.Local(), c.RejectedLocal(), "2026-08-02T00:00:00Z", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(guardado), `"roto"`) {
		t.Fatalf("guardar borró el perfil rechazado del archivo del usuario:\n%s", guardado)
	}

	// Y al releer sigue rechazado, no colado: conservarlo no es aceptarlo.
	ok2, bad2, err := ParseCatalogLayer(guardado, OriginMine)
	if err != nil {
		t.Fatal(err)
	}
	if len(ok2) != 1 || len(bad2) != 1 {
		t.Fatalf("tras el viaje de ida y vuelta: %d buenos, %d rechazados", len(ok2), len(bad2))
	}
}

// TestUnRechazadoDelBuiltinNoSeCopiaALocal: Program Files es de solo lectura y
// copiarlo crearía un perfil "mine" que tapa al builtin para siempre.
func TestUnRechazadoDelBuiltinNoSeCopiaALocal(t *testing.T) {
	roto := `{"id":"roto","schema":2,"name":"Roto","run_command":"calc",
	  "host_ports":[{"proto":"udp","range":"1234"}],"client_ports":[],
	  "connect_hint":{"kind":"direct_ip","text_es":"x"}}`
	_, bad, err := ParseCatalogLayer(archivoDeIntercambio(t, roto), OriginBuiltin)
	if err != nil {
		t.Fatal(err)
	}
	c := BuildCatalog(nil, bad)
	if len(c.RejectedLocal()) != 0 {
		t.Fatalf("un rechazo del builtin se iba a escribir en local.json: %+v", c.RejectedLocal())
	}
}
