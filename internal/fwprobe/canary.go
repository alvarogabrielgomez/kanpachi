package main

// Los dos subcomandos del canario, que son las dos mitades de la misma
// medición y por eso van juntos acá.
//
// Los dos corren CÓDIGO DEL PRODUCTO. El que abre usa `adapter/canary` y el que
// marca usa `adapter/probe`, sin una línea de red propia. Es la misma disciplina
// que el resto de este binario: si acá hiciera falta un atajo, el atajo taparía
// justo lo que hay que comprobar.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"net/netip"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/timing"
	"github.com/accentiostudios/kanpachi/daemon/adapter/canary"
	"github.com/accentiostudios/kanpachi/daemon/adapter/probe"
)

// canario abre el oyente y espera a que lo toquen.
//
// Se liga SOLO a la dirección que se le pase, jamás a todas las interfaces. Eso
// no es una opción de este comando: lo rechaza el propio adaptador, porque ligar
// en todas abriría un puerto en la red de casa de quien lo corre.
func canario(args []string) error {
	fs := flag.NewFlagSet("canary", flag.ExitOnError)
	addr := fs.String("addr", "", "dirección del adaptador donde ligar. NUNCA 0.0.0.0")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *addr == "" {
		return fmt.Errorf("falta -addr. Es la dirección del adaptador de la sala, y sin ella " +
			"el canario tendría que ligar en todas las interfaces")
	}

	at, err := netip.ParseAddr(*addr)
	if err != nil {
		return fmt.Errorf("-addr %q no es una IP: %w", *addr, err)
	}

	var nonce canary.Nonce
	if _, err := rand.Read(nonce[:]); err != nil {
		return fmt.Errorf("no se pudo generar el número del canario: %w", err)
	}

	// Sin `avoid`: este arnés no tiene un RuleSet del que sacar qué puertos
	// abrió el juego activo. El que sí lo tiene es el daemon, por `opener`.
	c, err := canary.Listen(at, nonce, timing.CanaryTTLMax, nil, logConsola{})
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	fmt.Println()
	fmt.Printf("  canario en %s\n", netip.AddrPortFrom(at, c.Port()))
	fmt.Printf("  desde la otra máquina:\n\n")
	fmt.Printf("    fwprobe canary-probe -host %s -port %d -nonce %s\n\n",
		at, c.Port(), hex.EncodeToString(nonce[:]))
	fmt.Printf("  se cierra solo en %v\n\n", timing.CanaryTTLMax)

	// Se corta con lo primero de tres: que lo toquen, el plazo duro, o Ctrl+C.
	// Cortar en el toque es lo que hace que el script de dos máquinas termine
	// enseguida en la fase con la compuerta purgada, en vez de esperar el plazo.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	select {
	case <-c.Touched():
	case <-sig:
	case <-time.After(timing.CanaryTTLMax):
	}

	// Lo que importa al final es `WasTouched`, que es un hecho PROPIO: si alguien
	// llegó hasta el socket, el paquete cruzó la compuerta, lo diga quien lo diga
	// desde la otra máquina.
	fmt.Println()
	if c.WasTouched() {
		fmt.Println("  LO TOCARON. El paquete cruzó: la compuerta no está bloqueando.")
		return nil
	}
	fmt.Println("  NADIE LO TOCÓ.")
	fmt.Println("  Ojo: esto solo prueba algo si la otra máquina marcó de verdad.")
	return nil
}

// canarioSonda marca el canario por los dos protocolos.
func canarioSonda(args []string) error {
	fs := flag.NewFlagSet("canary-probe", flag.ExitOnError)
	host := fs.String("host", "", "IP virtual del host")
	port := fs.Int("port", 0, "puerto que dijo el canario")
	nonceHex := fs.String("nonce", "", "el número que dijo el canario, en hexadecimal")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *host == "" || *port == 0 || *nonceHex == "" {
		return fmt.Errorf("hacen falta -host, -port y -nonce, y los tres los imprime el canario")
	}

	at, err := netip.ParseAddr(*host)
	if err != nil {
		return fmt.Errorf("-host %q no es una IP: %w", *host, err)
	}
	raw, err := hex.DecodeString(strings.TrimSpace(*nonceHex))
	if err != nil || len(raw) != canary.NonceSize {
		return fmt.Errorf("-nonce tiene que ser %d bytes en hexadecimal", canary.NonceSize)
	}
	// Del dominio y no de `canary.Nonce`: este subcomando es el que SONDEA, o sea
	// el lado del invitado, y ese lado no abre ningún oyente. Los dos tipos miden
	// lo mismo y viven en capas distintas a propósito.
	var nonce domain.CanaryNonce
	copy(nonce[:], raw)

	ctx, cancel := context.WithTimeout(context.Background(), 3*timing.ProbeDeadline)
	defer cancel()

	destino := netip.AddrPortFrom(at, uint16(*port))
	tcp, udp := probe.New().ProbeCanary(ctx, destino, nonce)

	fmt.Println()
	fmt.Printf("  %-22s TCP  %s\n", destino, strings.ToUpper(tcp.String()))
	fmt.Printf("  %-22s UDP  %s\n", destino, strings.ToUpper(udp.String()))
	fmt.Println()

	// La lectura, escrita acá para no tener que acordarse de ella al mirar la
	// salida. Es lo contrario de lo que uno espera: acá el silencio es la
	// buena noticia.
	switch {
	case tcp.Reached() || udp.Reached():
		fmt.Println("  CONTESTÓ. El paquete llegó al canario, o sea que la compuerta")
		fmt.Println("  NO está bloqueando ese adaptador.")
	default:
		fmt.Println("  SILENCIO en los dos, y esta vez el silencio SÍ prueba algo:")
		fmt.Println("  había un oyente detrás, así que lo paró el firewall.")
	}
	fmt.Println()
	return nil
}
