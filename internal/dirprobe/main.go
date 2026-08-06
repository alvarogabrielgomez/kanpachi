// Command dirprobe intenta pisar la tarjeta de una sala ajena.
//
// # Para qué existe, si el contrato ya está probado
//
// Porque el test de contrato habla con el registro EN PROCESO, y lo que queda
// sin medir es el de verdad: el que corre en el droplet, detrás de su proxy,
// con su TLS y su límite de tasa. El fijado de la llave del host es lo único
// que impide que un ex miembro que se quedó con el código se adelante al host
// cuando este reabre, así que conviene verlo negarse en el servidor real.
//
// Y existe como binario en vez de dentro del script de medición porque
// PowerShell 5.1 no puede firmar Ed25519. Mismo motivo y mismo patrón que
// `internal/netcfgprobe`.
//
// # Qué hace exactamente
//
// Genera una llave NUEVA, que es lo que lo convierte en el intruso, y hace dos
// intentos contra un invite ID que no es suyo:
//
//  1. Con una firma VÁLIDA de su propia llave. El registro tiene que contestar
//     403 porque la llave no es la que fijó ese ID.
//  2. Con una firma basura del largo correcto. También 403, por otro motivo.
//
// Los dos son 403 y son hechos distintos, así que se informan por separado: si
// alguna vez uno de los dos pasara a 204, el que se rompió se sabe sin
// adivinar.
//
// No usa el adaptador del daemon a propósito. El adaptador firma bien por
// construcción, así que probarlo con él mediría el adaptador; lo que hay que
// medir es al SERVIDOR negándose.
package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

func main() {
	seed := flag.String("seed", "kanpachi.accentio.dev", "host del registro")
	code := flag.String("code", "", "invite ID de la sala ajena, con o sin guion")
	flag.Parse()

	if *code == "" {
		fmt.Fprintln(os.Stderr, "dirprobe: hace falta -code")
		os.Exit(2)
	}
	// El guion es cosmético y el registro acepta las dos formas. Se quita para
	// que la ruta sea la misma que manda el daemon.
	crudo := ""
	for _, r := range *code {
		if r != '-' {
			crudo += string(r)
		}
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		fmt.Fprintln(os.Stderr, "dirprobe: generando la llave del intruso:", err)
		os.Exit(1)
	}
	fmt.Printf("intruso con llave nueva %s...\n", b64(pub)[:12])

	// Una tarjeta cualquiera. No hace falta que sea descifrable por nadie: el
	// registro no puede leerla, y lo que se mide es si acepta la ESCRITURA.
	tarjeta := []byte(`{"h":"intruso","r":"sala secuestrada"}`)

	fallos := 0
	fallos += intento("firma valida de otra llave", *seed, crudo, pub, ed25519.Sign(priv, tarjeta), tarjeta)
	fallos += intento("firma basura", *seed, crudo, pub, make([]byte, ed25519.SignatureSize), tarjeta)

	if fallos == 0 {
		fmt.Println("OK  el registro rechazo los dos intentos")
		return
	}
	fmt.Printf("MAL %d intento(s) no fueron rechazados\n", fallos)
	os.Exit(1)
}

// intento hace el PUT y devuelve 1 si el registro NO lo rechazó.
func intento(qué, seed, code string, pub ed25519.PublicKey, firma, tarjeta []byte) int {
	cuerpo, err := json.Marshal(map[string]string{
		"host_key": b64(pub),
		"card":     b64(tarjeta),
		"sig":      b64(firma),
	})
	if err != nil {
		fmt.Printf("MAL %s: armando el cuerpo: %v\n", qué, err)
		return 1
	}

	url := "https://" + seed + "/api/i/" + code
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(cuerpo))
	if err != nil {
		fmt.Printf("MAL %s: armando el pedido: %v\n", qué, err)
		return 1
	}
	req.Header.Set("Content-Type", "application/json")

	cli := &http.Client{Timeout: 15 * time.Second}
	resp, err := cli.Do(req)
	if err != nil {
		fmt.Printf("MAL %s: no se pudo hablar con el registro: %v\n", qué, err)
		return 1
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))

	if resp.StatusCode == http.StatusForbidden {
		fmt.Printf("OK  %s: 403, %s\n", qué, bytes.TrimSpace(raw))
		return 0
	}
	fmt.Printf("MAL %s: el registro contesto %d, %s\n", qué, resp.StatusCode, bytes.TrimSpace(raw))
	return 1
}

func b64(raw []byte) string { return base64.RawURLEncoding.EncodeToString(raw) }
