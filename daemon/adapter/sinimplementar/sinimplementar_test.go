package sinimplementar

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// El guardián de que ningún provisional se vuelva mentiroso.
//
// Un provisional que devuelve éxito hace la cuarentena inverificable desde
// dentro del programa: el firewall dice "purgado" sin purgar, la pantalla pinta
// verde y el producto afirma una promesa que nadie cumplió.
//
// Ese error no llega escribiendo mal: llega el día que alguien está probando
// otra cosa, se topa con un provisional que le corta el paso, y devuelve nil
// "un momento" para seguir. Este test es lo que hace que ese momento no se
// quede.

func ctx() context.Context { return context.Background() }

func exigeFallo(t *testing.T, qué string, err error) {
	t.Helper()
	if err == nil {
		t.Errorf("%s devolvió éxito. Un provisional que finge que funcionó hace que la\n"+
			"  promesa del producto no se pueda comprobar desde dentro del programa.", qué)
		return
	}
	if !errors.Is(err, ErrSinImplementar) {
		t.Errorf("%s falló con %v, que no es ErrSinImplementar: quien lo reciba no puede\n"+
			"  distinguir \"todavía no existe\" de \"falló\"", qué, err)
	}
}

func TestElFirewallProvisionalFallaEnTodo(t *testing.T) {
	f := Firewall{}
	exigeFallo(t, "Apply", f.Apply(ctx(), domain.RuleSet{}))
	exigeFallo(t, "PurgeOwned", f.PurgeOwned(ctx()))
	exigeFallo(t, "SuspendForeign", f.SuspendForeign(ctx(), nil))

	_, err := f.AuditForeign(ctx(), domain.GameProfile{})
	exigeFallo(t, "AuditForeign", err)

	// RestoreForeign es el que más invita a la excepción, porque el arranque lo
	// llama por si una salida sucia dejó algo desactivado. Devolver nil haría
	// que el daemon anotara "reglas ajenas restauradas" sin restaurar nada, y el
	// usuario se quedaría con reglas de su juego apagadas creyendo que volvieron.
	exigeFallo(t, "RestoreForeign", f.RestoreForeign(ctx()))
}

func TestElMotorProvisionalFallaEnTodoMenosSalir(t *testing.T) {
	e := Engine{}
	exigeFallo(t, "HostNetwork", e.HostNetwork(ctx(), domain.HostSpec{}))
	exigeFallo(t, "JoinRendezvous", e.JoinRendezvous(ctx(), domain.RendezvousSpec{}))
	exigeFallo(t, "LeaveRendezvous", e.LeaveRendezvous(ctx()))
	exigeFallo(t, "JoinWithCredential", e.JoinWithCredential(ctx(), domain.GuestSpec{}))
	exigeFallo(t, "Restart", e.Restart(ctx()))
	exigeFallo(t, "RevokeCredential", e.RevokeCredential(ctx(), ""))

	_, err := e.Peers(ctx())
	exigeFallo(t, "Peers", err)
	_, err = e.Diagnostics(ctx())
	exigeFallo(t, "Diagnostics", err)
	_, err = e.IssueCredential(ctx(), domain.CredentialRequest{})
	exigeFallo(t, "IssueCredential", err)
	_, err = e.ListCredentials(ctx())
	exigeFallo(t, "ListCredentials", err)

	// Leave es la única que devuelve nil, y no miente: lo llama el camino de
	// error de crear y de entrar, que puede correr antes de que exista una red.
	// Un error acá convertiría un fallo en dos.
	if err := e.Leave(ctx()); err != nil {
		t.Errorf("Leave devolvió %v, y tiene que ser idempotente", err)
	}
}

