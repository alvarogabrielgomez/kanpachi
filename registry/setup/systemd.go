package setup

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// La capa que habla con systemd. Se invoca systemctl como proceso en vez de
// usar D-Bus: son cuatro verbos, el binario está en cualquier máquina con
// systemd, y evita una dependencia grande para algo que se ejecuta dos veces
// en la vida de una instalación.

// EscribirUnits coloca las dos units y recarga systemd.
//
// Solo escribe si el contenido cambió. Reescribir una unit idéntica obliga a
// un daemon-reload innecesario, y `init` tiene que poder ejecutarse dos veces
// sin consecuencias.
func EscribirUnits(c Config) (bool, error) {
	cambios := false
	for nombre, contenido := range map[string]string{
		UnitMotor: UnitDelMotor(c),
		UnitReg:   UnitDelRegistro(c),
	} {
		ruta := filepath.Join(DirUnits, nombre)
		anterior, err := os.ReadFile(ruta)
		if err == nil && string(anterior) == contenido {
			continue
		}
		if err := os.WriteFile(ruta, []byte(contenido), 0o644); err != nil {
			return cambios, fmt.Errorf("escribiendo %s: %w", ruta, err)
		}
		cambios = true
	}
	if !cambios {
		return false, nil
	}
	return true, Systemctl("daemon-reload")
}

// Systemctl ejecuta systemctl y devuelve su error con la salida adjunta.
//
// Sin adjuntar la salida, un fallo llega como "exit status 1" y no dice nada,
// que es exactamente lo que hace inútil un mensaje de error.
func Systemctl(args ...string) error {
	cmd := exec.Command("systemctl", args...)
	var salida bytes.Buffer
	cmd.Stdout = &salida
	cmd.Stderr = &salida
	if err := cmd.Run(); err != nil {
		texto := strings.TrimSpace(salida.String())
		if texto == "" {
			return fmt.Errorf("systemctl %s: %w", strings.Join(args, " "), err)
		}
		return fmt.Errorf("systemctl %s: %w\n%s", strings.Join(args, " "), err, texto)
	}
	return nil
}

// EstadoUnit devuelve lo que systemd opina de una unit, en una palabra.
// Nunca falla: para diagnosticar, "no encontrada" es una respuesta.
func EstadoUnit(unit string) string {
	salida, _ := exec.Command("systemctl", "is-active", unit).Output()
	estado := strings.TrimSpace(string(salida))
	if estado == "" {
		return "desconocido"
	}
	return estado
}

// UnitHabilitada informa si arranca sola al reiniciar la máquina. Es una
// pregunta distinta de si está corriendo ahora, y la que importa para saber si
// el seed sobrevive a un reinicio del droplet.
func UnitHabilitada(unit string) bool {
	salida, _ := exec.Command("systemctl", "is-enabled", unit).Output()
	return strings.TrimSpace(string(salida)) == "enabled"
}

// HaySystemd informa si esta máquina usa systemd. Un mensaje claro vale más
// que una cascada de "systemctl: command not found".
func HaySystemd() bool {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return false
	}
	datos, err := os.ReadFile("/proc/1/comm")
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(datos)) == "systemd"
}

// LogsDeUnit devuelve las últimas líneas del diario de una unit, para que un
// fallo al arrancar se vea sin tener que ir a buscarlo a mano.
func LogsDeUnit(unit string, lineas int) string {
	salida, err := exec.Command("journalctl", "-u", unit, "-n", fmt.Sprint(lineas), "--no-pager", "-o", "cat").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(salida))
}
