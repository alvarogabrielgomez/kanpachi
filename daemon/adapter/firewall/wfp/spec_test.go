package wfp

import (
	"net/netip"
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

	cases := []struct {
		nombre string
		scope  Scope
	}{
		{"nada", Scope{}},
		{"solo el adaptador", Scope{LUID: 0x47008000000000}},
		{"solo el rango", Scope{Net: pfx("100.64.1.0/24")}},
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
	if c.LocalPort != 16261 || c.Proto != domain.ProtoUDP {
		t.Errorf("puerto/proto = %d/%v", c.LocalPort, c.Proto)
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

func TestAPortRangeIsRefusedInsteadOfSilentlyCovered(t *testing.T) {
	// La compuerta emite un filtro por PUERTO. Un rango aceptado acá cubriría
	// solo su primer puerto, y el resto quedaría cerrado mientras la regla
	// visible del Firewall dice que están abiertos.
	r := gameRule()
	r.To = 16265

	var rs domain.RuleSet
	rs.Rules = append(rs.Rules, r)

	if _, err := SpecsFor(rs, roomScope()); err == nil {
		t.Fatal("se aceptó un rango de puertos")
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
