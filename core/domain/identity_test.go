package domain

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

// TestAlfabetoTieneExactamente32Simbolos cuida dos propiedades que se rompen en
// silencio. Si alguien agrega o quita un símbolo, el muestreo de NewInviteID
// deja de ser uniforme y la entropía deja de ser 40 bits. Este test hace ruido.
func TestAlfabetoTieneExactamente32Simbolos(t *testing.T) {
	if got := len(Alphabet); got != 32 {
		t.Fatalf("el alfabeto tiene %d símbolos, deben ser exactamente 32: "+
			"%d caracteres × 5 bits = %d bits de entropía", got, InviteIDLen, InviteIDLen*5)
	}
	if 256%len(Alphabet) != 0 {
		t.Fatalf("256 no es múltiplo de %d, el enmascarado de NewInviteID queda sesgado", len(Alphabet))
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

func TestNewInviteIDProduceIDValido(t *testing.T) {
	// Lector determinista: 0,1,2,... para que el mapeo sea comprobable a mano.
	src := make([]byte, InviteIDLen)
	for i := range src {
		src[i] = byte(i)
	}
	id, err := NewInviteID(bytes.NewReader(src))
	if err != nil {
		t.Fatalf("NewInviteID devolvió error: %v", err)
	}
	if len(id.Raw()) != InviteIDLen {
		t.Fatalf("Raw() mide %d, se esperaban %d", len(id.Raw()), InviteIDLen)
	}
	// byte i produce Alphabet[i & 31], o sea Alphabet[0..7] para 0..7.
	if want := Alphabet[:InviteIDLen]; id.Raw() != want {
		t.Errorf("Raw() = %q, se esperaba %q", id.Raw(), want)
	}
	if got, want := id.String(), "2345-6789"; got != want {
		t.Errorf("String() = %q, se esperaba %q", got, want)
	}
	if _, err := ParseInviteID(id.String()); err != nil {
		t.Errorf("el ID generado no se puede volver a parsear: %v", err)
	}
}

func TestNewInviteIDFallaSinSuficienteAleatoriedad(t *testing.T) {
	if _, err := NewInviteID(bytes.NewReader([]byte{1, 2, 3})); err == nil {
		t.Fatalf("se esperaba error con menos de %d bytes disponibles", InviteIDLen)
	}
}

func TestParseInviteIDEsTolerante(t *testing.T) {
	const canonico = "A7K2M9QX"
	casos := []struct {
		nombre  string
		entrada string
	}{
		{"canónico sin guiones", "A7K2M9QX"},
		{"con guion", "A7K2-M9QX"},
		{"minúsculas", "a7k2m9qx"},
		{"minúsculas con guion", "a7k2-m9qx"},
		{"con espacio", "A7K2 M9QX"},
		{"con guion bajo", "A7K2_M9QX"},
		{"mezcla", "  A7k2-m9QX  "},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			got, err := ParseInviteID(c.entrada)
			if err != nil {
				t.Fatalf("ParseInviteID(%q) falló: %v", c.entrada, err)
			}
			if got.Raw() != canonico {
				t.Errorf("ParseInviteID(%q).Raw() = %q, se esperaba %q", c.entrada, got.Raw(), canonico)
			}
		})
	}
}

func TestParseInviteIDRechaza(t *testing.T) {
	casos := []struct {
		nombre  string
		entrada string
		wantErr error
	}{
		{"vacío", "", ErrInviteIDLength},
		{"corto", "A7K2M9Q", ErrInviteIDLength},
		{"largo", "A7K2M9QXY", ErrInviteIDLength},
		{"el largo viejo de 12", "KANP7X4MB2QF", ErrInviteIDLength},
		{"con cero", "07K2M9QX", ErrInviteIDSymbol},
		{"con O", "O7K2M9QX", ErrInviteIDSymbol},
		{"con uno", "17K2M9QX", ErrInviteIDSymbol},
		{"con I", "I7K2M9QX", ErrInviteIDSymbol},
		{"con símbolo", "A7K2M9Q!", ErrInviteIDSymbol},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			_, err := ParseInviteID(c.entrada)
			if err == nil {
				t.Fatalf("ParseInviteID(%q) debió fallar", c.entrada)
			}
			if !strings.Contains(err.Error(), c.wantErr.Error()) {
				t.Errorf("ParseInviteID(%q) dio %v, se esperaba que envolviera %v", c.entrada, err, c.wantErr)
			}
		})
	}
}

