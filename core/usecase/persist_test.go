package usecase

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// Los tests del estado que sobrevive al apagón, y del código que viaja a los
// que están dentro.

// reinicia simula que el daemon volvió a arrancar sobre el mismo disco.
//
// Construye una sesión nueva con las mismas dependencias, sin haber llamado a
// LeaveRoom: es exactamente lo que deja un corte de luz.
func reinicia(t *testing.T, b *banco) *banco {
	t.Helper()
	s, err := NewSession(ctx(), b.deps)
	if err != nil {
		t.Fatalf("no se pudo montar la sesión tras el reinicio: %v", err)
	}
	b.sesión = s
	return b
}

// TestSalirLimpioBorraLaSalaYMorirSucioLaDeja.
//
// La presencia del archivo ES la señal de mal cierre. No hay bandera `dirty`
// dentro, porque una bandera es un campo más que alguien puede escribir a mano
// y este hecho no se puede falsificar desde dentro del archivo.
func TestSalirLimpioBorraLaSalaYMorirSucioLaDeja(t *testing.T) {
	t.Run("morir sucio la deja", func(t *testing.T) {
		b := salaCreada(t)
		if len(b.estado.salaGuardada()) == 0 {
			t.Fatal("crear una sala no la guardó")
		}
		b = reinicia(t, b)
		if _, hay := b.sesión.PendingRoom(); !hay {
			t.Fatal("tras morir sucio no se detectó la sala anterior")
		}
	})

	t.Run("salir limpio la borra", func(t *testing.T) {
		b := salaCreada(t)
		b.sesión.LeaveRoom(ctx())
		if len(b.estado.salaGuardada()) != 0 {
			t.Fatal("salir por las buenas dejó la sala guardada")
		}
		b = reinicia(t, b)
		if _, hay := b.sesión.PendingRoom(); hay {
			t.Fatal("hay sala pendiente después de una salida limpia")
		}
	})
}

// TestArrancarConSalaPendienteNoRehospedaYPurgaIgual.
//
// Las dos mitades importan. Purgar primero deja la máquina en deny-all mientras
// la pregunta está en pantalla; no rehospedar es la invariante de que nada que
// llegue de fuera de la app surte efecto sin confirmación dentro de la app, y
// acá lo de fuera es un archivo del arranque anterior.
func TestArrancarConSalaPendienteNoRehospedaYPurgaIgual(t *testing.T) {
	b := salaCreada(t)
	purgasAntes := b.firewall.purgas
	b = reinicia(t, b)

	if st := b.sesión.Status(); st.Conn != domain.StateIdle {
		t.Fatalf("reabrió la sala sin preguntar: %s", st.Conn)
	}
	if b.firewall.purgas != purgasAntes+1 {
		t.Fatal("no purgó al arrancar con una sala pendiente")
	}
}

// TestReabrirConservaLaIdentidadDeLaRed.
//
// Es lo único que hace que esto sirva. Los invitados guardan una credencial
// contra esa red, así que si la identidad cambiara, reabrir sería otra sala y
// no reconectaría nadie.
func TestReabrirConservaLaIdentidadDeLaRed(t *testing.T) {
	b := salaCreada(t)
	antes := b.motor.hostSpec
	código := b.sesión.Status().Room

	b = reinicia(t, b)
	st, err := b.sesión.ResumeRoom(ctx())
	if err != nil {
		t.Fatal(err)
	}

	if b.motor.hostSpec.NetworkID != antes.NetworkID {
		t.Fatal("reabrir cambió el identificador de la red real")
	}
	if b.motor.hostSpec.NetworkSecret != antes.NetworkSecret {
		t.Fatal("reabrir cambió el secreto de la red real")
	}
	if b.motor.hostSpec.RealNetworkName() != antes.RealNetworkName() {
		t.Fatalf("el nombre de la red real cambió: %q contra %q",
			b.motor.hostSpec.RealNetworkName(), antes.RealNetworkName())
	}
	if st.Room != código {
		t.Fatalf("reabrir cambió el código: %v contra %v", st.Room, código)
	}
	if st.Conn != domain.StateConnected || st.Role != domain.RoleHost {
		t.Fatalf("estado tras reabrir: %s, %s", st.Conn, st.Role)
	}
	if _, hay := b.sesión.PendingRoom(); hay {
		t.Fatal("la sala pendiente sigue ahí después de reabrirla")
	}
}

