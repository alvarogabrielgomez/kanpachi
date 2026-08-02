package domain

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func nick(t *testing.T, s string) Nickname {
	t.Helper()
	n, err := ParseNickname(s)
	if err != nil {
		t.Fatalf("nick %q: %v", s, err)
	}
	return n
}

// El vector dorado del formato de tarjeta.
//
// EXISTE PARA QUE NADIE CAMBIE ESTO SIN DARSE CUENTA. La página de invitación
// descifra con WebCrypto: `crypto.subtle.decrypt({iv: blob.slice(0,12)}, k,
// blob.slice(12))` sobre AES-GCM y una clave de 32 bytes. Si acá se cambia el
// orden del nonce, el tamaño de la clave, el algoritmo o los nombres de las
// claves del JSON, la tarjeta deja de leerse y el síntoma es MUDO: la página
// se queda con la versión genérica y no dice por qué.
//
// El blob se generó sellando {host:"alvaro", room:"Los panas"} con un lector
// que repite 0x2a, o sea clave y nonce constantes. Es un vector de formato, no
// un secreto.
const (
	tarjetaDorada    = "KioqKioqKioqKioqNNWrspTGAQPdMtPSTH8Z_0uQu2MfqqM5OIf-1-u7Bgn8Rk5raG-foSstTU-LO3_iLqWzyg"
	claveDorada      = "KioqKioqKioqKioqKioqKioqKioqKioqKioqKioqKio"
	tarjetaDoradaSea = "Los panas"
)

func TestVectorDoradoDeLaTarjeta(t *testing.T) {
	blob, err := base64.RawURLEncoding.DecodeString(tarjetaDorada)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ParseCardKeyFragment(claveDorada)
	if err != nil {
		t.Fatal(err)
	}

	card, err := OpenRoomCard(blob, key)
	if err != nil {
		t.Fatalf("el vector dorado no se descifra: el formato cambió y la página de invitación ya no lee la tarjeta: %v", err)
	}
	if card.Host.String() != "alvaro" || card.Room != tarjetaDoradaSea {
		t.Fatalf("el vector dorado se descifra a otra cosa: %+v", card)
	}
	if len(blob) <= cardNonceLen {
		t.Fatal("el blob no lleva el nonce delante")
	}
}

