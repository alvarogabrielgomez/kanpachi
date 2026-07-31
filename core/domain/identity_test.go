package domain

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

// TestAlfabetoTieneExactamente32Simbolos cuida la propiedad de la que cuelga
// el modelo de seguridad entero. Si alguien agrega o quita un símbolo, la
// entropía deja de ser 60 bits y el muestreo de NewCode deja de ser uniforme,
// las dos cosas en silencio. Este test hace ruido.
func TestAlfabetoTieneExactamente32Simbolos(t *testing.T) {
	if got := len(Alphabet); got != 32 {
		t.Fatalf("el alfabeto tiene %d símbolos, deben ser exactamente 32: "+
			"12 caracteres × 5 bits = 60 bits de entropía", got)
	}
	if 256%len(Alphabet) != 0 {
		t.Fatalf("256 no es múltiplo de %d, el enmascarado de NewCode queda sesgado", len(Alphabet))
	}
	for _, prohibido := range []rune{'0', 'O', '1', 'I'} {
		if strings.ContainsRune(Alphabet, prohibido) {
			t.Errorf("el alfabeto no debe contener %q: se excluyen los dos miembros de cada par confuso", prohibido)
		}
	}
	if !strings.ContainsRune(Alphabet, 'L') {
		t.Error("la L se conserva: sin el 1 presente, en mayúsculas no se confunde con nada")
	}
	visto := map[rune]bool{}
	for _, r := range Alphabet {
		if visto[r] {
			t.Errorf("símbolo repetido en el alfabeto: %q", r)
		}
		visto[r] = true
	}
}

func TestNewCodeProduceCodigoValido(t *testing.T) {
	// Lector determinista: 0,1,2,... para que el mapeo sea comprobable a mano.
	src := make([]byte, CodeLen)
	for i := range src {
		src[i] = byte(i)
	}
	c, err := NewCode(bytes.NewReader(src))
	if err != nil {
		t.Fatalf("NewCode devolvió error: %v", err)
	}
	if len(c.Raw()) != CodeLen {
		t.Fatalf("Raw() mide %d, se esperaban %d", len(c.Raw()), CodeLen)
	}
	// byte i produce Alphabet[i & 31], o sea Alphabet[0..11] para 0..11.
	if want := Alphabet[:CodeLen]; c.Raw() != want {
		t.Errorf("Raw() = %q, se esperaba %q", c.Raw(), want)
	}
	if got, want := c.String(), "2345-6789-ABCD"; got != want {
		t.Errorf("String() = %q, se esperaba %q", got, want)
	}
	if _, err := ParseCode(c.String()); err != nil {
		t.Errorf("el código generado no se puede volver a parsear: %v", err)
	}
}

func TestNewCodeFallaSinSuficienteAleatoriedad(t *testing.T) {
	if _, err := NewCode(bytes.NewReader([]byte{1, 2, 3})); err == nil {
		t.Fatal("se esperaba error con menos de 12 bytes disponibles")
	}
}

func TestParseCodeEsTolerante(t *testing.T) {
	const canonico = "KANP7X4MB2QF"
	casos := []struct {
		nombre  string
		entrada string
	}{
		{"canónico sin guiones", "KANP7X4MB2QF"},
		{"con guiones", "KANP-7X4M-B2QF"},
		{"minúsculas", "kanp7x4mb2qf"},
		{"minúsculas con guiones", "kanp-7x4m-b2qf"},
		{"con espacios", "KANP 7X4M B2QF"},
		{"con guiones bajos", "KANP_7X4M_B2QF"},
		{"mezcla", "  Kanp-7x4M b2QF  "},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			got, err := ParseCode(c.entrada)
			if err != nil {
				t.Fatalf("ParseCode(%q) falló: %v", c.entrada, err)
			}
			if got.Raw() != canonico {
				t.Errorf("ParseCode(%q).Raw() = %q, se esperaba %q", c.entrada, got.Raw(), canonico)
			}
		})
	}
}

func TestParseCodeRechaza(t *testing.T) {
	casos := []struct {
		nombre  string
		entrada string
		wantErr error
	}{
		{"vacío", "", ErrCodeLength},
		{"corto", "KANP7X4M", ErrCodeLength},
		{"largo", "KANP7X4MB2QFX", ErrCodeLength},
		{"con cero", "0ANP7X4MB2QF", ErrCodeSymbol},
		{"con O", "OANP7X4MB2QF", ErrCodeSymbol},
		{"con uno", "1ANP7X4MB2QF", ErrCodeSymbol},
		{"con I", "IANP7X4MB2QF", ErrCodeSymbol},
		{"con símbolo", "KANP7X4MB2Q!", ErrCodeSymbol},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			_, err := ParseCode(c.entrada)
			if err == nil {
				t.Fatalf("ParseCode(%q) debió fallar", c.entrada)
			}
			if !strings.Contains(err.Error(), c.wantErr.Error()) {
				t.Errorf("ParseCode(%q) dio %v, se esperaba que envolviera %v", c.entrada, err, c.wantErr)
			}
		})
	}
}

