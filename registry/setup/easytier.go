package setup

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// VersionEasyTier está FIJADA. Jamás `latest`.
//
// Una actualización sorpresa del motor cambia el comportamiento de la red de
// todo el grupo sin que nadie lo haya pedido, y el síntoma aparecería como
// "ayer funcionaba". Subir de versión es un cambio de código, con su commit.
const VersionEasyTier = "v2.6.4"

// Los checksums se calcularon descargando los archivos, no se copiaron de
// ningún sitio. Verificarlos importa: esto se baja por internet y termina
// ejecutándose como servicio en un servidor con IP pública.
var lanzamientos = map[string]struct {
	Archivo string
	SHA256  string
}{
	"amd64": {
		Archivo: "easytier-linux-x86_64-" + VersionEasyTier + ".zip",
		SHA256:  "61b659eaedba658fa66fe47d17e1426cdd77e5d02fa15fed447bb4357c09dfd6",
	},
	"arm64": {
		Archivo: "easytier-linux-aarch64-" + VersionEasyTier + ".zip",
		SHA256:  "f533ec25a7ea714e09f645615012200278058525795cc3bb690ff011aec1a70f",
	},
}

// BinariosNecesarios son los dos únicos que se instalan. El resto del zip trae
// easytier-web y easytier-web-embed, que son un panel de administración web
// que este proyecto no usa y que sería superficie expuesta a cambio de nada.
var BinariosNecesarios = []string{"easytier-core", "easytier-cli"}

// ArchivoVersion guarda qué versión de EasyTier quedó instalada.
//
// Sin él, "ya están" se contestaba mirando solo si los archivos existían, así
// que subir `VersionEasyTier` en un release NO reemplazaba nada: el droplet se
// quedaba con el motor viejo y `kanpseed version` anunciaba el nuevo. Un
// desajuste que no da error es peor que uno que sí.
const ArchivoVersion = "easytier.version"

// URLEasyTier arma la dirección del lanzamiento fijado para esta arquitectura.
func URLEasyTier() (string, string, error) {
	l, ok := lanzamientos[runtime.GOARCH]
	if !ok {
		return "", "", fmt.Errorf("no hay un EasyTier fijado para %s: las arquitecturas soportadas son amd64 y arm64", runtime.GOARCH)
	}
	url := "https://github.com/EasyTier/EasyTier/releases/download/" + VersionEasyTier + "/" + l.Archivo
	return url, l.SHA256, nil
}

// InstalarEasyTier descarga, verifica y coloca los dos binarios en destino.
//
// Devuelve true si hizo falta descargar. Si los binarios ya están y la versión
// coincide, no toca nada: `init` tiene que poder ejecutarse dos veces seguidas
// sin consecuencias.
func InstalarEasyTier(destino string, progreso func(string)) (bool, error) {
	if yaEstan(destino) {
		return false, nil
	}

	url, quiero, err := URLEasyTier()
	if err != nil {
		return false, err
	}
	progreso(fmt.Sprintf("descargando EasyTier %s (%s)", VersionEasyTier, runtime.GOARCH))

	tmp, err := os.CreateTemp("", "easytier-*.zip")
	if err != nil {
		return false, err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	cliente := &http.Client{Timeout: 10 * time.Minute}
	res, err := cliente.Get(url)
	if err != nil {
		return false, fmt.Errorf("descargando %s: %w", url, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return false, fmt.Errorf("descargando %s: el servidor respondió %s", url, res.Status)
	}

	suma := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, suma), res.Body); err != nil {
		return false, fmt.Errorf("descargando %s: %w", url, err)
	}
	tengo := hex.EncodeToString(suma.Sum(nil))
	if tengo != quiero {
		return false, fmt.Errorf("el SHA256 no coincide\n  esperado %s\n  obtenido %s\n"+
			"  no se instala nada: esto termina ejecutándose como servicio en un servidor público", quiero, tengo)
	}
	progreso("SHA256 verificado")

	if err := os.MkdirAll(destino, 0o755); err != nil {
		return false, err
	}
	if err := extraer(tmp.Name(), destino); err != nil {
		return false, err
	}
	// La marca se escribe DESPUÉS de extraer. Al revés, un fallo a mitad
	// dejaría una marca que promete unos binarios que no están.
	if err := os.WriteFile(filepath.Join(destino, ArchivoVersion), []byte(VersionEasyTier+"\n"), 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// VersionInstalada lee la marca. Devuelve "" si no hay ninguna, que es lo que
// pasa en una instalación anterior a que esta marca existiera.
func VersionInstalada(destino string) string {
	crudo, err := os.ReadFile(filepath.Join(destino, ArchivoVersion))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(crudo))
}

func yaEstan(destino string) bool {
	for _, b := range BinariosNecesarios {
		if _, err := os.Stat(filepath.Join(destino, b)); err != nil {
			return false
		}
	}
	// Una instalación sin marca se REESCRIBE una vez, y está bien que así sea:
	// es la única forma de saber qué motor tiene, y volver a bajarlo cuesta
	// menos que adivinar.
	return VersionInstalada(destino) == VersionEasyTier
}

// extraer saca solo los binarios que hacen falta.
//
// Ignora la estructura de carpetas del zip y se queda con el nombre base, para
// no depender de cómo se llame la carpeta raíz dentro del archivo. Y rechaza
// cualquier entrada cuyo nombre base no esté en la lista, lo cual además
// neutraliza el zip slip: nunca se construye una ruta con nada que venga del
// archivo salvo un nombre que ya validamos contra una lista fija.
func extraer(zipPath, destino string) error {
	z, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer z.Close()

	quiero := map[string]bool{}
	for _, b := range BinariosNecesarios {
		quiero[b] = true
	}

	encontrados := 0
	for _, f := range z.File {
		nombre := filepath.Base(strings.ReplaceAll(f.Name, "\\", "/"))
		if !quiero[nombre] {
			continue
		}
		if err := copiarEntrada(f, filepath.Join(destino, nombre)); err != nil {
			return err
		}
		encontrados++
	}
	if encontrados != len(BinariosNecesarios) {
		return fmt.Errorf("el archivo de EasyTier no traía los %d binarios esperados, se hallaron %d",
			len(BinariosNecesarios), encontrados)
	}
	return nil
}

func copiarEntrada(f *zip.File, destino string) error {
	origen, err := f.Open()
	if err != nil {
		return err
	}
	defer origen.Close()

	// Se escribe a un temporal y se renombra, para que un fallo a mitad no
	// deje un binario truncado que systemd intentaría ejecutar.
	tmp := destino + ".tmp"
	salida, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(salida, origen); err != nil {
		salida.Close()
		os.Remove(tmp)
		return err
	}
	if err := salida.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, destino)
}
