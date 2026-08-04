//go:build windows

package windowscom

// Calling IDispatch::Invoke by hand, for the one property go-ole cannot set.
//
// # Why this file exists
//
// INetFwRule::put_Interfaces wants a VARIANT holding a SAFEARRAY of VARIANTs,
// each holding a BSTR, passed BY VALUE. go-ole can produce neither half: its
// []string case builds VT_ARRAY|VT_BSTR, and its only case for a *VARIANT wraps
// it in VT_VARIANT|VT_BYREF. Its DISPPARAMS type has unexported fields, so the
// call cannot be assembled from outside the package either.
//
// # What was measured, and it settles all of it
//
// Four shapes were tried against a real rule object on 2026-08-04:
//
//	VT_ARRAY|VT_VARIANT by value    works, and reads back as 0x200C
//	VT_ARRAY|VT_BSTR    by value    scode 0x80070057, E_INVALIDARG
//	anything            by ref      DISP_E_EXCEPTION with no description
//	a name no adapter has           scode 0x80070490, ERROR_NOT_FOUND
//
// The last line is worth keeping in mind: the property VALIDATES the name
// against the adapters the machine actually has. A rule scoped to an adapter
// that is not up fails loudly instead of quietly scoping to nothing, which is
// the direction this codebase wants.
//
// Without this, every rule write failed with a bare "Exception occurred", so
// the permit layer wrote nothing at all.

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/go-ole/go-ole"
	"golang.org/x/sys/windows"
)

var (
	oleaut32 = windows.NewLazySystemDLL("oleaut32.dll")

	procSafeArrayCreateVector = oleaut32.NewProc("SafeArrayCreateVector")
	procSafeArrayPutElement   = oleaut32.NewProc("SafeArrayPutElement")
	procSafeArrayDestroy      = oleaut32.NewProc("SafeArrayDestroy")
	procSysAllocString        = oleaut32.NewProc("SysAllocString")
	procSysFreeString         = oleaut32.NewProc("SysFreeString")
)

// dispParams mirrors go-ole's DISPPARAMS, whose fields are unexported.
type dispParams struct {
	rgvarg            uintptr
	rgdispidNamedArgs uintptr
	cArgs             uint32
	cNamedArgs        uint32
}

// excepInfo mirrors EXCEPINFO. It is read for scode, which is where the object
// says WHY it refused. go-ole surfaces only "Exception occurred", which named
// none of the four distinct failures above.
type excepInfo struct {
	wCode             uint16
	wReserved         uint16
	bstrSource        *uint16
	bstrDescription   *uint16
	bstrHelpFile      *uint16
	dwHelpContext     uint32
	_                 uint32
	pvReserved        uintptr
	pfnDeferredFillIn uintptr
	scode             uint32
	_                 uint32
}

const (
	dispatchPropertyPut = 4
	dispidPropertyPut   = -3
	localeUserDefault   = 0x400

	// errorNotFound is what put_Interfaces answers for a name no adapter has.
	errorNotFound = 0x80070490
)

var iidNull = ole.GUID{}

// setInterfaces scopes a rule to the named adapters.
//
// Windows stores this as the adapter's GUID and hands it back resolved to a
// name, which is where the two properties that matter come from: the scope
// survives the user renaming the connection, and it does NOT survive the
// adapter being recreated with a new GUID. The second one is exactly what Apply
// repairs by enumerating what is live and diffing.
func setInterfaces(rule *ole.IDispatch, names []string) error {
	if len(names) == 0 {
		// Writing an empty array scopes the rule to no interface at all on some
		// builds, which turns a permit into a rule that opens nothing.
		return nil
	}

	v, err := variantOfStrings(names)
	if err != nil {
		return err
	}
	defer func() { _ = v.Clear() }()

	scode, err := putByValue(rule, "Interfaces", *v)
	if err != nil {
		if scode == errorNotFound {
			return fmt.Errorf("no adapter on this machine is called %v, so the rule was "+
				"not written: %w", names, err)
		}
		return err
	}
	return nil
}

// putByValue sets a property passing the VARIANT as it is, not wrapped.
//
// Returns the scode from EXCEPINFO alongside the error so the caller can tell
// the failures apart, since they all arrive as the same DISP_E_EXCEPTION.
func putByValue(disp *ole.IDispatch, name string, v ole.VARIANT) (uint32, error) {
	ids, err := disp.GetIDsOfName([]string{name})
	if err != nil {
		return 0, fmt.Errorf("looking up the dispatch id of %s: %w", name, err)
	}

	named := int32(dispidPropertyPut)
	args := [1]ole.VARIANT{v}
	params := dispParams{
		rgvarg:            uintptr(unsafe.Pointer(&args[0])),
		rgdispidNamedArgs: uintptr(unsafe.Pointer(&named)),
		cArgs:             1,
		cNamedArgs:        1,
	}

	var info excepInfo
	var argErr uint32
	hr, _, _ := syscall.SyscallN(disp.VTable().Invoke,
		uintptr(unsafe.Pointer(disp)),
		uintptr(ids[0]),
		uintptr(unsafe.Pointer(&iidNull)),
		localeUserDefault,
		dispatchPropertyPut,
		uintptr(unsafe.Pointer(&params)),
		0,
		uintptr(unsafe.Pointer(&info)),
		uintptr(unsafe.Pointer(&argErr)),
	)

	runtime.KeepAlive(&args)
	runtime.KeepAlive(&named)
	runtime.KeepAlive(&params)

	if uint32(hr) == 0 {
		return 0, nil
	}
	desc := ""
	if info.bstrDescription != nil {
		desc = ole.BstrToString(info.bstrDescription)
	}
	return info.scode, fmt.Errorf("setting %s: Invoke returned 0x%X, scode 0x%X, %q",
		name, uint32(hr), info.scode, desc)
}

// variantOfStrings builds VT_ARRAY|VT_VARIANT holding BSTRs.
//
// The caller Clears the returned VARIANT, which destroys the array and every
// string in it.
func variantOfStrings(vals []string) (*ole.VARIANT, error) {
	sa, _, _ := procSafeArrayCreateVector.Call(uintptr(ole.VT_VARIANT), 0, uintptr(len(vals)))
	if sa == 0 {
		return nil, fmt.Errorf("SafeArrayCreateVector returned null")
	}

	for i, s := range vals {
		u16, err := windows.UTF16PtrFromString(s)
		if err != nil {
			procSafeArrayDestroy.Call(sa)
			return nil, err
		}
		bstr, _, _ := procSysAllocString.Call(uintptr(unsafe.Pointer(u16)))
		runtime.KeepAlive(u16)
		if bstr == 0 {
			procSafeArrayDestroy.Call(sa)
			return nil, fmt.Errorf("SysAllocString returned null")
		}

		elem := ole.NewVariant(ole.VT_BSTR, int64(bstr))
		idx := int32(i)
		r, _, _ := procSafeArrayPutElement.Call(sa,
			uintptr(unsafe.Pointer(&idx)), uintptr(unsafe.Pointer(&elem)))

		// SafeArrayPutElement COPIES the element, so this BSTR is ours to free.
		// Leaving it would leak one string per rule per Apply, forever.
		procSysFreeString.Call(bstr)

		if r != 0 {
			procSafeArrayDestroy.Call(sa)
			return nil, fmt.Errorf("SafeArrayPutElement returned 0x%X", uint32(r))
		}
	}

	v := ole.NewVariant(ole.VT_ARRAY|ole.VT_VARIANT, int64(sa))
	return &v, nil
}
