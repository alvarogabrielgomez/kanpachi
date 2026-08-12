package usecase

import (
	"errors"
	"testing"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/port"
)

// La tarjeta sellada sobrevive al apagón y se vuelve a subir al reabrir.
//
// # El agujero que esto cierra
//
// El registro guarda las tarjetas EN MEMORIA y con vencimiento. Una sala que
// dura más que eso, o un registro que se reinició, dejan la sala en pie y su
// tarjeta muerta: la página de invitación muestra la genérica sobre una partida
// que está corriendo, y nada en el producto la volvía a subir nunca.
//
// Se suben los MISMOS bytes y no una tarjeta nueva, que es lo que conserva
// válidos los enlaces ya repartidos: la clave que los abre es la que se acaba de
// cargar del disco, y volver a sellar produciría otra.
func TestReabrirRepublicaLaTarjeta(t *testing.T) {
	b := salaCreada(t)
	publicadaAlCrear := append([]byte(nil), b.registro.publicado...)
	if len(publicadaAlCrear) == 0 {
		t.Fatal("crear no publicó ninguna tarjeta, así que este test no mide nada")
	}

	b = reinicia(t, b)
	b.registro.publicado = nil
	antes := b.registro.publicaciones

	if _, err := b.sesión.ResumeRoom(ctx()); err != nil {
		t.Fatal(err)
	}
	if b.registro.publicaciones != antes+1 {
		t.Fatalf("reabrir publicó %d veces, se esperaba una", b.registro.publicaciones-antes)
	}
	if string(b.registro.publicado) != string(publicadaAlCrear) {
		t.Error("se republicó una tarjeta DISTINTA de la que se había subido.\n" +
			"  Los enlaces ya repartidos llevan la clave de la vieja, así que\n" +
			"  dejarían de descifrarla.")
	}
}

// Una sala abierta vuelve a publicar antes de que venza la tarjeta, y no en
// cada latido. El reloj fijo convierte la hora de producto en una prueba
// instantánea y determinista.
func TestUnaSalaAbiertaRepublicaLaTarjetaCadaHora(t *testing.T) {
	b := salaCreada(t)
	antes := b.registro.publicaciones

	b.reloj.avanza(RepublishInterval - time.Second)
	b.sesión.Tick(ctx())
	if b.registro.publicaciones != antes {
		t.Fatal("la tarjeta se republicó antes de cumplir el intervalo")
	}

	b.reloj.avanza(2 * time.Second)
	b.sesión.Tick(ctx())
	if b.registro.publicaciones != antes+1 {
		t.Fatalf("publicaciones tras una hora = %d, se esperaba 1",
			b.registro.publicaciones-antes)
	}

	b.sesión.Tick(ctx())
	if b.registro.publicaciones != antes+1 {
		t.Fatal("la tarjeta se volvió a publicar en el latido siguiente")
	}
}

// Que el registro no conteste después de haber declarado muerto el código no
// lo revive. La ausencia de respuesta no demuestra que la entrada exista; solo
// una publicación aceptada puede apagar el aviso.
func TestUnFalloTransitorioNoReviveUnCódigoPerdido(t *testing.T) {
	b := salaCreada(t)
	b.registro.err = port.ErrUnknownRoom
	b.reloj.avanza(RepublishInterval)
	if st := b.sesión.Tick(ctx()); !st.CodeLost {
		t.Fatal("el registro rechazó el código y el estado no lo marcó perdido")
	}

	b.registro.err = errors.New("el registro no contesta")
	b.reloj.avanza(RepublishInterval)
	if st := b.sesión.Tick(ctx()); !st.CodeLost {
		t.Fatal("un fallo transitorio revivió un código que el registro había perdido")
	}

	b.registro.err = nil
	b.reloj.avanza(RepublishInterval)
	if st := b.sesión.Tick(ctx()); st.CodeLost {
		t.Fatal("una publicación aceptada no apagó el aviso de código perdido")
	}
}

