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
// # No toma el candado de la SESIÓN, y sí el del fichero
//
// El de la sesión no, como [Session.SetOwnSeed] y por lo mismo: tomarlo metería
// una operación de disco dentro del candado que sostiene el estado que las
// caras consultan cada segundo, y que está tomado el minuto entero que tarda
// abrir una sala.
//
// El del fichero sí, y eso es nuevo. Desde que el perfil guarda cuatro cosas,
// este método **lee y reescribe** en vez de codificar un perfil con solo el
// nombre dentro: con un campo era lo mismo, con cuatro, guardar el nombre
// apagaría la narración y borraría el tamaño de la ventana. Y leer y reescribir
// es exactamente lo que se pierde cuando dos escrituras se cruzan.
func (s *Session) SetNickname(_ context.Context, nick string) (string, error) {
	limpio, err := domain.ParseNickname(nick)
	if err != nil {
		return "", err
	}
	s.profileMu.Lock()
	defer s.profileMu.Unlock()
	if _, err := s.guardarPerfilConCandado(func(p domain.Profile) domain.Profile {
		p.Nick = limpio
		return p
	}); err != nil {
		return "", err
	}
	s.deps.Log.Info("nombre de esta máquina", "apodo", limpio.String())
	return limpio.String(), nil
}

// Settings contesta lo que esta máquina recuerda de cómo se presenta.
//
// Nunca falla: un fichero ausente o ilegible da el perfil cero, que es lo que
// hay en un primer arranque, y cada cara sabe qué ofrecer con eso. Mismo
// criterio que [Session.Nickname], que lee del mismo sitio.
func (s *Session) Settings() domain.Profile {
	raw, err := s.deps.State.LoadProfile()
	if err != nil {
		return domain.Profile{}
	}
	return domain.ParseProfile(raw)
}

// SetSettings cambia SOLO los campos que llegan y devuelve lo que quedó.
//
// # Por qué un parche y no el perfil entero
//
// Porque quien manda un ajuste no conoce los demás. La ventana que apaga la
// narración no sabe qué versión publicada encontró la terminal hace un rato, y
// mandar el objeto entero lo pisaría con lo que esa ventana tuviera en memoria.
// Qué significa aplicar el parche lo decide [domain.ApplySettings], que es
// donde viven las invariantes.
//
// # Y por qué el nombre no viaja por acá
//
// Porque tiene validación y una sugerencia derivada, y las dos están en
// [Session.SetNickname]. Un escritor por hecho.
func (s *Session) SetSettings(_ context.Context, in domain.SettingsPatch) (domain.Profile, error) {
	s.profileMu.Lock()
	defer s.profileMu.Unlock()
	return s.guardarPerfilConCandado(func(p domain.Profile) domain.Profile {
		return domain.ApplySettings(p, in)
	})
}

// guardarPerfilConCandado lee, deja que le cambien lo suyo, y escribe.
//
// Con `profileMu` tomado por quien llama, que es lo que hace que el ciclo de
// leer y escribir sea uno solo y no dos.
func (s *Session) guardarPerfilConCandado(
	cambiar func(domain.Profile) domain.Profile,
) (domain.Profile, error) {
	// Un fichero que no se puede leer se trata como un primer arranque y no
	// como un fallo, por lo mismo que [Session.Nickname]: negarse acá dejaría a
	// alguien sin poder elegir su nombre porque el fichero anterior está roto.
	actual := domain.Profile{}
	if raw, err := s.deps.State.LoadProfile(); err == nil {
		actual = domain.ParseProfile(raw)
	}
	nuevo := cambiar(actual)
	raw, err := domain.EncodeProfile(nuevo)
	if err != nil {
		return domain.Profile{}, err
	}
	if err := s.deps.State.SaveProfile(raw); err != nil {
		return domain.Profile{}, err
	}
	return nuevo, nil
}
