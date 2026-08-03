//go:build windows

package windowscom

import (
	"context"
	"fmt"
	"runtime"

	"github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
)

// COM is apartment threaded, and go-ole inherits that whole world: the
// apartment belongs to the OS THREAD that initialised it, and Go moves
// goroutines between threads whenever it likes. Calling an interface from a
// thread that never called CoInitializeEx fails at best and corrupts at worst,
// and it does so intermittently, which is the worst way for it to fail.
//
// So every COM call in this package runs on ONE thread that is locked for the
// lifetime of the adapter. Callers send closures to it and read the result
// back. That also serialises access for free, which the firewall store wants
// anyway.

type job struct {
	fn   func(policy *ole.IDispatch) error
	done chan error
}

// apartment owns the locked thread and the policy object living on it.
type apartment struct {
	jobs chan job
	stop chan struct{}
	// ready reports how the thread came up. Nil means the apartment is usable.
	ready chan error
}

func newApartment() (*apartment, error) {
	a := &apartment{
		jobs:  make(chan job),
		stop:  make(chan struct{}),
		ready: make(chan error, 1),
	}
	go a.run()
	if err := <-a.ready; err != nil {
		return nil, err
	}
	return a, nil
}

func (a *apartment) run() {
	// The lock is never released. Unlocking would let Go hand this thread to
	// another goroutine, and the apartment would outlive its owner.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// COINIT_APARTMENTTHREADED is what HNetCfg.FwPolicy2 expects. RPC_E_CHANGED_MODE
	// means something else already initialised this thread differently, and
	// since the thread is ours and brand new, that would be a real surprise
	// rather than the usual benign case.
	if err := ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED); err != nil {
		oleErr, ok := err.(*ole.OleError)
		if !ok || oleErr.Code() != 0x80010106 {
			a.ready <- fmt.Errorf("initialising COM: %w", err)
			return
		}
	}
	defer ole.CoUninitialize()

	unknown, err := oleutil.CreateObject("HNetCfg.FwPolicy2")
	if err != nil {
		a.ready <- fmt.Errorf("creating HNetCfg.FwPolicy2, which is the Windows Firewall itself: %w", err)
		return
	}
	defer unknown.Release()

	policy, err := unknown.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		a.ready <- fmt.Errorf("querying IDispatch on the firewall policy: %w", err)
		return
	}
	defer policy.Release()

	a.ready <- nil

	for {
		select {
		case <-a.stop:
			return
		case j := <-a.jobs:
			j.done <- j.fn(policy)
		}
	}
}

// do runs fn on the apartment thread.
//
// It honours ctx on the WAY IN and not on the way out: a COM call already in
// flight cannot be cancelled, and abandoning it would leave the next caller
// reading someone else's answer. Cancellation means "do not start", never "stop
// halfway".
func (a *apartment) do(ctx context.Context, fn func(policy *ole.IDispatch) error) error {
	done := make(chan error, 1)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-a.stop:
		return fmt.Errorf("the firewall adapter is closed")
	case a.jobs <- job{fn: fn, done: done}:
	}
	return <-done
}

func (a *apartment) Close() error {
	select {
	case <-a.stop:
	default:
		close(a.stop)
	}
	return nil
}

// rulesOf gets the rule collection. The caller releases it.
func rulesOf(policy *ole.IDispatch) (*ole.IDispatch, error) {
	v, err := oleutil.GetProperty(policy, "Rules")
	if err != nil {
		return nil, fmt.Errorf("reading the rule collection: %w", err)
	}
	return v.ToIDispatch(), nil
}

// eachRule walks the collection and calls fn with every rule.
//
// Enumerating and releasing by hand is not optional here: the collection hands
// out a fresh IDispatch per rule, and a store with a thousand rules leaks a
// thousand objects per sweep if they are not released. The sweep runs every
// minute for as long as the daemon lives.
//
// fn returning false stops the walk early.
func eachRule(rules *ole.IDispatch, fn func(rule *ole.IDispatch) (bool, error)) error {
	enumV, err := oleutil.GetProperty(rules, "_NewEnum")
	if err != nil {
		return fmt.Errorf("enumerating the rules: %w", err)
	}
	enum, err := enumV.ToIUnknown().IEnumVARIANT(ole.IID_IEnumVariant)
	if err != nil {
		return fmt.Errorf("getting the rule enumerator: %w", err)
	}
	defer enum.Release()

	for {
		v, length, err := enum.Next(1)
		if err != nil {
			return fmt.Errorf("advancing the rule enumerator: %w", err)
		}
		if length == 0 {
			return nil
		}
		rule := v.ToIDispatch()
		cont, ferr := fn(rule)
		rule.Release()
		if ferr != nil {
			return ferr
		}
		if !cont {
			return nil
		}
	}
}

func strProp(d *ole.IDispatch, name string) string {
	v, err := oleutil.GetProperty(d, name)
	if err != nil {
		return ""
	}
	defer func() { _ = v.Clear() }()
	return v.ToString()
}

func intProp(d *ole.IDispatch, name string) int32 {
	v, err := oleutil.GetProperty(d, name)
	if err != nil {
		return 0
	}
	defer func() { _ = v.Clear() }()
	return int32(v.Val)
}

func boolProp(d *ole.IDispatch, name string) bool {
	v, err := oleutil.GetProperty(d, name)
	if err != nil {
		return false
	}
	defer func() { _ = v.Clear() }()
	return v.Value() == true
}
