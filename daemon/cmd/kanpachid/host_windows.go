//go:build windows

package main

import (
	"fmt"

	"golang.org/x/sys/windows/svc/mgr"
)

// Autostart lee si el servicio arranca con Windows.
//
// Se LEE del Administrador de servicios y no de un fichero de configuración
// propio, porque el Administrador de servicios es quien decide de verdad.
// Guardarlo aparte daría dos respuestas a la misma pregunta, y la que la
// pantalla enseñaría sería la que no manda.
func (h *procesoHost) Autostart() (bool, error) {
	s, cerrar, err := abrirServicio()
	if err != nil {
		return false, err
	}
	defer cerrar()

	cfg, err := s.Config()
	if err != nil {
		return false, fmt.Errorf("leyendo la configuración del servicio: %w", err)
	}
	return cfg.StartType == mgr.StartAutomatic, nil
}

// SetAutostart cambia entre arranque automático y a demanda.
//
// # Por qué lo hace el daemon y no la interfaz
//
// Porque cambiar el tipo de arranque exige `SERVICE_CHANGE_CONFIG`. El
// instalador le concede al usuario interactivo arrancar y detener ESTE servicio
// y nada más; darle además permiso para reconfigurarlo sería ampliar la
// concesión por comodidad. El daemon ya es SYSTEM, así que se lo pide a sí
// mismo y no hace falta conceder nada.
//
// # Qué NO cambia
//
// El servicio sigue siendo un servicio, y sigue corriendo como SYSTEM. Apagar
// esto solo hace que Windows no lo levante solo: sigue arrancando cuando
// alguien abre Kanpachi.
func (h *procesoHost) SetAutostart(enabled bool) error {
	s, cerrar, err := abrirServicio()
	if err != nil {
		return err
	}
	defer cerrar()

	cfg, err := s.Config()
	if err != nil {
		return fmt.Errorf("leyendo la configuración del servicio: %w", err)
	}

	if enabled {
		cfg.StartType = mgr.StartAutomatic
		// Retrasado, igual que lo deja el instalador. Un servicio que toca el
		// firewall y levanta un adaptador durante el arranque de Windows
		// compite con todo lo demás; retrasado llega cuando la máquina ya
		// respira, y nadie está jugando en el segundo cero.
		cfg.DelayedAutoStart = true
	} else {
		cfg.StartType = mgr.StartManual
		// A demanda no admite retraso, y dejarlo puesto hace que
		// `ChangeServiceConfig2` guarde una combinación que Windows ignora.
		cfg.DelayedAutoStart = false
	}

	if err := s.UpdateConfig(cfg); err != nil {
		return fmt.Errorf("cambiando el arranque del servicio: %w", err)
	}
	h.log.Info("arranque con Windows cambiado", "automático", enabled)
	return nil
}

// abrirServicio abre el propio servicio para leer y escribir su configuración.
func abrirServicio() (*mgr.Service, func(), error) {
	m, err := mgr.Connect()
	if err != nil {
		return nil, nil, fmt.Errorf("conectando al Administrador de servicios: %w", err)
	}
	s, err := m.OpenService(ServiceName)
	if err != nil {
		_ = m.Disconnect()
		return nil, nil, fmt.Errorf("abriendo el servicio %s: %w.\n"+
			"  Pasa si el daemon corre a mano en vez de instalado", ServiceName, err)
	}
	return s, func() {
		_ = s.Close()
		_ = m.Disconnect()
	}, nil
}
