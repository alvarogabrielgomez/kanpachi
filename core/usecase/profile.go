package usecase

// El nombre de esta máquina: leerlo, sugerirlo y cambiarlo.
//
// # Por qué lo guarda el daemon y no cada cara
//
// Porque hay una máquina y tres caras, así que hay UN nombre. Mientras no tuvo
// dueño, la ventana escribía su fichero y la terminal el suyo, los dos en la
// misma carpeta de datos, y ganaba la sala el que hubiera corrido último:
// medido el 2026-08-18, una ventana que decía «Alvaro» y una sala que enseñaba
// «AlvaroGDeskt». Es el mismo argumento del registro propio, en [Session.OwnSeed].
//
// Y hay uno más, que en Windows instalado es de corrección y no de gusto: el
// directorio de datos deja a Users en solo lectura, y la ventana corre con el
// token del usuario de la sesión. De los tres procesos, el daemon es el único
// que puede escribir ahí.
//
// # Lo que NO cambia
//
// El apodo sigue viajando como parámetro de `create_room` y `join_room`, y la
// sala se sigue construyendo con lo que pasó quien llamó. Este fichero no toca
// eso: cambia quién lo recuerda entre sala y sala, no cómo llega a una.
//
// Y no toca una sala viva. La tarjeta ya está sellada y el motor ya recibió el
// nombre, así que cambiar el apodo con la sala abierta cambia el nombre de la
// PRÓXIMA vez que se entre, jamás el que los demás están viendo ahora.

import (
	"context"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// Nickname contesta el nombre que esta máquina eligió, o "" si nadie eligió.
//
// Vacío es una respuesta legítima y no un fallo, igual que en [Session.OwnSeed]:
// es el estado de un primer arranque. Quien pregunta decide qué hacer con eso,
// y lo que hacen las caras es ofrecer elegirlo o usar [Session.SuggestedNickname].
func (s *Session) Nickname() string {
	raw, err := s.deps.State.LoadProfile()
	if err != nil {
		return ""
	}
	// La lectura del dominio es tolerante a propósito: un nombre que ya no
	// valida devuelve el perfil cero. Ver [domain.ParseProfile].
	return domain.ParseProfile(raw).Nick.String()
}

// SuggestedNickname es con qué se prellena la pantalla, y con qué entra a una
// sala quien nunca eligió nombre. Nunca falla y NUNCA se guarda.
//
// Que no se guarde es la mitad del arreglo: una sugerencia escrita en disco no
// se distingue de una elección, y entonces le gana al nombre de verdad. Ese fue
// el defecto medido, no la duplicación de ficheros.
func (s *Session) SuggestedNickname() string {
	return domain.NicknameFromHost(s.deps.Hostname).String()
}

// SetNickname fija el nombre de esta máquina y devuelve el que quedó puesto.
//
// # El orden importa, y es el de SetOwnSeed sin la parte de red
//
// Valida y escribe. No hay sondeo que hacer: un nombre no depende de que nadie
// conteste, así que esto funciona sin conexión, que es justo lo que hace falta
// en el alta de un primer arranque.
//
// # No toma el candado de la sesión
//
// Como [Session.SetOwnSeed] y por lo mismo: lo único que toca es el disco, la
// escritura del almacén ya es atómica, y tomar el candado de la sala metería
// una operación de disco dentro del candado que sostiene el estado que las
// caras consultan cada segundo.
func (s *Session) SetNickname(_ context.Context, nick string) (string, error) {
	limpio, err := domain.ParseNickname(nick)
	if err != nil {
		return "", err
	}
	raw, err := domain.EncodeProfile(domain.Profile{Nick: limpio})
	if err != nil {
		return "", err
	}
	if err := s.deps.State.SaveProfile(raw); err != nil {
		return "", err
	}
	s.deps.Log.Info("nombre de esta máquina", "apodo", limpio.String())
	return limpio.String(), nil
}
