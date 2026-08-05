package domain

import (
	"reflect"
	"strings"
	"testing"
)

// TestLaCuarentenaTapaTodoPuertoProhibidoEnLasCuatroCombinaciones es el guardián
// del caso real: alguien suma un puerto a la lista de prohibidos y la cuarentena
// sigue tapando la de ayer.
//
// Nada más lo cazaría. El perfil que pida ese puerto se rechaza igual, así que
// la mitad de la protección funciona y la otra mitad falta en silencio: la regla
// permisiva que dejó el instalador de un juego sigue en pie y nadie la tapa.
func TestLaCuarentenaTapaTodoPuertoProhibidoEnLasCuatroCombinaciones(t *testing.T) {
	type hueco struct {
		port  uint16
		proto Proto
		in    bool
	}

	tapado := make(map[hueco]bool)
	for _, r := range BaseQuarantine() {
		for p := r.From; ; p++ {
			tapado[hueco{port: p, proto: r.Proto, in: r.In}] = true
			if p == r.To {
				break
			}
		}
	}

	for _, port := range forbiddenPorts {
		for _, proto := range []Proto{ProtoTCP, ProtoUDP} {
			for _, in := range []bool{true, false} {
				if !tapado[hueco{port: port, proto: proto, in: in}] {
					sentido := "saliente"
					if in {
						sentido = "entrante"
					}
					t.Errorf("el puerto prohibido %d queda sin tapar en %v %s. "+
						"Un puerto que ningún perfil puede pedir y que la cuarentena "+
						"no bloquea deja en pie la regla ajena que este bloqueo existe "+
						"para ganarle", port, proto, sentido)
				}
			}
		}
	}
}

// TestLaCuarentenaNoTapaNadaQueNoEsteProhibido es la mitad opuesta, y hace falta
// por lo mismo que la de arriba.
//
// Estos bloqueos valen para TODA la máquina y sobreviven a Kanpachi apagado. Un
// puerto de más acá no es un exceso de celo: es un servicio del usuario que deja
// de funcionar para siempre, sin causa visible y sin nada que culpar.
func TestLaCuarentenaNoTapaNadaQueNoEsteProhibido(t *testing.T) {
	prohibido := make(map[uint16]bool, len(forbiddenPorts))
	for _, p := range forbiddenPorts {
		prohibido[p] = true
	}

	for _, r := range BaseQuarantine() {
		for p := r.From; ; p++ {
			if !prohibido[p] {
				t.Errorf("la cuarentena bloquea el puerto %d, que no está en forbiddenPorts. "+
					"Estos bloqueos duran con el servicio detenido, así que uno de más "+
					"rompe un servicio del usuario para siempre", p)
			}
			if p == r.To {
				break
			}
		}
	}
}

// TestElTipoDeLaCuarentenaNoPuedeExpresarUnPermiso es una afirmación
// ESTRUCTURAL, y por eso va por reflexión en vez de por los valores.
//
// La garantía de [QuarantineRule] no es que hoy nadie escriba un permiso: es que
// no se pueda. Un test que recorriera las reglas comprobando que todas bloquean
// pasaría igual el día que alguien agregue el campo, porque el valor por defecto
// de un bool nuevo seguiría siendo el bloqueo. Lo que hay que impedir es el
// CAMPO, no el valor.
func TestElTipoDeLaCuarentenaNoPuedeExpresarUnPermiso(t *testing.T) {
	prohibidos := []string{"action", "allow", "permit", "accept", "block"}

	tipo := reflect.TypeOf(QuarantineRule{})
	for i := 0; i < tipo.NumField(); i++ {
		nombre := strings.ToLower(tipo.Field(i).Name)
		for _, mal := range prohibidos {
			if strings.Contains(nombre, mal) {
				t.Errorf("QuarantineRule tiene el campo %q. Este tipo es un bloqueo "+
					"POR CONSTRUCCIÓN, y esa es toda su garantía: con un campo de "+
					"acción, la invariante pasa a depender de que cada llamador lo "+
					"ponga bien", tipo.Field(i).Name)
			}
		}
	}
}

