//go:build !windows && !linux

package main

// Fuera de Windows y de Linux no hay nada que diagnosticar, porque no hay
// producto que diagnosticar: el canal local está escrito para esos dos.
//
// Se declara igual para que `go build ./...` siga compilando, que es lo que
// mantiene el resto del binario compilable en el CI de cualquier sistema.

func chequeosDelSistema() []chequeo { return []chequeo{chequeoDelCanal()} }

func pistaDeElevación() string {
	return "The local channel is written for Windows and for Linux."
}

// rutaDelMotor contesta vacío: sin producto no hay motor que nombrar.
func rutaDelMotor() string { return "" }
