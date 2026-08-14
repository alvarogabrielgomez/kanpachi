package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
	kanpachiengine "github.com/accentiostudios/kanpachi/daemon/adapter/engine/kanpachi"
)

// plazoSeed es cuánto se espera a que el seed acepte la conexión.
//
// Cinco segundos: el seed está en un droplet al otro lado de internet, así que
// un segundo daría falsos negativos desde una conexión lenta, y quince
// convertirían la comprobación en una espera que nadie termina de mirar.
const plazoSeed = 5 * time.Second

// resultadoSeed es una dirección del seed y qué pasó al marcarla.
type resultadoSeed struct {
	Dir netip.Addr
	RTT time.Duration
	// Err nil significa que el seed aceptó la conexión. Con error, dice si fue
	// un rechazo del dominio (dirección no enrutable) o un fallo de red.
	Err error
}

// medirSeed contesta la pregunta que decide si dos máquinas pueden encontrarse:
// ¿esta llega al seed?
//
// # Por qué se mide acá y no se le pregunta al motor
//
// Porque el motor no lo cuenta. Su respuesta de diagnóstico lleva NAT, UDP,
// direcciones públicas, dirección virtual y MTU, y ni un campo del seed, así
// que [domain.NetCheck.SeedRTT] llega vacío SIEMPRE. Una comprobación sobre ese
// mapa dice "ningún seed contestó" con el seed caído y con el seed perfecto: es
// una alarma encendida a todas horas, que es lo mismo que ninguna alarma. Pasó:
// el 2026-08-08 esta sonda acusó de caído a un seed sano.
//
// # Qué mide
//
// Lo mismo que hace el motor para armar sus URIs en `seedURIs`: resolver el
// nombre, descartar toda dirección que [domain.CheckSeedAddr] rechace —el
// registro A de un dominio cualquiera puede apuntar a 192.168.1.1, y marcarla
// convertiría esto en un escáner de la LAN de quien lo corra— y abrir un TCP al
// puerto del seed, que no se negocia.
//
// Que el TCP entre no prueba que el motor vaya a encontrarse ahí, prueba que
// hay camino. Lo contrario sí es concluyente: sin camino no hay sala, salvo
// que las dos máquinas estén en la misma LAN.
func medirSeed(ctx context.Context, host string) ([]resultadoSeed, error) {
	if host == "" {
		return nil, errors.New("no hay ningún seed configurado")
	}
	dirs, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("%s no resolvió: %w", host, err)
	}
	if len(dirs) == 0 {
		return nil, fmt.Errorf("%s no resolvió a ninguna dirección", host)
	}

	out := make([]resultadoSeed, 0, len(dirs))
	for _, d := range dirs {
		d = d.Unmap()
		r := resultadoSeed{Dir: d}
		if err := domain.CheckSeedAddr(d); err != nil {
			r.Err = err
			out = append(out, r)
			continue
		}
		plazo, fin := context.WithTimeout(ctx, plazoSeed)
		inicio := time.Now()
		conn, err := (&net.Dialer{}).DialContext(plazo, "tcp",
			netip.AddrPortFrom(d, kanpachiengine.SeedPort).String())
		fin()
		if err != nil {
			r.Err = err
		} else {
			r.RTT = time.Since(inicio)
			_ = conn.Close()
		}
		out = append(out, r)
	}
	return out, nil
}

// alguienContesto dice si al menos una dirección del seed aceptó la conexión.
// Con varias direcciones basta una: el motor las marca todas.
func alguienContesto(rs []resultadoSeed) bool {
	for _, r := range rs {
		if r.Err == nil {
			return true
		}
	}
	return false
}
