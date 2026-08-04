package wfp

import (
	"net/netip"
	"strconv"
	"strings"
	"testing"

	"github.com/accentiostudios/kanpachi/core/domain"
)

func addr(s string) netip.Addr  { return netip.MustParseAddr(s) }
func pfx(s string) netip.Prefix { return netip.MustParsePrefix(s) }
func roomScope() Scope          { return Scope{LUID: 0x47008000000000, Net: pfx("100.64.1.0/24")} }
func gameRule() domain.FirewallRule {
	return domain.FirewallRule{
		Name:   "kanpachi-udp-16261",
		Proto:  domain.ProtoUDP,
		From:   16261,
		To:     16261,
		Local:  addr("100.64.1.1"),
		Remote: []netip.Addr{addr("100.64.1.5")},
	}
}

func TestTheGateRefusesToExistWithoutScope(t *testing.T) {
	// Es el fallo más caro posible de esta capa: un bloqueo de todo sin acotar
	// aplica a TODOS los adaptadores y deja al usuario sin su red de casa. No
	// falla en ningún test funcional y no se ve leyendo un diff.
	var rs domain.RuleSet
	rs.Rules = append(rs.Rules, gameRule())

	const luid = 0x47008000000000
	cases := []struct {
		nombre string
		scope  Scope
	}{
		{"nada", Scope{}},
		{"solo el adaptador", Scope{LUID: luid}},
		{"solo el rango", Scope{Net: pfx("100.64.1.0/24")}},
		// Los tres de abajo tienen los dos campos PUESTOS, y ninguno acota. Son
		// los que un `!= 0 && IsValid()` deja pasar, y los tres terminan en un
		// bloqueo duro sobre una red que no es de Kanpachi.
		{"todo internet", Scope{LUID: luid, Net: pfx("0.0.0.0/0")}},
		{"255 salas ajenas", Scope{LUID: luid, Net: pfx("100.64.0.0/16")}},
		{"la LAN de casa", Scope{LUID: luid, Net: pfx("192.168.1.0/24")}},
	}
	for _, c := range cases {
		t.Run(c.nombre, func(t *testing.T) {
			if _, err := SpecsFor(rs, c.scope); err == nil {
				t.Fatal("se aceptó un alcance incompleto")
			}
		})
	}
}

func TestTheBlockIsEmittedTwice(t *testing.T) {
	// La mitigación del riesgo que queda abierto: si la condición de interfaz
	// llegara vacía al reautorizar un flujo, el bloqueo por rango sigue casando.
	// Ninguna de las dos vías es el único asidero.
	var rs domain.RuleSet
	specs, err := SpecsFor(rs, roomScope())
	if err != nil {
		t.Fatal(err)
	}

	porAdaptador, porRango := 0, 0
	for _, s := range specs {
		if s.Action != Block || s.Layer != RecvAcceptV4 {
			continue
		}
		if s.Conditions.LUID != 0 && !s.Conditions.LocalNet.IsValid() {
			porAdaptador++
		}
		if s.Conditions.LocalNet.IsValid() && s.Conditions.LUID == 0 {
			porRango++
		}
	}
	if porAdaptador != 1 || porRango != 1 {
		t.Errorf("bloqueos IPv4: %d por adaptador y %d por rango, se esperaba uno de cada.\n"+
			"  Con uno solo, que su condición deje de casar apaga la compuerta en silencio",
			porAdaptador, porRango)
	}
}

func TestIPv6IsClosedWithNoPermits(t *testing.T) {
	// Kanpachi direcciona en IPv4 dentro de 100.64.0.0/10, así que cualquier
	// cosa que llegue por IPv6 a ese adaptador no es nuestra. Dejarla pasar
	// sería un agujero con la puerta de al lado cerrada.
	var rs domain.RuleSet
	rs.Rules = append(rs.Rules, gameRule())

	specs, err := SpecsFor(rs, roomScope())
	if err != nil {
		t.Fatal(err)
	}

	bloqueos, permisos := 0, 0
	for _, s := range specs {
		if s.Layer != RecvAcceptV6 {
			continue
		}
		if s.Action == Block {
			bloqueos++
		} else {
			permisos++
		}
	}
	if bloqueos == 0 {
		t.Error("IPv6 quedó sin bloquear en el adaptador virtual")
	}
	if permisos != 0 {
		t.Errorf("se emitieron %d permisos IPv6, y no debería haber ninguno", permisos)
	}
}