// TestParseRoomAceptaLasSeisFormas es la tabla de docs/03-arquitectura.md.
// Las seis producen el mismo código, que es la promesa de "el usuario nunca
// tiene que saber cuál es la correcta".
func TestParseRoomAceptaLasSeisFormas(t *testing.T) {
	const canonico = "KANP7X4MB2QF"
	casos := []struct {
		entrada  string
		wantSeed string
	}{
		{"KANP7X4MB2QF", DefaultSeedHost},
		{"kanp-7x4m-b2qf", DefaultSeedHost},
		{"kanpachi://KANP-7X4M-B2QF", DefaultSeedHost},
		{"KANP-7X4M-B2QF@seed.midominio.com", "seed.midominio.com"},
		{"kanpachi.accentio.dev/#KANP-7X4M-B2QF", "kanpachi.accentio.dev"},
		{"https://kanpachi.accentio.dev/#KANP-7X4M-B2QF", "kanpachi.accentio.dev"},
	}
	for _, c := range casos {
		t.Run(c.entrada, func(t *testing.T) {
			r, err := ParseRoom(c.entrada)
			if err != nil {
				t.Fatalf("ParseRoom(%q) falló: %v", c.entrada, err)
			}
			if r.Code.Raw() != canonico {
				t.Errorf("código = %q, se esperaba %q", r.Code.Raw(), canonico)
			}
			if r.Seed != c.wantSeed {
				t.Errorf("seed = %q, se esperaba %q", r.Seed, c.wantSeed)
			}
		})
	}
}

// TestParseRoomRechazaEntradaHostil cubre el canal kanpachi://, que queda
// expuesto a toda la web. Nada de esto debe interpretarse.
func TestParseRoomRechazaEntradaHostil(t *testing.T) {
	casos := []struct{ nombre, entrada string }{
		{"vacío", ""},
		{"solo espacios", "   "},
		{"demasiado largo", "kanpachi://" + strings.Repeat("A", MaxInputLen)},
		{"ruta de archivo", "kanpachi://C:/Windows/System32/calc.exe"},
		{"ruta unix", "kanpachi:///etc/passwd"},
		{"argumentos", "kanpachi://KANP-7X4M-B2QF --console"},
		{"query string", "kanpachi.accentio.dev/#KANP-7X4M-B2QF?x=1"},
		{"ruta extra", "kanpachi.accentio.dev/algo/#KANP-7X4M-B2QF"},
		{"arroba y barra a la vez", "KANP-7X4M-B2QF@host.com/#OTRO"},
		{"host sin punto", "KANP-7X4M-B2QF@localhost"},
		{"host vacío", "KANP-7X4M-B2QF@"},
		{"dos arrobas", "KANP-7X4M-B2QF@a.com@b.com"},
		{"host con guion al inicio", "KANP-7X4M-B2QF@-mal.com"},
		{"host con carácter raro", "KANP-7X4M-B2QF@ma l.com"},
		{"etiqueta vacía", "KANP-7X4M-B2QF@a..com"},
		{"solo el host", "kanpachi.accentio.dev/#"},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			if r, err := ParseRoom(c.entrada); err == nil {
				t.Errorf("ParseRoom(%q) debió fallar, devolvió %+v", c.entrada, r)
			}
		})
	}
}

func TestParseRoomTopeDeLongitudSeAplicaAntesDeParsear(t *testing.T) {
	entrada := strings.Repeat("A", MaxInputLen+1)
	_, err := ParseRoom(entrada)
	if err == nil {
		t.Fatal("se esperaba error por longitud")
	}
	if !strings.Contains(err.Error(), ErrInputTooLong.Error()) {
		t.Errorf("el error fue %v, se esperaba el de longitud: el tope va antes de todo lo demás", err)
	}
}

func TestInviteURLSeVuelveAParsear(t *testing.T) {
	r, err := ParseRoom("kanp-7x4m-b2qf")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	url := r.InviteURL()
	if want := "kanpachi.accentio.dev/#KANP-7X4M-B2QF"; url != want {
		t.Fatalf("InviteURL() = %q, se esperaba %q", url, want)
	}
	back, err := ParseRoom(url)
	if err != nil {
		t.Fatalf("la URL generada no se puede volver a parsear: %v", err)
	}
	if back.Code.Raw() != r.Code.Raw() || back.Seed != r.Seed {
		t.Errorf("ida y vuelta cambió la sala: %+v contra %+v", back, r)
	}
}