// TestReabrirReponeElJuegoResolviéndoloContraElCatálogoPropio.
//
// Lo guardado es el ID y jamás el perfil, igual que en el anuncio del host: las
// reglas salen del catálogo de ESTA máquina, con sus invariantes y sus puertos
// prohibidos.
func TestReabrirReponeElJuegoResolviéndoloContraElCatálogoPropio(t *testing.T) {
	b := salaCreada(t)
	if _, err := b.sesión.ActivateProfile(ctx(), "project-zomboid"); err != nil {
		t.Fatal(err)
	}
	b = reinicia(t, b)

	st, err := b.sesión.ResumeRoom(ctx())
	if err != nil {
		t.Fatal(err)
	}
	if st.Game.ID != "project-zomboid" {
		t.Fatalf("no se repuso el juego: %q", st.Game.ID)
	}
	// Con la sala vacía no se abre ningún puerto de juego, con el juego puesto
	// y todo. La cuarentena no depende de que el archivo diga la verdad.
	if reglas := b.firewall.estado().GameRules(); len(reglas) > 0 {
		t.Fatalf("reabrir abrió puertos con la sala vacía: %+v", reglas)
	}
}

// TestReabrirSinElPerfilDejaLaSalaSinJuego: el estado por defecto y el seguro.
func TestReabrirSinElPerfilDejaLaSalaSinJuego(t *testing.T) {
	b := salaCreada(t)
	if _, err := b.sesión.ActivateProfile(ctx(), "project-zomboid"); err != nil {
		t.Fatal(err)
	}
	// El catálogo de la máquina que reabre ya no tiene ese perfil.
	b.almacén.builtin = []byte(`{"kanpachi_catalog":1,"profiles":[]}`)
	b = reinicia(t, b)

	st, err := b.sesión.ResumeRoom(ctx())
	if err != nil {
		t.Fatal(err)
	}
	if st.Game.ID != "" {
		t.Fatalf("repuso un juego que no está en el catálogo: %q", st.Game.ID)
	}
	if st.Conn != domain.StateConnected {
		t.Fatal("un perfil que falta impidió reabrir la sala")
	}
}

// TestReabrirNoRestauraMiembros: los miembros son lo que reporte el motor.
// Restaurarlos del archivo abriría puertos hacia direcciones que hoy pueden no
// ser de nadie.
func TestReabrirNoRestauraMiembros(t *testing.T) {
	b, _ := salaConDosYJuego(t)
	b.motor.peers = nil
	b = reinicia(t, b)

	st, err := b.sesión.ResumeRoom(ctx())
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Peers) != 1 || !st.Peers[0].Self {
		t.Fatalf("reabrir trajo miembros del disco: %+v", st.Peers)
	}
}

// TestDescartarLaSalaAnteriorLaBorraYDejaEnIdle.
func TestDescartarLaSalaAnteriorLaBorraYDejaEnIdle(t *testing.T) {
	b := salaCreada(t)
	b = reinicia(t, b)

	if err := b.sesión.DiscardPendingRoom(ctx()); err != nil {
		t.Fatal(err)
	}
	if len(b.estado.salaGuardada()) != 0 {
		t.Fatal("descartar no borró el archivo")
	}
	if _, hay := b.sesión.PendingRoom(); hay {
		t.Fatal("descartar dejó la sala pendiente")
	}
	if st := b.sesión.Status(); st.Conn != domain.StateIdle {
		t.Fatalf("descartar movió el estado: %s", st.Conn)
	}
	if err := b.sesión.DiscardPendingRoom(ctx()); !errors.Is(err, ErrNoPendingRoom) {
		t.Fatalf("descartar dos veces = %v", err)
	}
}

