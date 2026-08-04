package zzclaim

// Mide la afirmación: nada mantiene el puerto del canario fuera de un rango
// permitido. Se borra después de medir.

import (
	"fmt"
	"net/netip"
	"testing"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/daemon/adapter/canary"
	"github.com/accentiostudios/kanpachi/daemon/adapter/firewall/wfp"
)

type logMudo struct{}

func (logMudo) Info(string, ...any) {}
func (logMudo) Warn(string, ...any) {}

// 1. ¿De qué rango salen los puertos que el sistema le da al canario?
func TestClaimPuertoDelCanario(t *testing.T) {
	at := netip.MustParseAddr("127.0.0.1")
	var mín, máx uint16 = 65535, 0
	dentro := 0
	const rondas = 30
	for i := 0; i < rondas; i++ {
		c, err := canary.Listen(at, canary.Nonce{1}, time.Second, logMudo{})
		if err != nil {
			t.Fatal(err)
		}
		p := c.Port()
		if p < mín {
			mín = p
		}
		if p > máx {
			máx = p
		}
		if p >= 49152 && p <= 65535 {
			dentro++
		}
		_ = c.Close()
	}
	t.Logf("%d puertos sorteados: mín=%d máx=%d, dentro de 49152-65535: %d/%d",
		rondas, mín, máx, dentro, rondas)
}

// 2. ¿Acepta el dominio un perfil que pida el rango dinámico entero, y qué
// emite BuildRuleSet + SpecsFor con él?
func TestClaimPerfilQueTapaElRangoDinámico(t *testing.T) {
	raw := []byte(`{
	  "id": "juego-x",
	  "schema": 2,
	  "name": "Juego X",
	  "detect": {},
	  "host_ports": [{"proto": "both", "range": "49152-65535"}],
	  "client_ports": [],
	  "lan_discovery": false,
	  "system_tweaks": {"broadcast_route": false, "multicast_route": false, "prefer_ipv4": false, "directplay": false},
	  "connect_hint": {"kind": "direct_ip", "text_es": "conectate a la IP del host"},
	  "bind_hint": {},
	  "verified": null
	}`)
	p, err := domain.ParseGameProfile(raw, domain.OriginImported)
	if err != nil {
		t.Fatalf("REFUTADO en el parseo: %v", err)
	}
	t.Logf("perfil aceptado: %s, host_ports=%v", p.ID, p.HostPorts)

	local := netip.MustParseAddr("100.64.7.1")
	miembro := netip.MustParseAddr("100.64.7.2")

	rs, err := domain.BuildRuleSet(p, domain.RoleHost, local, []netip.Addr{miembro})
	if err != nil {
		t.Fatalf("REFUTADO en BuildRuleSet: %v", err)
	}
	ctrl, err := domain.ControlRules(domain.RoleHost, netip.MustParseAddr("100.127.255.9"), local, []netip.Addr{miembro})
	if err != nil {
		t.Fatal(err)
	}
	rs.Add(ctrl...)
	for _, r := range rs.Rules {
		t.Logf("regla: %-40q proto=%v %d-%d local=%v remote=%v nets=%v",
			r.Name, r.Proto, r.From, r.To, r.Local, r.Remote, r.Nets)
	}

	specs, err := wfp.SpecsFor(rs, wfp.Scope{LUID: 42, Net: netip.MustParsePrefix("100.64.7.0/24")})
	if err != nil {
		t.Fatalf("REFUTADO en SpecsFor: %v", err)
	}
	for _, s := range specs {
		conds, err := s.Conditions.Expand()
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("filtro slot=%d %-45s acción=%v peso=%d", s.Slot, s.Label, s.Action, s.Weight)
		for _, c := range conds {
			t.Logf("      cond campo=%v match=%v kind=%v num=%d from=%d to=%d addr=%08x",
				c.Field, c.Match, c.Kind, c.Num, c.From, c.To, c.Addr)
		}
	}

	// 3. El cruce: el puerto que el sistema le da al canario, ¿cae dentro de
	// alguna regla de este conjunto?
	at := netip.MustParseAddr("127.0.0.1")
	choques := 0
	const rondas = 20
	for i := 0; i < rondas; i++ {
		c, err := canary.Listen(at, canary.Nonce{1}, time.Second, logMudo{})
		if err != nil {
			t.Fatal(err)
		}
		port := c.Port()
		_ = c.Close()
		for _, r := range rs.Rules {
			if r.Local == local && port >= r.From && port <= r.To {
				choques++
				break
			}
		}
	}
	fmt.Printf("choques del puerto del canario con una regla permitida: %d/%d\n", choques, rondas)
	t.Logf("choques del puerto del canario con una regla permitida: %d/%d", choques, rondas)

	// 4. Y el veredicto que saldría de un choque.
	chk := domain.CanaryCheck{
		MeasuredAt: time.Now(), Port: 50000, Touched: true, Answered: true,
		ReportedTCP: domain.ProbeAnswered, ReportedUDP: domain.ProbeAnswered,
	}
	t.Logf("veredicto con Touched=true: %v (atención=%v)", chk.Verdict(), chk.NeedsAttention())
}
