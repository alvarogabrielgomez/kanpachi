package usecase

// El password del registro propio, para poder hospedar en él.
//
// # Lo que este fichero existe para NO hacer
//
// El password entra, se convierte en una prueba y se olvida. No se guarda, no se
// registra, no entra en el diario de progreso y no vuelve por el cable. Lo único
// que sobrevive es el refresh token que el adaptador deja sellado en disco, que
// caduca y que el operador revoca cambiando el password.
//
// Por eso este caso de uso NO usa el diario: la función que lo llevaría pintaría
// sus parámetros en una pantalla que se copia al portapapeles y se pega en un
// grupo.

import (
	"context"
	"fmt"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// SeedPassword entrega el password del registro de esta máquina.
//
// # Por qué el registro sale de acá y no del que llama
//
// Porque el host va DENTRO de la prueba, y equivocarlo produce una credencial
// que no vale en ningún sitio. Quien sabe a qué registro abre salas esta máquina
// es esta capa, así que el parámetro es solo el password y no puede desalinearse
// con nada.
//
// Sin registro configurado devuelve [port.ErrNoOwnSeed], que lleva a la pantalla
// de configuración y no a reintentar.
func (s *Session) SeedPassword(ctx context.Context, password string) error {
	if err := domain.ValidateSeedPassword(password); err != nil {
		return err
	}
	dir, err := s.deps.Directories.Own()
	if err != nil {
		return err
	}
	proof, err := domain.SeedAuthProof(dir.Seed(), password)
	if err != nil {
		return err
	}
	if err := dir.Authenticate(ctx, proof); err != nil {
		// El error se envuelve SIN el password y sin la prueba. Es la misma
		// clase de fuga que ya se cerró en el log del motor, y falla igual de
		// callada: nadie mira un mensaje de error hasta que lo pega en un chat.
		return fmt.Errorf("el registro %s no aceptó la credencial: %w", dir.Seed(), err)
	}
	s.deps.Log.Info("credencial aceptada por el registro", "seed", dir.Seed())
	return nil
}

// No hay ningún "¿este registro pide password?" y esa ausencia es deliberada.
//
// Preguntarlo por adelantado sería un viaje que casi siempre sobra, porque casi
// ningún seed va a tener password, y una respuesta que envejece entre que se
// pide y que hace falta. Lo que dice la verdad es intentar abrir la sala: el
// registro contesta [port.ErrSeedPassword] y la cara lleva a escribirlo. La
// única pantalla que aparece es la que hace falta, y aparece cuando hace falta.
