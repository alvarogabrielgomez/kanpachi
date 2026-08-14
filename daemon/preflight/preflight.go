// Package preflight answers, before anything is built, whether this machine is
// in a state worth building on. Read-only: nothing here writes, opens ports or
// elevates.
//
// # Why it exists
//
// Three surfaces were measuring the same machine, each with its own copy:
// `kanpachi doctor`, roomprobe's checks, and the daemon's startup. The copies
// already drifted the way copies do — the daemon's service NAME alone was
// declared three times, one of them with a comment admitting it ("está escrito
// otra vez acá y no importado"). A check written twice answers about two
// slightly different things, and the one that matters is the one the product
// runs for real.
//
// Per-system knowledge lives INSIDE this package, in `_windows`/`_linux`
// files: a caller asks the question and never asks which OS it is on.
package preflight

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"
)

// probeTimeout bounds every check that leaves the process.
const probeTimeout = 5 * time.Second

// EngineAt says whether the engine binary is where the caller will launch it
// from. It does not run it: presence is the preflight question, and the deep
// probe (building an adapter and tearing it down) is [kanpachiengine.Preflight],
// which is privileged and belongs to whoever holds the consequences.
func EngineAt(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("no está el motor en %s", path)
	}
	if info.IsDir() || info.Size() == 0 {
		return fmt.Errorf("lo que hay en %s no es un ejecutable", path)
	}
	return nil
}

// ClockSkew measures how far AHEAD this machine's clock is of the registry's,
// so two machines' logs can be aligned by subtraction.
//
// The registry serves as the reference because it is the one clock both
// machines can see. HTTP's `Date` header has one-second resolution, which is
// plenty: what has to be ruled out are skews of tens of seconds. The midpoint
// of the round trip is the best estimate of when the registry wrote the
// header; without it the whole latency lands in the skew, which from a bad
// connection would matter.
//
// It fails without consequence: this is an aid for reading logs, not a check.
func ClockSkew(ctx context.Context, registryHost string) (time.Duration, error) {
	if registryHost == "" {
		return 0, errors.New("no hay registro con el que comparar")
	}
	plazo, fin := context.WithTimeout(ctx, probeTimeout)
	defer fin()
	req, err := http.NewRequestWithContext(plazo, http.MethodHead, "https://"+registryHost+"/", nil)
	if err != nil {
		return 0, err
	}
	antes := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	_ = resp.Body.Close()
	medio := antes.Add(time.Since(antes) / 2)
	suyo, err := http.ParseTime(resp.Header.Get("Date"))
	if err != nil {
		return 0, fmt.Errorf("el registro no mandó una cabecera Date legible: %w", err)
	}
	return medio.Sub(suyo), nil
}
