package domain

import (
	"errors"
	"strings"
	"testing"
)

// perfilValido es el mínimo que pasa las invariantes. Los tests lo mutan en un
// solo campo cada uno, que es lo que hace que un fallo señale la invariante
// exacta y no "el perfil está mal".
func perfilValido() GameProfile {
	return GameProfile{
		ID:        "project-zomboid",
		Name:      "Project Zomboid",
		Origin:    OriginBuiltin,
		HostPorts: []PortRange{{Proto: ProtoUDP, From: 16261, To: 16262}},
		Connect:   ConnectHint{Kind: ConnectDirectIP, TextES: "Join, escribe la IP del host"},
	}
}

// TestPuertosProhibidosSeRechazanAunqueVenganDentroDeUnRango es la invariante
// que más caro sale romper.
//
// El caso que importa no es pedir el 445 por su nombre, que es lo que se
// prueba solo: es pedir 440-450, que lo abre igual y no lo nombra. Quien
// escriba un perfil malicioso lo escribiría exactamente así.
func TestPuertosProhibidosSeRechazanAunqueVenganDentroDeUnRango(t *testing.T) {
	casos := []struct {
		nombre string
		spec   string
	}{
		{"el puerto pelado", "445"},
		{"un rango que lo contiene", "440-450"},
		{"el rango entero de NetBIOS", "137-139"},
		{"SSH", "22"},
		{"escritorio remoto", "3389"},
		{"WinRM", "5985-5986"},
		{"un rango absurdo que se lleva todo por delante", "1-65535"},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			_, err := ParsePortRange(ProtoTCP, c.spec)
			if !errors.Is(err, ErrPortForbidden) {
				t.Fatalf("ParsePortRange(%q) = %v, se esperaba ErrPortForbidden", c.spec, err)
			}
		})
	}
}

func TestRangosDePuertosLegítimos(t *testing.T) {
	casos := []struct {
		spec       string
		desde      uint16
		hasta      uint16
		vuelveComo string
	}{
		{"27015", 27015, 27015, "27015"},
		{"2456-2458", 2456, 2458, "2456-2458"},
		{" 16261 ", 16261, 16261, "16261"},
		{"16261-16261", 16261, 16261, "16261"},
	}
	for _, c := range casos {
		r, err := ParsePortRange(ProtoUDP, c.spec)
		if err != nil {
			t.Fatalf("ParsePortRange(%q) falló: %v", c.spec, err)
		}
		if r.From != c.desde || r.To != c.hasta {
			t.Errorf("ParsePortRange(%q) = %d-%d, se esperaba %d-%d", c.spec, r.From, r.To, c.desde, c.hasta)
		}
		if got := r.Spec(); got != c.vuelveComo {
			t.Errorf("Spec() de %q = %q, se esperaba %q", c.spec, got, c.vuelveComo)
		}
	}
}

func TestRangosDePuertosMalEscritos(t *testing.T) {
	casos := []struct {
		spec   string
		quiero error
	}{
		{"", ErrPortRangeShape},
		{"27015-", ErrPortRangeShape},
		{"-27015", ErrPortRangeShape},
		{"abc", ErrPortRangeShape},
		{"27015 27016", ErrPortRangeShape},
		{"70000", ErrPortRangeShape}, // no cabe en uint16
		{"0", ErrPortZero},
		{"2458-2456", ErrPortRangeOrder},
	}
	for _, c := range casos {
		if _, err := ParsePortRange(ProtoTCP, c.spec); !errors.Is(err, c.quiero) {
			t.Errorf("ParsePortRange(%q) = %v, se esperaba %v", c.spec, err, c.quiero)
		}
	}
}

// TestTopeDeRangosCuentaHostYClienteJuntos: el tope protege de que un archivo
// compartido genere cien reglas de firewall, y contarlos por lista lo dejaría
// en dieciséis por la puerta de atrás.
func TestTopeDeRangosCuentaHostYClienteJuntos(t *testing.T) {
	p := perfilValido()
	p.HostPorts = nil
	p.ClientPorts = nil
	for i := 0; i < 5; i++ {
		p.HostPorts = append(p.HostPorts, PortRange{Proto: ProtoUDP, From: uint16(30000 + i), To: uint16(30000 + i)})
		p.ClientPorts = append(p.ClientPorts, PortRange{Proto: ProtoTCP, From: uint16(40000 + i), To: uint16(40000 + i)})
	}
	if _, err := NewGameProfile(p); !errors.Is(err, ErrTooManyRanges) {
		t.Fatalf("un perfil con 10 rangos repartidos entre las dos listas fue aceptado: %v", err)
	}
}

