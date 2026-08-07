package usecase

import (
	"testing"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// TestCardKeyOfAceptaElURIQueEntregaWindows cubre la otra mitad del enlace
// real: además de parsear la sala, la barra vacía que agrega Chromium no puede
// hacer que se pierda la clave con que se abre su tarjeta de presentación.
func TestCardKeyOfAceptaElURIQueEntregaWindows(t *testing.T) {
	const fragmento = "z39-MCRbmvy94i8hoxe9O_yGveuMhObC5XiZKhde9Gw"
	key, ok := cardKeyOf("kanpachi://AB4N548B/#" + fragmento)
	if !ok {
		t.Fatal("no se extrajo una clave válida del URI canonicalizado por Windows")
	}
	if got := domain.CardKeyFragment(key); got != fragmento {
		t.Errorf("fragmento reconstruido = %q, se esperaba %q", got, fragmento)
	}
}
