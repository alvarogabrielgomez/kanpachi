package wfp

import "github.com/accentiostudios/kanpachi/core/domain"

// Measurement es lo que la compuerta tiene puesto AHORA.
//
// MIDE y jamás juzga. Si eso es lo que se pidió lo decide
// [domain.Enforcement.Diff], que es dominio y corre sin Windows.
//
// # Por qué vive en un archivo portable y no junto a Measure
//
// Porque [daemon/adapter/firewall.Gate] la nombra en su firma, y ese archivo no
// lleva etiqueta de compilación. Con este tipo dentro de `gate_windows.go`, el
// paquete del firewall NO COMPILA en Linux, así que el CI se quedaba sin
// comprobar el adaptador compuesto entero: sus tests puros existen y no llegaban
// a ejecutarse.
//
// El tipo es datos y nada más. No hay una sola llamada a Windows acá, y por eso
// no tiene por qué costar la portabilidad del paquete que lo usa.
type Measurement struct {
	Gate  domain.GateState
	Rules []domain.AppliedRule
}