func TestElRestoDeProvisionalesFalla(t *testing.T) {
	n := NetConfig{}
	exigeFallo(t, "ApplyAdapter", n.ApplyAdapter(ctx(), domain.AdapterState{}))
	exigeFallo(t, "RevertTweaks", n.RevertTweaks(ctx()))
	exigeFallo(t, "SetDirectPlay", n.SetDirectPlay(ctx(), true))
	_, err := n.ProbeMTU(ctx())
	exigeFallo(t, "ProbeMTU", err)

	// Devolver la lista vacía sería peor de lo que parece: sin prefijos locales,
	// el plan de direcciones concluye que NINGÚN rango choca con la red de casa
	// y elige justo uno que sí choca.
	_, err = Routing{}.LocalPrefixes(ctx())
	exigeFallo(t, "LocalPrefixes", err)

	_, err = Library{}.Installed(ctx())
	exigeFallo(t, "Installed", err)

	_, err = Inspector{}.Snapshot(ctx(), domain.ProcessRef{})
	exigeFallo(t, "Snapshot", err)

	a := Audit{}
	_, err = a.FirewallEnabled(ctx())
	exigeFallo(t, "FirewallEnabled", err)
	_, err = a.Enforcement(ctx())
	exigeFallo(t, "Enforcement", err)
	_, err = a.RouterMappings(ctx())
	exigeFallo(t, "RouterMappings", err)

	d := Directory{}
	_, err = d.Open(ctx(), nil)
	exigeFallo(t, "Open", err)
	_, _, err = d.Lookup(ctx(), domain.InviteID{})
	exigeFallo(t, "Lookup", err)
	exigeFallo(t, "Publish", d.Publish(ctx(), domain.InviteID{}, nil))
}

// TestLaAuditoriaProvisionalLevantaLaAlertaDeQueNadieMira.
//
// Es la prueba de que AlertAuditFailed hacía falta: con este adaptador puesto,
// el daemon no puede comprobar nada, y sin esa alerta la pantalla se quedaría
// en verde diciendo que todo está bien.
func TestLaAuditoriaProvisionalLevantaLaAlertaDeQueNadieMira(t *testing.T) {
	if _, err := (Audit{}).FirewallEnabled(ctx()); err == nil {
		t.Fatal("la auditoría provisional contestó, así que este test no prueba nada")
	}
	// El caso de uso que traduce eso en la alerta vive en core y tiene sus
	// propios tests. Acá se afirma la mitad que es de este paquete: que falla.
}

// TestLosEventosDelSistemaSonLaUnicaExcepcion.
//
// No pueden fallar porque su firma no tiene por dónde: los tres métodos
// devuelven canales. Devolver canales mudos no es fingir que funciona, es decir
// la verdad, que no hay eventos.
func TestLosEventosDelSistemaSonLaUnicaExcepcion(t *testing.T) {
	e := NewEvents()

	// Ninguno emite.
	for _, c := range []struct {
		nombre string
		ch     <-chan struct{}
	}{
		{"NetworkIdentified", e.NetworkIdentified()},
		{"Resumed", e.Resumed()},
		{"NetworkChanged", e.NetworkChanged()},
	} {
		select {
		case <-c.ch:
			t.Errorf("%s emitió un evento que no existió", c.nombre)
		case <-time.After(20 * time.Millisecond):
		}
	}

	// Close cierra los tres y NUNCA espera a que alguien lea: un Close que
	// bloqueara colgaría el apagado del servicio entero.
	hecho := make(chan struct{})
	go func() {
		_ = e.Close()
		close(hecho)
	}()
	select {
	case <-hecho:
	case <-time.After(2 * time.Second):
		t.Fatal("Close se colgó esperando lector")
	}

	for _, ch := range []<-chan struct{}{e.NetworkIdentified(), e.Resumed(), e.NetworkChanged()} {
		select {
		case <-ch:
		case <-time.After(time.Second):
			t.Error("un canal no se cerró tras Close")
		}
	}
}

// TestElBuildPorDefectoLlevaProvisionales.
//
// La etiqueta va al revés de lo intuitivo a propósito, y este test fija esa
// dirección: si alguien la invierte, un binario de release sin la etiqueta
// compilaría, arrancaría, se instalaría como servicio y no haría nada de lo que
// promete. Con esta dirección, el olvido produce un binario que se NIEGA a
// instalarse. El olvido tiene que doler del lado seguro.
func TestElBuildPorDefectoLlevaProvisionales(t *testing.T) {
	if !Presente {
		t.Error("el build por defecto dice que NO lleva provisionales.\n" +
			"  Si esto se compiló con la etiqueta de release, bien. Si no, la dirección\n" +
			"  de la etiqueta se invirtió y un release olvidado pasa desapercibido.")
	}
}