// TestParseRoomAceptaLasSeisFormas es la tabla de docs/03-arquitectura.md.
// Las seis producen el mismo invite ID, que es la promesa de "el usuario nunca
// tiene que saber cuál es la correcta".
func TestParseRoomAceptaLasSeisFormas(t *testing.T) {
	const canonico = "A7K2M9QX"
	casos := []struct {
		entrada  string
		wantSeed string
	}{
		{"A7K2M9QX", DefaultSeedHost},
		{"a7k2-m9qx", DefaultSeedHost},
		{"kanpachi://A7K2-M9QX", DefaultSeedHost},
		{"A7K2-M9QX@seed.midominio.com", "seed.midominio.com"},
		{"kanpachi.accentio.dev/A7K2-M9QX", "kanpachi.accentio.dev"},
		{"https://kanpachi.accentio.dev/A7K2-M9QX", "kanpachi.accentio.dev"},
	}
	for _, c := range casos {
		t.Run(c.entrada, func(t *testing.T) {
			r, err := ParseRoom(c.entrada)
			if err != nil {
				t.Fatalf("ParseRoom(%q) falló: %v", c.entrada, err)
			}
			if r.InviteID.Raw() != canonico {
				t.Errorf("invite ID = %q, se esperaba %q", r.InviteID.Raw(), canonico)
			}
			if r.Seed != c.wantSeed {
				t.Errorf("seed = %q, se esperaba %q", r.Seed, c.wantSeed)
			}
		})
	}
}