// TestElCódigoNuevoLlegaALosQueEstánDentro.
//
// Arregla un efecto secundario que nadie había nombrado: renovar dejaba a los
// presentes con el código viejo guardado, o sea con un código muerto el día que
// quisieran volver.
func TestElCódigoNuevoLlegaALosQueEstánDentro(t *testing.T) {
	b, _ := salaConDosYJuego(t)
	viejo := b.sesión.Status().Room
	b.registro.siguiente = "B8L3N4PZ"

	st, err := b.sesión.RotateInviteCode(ctx())
	if err != nil {
		t.Fatal(err)
	}
	repartidos := b.control.códigosRepartidos()
	if len(repartidos) != 1 {
		t.Fatalf("códigos repartidos = %d", len(repartidos))
	}
	if repartidos[0] != st.Room {
		t.Fatalf("se repartió %v y el vigente es %v", repartidos[0], st.Room)
	}
	if st.Room == viejo {
		t.Fatal("el código no cambió")
	}
}

// TestQueFalleElRepartoNoInvalidaLaRenovación: el código nuevo ya es el bueno y
// el vestíbulo ya está levantado con él. Lo que se pierde es que el otro lo
// tenga guardado.
func TestQueFalleElRepartoNoInvalidaLaRenovación(t *testing.T) {
	b, _ := salaConDosYJuego(t)
	b.control.errCódigo = errors.New("no se pudo mandar")

	if _, err := b.sesión.RotateInviteCode(ctx()); err != nil {
		t.Fatalf("un reparto fallido invalidó la renovación: %v", err)
	}
}

// TestElInvitadoCambiaSuCódigoYElGuardado.
func TestElInvitadoCambiaSuCódigoYElGuardado(t *testing.T) {
	b := salaConInvitado(t)
	nuevo, err := domain.ParseRoom("B8L3N4PZ")
	if err != nil {
		t.Fatal(err)
	}

	st := b.sesión.OnCodeRotated(ctx(), nuevo)
	if st.Room != nuevo {
		t.Fatalf("el invitado no tomó el código nuevo: %v", st.Room)
	}
	last, hay := b.sesión.LastRoom()
	if !hay {
		t.Fatal("no quedó última sala guardada")
	}
	if last.Room != nuevo {
		t.Fatalf("la última sala guardada quedó con el código viejo: %v", last.Room)
	}
}

// TestUnHostNoTomaCódigosDeNadie: aceptarlos le permitiría a un miembro
// modificado dejarlo publicando una puerta que no existe.
func TestUnHostNoTomaCódigosDeNadie(t *testing.T) {
	b := salaCreada(t)
	suyo := b.sesión.Status().Room
	otro, err := domain.ParseRoom("B8L3N4PZ")
	if err != nil {
		t.Fatal(err)
	}

	if st := b.sesión.OnCodeRotated(ctx(), otro); st.Room != suyo {
		t.Fatalf("un host tomó el código de otro: %v", st.Room)
	}
}

// TestUnCódigoNuevoIncompletoSeIgnora. Llega de otra máquina, así que se
// comprueba acá y no se confía en que el otro lado mandara algo coherente.
func TestUnCódigoNuevoIncompletoSeIgnora(t *testing.T) {
	b := salaConInvitado(t)
	suyo := b.sesión.Status().Room

	if st := b.sesión.OnCodeRotated(ctx(), domain.Room{}); st.Room != suyo {
		t.Fatalf("se tomó un código vacío: %v", st.Room)
	}
}

// TestLaÚltimaSalaNoLlevaCredencialNiIdentidadDeRed.
//
// Volver pasa otra vez por el vestíbulo: el host reemite y ve llegar a quien
// llega, y eso es lo que mantiene con sentido a la revocación. Un archivo con
// credencial dentro sería una llave de sala tirada en disco.
func TestLaÚltimaSalaNoLlevaCredencialNiIdentidadDeRed(t *testing.T) {
	b := salaConInvitado(t)

	raw, err := b.estado.LoadLast()
	if err != nil {
		t.Fatalf("entrar no guardó la última sala: %v", err)
	}
	texto := string(raw)
	for _, prohibido := range []string{"kanpachi-real", "token", "credential", "network_secret", "network_id"} {
		if strings.Contains(strings.ToLower(texto), prohibido) {
			t.Fatalf("la última sala guardada contiene %q:\n%s", prohibido, texto)
		}
	}

	last, hay := b.sesión.LastRoom()
	if !hay {
		t.Fatal("no se pudo releer la última sala")
	}
	if last.Room.InviteID.String() != "A7K2-M9QX" {
		t.Fatalf("código guardado = %q", last.Room.InviteID.String())
	}
	if last.Nick.String() != "humberto" {
		t.Fatalf("nick guardado = %q", last.Nick.String())
	}
}

