//go:build windows

package netcfg

import (
	"context"
	"fmt"
	"net/netip"
	"os/exec"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/port"
)

var (
	iphlpapi = windows.NewLazySystemDLL("iphlpapi.dll")

	// Not in golang.org/x/sys/windows, which ships only the Get halves.
	procSetIpInterfaceEntry  = iphlpapi.NewProc("SetIpInterfaceEntry")
	procCreateIpForwardEntry = iphlpapi.NewProc("CreateIpForwardEntry2")
	procDeleteIpForwardEntry = iphlpapi.NewProc("DeleteIpForwardEntry2")
	procIcmpCreateFile       = iphlpapi.NewProc("IcmpCreateFile")
	procIcmpCloseHandle      = iphlpapi.NewProc("IcmpCloseHandle")
	procIcmpSendEcho         = iphlpapi.NewProc("IcmpSendEcho")
)

// Config implements [port.NetConfigPort].
type Config struct {
	log  port.Logger
	book book

	// mu serialises the whole adapter pass. The supervisor reapplies on a timer
	// AND on every network identification event, so two passes overlapping is
	// the normal case rather than the rare one, and they write the same rows.
	mu sync.Mutex
}

func New(dataDir string, log port.Logger) *Config {
	return &Config{log: log, book: newBook(dataDir)}
}

// The check that this still fits the port. If a signature in core changes, the
// error comes out here and not where it gets wired.
var _ port.NetConfigPort = (*Config)(nil)

// ApplyAdapter brings the virtual adapter to the requested state.
//
// # Why nothing here is fatal on its own
//
// The caller treats a failure as a warning, and that is right: a wrong metric
// degrades a game and a missing route breaks LAN discovery, while aborting the
// room would leave the user with nothing instead of with something imperfect.
// So every step is attempted, its failure is collected, and the rest continues.
// Stopping at the first would leave the adapter in a state nobody chose.
func (c *Config) ApplyAdapter(ctx context.Context, want domain.AdapterState) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !want.Address.IsValid() {
		return nil // sin sala no hay adaptador que ajustar
	}
	luid, err := luidOf(domain.AdapterName)
	if err != nil {
		return err
	}

	var fallos []error
	añadir := func(qué string, err error) {
		if err != nil {
			fallos = append(fallos, fmt.Errorf("%s: %w", qué, err))
		}
	}

	// Metric and MTU travel in the same row, so both families are one
	// read-modify-write each.
	añadir("métrica IPv4", c.setInterface(luid, windows.AF_INET, want.MetricIPv4, mtuFor(want)))
	añadir("métrica IPv6", c.setInterface(luid, windows.AF_INET6, want.MetricIPv6, mtuFor(want)))
	añadir("rutas", c.syncRoutes(luid, wantedRoutes(want)))
	añadir("política de prefijo", c.setPrefixPolicy(ctx, want.PreferIPv4))

	if want.PrivateCategory {
		// Best effort by design. An adapter with no gateway stays "unidentified"
		// and Windows files it under Public, because NLA identifies a network by
		// the gateway's MAC. Kanpachi tries and does NOT depend on succeeding:
		// that is why every rule is written to all three profiles.
		if err := setPrivateCategory(); err != nil {
			c.log.Info("no se pudo marcar la red como privada, y no hace falta", "detalle", err)
		}
	}

	if len(fallos) > 0 {
		return fmt.Errorf("ajustando %s: %v", domain.AdapterName, fallos)
	}
	return nil
}

