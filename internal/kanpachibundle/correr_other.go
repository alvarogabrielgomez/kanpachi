//go:build !windows

package main

import "errors"

// Lo que este bundle transporta son binarios de Windows: el daemon, la
// interfaz, el motor, el driver del adaptador virtual y las DLL. Fuera de
// Windows no hay nada que soltar ni nada que ejecutar.
//
// Existe para que el job de Linux del CI compile y vete el paquete, igual que
// el gemelo de roombundle.
func correr() error {
	return errors.New("kanpachibundle transporta binarios de Windows, así que solo corre ahí")
}