func TestEveryPermitOutweighsTheBlock(t *testing.T) {
	// Si un permiso no le gana al bloqueo de todo dentro del sublayer, el puerto
	// del juego no se abre y la sala no sirve para nada.
	var rs domain.RuleSet
	rs.Rules = append(rs.Rules, gameRule())

	specs, err := SpecsFor(rs, roomScope())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range specs {
		if s.Action == Permit && s.Weight <= WeightBlockAll {
			t.Errorf("el permiso %q pesa %d y el bloqueo de todo pesa %d",
				s.Label, s.Weight, WeightBlockAll)
		}
	}
}

func TestThePermitMirrorsTheRule(t *testing.T) {
	// Que sea espejo es lo que hace que la compuerta no abra nada que los
	// permisos visibles no abran ya. Quien audite con sus herramientas de
	// siempre ve la lista completa.
	var rs domain.RuleSet
	rs.Rules = append(rs.Rules, gameRule())

	specs, err := SpecsFor(rs, roomScope())
	if err != nil {
		t.Fatal(err)
	}

	var permit *FilterSpec
	for i := range specs {
		if specs[i].Action == Permit {
			permit = &specs[i]
		}
	}
	if permit == nil {
		t.Fatal("no se emitió ningún permiso para la regla del juego")
	}

	c := permit.Conditions
	if c.LUID != roomScope().LUID {
		t.Error("el permiso no lleva el adaptador")
	}
	if c.LocalAddr != gameRule().Local {
		t.Errorf("dirección local = %v", c.LocalAddr)
	}
	if c.LocalPortFrom != 16261 || c.LocalPortTo != 16261 || c.Proto != domain.ProtoUDP {
		t.Errorf("puertos/proto = %d-%d/%v", c.LocalPortFrom, c.LocalPortTo, c.Proto)
	}
	if len(c.Remote) != 1 || c.Remote[0] != addr("100.64.1.5") {
		t.Errorf("alcance remoto = %v", c.Remote)
	}
}

func TestAPermitWithNoRemoteScopeIsRefused(t *testing.T) {
	// El dominio ya lo prohíbe. Se recomprueba porque un permiso sin alcance
	// remoto abriría el puerto a cualquiera que alcance el adaptador, que es
	// justo lo que la compuerta existe para impedir.
	r := gameRule()
	r.Remote = nil

	var rs domain.RuleSet
	rs.Rules = append(rs.Rules, r)

	if _, err := SpecsFor(rs, roomScope()); err == nil {
		t.Fatal("se aceptó un permiso sin alcance remoto")
	} else if !strings.Contains(err.Error(), "cualquiera") {
		t.Errorf("el error no dice por qué importa: %v", err)
	}
}

func TestAPortRangeSurvivesWhole(t *testing.T) {
	// El catálogo no pone tope a la amplitud de un rango, así que un perfil
	// puede pedir 27000-27100 legítimamente. Expandirlo a cien filtros sería
	// absurdo y rechazarlo rompería perfiles que el dominio acepta: WFP tiene
	// condición de rango y es lo que corresponde.
	//
	// Este test existió al revés durante un rato, exigiendo que un rango se
	// RECHAZARA. Era una limitación inventada acá que habría roto en silencio la
	// mitad de los juegos con puertos consecutivos.
	r := gameRule()
	r.From, r.To = 27000, 27100

	var rs domain.RuleSet
	rs.Rules = append(rs.Rules, r)

	specs, err := SpecsFor(rs, roomScope())
	if err != nil {
		t.Fatal(err)
	}

	permisos := 0
	for _, s := range specs {
		if s.Action != Permit {
			continue
		}
		permisos++
		if s.Conditions.LocalPortFrom != 27000 || s.Conditions.LocalPortTo != 27100 {
			t.Errorf("el rango llegó como %d-%d", s.Conditions.LocalPortFrom, s.Conditions.LocalPortTo)
		}
	}
	if permisos != 1 {
		t.Errorf("se emitieron %d permisos para un rango, se esperaba UNO con condición "+
			"de rango", permisos)
	}
}

func TestAPermitWithNoPortIsRefused(t *testing.T) {
	// Un permiso sin puerto abre el adaptador entero para esos miembros, que es
	// justo lo que la compuerta existe para impedir.
	r := gameRule()
	r.From, r.To = 0, 0

	var rs domain.RuleSet
	rs.Rules = append(rs.Rules, r)

	if _, err := SpecsFor(rs, roomScope()); err == nil {
		t.Fatal("se aceptó un permiso sin puerto")
	}
}