// TestParseRoomDescartaLaClaveDeTarjeta cubre el caso real de uso: el usuario
// copia el enlace ENTERO que le llegó y lo pega en la app. Ese enlace trae la
// clave con que la página descifra la tarjeta de presentación, y a la app no le
// sirve para nada. Rechazar la entrada sería castigar al usuario por pegar lo
// que recibió.
func TestParseRoomDescartaLaClaveDeTarjeta(t *testing.T) {
	casos := []string{
		"kanpachi.accentio.dev/A7K2-M9QX#k7Rm2xQv",
		"https://kanpachi.accentio.dev/A7K2M9QX#k7Rm2xQv",
		"kanpachi.accentio.dev/A7K2M9QX#",
	}
	for _, entrada := range casos {
		t.Run(entrada, func(t *testing.T) {
			r, err := ParseRoom(entrada)
			if err != nil {
				t.Fatalf("ParseRoom(%q) falló: %v", entrada, err)
			}
			if r.InviteID.Raw() != "A7K2M9QX" {
				t.Errorf("invite ID = %q, se esperaba A7K2M9QX", r.InviteID.Raw())
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
		{"argumentos", "kanpachi://A7K2-M9QX --console"},
		{"query string", "kanpachi.accentio.dev/A7K2-M9QX?x=1"},
		{"ruta extra", "kanpachi.accentio.dev/algo/A7K2-M9QX"},
		{"arroba y barra a la vez", "A7K2-M9QX@host.com/OTRO"},
		{"host sin punto", "A7K2-M9QX@localhost"},
		{"host vacío", "A7K2-M9QX@"},
		{"dos arrobas", "A7K2-M9QX@a.com@b.com"},
		{"host con guion al inicio", "A7K2-M9QX@-mal.com"},
		{"host con carácter raro", "A7K2-M9QX@ma l.com"},
		{"etiqueta vacía", "A7K2-M9QX@a..com"},
		{"solo el host", "kanpachi.accentio.dev/"},
		{"la forma vieja con fragmento", "kanpachi.accentio.dev/#A7K2-M9QX"},
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
	r, err := ParseRoom("a7k2-m9qx")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	url := r.InviteURL()
	if want := "kanpachi.accentio.dev/A7K2-M9QX"; url != want {
		t.Fatalf("InviteURL() = %q, se esperaba %q", url, want)
	}
	back, err := ParseRoom(url)
	if err != nil {
		t.Fatalf("la URL generada no se puede volver a parsear: %v", err)
	}
	if back.InviteID.Raw() != r.InviteID.Raw() || back.Seed != r.Seed {
		t.Errorf("ida y vuelta cambió la sala: %+v contra %+v", back, r)
	}
}

// TestDeriveRendezvousVectorDorado congela los parámetros de Argon2id y los
// salts. Si alguien los toca, dos clientes con el mismo invite ID derivan redes
// de encuentro distintas y dejan de verse, con un síntoma que en producción
// sería imposible de diagnosticar: "pegué el mismo código y estoy solo".
func TestDeriveRendezvousVectorDorado(t *testing.T) {
	id, err := ParseInviteID("A7K2-M9QX")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	rdv := DeriveRendezvous(id)

	// El networkID de encuentro es lo que ve el seed, así que se fija tal cual.
	const wantName = "kanpachi-92ec779e312e4e165e0a23dbb6fc1dcd"
	if got := rdv.NetworkName(); got != wantName {
		t.Errorf("NetworkName() = %q\n  se esperaba %q\n"+
			"  si este cambio es intencional, los clientes viejos dejan de conectar: usa un salt v2", got, wantName)
	}

	// Del secreto se fija su SHA-256, no su valor. Congela los parámetros igual
	// de bien, y evita dejar algo con forma de secreto escrito en el
	// repositorio, que es un mal precedente para un proyecto cuyo argumento
	// central es que los secretos no viajan ni se guardan donde no deben.
	const wantSecretHash = "13bc10c71640ceb23a357bbbde6fbf208de0c911de7fc34fc70504633ac1e28c"
	sum := sha256.Sum256([]byte(rdv.EngineSecret()))
	if got := hex.EncodeToString(sum[:]); got != wantSecretHash {
		t.Errorf("SHA-256 del secreto = %q, se esperaba %q\n"+
			"  mismo problema que arriba: cambiar los parámetros parte la compatibilidad", got, wantSecretHash)
	}
}

func TestDeriveRendezvousEsDeterministaYDependeDelID(t *testing.T) {
	a, _ := ParseInviteID("A7K2-M9QX")
	b, _ := ParseInviteID("a7k2 m9qx") // el mismo, escrito distinto
	d, _ := ParseInviteID("A7K2-M9QZ") // un carácter distinto

	if DeriveRendezvous(a).NetworkName() != DeriveRendezvous(b).NetworkName() {
		t.Error("la forma en que se escribe el ID no puede cambiar la red de encuentro")
	}
	if DeriveRendezvous(a).EngineSecret() != DeriveRendezvous(b).EngineSecret() {
		t.Error("la forma en que se escribe el ID no puede cambiar el secreto de encuentro")
	}
	if DeriveRendezvous(a).NetworkName() == DeriveRendezvous(d).NetworkName() {
		t.Error("IDs distintos deben dar redes de encuentro distintas")
	}
}

func TestDeriveRendezvousSeparaIDDeSecreto(t *testing.T) {
	id, _ := ParseInviteID("A7K2-M9QX")
	rdv := DeriveRendezvous(id)
	// Los salts versionados existen para que un networkID nunca pueda coincidir
	// con un secret aunque el invite ID sea el mismo.
	if strings.Contains(rdv.EngineSecret(), strings.TrimPrefix(rdv.NetworkName(), "kanpachi-")) {
		t.Error("el secreto contiene al networkID: los salts no están separando las derivaciones")
	}
}

// TestRendezvousNoFiltraElSecretoAlLoguear protege un hábito, más que un
// secreto. Este valor en concreto lo puede derivar cualquiera con el invite ID.
// El punto es que redactar sea lo normal, porque el mismo tipo de valor en la
// red REAL de la sala sí es sensible, y los logs se copian al portapapeles con
// el botón de diagnóstico para pegarlos en el grupo.
func TestRendezvousNoFiltraElSecretoAlLoguear(t *testing.T) {
	id, _ := ParseInviteID("A7K2-M9QX")
	rdv := DeriveRendezvous(id)
	rendered := rdv.String()
	if strings.Contains(rendered, rdv.EngineSecret()) {
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