// Que la republicación falle NO impide reabrir.
//
// Es lo mismo que promete el puerto entero: el registro es presentación, y una
// sala que no se puede reabrir porque un servidor no contestó sería cambiar la
// tarjeta por la partida.
func TestSiLaRepublicaciónFallaLaSalaSeReabreIgual(t *testing.T) {
	b := salaCreada(t)
	b = reinicia(t, b)
	b.registro.err = errors.New("el registro no está")

	st, err := b.sesión.ResumeRoom(ctx())
	if err != nil {
		t.Fatalf("la sala no se reabrió por culpa de la tarjeta: %v", err)
	}
	if st.Conn != domain.StateConnected {
		t.Fatalf("la sala quedó en %s", st.Conn)
	}
	if b.registro.publicaciones == 0 {
		t.Error("ni siquiera se intentó republicar")
	}
}

// Una sala guardada SIN tarjeta no llama al registro.
//
// Es el archivo escrito antes de que el campo existiera, y también el respaldo
// de crear: ahí el invite ID lo generó esta máquina y el registro no lo emitió
// nunca, así que pedirle que republique es pedirle que reabra una sala que no
// conoce. Contestaría que no existe, y sería un aviso en el log por cada
// reapertura, para siempre.
func TestUnaSalaGuardadaSinTarjetaNoLlamaAlRegistro(t *testing.T) {
	b := salaCreada(t)
	b = reinicia(t, b)

	// Se le quita la tarjeta a lo guardado, que es exactamente la forma de un
	// archivo de una versión anterior.
	sinTarjeta := b.sesión.pending
	sinTarjeta.Card = nil
	b.sesión.pending = sinTarjeta

	antes := b.registro.publicaciones
	if _, err := b.sesión.ResumeRoom(ctx()); err != nil {
		t.Fatal(err)
	}
	if b.registro.publicaciones != antes {
		t.Errorf("se llamó al registro %d vez/veces sin tener tarjeta que subir",
			b.registro.publicaciones-antes)
	}
}

// Crear SIN registro no deja NADA guardado.
//
// Es la segunda invariante del par: solo se persiste una sala que el registro
// aceptó. Antes esto comprobaba algo más flojo, que la tarjeta del respaldo no
// se guardara, porque crear sin registro producía una sala con un código
// generado en esta máquina. Ese camino ya no existe: sin registro no hay sala,
// así que tampoco hay nada que escribir en disco.
//
// Lo que sigue protegiendo es lo mismo de siempre, y por eso vale conservarlo:
// una sala guardada se reabre sola al arrancar, y guardar una que el registro
// nunca conoció haría que cada reapertura intentara republicar algo que del otro
// lado no existe.
func TestCrearSinRegistroNoGuardaNada(t *testing.T) {
	b := nuevoBanco(t)
	b.registro.err = errors.New("el registro no está")

	if _, err := b.sesión.CreateRoom(ctx(), nick(t, "alvaro"), "Los panas"); !errors.Is(err, ErrNoRegistry) {
		t.Fatalf("sin registro se creó la sala, o falló por otra cosa: %v", err)
	}
	if raw, err := b.estado.LoadRoom(); err == nil && len(raw) > 0 {
		t.Errorf("quedó una sala guardada de %d bytes que el registro nunca conoció", len(raw))
	}
}

// Crear CON registro guarda lo que se publicó, y la clave lo abre.
func TestCrearGuardaLaTarjetaQueSePublicó(t *testing.T) {
	b := salaCreada(t)
	guardada := loGuardado(t, b)

	if len(guardada.Card) == 0 {
		t.Fatal("no se guardó la tarjeta que se publicó")
	}
	if string(guardada.Card) != string(b.registro.publicado) {
		t.Error("lo guardado no es lo que se publicó")
	}
	if _, err := domain.OpenRoomCard(guardada.Card, guardada.CardKey); err != nil {
		t.Errorf("la clave guardada no abre la tarjeta guardada: %v", err)
	}
}