// TestExpulsarNoBorraLaÚltimaSala: expulsar no es banear, y volver tiene que
// seguir siendo un click.
func TestExpulsarNoBorraLaÚltimaSala(t *testing.T) {
	b := salaConInvitado(t)
	b.sesión.OnRoomNotice(ctx(), domain.RoomNotice{Kind: domain.NoticeKicked})

	if _, hay := b.sesión.LastRoom(); !hay {
		t.Fatal("que te expulsen borró la última sala")
	}
}

// TestUnArchivoDeSalaIlegibleNoImpideArrancar.
func TestUnArchivoDeSalaIlegibleNoImpideArrancar(t *testing.T) {
	b := nuevoBanco(t)
	b.estado.sala = []byte(`{"invite_id":"A7K2M9QX","seed":`)

	b = reinicia(t, b)
	if _, hay := b.sesión.PendingRoom(); hay {
		t.Fatal("un archivo cortado se tomó por sala válida")
	}
	if st := b.sesión.Status(); st.Conn != domain.StateIdle {
		t.Fatalf("estado tras un archivo ilegible: %s", st.Conn)
	}
}

// TestUnaSalaGuardadaConUnCampoDeMásSeRechazaEntera.
//
// El archivo lleva identidad y referencias, jamás política. Un campo que no
// existe en el esquema invalida el archivo completo, por lo mismo que un perfil
// de catálogo inválido se rechaza completo.
func TestUnaSalaGuardadaConUnCampoDeMásSeRechazaEntera(t *testing.T) {
	b := salaCreada(t)
	bueno := b.estado.salaGuardada()

	casos := map[string]string{
		"un plazo colado":  `"host_absence_limit": 0`,
		"un puerto colado": `"host_ports": [{"proto":"tcp","range":"445"}]`,
		"campo inventado":  `"algo": 1`,
	}
	for nombre, extra := range casos {
		t.Run(nombre, func(t *testing.T) {
			roto := bytes.Replace(bueno, []byte("{\n"), []byte("{\n  "+extra+",\n"), 1)
			if _, err := domain.DecodePersistedRoom(roto); !errors.Is(err, domain.ErrPersistedShape) {
				t.Fatalf("se aceptó un archivo con %s: %v", nombre, err)
			}
		})
	}
}

// TestLaSalaGuardadaSobreviveUnaVuelta: lo que se escribe se vuelve a leer
// igual, incluido el juego activo.
func TestLaSalaGuardadaSobreviveUnaVuelta(t *testing.T) {
	b := salaCreada(t)
	if _, err := b.sesión.ActivateProfile(ctx(), "project-zomboid"); err != nil {
		t.Fatal(err)
	}
	st := b.sesión.Status()

	guardada, err := domain.DecodePersistedRoom(b.estado.salaGuardada())
	if err != nil {
		t.Fatal(err)
	}
	switch {
	case guardada.Room != st.Room:
		t.Errorf("código: %v contra %v", guardada.Room, st.Room)
	case guardada.Subnet != st.Subnet:
		t.Errorf("subred: %v contra %v", guardada.Subnet, st.Subnet)
	case guardada.GameID != "project-zomboid":
		t.Errorf("juego: %q", guardada.GameID)
	case guardada.Name != st.Name:
		t.Errorf("nombre: %q contra %q", guardada.Name, st.Name)
	case guardada.Host.String() != "alvaro":
		t.Errorf("host: %q", guardada.Host)
	case guardada.SavedAt.IsZero():
		t.Error("sin fecha de guardado")
	}
}
