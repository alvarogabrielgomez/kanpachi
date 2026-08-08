package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
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

// desfaseDeReloj compara el reloj de esta máquina con el del registro.
//
// # Por qué hace falta
//
// Los dos logs de una prueba se leen JUNTOS, y sin un patrón común el orden
// entre máquinas es adivinanza. El 2026-08-08 los relojes iban 52 segundos
// separados: el log del host anotaba "credencial emitida" casi un minuto
// DESPUÉS de que el log del invitado dijera "credencial recibida", que es
// imposible. Con este renglón al principio de cada fichero, las dos líneas de
// tiempo se alinean restando.
//
// El registro sirve de patrón porque es el único reloj que las dos máquinas
// ven. La cabecera `Date` de HTTP tiene resolución de un segundo y basta de
// sobra: lo que hay que descartar son desfases de decenas de segundos.
//
// Devuelve cuánto va ADELANTADO el reloj local. Falla sin consecuencias: es una
// ayuda para leer, no una comprobación.
func desfaseDeReloj(ctx context.Context, seed string) (time.Duration, error) {
	if seed == "" {
		return 0, errors.New("no hay registro con el que comparar")
	}
	plazo, fin := context.WithTimeout(ctx, plazoSeed)
	defer fin()
	req, err := http.NewRequestWithContext(plazo, http.MethodHead, "https://"+seed+"/", nil)
	if err != nil {
		return 0, err
	}
	antes := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	_ = resp.Body.Close()
	// El punto medio de la ida y vuelta es la mejor estimación de cuándo el
	// registro escribió la cabecera. Sin esto, el desfase se lleva la latencia
	// entera sumada, que acá son unos 150 ms y no cambia nada, pero sí
	// cambiaría desde una conexión mala.
	medio := antes.Add(time.Since(antes) / 2)
	suyo, err := http.ParseTime(resp.Header.Get("Date"))
	if err != nil {
		return 0, fmt.Errorf("el registro no mandó una cabecera Date legible: %w", err)
	}
	return medio.Sub(suyo), nil
}
