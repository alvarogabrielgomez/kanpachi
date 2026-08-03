package pipe

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
)

// TokenFile es el nombre del archivo dentro de ProgramData\Kanpachi.
const TokenFile = "api.token"

// tokenBytes son 32, o sea 256 bits.
//
// Con eso la fuerza bruta deja de ser un ataque y pasa a ser aritmética. No
// hace falta más y hace falta que no sea menos: un token corto se adivina desde
// otro proceso del usuario, que es exactamente quien puede hablarle al pipe.
const tokenBytes = 32

// NewToken genera un token nuevo.
//
// base64url sin relleno para que sea una sola palabra sin caracteres que haya
// que escapar en ningún sitio: viaja en JSON, se lee de un archivo y a veces se
// pega en una línea de comandos.
func NewToken() (string, error) {
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		// Sin aleatoriedad no se inventa un respaldo: un token predecible es
		// peor que no arrancar, porque parece que la puerta está cerrada.
		return "", fmt.Errorf("pipe: no se pudo generar el token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// WriteToken escribe el token en el directorio de datos.
//
// # Por qué no usa safewrite
//
// [safewrite.File] hace MkdirAll, o sea que crearía `ProgramData\Kanpachi` con
// los permisos que herede del padre si no existiera. Ese directorio lo crea el
// INSTALADOR con una ACL propia, escritura solo para SYSTEM y administradores, y
// esa ACL es la mitad de la protección de todo lo que hay dentro. Crearlo por
// accidente desde acá la perdería en silencio.
//
// Así que este escribe con apertura propia y falla si el directorio no está: un
// daemon que arranca sin su directorio tiene un problema de instalación, y
// taparlo creándolo mal es peor que decirlo.
//
// # Por qué no es atómico
//
// El patrón de escribir a un temporal y renombrar sirve para no dejar un
// archivo a medias si el proceso muere. Acá no aplica: el token se escribe una
// vez al arrancar, antes de que exista nadie que pueda leerlo, y si el proceso
// muere ahí no hay daemon al que hablarle. Un temporal con el token dentro sería
// una copia más del secreto en disco sin comprar nada.
func WriteToken(dir, token string) error {
	if token == "" {
		return fmt.Errorf("pipe: no se escribe un token vacío")
	}
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("pipe: el directorio de datos %s no está: %w", dir, err)
	}

	ruta := filepath.Join(dir, TokenFile)
	// O_TRUNC porque el del arranque anterior puede seguir ahí si el daemon
	// murió sucio. 0600 es lo que Go traduce en Windows a una ACL restrictiva,
	// y la protección de verdad la da la ACL heredada del directorio.
	f, err := os.OpenFile(ruta, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("pipe: abriendo %s: %w", ruta, err)
	}
	if _, err := f.WriteString(token); err != nil {
		_ = f.Close()
		return fmt.Errorf("pipe: escribiendo %s: %w", ruta, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("pipe: cerrando %s: %w", ruta, err)
	}
	return nil
}

// RemoveToken borra el archivo.
//
// **Se llama en TODO camino de salida, con defer y no solo tras cerrar el
// oyente.** El token vale exactamente lo que dura el proceso: rota una vez por
// vida, así que uno que sobreviva al proceso no abre nada y solo es un secreto
// muerto en disco esperando a que alguien lo lea.
//
// Y de paso contesta una pregunta que los docs dejaban abierta: qué pasa con
// una UI conectada cuando el servicio reinicia. No pasa nada, porque no existe
// ese caso: el pipe muere con el proceso y el token siguiente es otro.
//
// Que el archivo no esté NO es un error: es lo normal si nunca se escribió.
func RemoveToken(dir string) error {
	err := os.Remove(filepath.Join(dir, TokenFile))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("pipe: borrando el token: %w", err)
	}
	return nil
}

// ReadToken lee el token. Lo usa el CLIENTE, no el daemon.
//
// Se relee en CADA conexión y no se recuerda: el daemon lo rota al arrancar, así
// que un token guardado de la sesión anterior es siempre el equivocado, y el
// síntoma sería un "unauthorized" que nadie sabe explicar.
func ReadToken(dir string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(dir, TokenFile))
	if err != nil {
		return "", fmt.Errorf("pipe: leyendo el token: %w", err)
	}
	if len(raw) == 0 {
		return "", fmt.Errorf("pipe: el token está vacío, el daemon no terminó de arrancar")
	}
	return string(raw), nil
}