// TestLaCuarentenaNoSeAcotaAlAdaptador vigila la asimetría que más fácil se
// "corrige" en la dirección equivocada.
//
// Acotar un PERMISO al adaptador virtual es correcto y ya se hace. Acotar un
// BLOQUEO es un agujero, porque un alcance que deja de casar convierte el
// permiso en un cierre y **el bloqueo en nada**. El adaptador virtual solo
// existe con una sala abierta, así que un bloqueo acotado a él no protege nada
// el resto del tiempo, que es cuando la cuarentena tiene que estar haciendo su
// trabajo.
func TestLaCuarentenaNoSeAcotaAlAdaptador(t *testing.T) {
	sospechosos := []string{"local", "remote", "adapter", "interface", "nets", "scope", "addr"}

	tipo := reflect.TypeOf(QuarantineRule{})
	for i := 0; i < tipo.NumField(); i++ {
		nombre := strings.ToLower(tipo.Field(i).Name)
		for _, mal := range sospechosos {
			if strings.Contains(nombre, mal) {
				t.Errorf("QuarantineRule tiene el campo %q, que parece un alcance. "+
					"Un bloqueo que deja de casar ABRE, así que la cuarentena vale "+
					"para toda la máquina o no vale", tipo.Field(i).Name)
			}
		}
	}
}

// TestLosNombresDeLaCuarentenaSonUnicosYDeterministas.
//
// Reponer la cuarentena compara contra lo vivo por NOMBRE, sin guardar
// identificadores. Dos reglas distintas con el mismo nombre harían que reponer
// diera por puesta una que falta, y un nombre que cambie entre dos llamadas haría
// que cada arranque escribiera la cuarentena entera de nuevo.
func TestLosNombresDeLaCuarentenaSonUnicosYDeterministas(t *testing.T) {
	primera := BaseQuarantine()
	segunda := BaseQuarantine()

	if !reflect.DeepEqual(primera, segunda) {
		t.Fatal("dos llamadas a BaseQuarantine dieron listas distintas: reponerla " +
			"reescribiría el firewall entero en cada arranque")
	}

	vistos := make(map[string]QuarantineRule, len(primera))
	for _, r := range primera {
		if otra, ok := vistos[r.Name]; ok {
			t.Errorf("el nombre %q lo usan dos reglas distintas: %+v y %+v. "+
				"Reponer daría por puesta una regla que falta", r.Name, otra, r)
		}
		vistos[r.Name] = r
	}
}

// TestLaCuarentenaVaEtiquetadaConElGrupoBase.
//
// El grupo es lo que la salva de la purga del arranque, que se lleva todo lo
// etiquetado con [FirewallGroup]. Una regla de la cuarentena que saliera con el
// grupo de la sala duraría hasta el primer reinicio del servicio, y el fallo
// sería invisible: todo sigue funcionando igual, solo que sin protección.
func TestLaCuarentenaVaEtiquetadaConElGrupoBase(t *testing.T) {
	for _, r := range BaseQuarantine() {
		if !strings.HasPrefix(r.Name, FirewallGroupBase+":") {
			t.Errorf("la regla %q no se nombra con %q. El nombre es lo único que "+
				"el usuario ve en la consola del firewall, y ahí tiene que poder "+
				"saber quién la puso", r.Name, FirewallGroupBase)
		}
	}
}

// TestCadaPuertoProhibidoSeBloqueaEnLasDosDirecciones deja escrito POR QUÉ son
// las dos, que es lo que un lector futuro va a querer recortar.
//
// La entrante es la protección. La saliente cierra el mismo servicio por el otro
// lado, y **no impide que esta máquina sea cliente**: el puerto de las reglas es
// siempre el LOCAL, así que montar un disco de red o usar git por SSH, que salen
// de un puerto efímero hacia el 445 o el 22 del OTRO, no los toca ninguna.
func TestCadaPuertoProhibidoSeBloqueaEnLasDosDirecciones(t *testing.T) {
	entrantes, salientes := 0, 0
	for _, r := range BaseQuarantine() {
		if r.In {
			entrantes++
			continue
		}
		salientes++
	}

	if entrantes == 0 || salientes == 0 || entrantes != salientes {
		t.Fatalf("la cuarentena tiene %d reglas entrantes y %d salientes, y tienen "+
			"que ser el mismo número y ninguno cero", entrantes, salientes)
	}
}
