//go:build !windows

package netcfg

import (
	"context"
	"errors"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/port"
)

// Kanpachi's client is Windows only. This file exists so the rest of the
// package keeps compiling and being tested on the Linux CI job, which is where
// the tweak book and the route decisions are proven.
type Config struct {
	log  port.Logger
	book book
}

func New(dataDir string, log port.Logger) *Config {
	return &Config{log: log, book: newBook(dataDir)}
}

var _ port.NetConfigPort = (*Config)(nil)

var errSoloWindows = errors.New("netcfg: los ajustes del adaptador son de Windows, y este binario no lo es")

func (*Config) ApplyAdapter(context.Context, domain.AdapterState) error { return errSoloWindows }
func (*Config) SetDirectPlay(context.Context, bool) error               { return errSoloWindows }

// ProbeMTU devuelve cero Y error, y las dos mitades importan. El cero es lo que
// quien llama interpreta como "no se sondeó", que lo lleva a dejar el MTU que
// haya en vez de escribir uno inventado.
func (*Config) ProbeMTU(context.Context) (int, error) { return 0, errSoloWindows }

// RevertTweaks es lo único que NO falla acá.
//
// Su contrato es dejar la máquina sin nada pendiente que deshacer, y en un
// sistema donde nada se pudo aplicar eso ya es cierto. Devolver error obligaría
// al camino de salida de la sala a registrar un fallo por algo que no pasó.
func (c *Config) RevertTweaks(context.Context) error {
	t, err := c.book.load()
	if err != nil {
		return err
	}
	if t.empty() {
		return nil
	}
	return errSoloWindows
}
