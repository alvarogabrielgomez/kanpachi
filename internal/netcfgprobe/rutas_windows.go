//go:build windows

package main

import (
	"fmt"
	"net"
	"net/netip"
	"os/exec"
	"strconv"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// métricaDeMentira es altísima a propósito.
//
// La ruta por defecto que este arnés fabrica existe para que netcfg la borre, y
// mientras tanto no puede llevarse ni un paquete del usuario. Con esta métrica
// pierde contra cualquier ruta real de la máquina.
const métricaDeMentira = 9999

// adaptadorDeLaSala lee la IP virtual de la sala abierta.
//
// Falla si no hay sala, y ese es el punto: sin adaptador este arnés no mide
// nada, igual que una medición de sockets con `no_tun` no medía nada.
func adaptadorDeLaSala() (netip.Addr, netip.Prefix, error) {
	iface, err := net.InterfaceByName(domain.AdapterName)
	if err != nil {
		return netip.Addr{}, netip.Prefix{}, fmt.Errorf(
			"no hay adaptador %s. Abre una sala primero: este arnés mide sobre una sala viva",
			domain.AdapterName)
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return netip.Addr{}, netip.Prefix{}, err
	}
	for _, a := range addrs {
		n, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		addr, ok := netip.AddrFromSlice(n.IP)
		if !ok || !addr.Unmap().Is4() {
			continue
		}
		ones, _ := n.Mask.Size()
		return addr.Unmap(), netip.PrefixFrom(addr.Unmap(), ones).Masked(), nil
	}
	return netip.Addr{}, netip.Prefix{}, fmt.Errorf("%s no tiene dirección IPv4", domain.AdapterName)
}

// hayRuta pregunta al SISTEMA si el destino está sobre el adaptador de la sala.
//
// Se le pregunta al sistema y nunca a netcfg. Si el que verifica usara el
// recuerdo del que escribe, un `Apply` que no escribió nada y se lo apuntó igual
// pasaría en verde.
func hayRuta(p netip.Prefix) bool {
	luid, err := luidDeLaSala()
	if err != nil {
		return false
	}

	var table *windows.MibIpForwardTable2
	if err := windows.GetIpForwardTable2(windows.AF_UNSPEC, &table); err != nil {
		return false
	}
	defer windows.FreeMibTable(unsafe.Pointer(table))

	for _, r := range table.Rows() {
		if r.InterfaceLuid != luid || r.DestinationPrefix.Prefix.Family != windows.AF_INET {
			continue
		}
		raw := (*windows.RawSockaddrInet4)(unsafe.Pointer(&r.DestinationPrefix.Prefix))
		got := netip.PrefixFrom(netip.AddrFrom4(raw.Addr), int(r.DestinationPrefix.PrefixLength))
		if got == p.Masked() {
			return true
		}
	}
	return false
}

func luidDeLaSala() (uint64, error) {
	iface, err := net.InterfaceByName(domain.AdapterName)
	if err != nil {
		return 0, err
	}
	var luid uint64
	proc := windows.NewLazySystemDLL("iphlpapi.dll").NewProc("ConvertInterfaceIndexToLuid")
	if r, _, _ := proc.Call(uintptr(iface.Index), uintptr(unsafe.Pointer(&luid))); r != 0 {
		return 0, fmt.Errorf("ConvertInterfaceIndexToLuid devolvió %d", r)
	}
	return luid, nil
}

// ponerRutaPorDefecto fabrica el caso que netcfg tiene que limpiar.
//
// Con `route.exe`, que es una herramienta del sistema y no nuestro código: así
// la ruta que se va a borrar la puso alguien de fuera, que es exactamente el
// caso real (el motor instalando lo que aprendió de la red).
func ponerRutaPorDefecto() error {
	args := []string{"add", "0.0.0.0", "mask", "0.0.0.0", "0.0.0.0",
		"metric", strconv.Itoa(métricaDeMentira), "if", ifIndex()}
	if out, err := exec.Command("route.exe", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("route %v: %w (%s)", args, err, out)
	}
	return nil
}

func quitarRutaPorDefecto() error {
	return exec.Command("route.exe", "delete", "0.0.0.0", "mask", "0.0.0.0", "if", ifIndex()).Run()
}

func ifIndex() string {
	iface, err := net.InterfaceByName(domain.AdapterName)
	if err != nil {
		return "0"
	}
	return strconv.Itoa(iface.Index)
}