// setInterface writes the metric and the MTU of one address family.
func (c *Config) setInterface(luid uint64, family uint16, metric, mtu int) error {
	row := windows.MibIpInterfaceRow{Family: family, InterfaceLuid: luid}
	if err := windows.GetIpInterfaceEntry(&row); err != nil {
		// A family that is off on this adapter is not a failure. IPv6 can be
		// disabled machine-wide, and Kanpachi addresses in IPv4 anyway.
		if family == windows.AF_INET6 {
			return nil
		}
		return fmt.Errorf("leyendo la interfaz: %w", err)
	}

	if metric > 0 {
		// Both halves are needed. Windows recomputes the metric from link speed
		// on every reconnection unless the automatic one is switched off, so
		// writing the number without this lasts until the next event.
		row.UseAutomaticMetric = 0
		row.Metric = uint32(metric)
	}
	if mtu > 0 {
		row.NlMtu = uint32(mtu)
	}
	// Documented trap: SetIpInterfaceEntry rejects the row unless
	// SitePrefixLength is zeroed first for IPv4. The value read back is not
	// valid as input, which is the kind of thing that fails with a plain
	// ERROR_INVALID_PARAMETER and no hint.
	row.SitePrefixLength = 0

	r, _, _ := procSetIpInterfaceEntry.Call(uintptr(unsafe.Pointer(&row)))
	if r != 0 {
		return fmt.Errorf("SetIpInterfaceEntry devolvió %d", r)
	}
	return nil
}

// syncRoutes makes the adapter's extra routes be exactly `want`.
//
// Declarative like everything else: what is missing gets added, what is left
// over gets deleted, so leaving a room or changing game cannot leave a route
// hanging from the previous calculation.
//
// **A default route is always deleted, whoever wrote it.** Kanpachi never routes
// the internet. This is not about our own writes: the engine can install routes
// it learned from the network.
func (c *Config) syncRoutes(luid uint64, want []netip.Prefix) error {
	have, err := routesOn(luid)
	if err != nil {
		return err
	}

	quiere := make(map[netip.Prefix]bool, len(want))
	for _, p := range want {
		quiere[p] = true
	}

	var fallos []error
	for _, p := range have {
		if isDefaultRoute(p) {
			if err := deleteRoute(luid, p); err != nil {
				fallos = append(fallos, fmt.Errorf("borrando la ruta por defecto %v: %w", p, err))
			} else {
				c.log.Warn("había una ruta por defecto sobre el adaptador virtual y se quitó",
					"ruta", p.String(), "adaptador", domain.AdapterName)
			}
			continue
		}
		if quiere[p] {
			delete(quiere, p) // ya está puesta
			continue
		}
		// Only the two routes this package knows how to ask for are removed.
		// Anything else on the adapter belongs to the engine's own network and
		// deleting it would cut the room.
		if p == BroadcastRoute || p == MulticastRoute {
			if err := deleteRoute(luid, p); err != nil {
				fallos = append(fallos, fmt.Errorf("quitando %v: %w", p, err))
			}
		}
	}

	for p := range quiere {
		if err := addRoute(luid, p); err != nil {
			fallos = append(fallos, fmt.Errorf("poniendo %v: %w", p, err))
		}
	}

	if len(fallos) > 0 {
		return fmt.Errorf("%v", fallos)
	}
	return nil
}

// setPrefixPolicy makes IPv4 win name resolution on dual-stack machines.
//
// RFC 6724 row, `::ffff:0:0/96 100 4`. It does NOT disable IPv6 and does not
// touch the engine's transport: what it changes is which address a name
// resolves to first, which is what old games get wrong.
//
// It is machine-wide and survives a reboot, so it goes in the book with what
// was there before. netsh with LITERAL arguments and no interpolation: this
// runs as SYSTEM, and it is the reason the rule about never building a shell
// string exists.
func (c *Config) setPrefixPolicy(ctx context.Context, want bool) error {
	t, err := c.book.load()
	if err != nil {
		return err
	}
	if want == t.PrefixPolicyAdded {
		return nil
	}

	args := []string{"interface", "ipv6", "add", "prefixpolicy", "::ffff:0:0/96", "100", "4"}
	if !want {
		args = []string{"interface", "ipv6", "delete", "prefixpolicy", "::ffff:0:0/96"}
	}
	if out, err := exec.CommandContext(ctx, "netsh.exe", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("netsh %v: %w (%s)", args, err, out)
	}

	t.PrefixPolicyAdded = want
	return c.book.save(t)
}

