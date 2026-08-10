//go:build windows

package netfw

import (
	"context"
	"errors"
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
		if err != nil && !isEndOfEnum(err) {
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

// sFalse is S_FALSE, and it is a SUCCESS code even though it is not zero.
//
// IEnumVARIANT::Next returns it to say "I gave you fewer items than you asked
// for", which is exactly what happens on the last call of every enumeration.
const sFalse = 1

// isEndOfEnum says whether an error from Next is really the end of the walk.
//
// # Why this exists, and what it cost not to have it
//
// go-ole turns ANY non-zero HRESULT into an error, S_FALSE included. So the
// normal, unavoidable end of every enumeration arrived here as a failure, and
// the message it carried was "Incorrect function", because 1 read as a system
// error code is ERROR_INVALID_FUNCTION.
//
// The effect was total: every sweep of the rule store ended in an error, so
// Apply, PurgeOwned, AuditForeign and Enforcement all failed on any real
// machine, always. Not one test caught it, and none could have: the whole thing
// lives inside a COM call. It took running the adapter for the first time.
//
// The check is on the code and not on the message, because the message is
// localised and this program refuses to parse localised text. That is the same
// reason the adapter uses COM instead of netsh.
func isEndOfEnum(err error) bool {
	var oleErr *ole.OleError
	return errors.As(err, &oleErr) && oleErr.Code() == sFalse
}

// propReader reads properties off one rule and remembers the FIRST failure.
//
// # Why a failed read cannot return a zero value
//
// This started as three helpers that returned "" or 0 when GetProperty failed,
// and that shape is dangerous in a very specific way: Action 0 IS
// NET_FW_ACTION_BLOCK, so a failed read turns a live permissive rule into a
// block, the audit drops it, and the screen says there is nothing to see.
//
// A measurement that failed must never be indistinguishable from a measurement
// that came back clean. That is the whole reason [domain.AlertAuditFailed]
// exists, and it would have been defeated one layer below it.
//
// The first error is kept and the rest of the reads run anyway, so the struct
// is filled in one readable block instead of ten `if err != nil`.
type propReader struct {
	rule *ole.IDispatch
	err  error
}

func (r *propReader) get(name string) (*ole.VARIANT, bool) {
	v, err := oleutil.GetProperty(r.rule, name)
	if err != nil {
		if r.err == nil {
			r.err = fmt.Errorf("reading property %s of a firewall rule: %w", name, err)
		}
		return nil, false
	}
	return v, true
}

func (r *propReader) str(name string) string {
	v, ok := r.get(name)
	if !ok {
		return ""
	}
	defer func() { _ = v.Clear() }()
	// ToString guards on the type itself and hands back "" for VT_NULL, which
	// is what a rule with no program has in ApplicationName. That is not a
	// failure, it is the honest answer.
	return v.ToString()
}

func (r *propReader) int32(name string) int32 {
	v, ok := r.get(name)
	if !ok {
		return 0
	}
	defer func() { _ = v.Clear() }()
	return int32(v.Val)
}

func (r *propReader) bool(name string) bool {
	v, ok := r.get(name)
	if !ok {
		return false
	}
	defer func() { _ = v.Clear() }()
	return v.Value() == true
}

// strs reads a property that arrives as a VARIANT holding an array of strings.
//
// Empty is normal and is not an error: most rules carry no interface scope, and
// Windows answers those with VT_EMPTY rather than an array of length zero.
//
// # Why this does not call ToStringArray, which is the obvious call
//
// Because INetFwRule::get_Interfaces hands back VT_ARRAY|VT_VARIANT, an array of
// VARIANTs that each hold a BSTR, and not the VT_ARRAY|VT_BSTR that the name
// suggests. ToStringArray reads every element as if it were a BSTR pointer, so
// on the first interface-scoped rule it dereferences the low bytes of a VARIANT
// header as an address and the process dies with an access violation.
//
// This is not theoretical and it is not rare. It crashed on the FIRST real run,
// on a machine whose only interface-scoped rules are the ones Microsoft ships
// for the WSL virtual switch. In the daemon that is a process running as SYSTEM
// taking down the whole firewall layer, and no unit test would have found it:
// the crash is inside the COM call.
//
// ToValueArray asks the SAFEARRAY itself what its elements are, so it handles
// both shapes and there is nothing here left to guess.
func (r *propReader) strs(name string) []string {
	v, ok := r.get(name)
	if !ok {
		return nil
	}
	defer func() { _ = v.Clear() }()

	if v.VT&ole.VT_ARRAY == 0 {
		// VT_EMPTY or VT_NULL: no interface scope. The honest answer.
		return nil
	}
	arr := v.ToArray()
	if arr == nil {
		return nil
	}
	// No Release on arr: the VARIANT owns the array and v.Clear frees it.

	vals := arr.ToValueArray()
	out := make([]string, 0, len(vals))
	for i, raw := range vals {
		s, ok := raw.(string)
		if !ok {
			// Not dropped quietly. An interface that vanishes from the list makes
			// a scoped rule look unscoped, and the diff would then rewrite it on
			// every heartbeat forever.
			if r.err == nil {
				r.err = fmt.Errorf("element %d of property %s is a %T and not a string", i, name, raw)
			}
			return nil
		}
		if s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (r *propReader) Err() error { return r.err }
