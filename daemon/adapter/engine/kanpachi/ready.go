package kanpachi

import (
	"context"
	"fmt"
	"net"
	"net/netip"
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
			return fmt.Errorf("el adaptador %q no tomó la dirección %v en %s: %w (%v)",
				adapter, want, deadline, ctx.Err(), last)
		case <-time.After(addressPoll):
		}
	}
}

// addrLister reads the addresses configured on one adapter.
//
// Injected so the waiting itself is tested on Linux, where none of these
// adapters exist. What can be got wrong here is the loop and its deadline, not
// the system call.
type addrLister func(adapter string) ([]netip.Addr, error)

// addressesOf is the real one. It is `net` and not a Windows call, so it needs
// no build tag and no privileges: reading an interface is not listening on one.
func addressesOf(adapter string) ([]netip.Addr, error) {
	iface, err := net.InterfaceByName(adapter)
	if err != nil {
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