// RevertTweaks undoes what a game profile asked for.
//
// Reads the book rather than remembering: the whole point is that this works
// after a dirty death, on the next start, with nothing in memory.
func (c *Config) RevertTweaks(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	t, err := c.book.load()
	if err != nil {
		return err
	}
	if t.empty() {
		return nil
	}

	var fallos []error
	if t.PrefixPolicyAdded {
		args := []string{"interface", "ipv6", "delete", "prefixpolicy", "::ffff:0:0/96"}
		if out, err := exec.CommandContext(ctx, "netsh.exe", args...).CombinedOutput(); err != nil {
			fallos = append(fallos, fmt.Errorf("quitando la política de prefijo: %w (%s)", err, out))
		} else {
			t.PrefixPolicyAdded = false
		}
	}
	if t.DirectPlayWas != nil {
		if err := c.applyDirectPlay(ctx, *t.DirectPlayWas); err != nil {
			fallos = append(fallos, fmt.Errorf("devolviendo DirectPlay a %v: %w", *t.DirectPlayWas, err))
		} else {
			t.DirectPlayWas = nil
		}
	}

	// The book is saved with whatever DID get undone, so a second attempt only
	// retries what is actually left.
	if err := c.book.save(t); err != nil {
		fallos = append(fallos, err)
	}
	if len(fallos) > 0 {
		return fmt.Errorf("revirtiendo los ajustes: %v", fallos)
	}
	return nil
}

// SetDirectPlay turns the legacy Windows component on or off.
//
// Only games from before about 2005 need it. It is an optional feature of the
// system and not a setting of the adapter, which is why it is a method of its
// own: folding it into the declarative state would make every network
// identification event, several per session, touch Windows feature installation.
func (c *Config) SetDirectPlay(ctx context.Context, want bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	t, err := c.book.load()
	if err != nil {
		return err
	}

	// El atajo que hace usable esta función, y NO es una optimización suelta.
	//
	// Preguntarle a DISM tarda SEGUNDOS, y esto se llama al crear la sala, al
	// entrar y en cada cambio de juego. El caso abrumadoramente normal es
	// "apágalo" sin que nadie lo haya encendido nunca, o sea: no hay nada que
	// hacer. Sin este atajo, crear una sala sin juego pagaba una consulta a la
	// instalación de características de Windows, y la API local se pasaba de
	// plazo con la sala ya levantada. Medido.
	//
	// Es correcto además de rápido: solo hay que apagar lo que encendimos
	// nosotros, y el libro es quien lo sabe.
	if !want && t.DirectPlayWas == nil {
		return nil
	}

	tiene, err := directPlayEnabled(ctx)
	if err != nil {
		return err
	}
	if tiene == want {
		return nil
	}

	// The previous value is recorded BEFORE touching anything, and only the
	// first time. Recording it again on the second change would overwrite the
	// state the user actually had with one Kanpachi itself produced.
	if t.DirectPlayWas == nil {
		anterior := tiene
		t.DirectPlayWas = &anterior
		if err := c.book.save(t); err != nil {
			return err
		}
	}

	if err := c.applyDirectPlay(ctx, want); err != nil {
		return err
	}
	c.log.Info("DirectPlay cambiado", "quería", want, "estaba", tiene)
	return nil
}

func (c *Config) applyDirectPlay(ctx context.Context, want bool) error {
	verbo := "/disable-feature"
	if want {
		verbo = "/enable-feature"
	}
	// Constant argument list, never a shell string. The only variable part is
	// one of two literals chosen above.
	args := []string{"/online", verbo, "/featurename:DirectPlay", "/norestart"}
	if out, err := exec.CommandContext(ctx, "dism.exe", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("dism %v: %w (%s)", args, err, out)
	}
	return nil
}