// Renombrar deja la clave y la tarjeta CONSISTENTES EN DISCO.
//
// # El desajuste que esto cierra, y que estaba vivo
//
// `RenameRoom` guardaba a disco ANTES de sellar la tarjeta nueva, y después
// fijaba la clave nueva solo en memoria. O sea que el archivo quedaba con el
// nombre nuevo y la clave VIEJA. Un apagón después de renombrar, y al reabrir el
// enlace repartido mostraba la tarjeta genérica: la clave del disco no abría la
// tarjeta que el registro tenía, y nada había fallado en ningún sitio.
//
// La comprobación es sobre lo persistido y no sobre la memoria, porque el
// desajuste solo existía en disco.
func TestRenombrarDejaLaClaveYLaTarjetaConsistentesEnDisco(t *testing.T) {
	b := salaCreada(t)

	if _, err := b.sesión.RenameRoom(ctx(), "Los panas 2"); err != nil {
		t.Fatal(err)
	}

	guardada := loGuardado(t, b)
	tarjeta, err := domain.OpenRoomCard(guardada.Card, guardada.CardKey)
	if err != nil {
		t.Fatalf("la clave guardada no abre la tarjeta guardada: %v", err)
	}
	if tarjeta.Room != "Los panas 2" {
		t.Errorf("en disco quedó la tarjeta de %q y la sala se llama %q",
			tarjeta.Room, guardada.Name)
	}
	if string(guardada.Card) != string(b.registro.publicado) {
		t.Error("lo guardado no es lo que se publicó al renombrar")
	}
}

// Y si al renombrar el registro no acepta, lo de disco NO se toca.
//
// La tarjeta que el registro sirve sigue siendo la anterior, así que la clave
// que la abre también. Guardar la nueva dejaría el enlace repartido apuntando a
// una clave que no descifra lo que la página va a recibir.
func TestSiRenombrarNoSePublicaLoDeDiscoNoCambia(t *testing.T) {
	b := salaCreada(t)
	antes := loGuardado(t, b)

	b.registro.err = errors.New("el registro no está")
	if _, err := b.sesión.RenameRoom(ctx(), "Los panas 2"); err != nil {
		t.Fatal(err)
	}

	después := loGuardado(t, b)
	if string(después.Card) != string(antes.Card) {
		t.Error("se guardó una tarjeta que el registro no aceptó")
	}
	if después.CardKey != antes.CardKey {
		t.Error("se guardó una clave que no abre la tarjeta que el registro tiene")
	}
}

// Renombrar usa el mismo Publish que el latido. Si ahí el registro afirma que
// ya no conoce la sala, la UI tiene que enterarse en esa misma respuesta y no
// una hora después.
func TestRenombrarDetectaQueElCódigoSePerdió(t *testing.T) {
	b := salaCreada(t)
	b.registro.err = port.ErrUnknownRoom

	st, err := b.sesión.RenameRoom(ctx(), "Los panas 2")
	if err != nil {
		t.Fatal(err)
	}
	if !st.CodeLost {
		t.Fatal("el registro rechazó el código al renombrar y no se marcó perdido")
	}

	b.registro.err = nil
	st, err = b.sesión.RenameRoom(ctx(), "Los panas 3")
	if err != nil {
		t.Fatal(err)
	}
	if st.CodeLost {
		t.Fatal("una publicación aceptada al renombrar no apagó el aviso")
	}
}

// loGuardado lee lo ÚLTIMO que se escribió en disco, decodificado.
func loGuardado(t *testing.T, b *banco) domain.PersistedRoom {
	t.Helper()
	raw, err := b.estado.LoadRoom()
	if err != nil {
		t.Fatalf("no hay sala guardada: %v", err)
	}
	p, err := domain.DecodePersistedRoom(raw)
	if err != nil {
		t.Fatalf("lo guardado no se puede releer: %v", err)
	}
	return p
}
