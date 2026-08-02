// Package safewrite escribe archivos de forma que un apagón no deje basura.
//
// Lo usan los dos adaptadores de disco. Está aparte porque la propiedad que da
// es la misma para los dos y equivocarse en ella tiene el mismo precio: un
// archivo a medias en ProgramData es un daemon que no arranca, o peor, una sala
// que no se puede reabrir.
package safewrite

import (
	"fmt"
	"os"
	"path/filepath"
)

// File escribe raw en ruta de forma atómica.
//
// # Cómo, y por qué así
//
// Se escribe a un temporal en el MISMO directorio, se fuerza a disco, y se
// renombra encima. Las tres partes hacen falta:
//
//   - El mismo directorio, porque un rename entre volúmenes no es atómico y se
//     degrada a copiar y borrar, que es justo lo que se quiere evitar.
//   - El `Sync`, porque sin él el rename puede llegar al disco antes que el
//     contenido. Tras un corte de luz eso deja un archivo con el nombre bueno y
//     ceros dentro, que es peor que no tenerlo: parece válido.
//   - El rename, porque en Windows y en Linux reemplaza en un solo paso. Nadie
//     puede leer un archivo a medio escribir.
//
// El temporal se limpia si algo falla. Si el rename ya ocurrió, no hay nada que
// limpiar.
func File(ruta string, raw []byte, perm os.FileMode) error {
	dir := filepath.Dir(ruta)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creando %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(ruta)+".tmp-*")
	if err != nil {
		return fmt.Errorf("creando el temporal de %s: %w", ruta, err)
	}
	nombreTmp := tmp.Name()

	limpiar := func(causa error) error {
		tmp.Close()
		// El borrado no se comprueba: ya hay un error que reportar, y encadenar
		// el del borrado escondería la causa real detrás de un síntoma.
		_ = os.Remove(nombreTmp)
		return causa
	}

	if _, err := tmp.Write(raw); err != nil {
		return limpiar(fmt.Errorf("escribiendo %s: %w", ruta, err))
	}
	if err := tmp.Sync(); err != nil {
		return limpiar(fmt.Errorf("forzando %s a disco: %w", ruta, err))
	}
	// Los permisos se ponen antes del rename, no después: entre las dos
	// operaciones habría un instante con el archivo ya visible y abierto de más.
	if err := tmp.Chmod(perm); err != nil {
		return limpiar(fmt.Errorf("ajustando permisos de %s: %w", ruta, err))
	}
	if err := tmp.Close(); err != nil {
		return limpiar(fmt.Errorf("cerrando el temporal de %s: %w", ruta, err))
	}
	if err := os.Rename(nombreTmp, ruta); err != nil {
		_ = os.Remove(nombreTmp)
		return fmt.Errorf("reemplazando %s: %w", ruta, err)
	}
	return nil
}

// Backup guarda una copia de lo que había antes de escribir.
//
// No es control de versiones: es una sola copia, la anterior, y su único
// trabajo es que un archivo que se corrompió tenga de dónde volver a mano. Que
// no exista el original no es un error, es una instalación nueva.
func Backup(ruta, sufijo string) error {
	raw, err := os.ReadFile(ruta)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("leyendo %s para respaldarlo: %w", ruta, err)
	}
	return File(ruta+sufijo, raw, 0o644)
}