// TestElNonceVaDelanteYMideDoce es lo que la página asume literalmente en su
// llamada a decrypt. Se comprueba aparte del vector para que el fallo diga
// cuál de las dos cosas se rompió.
func TestElNonceVaDelanteYMideDoce(t *testing.T) {
	blob, key, err := SealRoomCard(RoomCard{Host: nick(t, "alvaro"), Room: "x"}, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	// GCM añade 16 bytes de etiqueta, así que el blob mide nonce + texto + 16.
	if len(blob) < cardNonceLen+16 {
		t.Fatalf("el blob mide %d, no cabe nonce y etiqueta", len(blob))
	}
	if _, err := OpenRoomCard(blob, key); err != nil {
		t.Fatalf("no se descifra lo que se acaba de cifrar: %v", err)
	}
}

func TestLaTarjetaManipuladaSeDescartaEntera(t *testing.T) {
	blob, key, err := SealRoomCard(RoomCard{Host: nick(t, "alvaro"), Room: "Los panas"}, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	roto := append([]byte(nil), blob...)
	roto[len(roto)-1] ^= 0x01

	if _, err := OpenRoomCard(roto, key); !errors.Is(err, ErrCardDecrypt) {
		t.Fatalf("una tarjeta con un bit cambiado se aceptó: %v", err)
	}
}

func TestLaClaveEquivocadaNoDescifra(t *testing.T) {
	blob, _, err := SealRoomCard(RoomCard{Host: nick(t, "alvaro"), Room: "Los panas"}, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var otra [CardKeyLen]byte
	if _, err := OpenRoomCard(blob, otra); !errors.Is(err, ErrCardDecrypt) {
		t.Fatalf("descifró con la clave equivocada: %v", err)
	}
}

// TestElNickSeRevalidaAlSalirDelSobre: la firma prueba quién escribió la
// tarjeta, no que lo que escribió sea un nick legal, y este valor termina en
// la pantalla de alguien.
func TestElNickSeRevalidaAlSalirDelSobre(t *testing.T) {
	// Se sella a mano, saltándose ParseNickname, que es lo que haría un host
	// modificado.
	blob, key, err := sellarCrudo(t, `{"host":"a‮varo","room":"x"}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := OpenRoomCard(blob, key); !errors.Is(err, ErrCardShape) {
		t.Fatalf("entró un nick con un carácter de control de dirección: %v", err)
	}
}

func sellarCrudo(t *testing.T, plano string) ([]byte, [CardKeyLen]byte, error) {
	t.Helper()
	var key [CardKeyLen]byte
	if _, err := rand.Read(key[:]); err != nil {
		return nil, key, err
	}
	gcm, err := newCardGCM(key[:])
	if err != nil {
		return nil, key, err
	}
	nonce := make([]byte, cardNonceLen)
	if _, err := rand.Read(nonce); err != nil {
		return nil, key, err
	}
	return gcm.Seal(nonce, nonce, []byte(plano), nil), key, nil
}

func TestLaTarjetaNoPasaDelTope(t *testing.T) {
	_, _, err := SealRoomCard(RoomCard{Host: nick(t, "alvaro"), Room: strings.Repeat("x", 600)}, rand.Reader)
	if !errors.Is(err, ErrCardTooBig) {
		t.Fatalf("una sala con un nombre de 600 caracteres pasó: %v", err)
	}
}

func TestElFragmentoDeLaClaveVaYVuelve(t *testing.T) {
	var key [CardKeyLen]byte
	if _, err := rand.Read(key[:]); err != nil {
		t.Fatal(err)
	}
	frag := CardKeyFragment(key)
	if strings.ContainsAny(frag, "+/=") {
		t.Fatalf("el fragmento no es base64url sin relleno: %q", frag)
	}
	back, err := ParseCardKeyFragment(frag)
	if err != nil || back != key {
		t.Fatalf("la clave no volvió igual: %v", err)
	}
	// Con relleno también, porque un enlace copiado a mano puede traerlo y
	// fallar por un "=" sería fallar por algo que el usuario no puede ver.
	if _, err := ParseCardKeyFragment(base64.URLEncoding.EncodeToString(key[:])); err != nil {
		t.Fatalf("no aceptó la forma con relleno: %v", err)
	}
	if _, err := ParseCardKeyFragment("aaa"); !errors.Is(err, ErrCardKeyLen) {
		t.Error("aceptó una clave que no mide 32 bytes")
	}
}

// TestElEnlaceLlevaLaClaveYLaURLDictadaNo son las dos formas del producto: una
// se pega en Telegram y la otra se dicta por teléfono.
func TestElEnlaceLlevaLaClaveYLaURLDictadaNo(t *testing.T) {
	id, err := ParseInviteID("A7K2M9QX")
	if err != nil {
		t.Fatal(err)
	}
	room := Room{InviteID: id, Seed: DefaultSeedHost}

	var key [CardKeyLen]byte
	link := room.InviteLink(key)
	if !strings.HasPrefix(link, "https://kanpachi.accentio.dev/A7K2-M9QX#") {
		t.Fatalf("el enlace no tiene la forma esperada: %q", link)
	}
	if strings.Contains(room.InviteURL(), "#") {
		t.Fatalf("la URL dictada lleva fragmento: %q", room.InviteURL())
	}
}

func TestUnSobreDemasiadoCortoNoSeIntentaDescifrar(t *testing.T) {
	var key [CardKeyLen]byte
	if _, err := OpenRoomCard(bytes.Repeat([]byte{0}, cardNonceLen), key); !errors.Is(err, ErrCardShape) {
		t.Fatal("un blob sin texto cifrado no dio ErrCardShape")
	}
}

// TestElNombreDeSalaSeAcotaAlAbrir: es texto libre que escribió otra máquina y
// termina en la pantalla de alguien.
func TestElNombreDeSalaSeAcotaAlAbrir(t *testing.T) {
	largo := strings.Repeat("x", 300)
	blob, key, err := sellarCrudo(t, `{"host":"alvaro","room":"`+largo+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	card, err := OpenRoomCard(blob, key)
	if err != nil {
		t.Fatal(err)
	}
	if len(card.Room) != MaxRoomNameLen {
		t.Fatalf("el nombre salió con %d caracteres", len(card.Room))
	}
}