func TestTheKeyDoesNotDependOnTheGame(t *testing.T) {
	// El guardián de los huérfanos, y es el motivo de que la clave salga de la
	// RANURA y no de la etiqueta.
	//
	// Derivándola de la etiqueta, el permiso espejo lleva dentro el nombre de la
	// regla, o sea el juego. Cambiar de juego cambiaría las claves y los filtros
	// del juego anterior quedarían puestos y sin dueño: un puerto abierto que ya
	// nadie pidió, invisible, porque un filtro de WFP no sale ni en `wf.msc` ni
	// en `Get-NetFirewallRule`.
	uno, dos := gameRule(), gameRule()
	dos.Name = "kanpachi-otro-juego-tcp-7777"
	dos.From, dos.To = 7777, 7777
	dos.Proto = domain.ProtoTCP

	var a, b domain.RuleSet
	a.Rules = append(a.Rules, uno)
	b.Rules = append(b.Rules, dos)

	specsA, err := SpecsFor(a, roomScope())
	if err != nil {
		t.Fatal(err)
	}
	specsB, err := SpecsFor(b, roomScope())
	if err != nil {
		t.Fatal(err)
	}
	if len(specsA) != len(specsB) {
		t.Fatalf("los dos conjuntos tienen %d y %d filtros", len(specsA), len(specsB))
	}
	for i := range specsA {
		if specsA[i].Key != specsB[i].Key {
			t.Errorf("la ranura %d cambia de clave al cambiar de juego: %q contra %q.\n"+
				"  Los filtros del juego anterior quedarían huérfanos y abiertos",
				i, specsA[i].Label, specsB[i].Label)
		}
	}
	// Y las etiquetas SÍ cambian, que es lo que hace legible `netsh wfp show
	// filters`. Si fueran iguales, este test estaría comparando nada.
	if specsA[3].Label == specsB[3].Label {
		t.Error("las etiquetas no distinguen una regla de otra")
	}
}

func TestTheFixedSlotsAreWhereEnforcementLooks(t *testing.T) {
	// Medir si la compuerta está puesta preguntando por UNA clave conocida solo
	// funciona si la ranura cero es siempre el bloqueo por adaptador.
	var rs domain.RuleSet
	rs.Rules = append(rs.Rules, gameRule())

	specs, err := SpecsFor(rs, roomScope())
	if err != nil {
		t.Fatal(err)
	}
	if specs[0].Slot != 0 || specs[0].Action != Block || specs[0].Conditions.LUID == 0 {
		t.Fatalf("la ranura 0 es %+v, y tiene que ser el bloqueo por adaptador", specs[0])
	}
	for i, s := range specs {
		if s.Slot != i {
			t.Errorf("el filtro %d dice ocupar la ranura %d", i, s.Slot)
		}
	}
}

func TestTheSweepCoversEverythingThatCanBeEmitted(t *testing.T) {
	// La limpieza al arrancar barre ranuras, no un conjunto recordado. Si un
	// filtro pudiera caer fuera del barrido, quedaría puesto para siempre.
	var rs domain.RuleSet
	for i := 0; i < domain.MaxPortRanges*2; i++ {
		r := gameRule()
		r.Name = "kanpachi-udp-" + strconv.Itoa(30000+i)
		r.From, r.To = uint16(30000+i), uint16(30000+i)
		rs.Rules = append(rs.Rules, r)
	}

	specs, err := SpecsFor(rs, roomScope())
	if err != nil {
		t.Fatal(err)
	}

	barrido := map[[16]byte]bool{}
	for _, k := range AllKeys() {
		barrido[k] = true
	}
	for _, s := range specs {
		if !barrido[s.Key] {
			t.Errorf("la clave de %q (ranura %d) no está en el barrido de %d ranuras",
				s.Label, s.Slot, MaxFilters)
		}
	}
}

func TestTooManyFiltersAreRefusedInsteadOfTruncated(t *testing.T) {
	// Recortar dejaría la sala configurada a medias, y el jugador que no entra
	// no tendría nada que mirar.
	var rs domain.RuleSet
	for i := 0; i <= MaxFilters; i++ {
		r := gameRule()
		r.Name = "kanpachi-udp-" + strconv.Itoa(40000+i)
		r.From, r.To = uint16(40000+i), uint16(40000+i)
		rs.Rules = append(rs.Rules, r)
	}

	if _, err := SpecsFor(rs, roomScope()); err == nil {
		t.Fatal("se aceptaron más filtros de los que la limpieza barre")
	}
}

