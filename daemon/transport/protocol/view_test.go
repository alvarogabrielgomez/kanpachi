package protocol

import (
	"strings"
	"testing"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// El guardián del contrato de alertas con la UI.
//
// # El fallo que esto atrapa, y que ya ocurrió
//
// `AlertAuditFailed` se agregó al dominio y nadie tocó [alertName], así que la
// alerta viajaba a la UI como "unknown". La ironía es exacta: la alerta que
// existe para que el módulo de exposición no se calle cuando deja de mirar,
// llegaba muda.
//
// Nada falló. El daemon compilaba, los tests pasaban, el `default` del switch
// devolvía una cadena perfectamente válida, y la pantalla no pintaba nada. Ese
// es el modo de fallo que este proyecto persigue: silencioso y con todo en
// verde.
//
// # Por qué acá y no en el dominio
//
// El nombre es contrato de TRANSPORTE, no del dominio: es lo que la UI lee del
// JSON. El dominio aporta la lista enumerable, [domain.AllAlertKinds], y este
// paquete es el que promete que cada elemento tiene nombre.

// TestCadaAlertaTieneNombreEnLaAPI.
func TestCadaAlertaTieneNombreEnLaAPI(t *testing.T) {
	todas := domain.AllAlertKinds()
	if len(todas) == 0 {
		t.Fatal("AllAlertKinds está vacía, así que este test no probaría nada")
	}

	vistos := map[string]domain.AlertKind{}
	for _, k := range todas {
		nombre := alertName(k)

		if nombre == "unknown" {
			t.Errorf("la alerta %d cae en el default de alertName.\n"+
				"  Viajaría a la UI como \"unknown\" y no la pintaría nadie, así que el aviso\n"+
				"  se pierde justo cuando hace falta. Dale su nombre en view.go.", k)
			continue
		}
		if nombre == "" {
			t.Errorf("la alerta %d se serializa como cadena vacía", k)
			continue
		}
		if otra, repetido := vistos[nombre]; repetido {
			t.Errorf("las alertas %d y %d comparten el nombre %q: la UI no puede distinguirlas",
				otra, k, nombre)
			continue
		}
		vistos[nombre] = k

		// El nombre es una clave de API, no un texto: la UI la usa para elegir
		// qué mensaje mostrar. Con mayúsculas o espacios, un día alguien lo
		// "corrige" y rompe la pantalla de todos los que no actualizaron.
		if nombre != strings.ToLower(nombre) || strings.ContainsAny(nombre, " -.") {
			t.Errorf("el nombre %q de la alerta %d no es una clave estable: minúsculas y guión bajo",
				nombre, k)
		}
	}
}

// TestElNombreDeUnaAlertaQueNoExisteEsUnknown comprueba el propio detector.
//
// Un guardián que nunca se probó no es un guardián: si alertName devolviera
// algo distinto de "unknown" en el default, el test de arriba pasaría siempre y
// no estaría vigilando nada.
func TestElNombreDeUnaAlertaQueNoExisteEsUnknown(t *testing.T) {
	inventada := domain.AlertKind(200)
	if nombre := alertName(inventada); nombre != "unknown" {
		t.Errorf("alertName(%d) = %q, se esperaba \"unknown\"", inventada, nombre)
	}
}

// TestCadaClaseDeReglaTieneNombreEnLaAPI es lo mismo para [domain.RuleClass],
// y el precio de que falte es mayor.
//
// Una alerta sin nombre no se pinta: se pierde el aviso. Una CLASE sin nombre se
// pinta igual, como una fila más de la lista de reglas ajenas, así que el fallo
// no es una ausencia sino una minimización: lo que entrega la máquina entera se
// muestra con el mismo peso que una regla de más para el juego.
func TestCadaClaseDeReglaTieneNombreEnLaAPI(t *testing.T) {
	todas := domain.AllRuleClasses()
	if len(todas) == 0 {
		t.Fatal("AllRuleClasses está vacía, así que este test no probaría nada")
	}

	vistos := map[string]domain.RuleClass{}
	for _, c := range todas {
		nombre := ruleClassName(c)

		if nombre == "unknown" {
			t.Errorf("la clase %d cae en el default de ruleClassName.\n"+
				"  Viajaría a la UI como \"unknown\" y se pintaría como una fila cualquiera.", c)
			continue
		}
		if nombre == "" {
			t.Errorf("la clase %d se serializa como cadena vacía", c)
			continue
		}
		if otra, repetido := vistos[nombre]; repetido {
			t.Errorf("las clases %d y %d comparten el nombre %q: la UI no puede distinguirlas",
				otra, c, nombre)
			continue
		}
		vistos[nombre] = c

		if nombre != strings.ToLower(nombre) || strings.ContainsAny(nombre, " -.") {
			t.Errorf("el nombre %q de la clase %d no es una clave estable: minúsculas y guión bajo",
				nombre, c)
		}
	}
}

// TestElNombreDeUnaClaseQueNoExisteEsUnknown comprueba el propio detector, por
// lo mismo que su gemelo de las alertas.
func TestElNombreDeUnaClaseQueNoExisteEsUnknown(t *testing.T) {
	inventada := domain.RuleClass(200)
	if nombre := ruleClassName(inventada); nombre != "unknown" {
		t.Errorf("ruleClassName(%d) = %q, se esperaba \"unknown\"", inventada, nombre)
	}
}

// TestLaClaseNoSeReconstruyeDesdeElCliente.
//
// La clase es política del dominio, calculada sobre lo que el firewall tiene
// puesto. Si el camino de vuelta la aceptara, quien hable por el pipe podría
// reetiquetar una regla de escritorio remoto como una del juego, y esa es justo
// la distinción que decide si la sala se puede abrir.
func TestLaClaseNoSeReconstruyeDesdeElCliente(t *testing.T) {
	entrada := []ForeignView{{
		Name:       "Parsec",
		Executable: `C:\Program Files\Parsec\parsecd.exe`,
		Profiles:   []string{"privado"},
		Class:      "game",
		Blocking:   false,
	}}

	vuelta := foreignRules(entrada)
	if len(vuelta) != 1 {
		t.Fatalf("la regla no sobrevivió la vuelta: %+v", vuelta)
	}
	if vuelta[0].Class != domain.RuleClass(0) {
		t.Errorf("la clase volvió como %v en vez de quedar sin decidir: el cliente "+
			"acaba de elegir cómo se clasifica una regla suya", vuelta[0].Class)
	}
	if vuelta[0].Blocking() {
		t.Error("una clase sin decidir salió como bloqueante")
	}
}