func TestPerfilesMalFormados(t *testing.T) {
	casos := []struct {
		nombre string
		muta   func(*GameProfile)
		quiero error
	}{
		{"sin id", func(p *GameProfile) { p.ID = "" }, ErrProfileID},
		{"id con mayúsculas", func(p *GameProfile) { p.ID = "Project-Zomboid" }, ErrProfileID},
		{"id con espacios", func(p *GameProfile) { p.ID = "project zomboid" }, ErrProfileID},
		{"sin nombre", func(p *GameProfile) { p.Name = "  " }, ErrProfileName},
		{"sin puertos", func(p *GameProfile) { p.HostPorts = nil }, ErrProfileNoPorts},
		{"sin forma de conectarse", func(p *GameProfile) { p.Connect.Kind = 0 }, ErrConnectHintKind},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			p := perfilValido()
			c.muta(&p)
			if _, err := NewGameProfile(p); !errors.Is(err, c.quiero) {
				t.Fatalf("NewGameProfile = %v, se esperaba %v", err, c.quiero)
			}
		})
	}
}

const perfilJSON = `{
  "id": "project-zomboid",
  "schema": 2,
  "name": "Project Zomboid",
  "detect": { "steam_appid": 108600, "executables": ["ProjectZomboid64.exe"] },
  "host_ports":   [{ "proto": "udp", "range": "16261-16262" }],
  "client_ports": [],
  "lan_discovery": false,
  "system_tweaks": { "broadcast_route": false, "multicast_route": false, "prefer_ipv4": false, "directplay": false },
  "connect_hint": { "kind": "direct_ip", "text_es": "En el juego: Join > escribe la IP del host" },
  "bind_hint": { "file": "Zomboid/Server/servertest.ini", "key": "ServerIP", "note_es": "se puede fijar" },
  "verified": { "date": "2026-07-31", "by": "alvaro", "method": "partida real, 4 jugadores", "game_version": "41.78" }
}`

func TestParseGameProfileLeeElEsquemaDelDocumento(t *testing.T) {
	p, err := ParseGameProfile([]byte(perfilJSON), OriginBuiltin)
	if err != nil {
		t.Fatalf("el perfil de ejemplo de 06-catalogo.md no se pudo leer: %v", err)
	}
	if p.ID != "project-zomboid" || p.Detect.SteamAppID != 108600 {
		t.Errorf("perfil mal leído: %+v", p)
	}
	if len(p.HostPorts) != 1 || p.HostPorts[0].Proto != ProtoUDP || p.HostPorts[0].To != 16262 {
		t.Errorf("host_ports mal leídos: %+v", p.HostPorts)
	}
	if p.IsMesh() {
		t.Error("client_ports vacío tiene que ser estrella, no malla")
	}
	if p.Verified == nil || p.Verified.By != "alvaro" {
		t.Errorf("verified mal leído: %+v", p.Verified)
	}
}

// TestUnCampoDesconocidoRechazaElPerfilEntero es la invariante 8.
//
// Ignorar la clave sería cargar un archivo distinto del que alguien escribió,
// y la clave que se ignoraría es justamente la que pide algo que esta versión
// no sabe evaluar.
func TestUnCampoDesconocidoRechazaElPerfilEntero(t *testing.T) {
	casos := []string{
		`{"id":"x","schema":2,"name":"X","run_command":"cmd.exe /c calc",
		  "host_ports":[{"proto":"udp","range":"1234"}],"connect_hint":{"kind":"direct_ip"}}`,
		`{"id":"x","schema":2,"name":"X",
		  "system_tweaks":{"broadcast_route":true,"enable_file_sharing":true},
		  "host_ports":[{"proto":"udp","range":"1234"}],"connect_hint":{"kind":"direct_ip"}}`,
	}
	for _, raw := range casos {
		if _, err := ParseGameProfile([]byte(raw), OriginImported); !errors.Is(err, ErrUnknownField) {
			t.Errorf("un perfil con un campo inventado fue aceptado: %v", err)
		}
	}
}