func TestKeysAreStableAndDistinct(t *testing.T) {
	// La limpieza al arrancar tiene que encontrar lo que dejó la ejecución
	// anterior sin recordar nada entre arranques.
	var rs domain.RuleSet
	rs.Rules = append(rs.Rules, gameRule())

	first, err := SpecsFor(rs, roomScope())
	if err != nil {
		t.Fatal(err)
	}
	second, err := SpecsFor(rs, roomScope())
	if err != nil {
		t.Fatal(err)
	}

	seen := map[[16]byte]string{}
	for i := range first {
		if first[i].Key != second[i].Key {
			t.Errorf("la clave de %q cambia entre llamadas", first[i].Label)
		}
		if otra, dup := seen[first[i].Key]; dup {
			t.Errorf("%q y %q comparten clave", otra, first[i].Label)
		}
		seen[first[i].Key] = first[i].Label
	}

	// Y bien formada como UUID v4, que es lo que esperan las herramientas que la
	// muestren.
	k := first[0].Key
	if k[6]&0xf0 != 0x40 {
		t.Errorf("la clave no lleva la versión de UUID: %#x", k[6])
	}
	if k[8]&0xc0 != 0x80 {
		t.Errorf("la clave no lleva la variante de UUID: %#x", k[8])
	}
}

func TestValidateCatchesWhatTheConstructorCannot(t *testing.T) {
	// Validate es la última puerta antes de la API de Windows, y existe además
	// del guardián de arquitectura: uno vigila cómo se escribe el código y este
	// vigila lo que de verdad se va a instalar.
	sinAlcance := FilterSpec{
		Label: "inventado a mano", Layer: RecvAcceptV4, Action: Block, Weight: WeightBlockAll,
	}
	if err := sinAlcance.Validate(); err == nil {
		t.Fatal("un filtro sin alcance pasó la validación")
	} else if !strings.Contains(err.Error(), "SIN ALCANCE") {
		t.Errorf("el error no nombra el problema: %v", err)
	}

	permisoFlojo := FilterSpec{
		Label: "permiso que no gana", Layer: RecvAcceptV4, Action: Permit,
		Weight: WeightBlockAll, Conditions: Conditions{LUID: 1},
	}
	if err := permisoFlojo.Validate(); err == nil {
		t.Fatal("un permiso que no le gana al bloqueo pasó la validación")
	}
}

func TestTheTwoLocalConditionsTogetherAreRefused(t *testing.T) {
	// WFP une con O las condiciones del MISMO campo. La red local y la dirección
	// local son el mismo campo, así que pedir las dos ENSANCHA en vez de acotar:
	// un permiso pensado para la IP del host abriría el rango entero de la sala.
	// Y se lee perfectamente razonable.
	ancho := FilterSpec{
		Label: "acota dos veces y abre", Layer: RecvAcceptV4, Action: Permit,
		Weight: WeightPermit,
		Conditions: Conditions{
			LUID:      1,
			LocalNet:  pfx("100.64.1.0/24"),
			LocalAddr: addr("100.64.1.1"),
		},
	}
	if err := ancho.Validate(); err == nil {
		t.Fatal("se aceptó un filtro con red local Y dirección local")
	} else if !strings.Contains(err.Error(), "ENSANCHA") {
		t.Errorf("el error no dice qué pasa de verdad: %v", err)
	}
}

func TestAnIPv4ConditionInTheIPv6LayerIsRefused(t *testing.T) {
	// Un bloqueo IPv6 con una condición de dirección IPv4 no casa con nada y no
	// falla: quedaría puesto sin bloquear, y la medición lo contaría como
	// presente.
	mudo := FilterSpec{
		Label: "bloqueo IPv6 que no bloquea", Layer: RecvAcceptV6, Action: Block,
		Weight:     WeightBlockAll,
		Conditions: Conditions{LUID: 1, LocalNet: pfx("100.64.1.0/24")},
	}
	if err := mudo.Validate(); err == nil {
		t.Fatal("se aceptó un filtro IPv6 con condiciones IPv4")
	}
}

func TestTheMirrorNamesTheRuleItMirrors(t *testing.T) {
	// La medición nombra lo que encontró con esto, en vez de recortar la etiqueta
	// con un corte de cadena que se rompe el día que cambie el prefijo.
	var rs domain.RuleSet
	rs.Rules = append(rs.Rules, gameRule())

	specs, err := SpecsFor(rs, roomScope())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range specs {
		switch s.Action {
		case Block:
			if s.Rule != "" {
				t.Errorf("el bloqueo %q dice espejar la regla %q", s.Label, s.Rule)
			}
		case Permit:
			if s.Rule != gameRule().Name {
				t.Errorf("el permiso %q dice espejar %q", s.Label, s.Rule)
			}
		}
	}
}