// TestDeriveIdentityVectorDorado congela los parámetros de Argon2id y los
// salts. Si alguien los toca, dos clientes con el mismo código dejan de verse,
// con un síntoma que en producción sería imposible de diagnosticar: "pegué el
// mismo código y estoy solo en la sala".
func TestDeriveIdentityVectorDorado(t *testing.T) {
	c, err := ParseCode("KANP-7X4M-B2QF")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	id := DeriveIdentity(c)

	// El networkID es opaco y va en claro al seed, así que se fija tal cual.
	const wantName = "kanpachi-c74a27b8f9825b1159545b4a35bd51e6"
	if got := id.NetworkName(); got != wantName {
		t.Errorf("NetworkName() = %q\n  se esperaba %q\n"+
			"  si este cambio es intencional, los clientes viejos dejan de conectar: usa un salt v2", got, wantName)
	}

	// Del secreto se fija su SHA-256, no su valor. Congela los parámetros
	// igual de bien, y evita dejar algo con forma de secreto escrito en el
	// repositorio, que es un mal precedente para un proyecto cuyo argumento
	// central es que los secretos no viajan ni se guardan donde no deben.
	const wantSecretHash = "e6c9379db3dfe88d1bc043913b3f8cfe914c5a8919f1c2637dbaa9b493f22bb2"
	sum := sha256.Sum256([]byte(id.EngineSecret()))
	if got := hex.EncodeToString(sum[:]); got != wantSecretHash {
		t.Errorf("SHA-256 del secreto = %q, se esperaba %q\n"+
			"  mismo problema que arriba: cambiar los parámetros parte la compatibilidad", got, wantSecretHash)
	}
}

func TestDeriveIdentityEsDeterministaYDependeDelCodigo(t *testing.T) {
	a, _ := ParseCode("KANP-7X4M-B2QF")
	b, _ := ParseCode("kanp 7x4m b2qf") // el mismo, escrito distinto
	d, _ := ParseCode("KANP-7X4M-B2QG") // un carácter distinto

	if DeriveIdentity(a).NetworkName() != DeriveIdentity(b).NetworkName() {
		t.Error("la forma en que se escribe el código no puede cambiar la red")
	}
	if DeriveIdentity(a).EngineSecret() != DeriveIdentity(b).EngineSecret() {
		t.Error("la forma en que se escribe el código no puede cambiar el secreto")
	}
	if DeriveIdentity(a).NetworkName() == DeriveIdentity(d).NetworkName() {
		t.Error("códigos distintos deben dar redes distintas")
	}
}

func TestDeriveIdentitySeparaIDDeSecreto(t *testing.T) {
	c, _ := ParseCode("KANP-7X4M-B2QF")
	id := DeriveIdentity(c)
	// Los salts versionados existen para que un networkID nunca pueda
	// coincidir con un secret aunque el código sea el mismo.
	if strings.Contains(id.EngineSecret(), strings.TrimPrefix(id.NetworkName(), "kanpachi-")) {
		t.Error("el secreto contiene al networkID: los salts no están separando las derivaciones")
	}
}

// TestNetworkIdentityNoFiltraElSecretoAlLoguear protege una fuga real: los
// logs son locales y el botón de diagnóstico los copia al portapapeles para
// pegarlos en el grupo. Un %v descuidado publicaría el secreto de la sala.
func TestNetworkIdentityNoFiltraElSecretoAlLoguear(t *testing.T) {
	c, _ := ParseCode("KANP-7X4M-B2QF")
	id := DeriveIdentity(c)
	rendered := id.String()
	if strings.Contains(rendered, id.EngineSecret()) {
		t.Fatal("String() expone el secreto")
	}
	if !strings.Contains(rendered, "REDACTADO") {
		t.Errorf("String() = %q, debería marcar el secreto como redactado", rendered)
	}
}

func TestParseNickname(t *testing.T) {
	buenos := []string{"Alvaro", "santiago", "Victor99", "a", "AAAAAAAAAAAA"}
	for _, s := range buenos {
		if _, err := ParseNickname(s); err != nil {
			t.Errorf("ParseNickname(%q) debió pasar: %v", s, err)
		}
	}
	malos := []struct{ nombre, entrada string }{
		{"vacío", ""},
		{"trece caracteres", "AAAAAAAAAAAAA"},
		{"con espacio", "Alvaro G"},
		{"con guion", "alvaro-g"},
		{"con acento", "Álvaro"},
		{"cirílico que imita latín", "Аlvaro"},
		{"salto de línea", "alvaro\n"},
		{"carácter invisible", "alva\u200bro"},
		{"emoji", "alvaro🎮"},
	}
	for _, c := range malos {
		t.Run(c.nombre, func(t *testing.T) {
			if _, err := ParseNickname(c.entrada); err == nil {
				t.Errorf("ParseNickname(%q) debió fallar", c.entrada)
			}
		})
	}
}