// TestElEsquemaViejoYElNuevoSeDistinguen.
//
// La regla 6 del importador los trata distinto y la UI dice cosas opuestas:
// uno pide actualizar Kanpachi, el otro pide una migración. Mandar a alguien a
// actualizar por un perfil viejo lo manda a buscar el fallo al otro lado.
func TestElEsquemaViejoYElNuevoSeDistinguen(t *testing.T) {
	nuevo := strings.Replace(perfilJSON, `"schema": 2`, `"schema": 9`, 1)
	if _, err := ParseGameProfile([]byte(nuevo), OriginBuiltin); !errors.Is(err, ErrProfileSchemaNewer) {
		t.Fatalf("schema 9: %v", err)
	}
	viejo := strings.Replace(perfilJSON, `"schema": 2`, `"schema": 1`, 1)
	err := ParseAndDiscard(viejo)
	if !errors.Is(err, ErrProfileSchemaOlder) {
		t.Fatalf("schema 1: %v", err)
	}
	if strings.Contains(humanReason(err), "más nueva") {
		t.Fatalf("a un perfil viejo se le dice que es de una versión más nueva: %q", humanReason(err))
	}
}

// ParseAndDiscard existe solo para leer el error.
func ParseAndDiscard(raw string) error {
	_, err := ParseGameProfile([]byte(raw), OriginBuiltin)
	return err
}

// TestElOrigenLoFijaQuienCargaYNoElArchivo cierra la puerta a que un .json
// compartido se declare "mine" y le gane en precedencia a un builtin
// verificado.
func TestElOrigenLoFijaQuienCargaYNoElArchivo(t *testing.T) {
	raw := strings.Replace(perfilJSON, `"schema": 2`, `"schema": 2, "origin": "mine"`, 1)
	p, err := ParseGameProfile([]byte(raw), OriginImported)
	if err != nil {
		t.Fatalf("no se pudo leer: %v", err)
	}
	if p.Origin != OriginImported {
		t.Fatalf("el archivo consiguió declararse %s", p.Origin)
	}
}

func TestProtocolos(t *testing.T) {
	for texto, quiero := range map[string]Proto{
		"tcp": ProtoTCP, "UDP": ProtoUDP, "both": ProtoBoth, "tcp/udp": ProtoBoth,
	} {
		if got, err := ParseProto(texto); err != nil || got != quiero {
			t.Errorf("ParseProto(%q) = %v, %v", texto, got, err)
		}
	}
	if _, err := ParseProto("icmp"); !errors.Is(err, ErrProto) {
		t.Error("ParseProto aceptó icmp")
	}
}

// TestSystemTweaksEsUnConjuntoCerrado: no hay forma de expresar "ejecuta este
// comando" ni "habilita este grupo del firewall", y la prueba de que no la hay
// es que el tipo tiene exactamente cuatro booleanos.
func TestSystemTweaksEsUnConjuntoCerrado(t *testing.T) {
	var t0 SystemTweaks
	if t0.Any() {
		t.Error("el cero de SystemTweaks tiene que no pedir nada")
	}
	t0.MulticastRoute = true
	if !t0.Any() {
		t.Error("Any() no vio el ajuste encendido")
	}
}

// TestNoSeAceptaContenidoDetrásDelPerfil.
//
// El decoder se detiene al cerrar el primer objeto, así que un archivo con
// `{perfil bueno}{perfil malo}` pasaría entero mostrando solo el primero. Es la
// forma clásica de colar contenido que el revisor humano no ve.
func TestNoSeAceptaContenidoDetrásDelPerfil(t *testing.T) {
	raw := perfilJSON + `{"id":"colado","schema":2,"name":"Colado"}`
	if _, err := ParseGameProfile([]byte(raw), OriginImported); !errors.Is(err, ErrUnknownField) {
		t.Fatalf("se aceptó un segundo objeto pegado detrás: %v", err)
	}
}
