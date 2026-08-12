package kanpachi

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"time"
)

// AddressDeadline is how long a start command waits for the virtual address.
//
// Generous on purpose. Creating a wintun adapter, naming it and configuring an
// address is several privileged operations, and on a cold machine the driver
// gets loaded on the way. Failing early here would report a broken network on a
// machine that was merely slow.
const AddressDeadline = 30 * time.Second

// addressPoll is how often the interface is re-read while waiting.
const addressPoll = 250 * time.Millisecond

// waitForAddress blocks until `want` is configured on `adapter`.
//
// # The failure this exists to fix, measured with the real daemon
//
// A start command returned as soon as the engine acknowledged it, which is
// BEFORE the adapter exists. Everything downstream needs it: the control
// channel binds to the host's virtual address, the firewall gate resolves the
// adapter to a LUID, and netcfg writes its metric. The first one to run failed:
//
//	listen tcp 100.127.255.1:57623: bind: The requested address is not valid
//	in its context
//
// Every caller could wait on its own, and then every caller would have to
// remember to. Waiting once, here, makes the port's contract true: when a start
// command returns, the network is usable.
//
// # Why an error and not a warning
//
// Because a room reported as open on an adapter that never appeared is worse
// than one that failed to open. The error path already tears everything down,
// and `Leave` is idempotent precisely so it can run this early.
func waitForAddress(ctx context.Context, addrs addrLister, adapter string, want netip.Addr, deadline time.Duration) error {
	if !want.IsValid() {
		return fmt.Errorf("no se puede esperar a una dirección vacía en %q", adapter)
	}

	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()

	// The last error is kept rather than reported as it happens: while the
	// adapter is being created, "no such network interface" is the NORMAL
	// answer, and logging each attempt would bury the real one.
	var last error
	for {
		got, err := addrs(adapter)
		if err != nil {
			last = err
		} else {
			for _, a := range got {
				if a == want {
					return nil
				}
			}
			last = fmt.Errorf("el adaptador existe con %v", got)
		}

		select {
		case <-ctx.Done():
			return agotado(adapter, want, deadline, last)
		case <-time.After(addressPoll):
		}
	}
}

// agotado convierte lo último que se vio en la frase que nombra la CAUSA.
//
// # Por qué hace falta traducirlo
//
// Porque los dos finales posibles son problemas distintos y se leían igual. El
// mensaje que salía terminaba en `(route ip+net: no such network interface)`,
// que es lo que dice Go cuando el adaptador NO EXISTE, y quien lo recibe no
// tiene por qué saber que eso significa "el driver no llegó a crear nada" y no
// "la dirección no se pudo poner".
//
// Medido el 2026-08-11 en la máquina de un invitado que no podía entrar: treinta
// segundos, ese mensaje, y una ronda entera de diagnóstico buscando un conflicto
// de rangos que no existía. La información para descartarlo ya estaba en el
// error, ilegible.
//
// Los dos casos y lo que significan:
//
//   - **El adaptador no existe.** Es wintun, no direccionamiento: el driver no
//     está, no se pudo cargar, o algo lo bloqueó. Nada de lo que Kanpachi elija
//     lo cambia.
//   - **El adaptador existe con otras direcciones.** Ahí sí es direccionamiento,
//     y las direcciones que tiene son la pista, así que se dicen.
func agotado(adapter string, want netip.Addr, deadline time.Duration, last error) error {
	if last != nil && errors.Is(last, errSinInterfaz) {
		return fmt.Errorf("el adaptador virtual %q no llegó a existir en %s. "+
			"No es la dirección %v: el sistema dice que esa interfaz no está, o sea que "+
			"el driver de red virtual (wintun) no la creó. Suele ser un antivirus que lo "+
			"bloquea o que Kanpachi no corre como administrador; kanpachi-engine.log "+
			"tiene el motivo exacto",
			adapter, deadline, want)
	}
	return fmt.Errorf("el adaptador %q existe y no tomó la dirección %v en %s: %v",
		adapter, want, deadline, last)
}

// addrLister reads the addresses configured on one adapter.
//
// Injected so the waiting itself is tested on Linux, where none of these
// adapters exist. What can be got wrong here is the loop and its deadline, not
// the system call.
type addrLister func(adapter string) ([]netip.Addr, error)

// errSinInterfaz es que el adaptador NO EXISTE, y va aparte de cualquier otro
// fallo al leerlo.
//
// Se marca acá, en el único sitio que sabe qué preguntó, en vez de reconocer el
// texto de Go más arriba. Quien puede afirmar "no está" es quien lo buscó, y así
// la cadena frágil queda en UNA línea en vez de repartida.
var errSinInterfaz = errors.New("el adaptador no existe")

// sinInterfaz reconoce ese caso en lo que devuelve `net`, que es por texto
// porque no hay otra forma. **Medido**, no supuesto:
//
//	texto      : route ip+net: no such network interface
//	tipo       : *net.OpError
//	errors.As  : false        ← con net.UnknownNetworkError
//	op="route" net="ip+net" err=no such network interface
//
// El error de dentro es `net.errNoSuchInterface`, que no se exporta, así que no
// hay centinela contra el que comparar y ningún tipo público que distinga este
// fallo de los otros del mismo `OpError`. La primera versión de esto probaba
// también `errors.As` contra `net.UnknownNetworkError`; se quitó al medir que no
// casa nunca, porque una rama muerta hace creer que hay un camino tipado.
//
// El coste de equivocarse es un mensaje menos preciso, nunca un comportamiento
// distinto: quien lo llama ya está en el camino de fallo.
func sinInterfaz(err error) bool {
	return strings.Contains(err.Error(), "no such network interface")
}

// addressesOf is the real one. It is `net` and not a Windows call, so it needs
// no build tag and no privileges: reading an interface is not listening on one.
func addressesOf(adapter string) ([]netip.Addr, error) {
	iface, err := net.InterfaceByName(adapter)
	if err != nil {
		// El caso de no encontrarlo se distingue del resto, porque significa una
		// cosa distinta y con otro culpable. Ver [agotado].
		if sinInterfaz(err) {
			return nil, fmt.Errorf("%w: %s: %w", errSinInterfaz, adapter, err)
		}
		return nil, err
	}
	raw, err := iface.Addrs()
	if err != nil {
		return nil, err
	}

	out := make([]netip.Addr, 0, len(raw))
	for _, a := range raw {
		// Addrs returns *net.IPNet in practice, and an interface with no
		// address returns an empty slice rather than an error, which is exactly
		// the state being waited out.
		n, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		addr, ok := netip.AddrFromSlice(n.IP)
		if !ok {
			continue
		}
		out = append(out, addr.Unmap())
	}
	return out, nil
}
