<#
.SYNOPSIS
    Construye y puebla el stage que todos los scripts de medicion dan por hecho.

.DESCRIPTION
    Cada medir-*.ps1 recibe -Stage "C:\kt\stage" y ASUME que ahi dentro estan
    los binarios y las DLL. Nada en el repositorio lo poblaba: se armaba a mano,
    y el sintoma de un stage a medias no se parece a la causa. Falta
    Packet.dll y el motor muere con 0xC0000135 sin nombrar cual falta; falta
    builtin.json y la sala se abre sin ningun juego que elegir.

    Que deja:

      kanpachid.exe        el daemon
      kanpctl.exe          el cliente de linea de comandos
      dirprobe.exe         la sonda del registro del seed
      engineprobe.exe      la sonda del motor
      netcfgprobe.exe      la sonda de los ajustes del adaptador
      fwprobe.exe          la sonda del firewall y de la compuerta
      builtin.json         el catalogo que viene con la app
      Packet.dll           importacion DURA del motor, sin ella no arranca
      wintun.dll           el adaptador virtual
      WinDivert64.sys      lo consume el motor

    Que NO deja, y por que: kanpachi-engine.exe vive en otro repositorio y se
    construye aparte. Si ya esta en el stage no se toca, y si falta se avisa
    sin fallar, que es lo unico honesto que se puede hacer desde aca.

    NO hace falta consola elevada: esto compila y copia. Elevar hace falta para
    correr lo que deja.
#>
[CmdletBinding()]
param(
    [string]$Stage = "C:\kt\stage"
)

$ErrorActionPreference = 'Stop'

function Paso($t) { Write-Host "`n=== $t ===" -ForegroundColor Cyan }
function Bien($t) { Write-Host "  OK  $t" -ForegroundColor Green }
function Mal($t) { Write-Host "  MAL $t" -ForegroundColor Red }
function Nota($t) { Write-Host "  --  $t" -ForegroundColor DarkGray }

# El repositorio sale de la ubicacion del script y jamas del directorio actual:
# una consola elevada arranca en system32.
$repo = Split-Path -Parent $PSScriptRoot

$fallos = 0

if (-not (Test-Path $Stage)) {
    New-Item -ItemType Directory -Force -Path $Stage | Out-Null
    Nota "creado $Stage"
}

Paso "compilando"

# Sin -ldflags "-s -w": quitar los simbolos dispara falsos positivos de Defender
# sobre binarios de Go, y el binario que se mide tiene que ser el que se envia.
$binarios = @(
    @{ nombre = 'kanpachid.exe';    paquete = './daemon/cmd/kanpachid' },
    @{ nombre = 'kanpctl.exe';      paquete = './internal/kanpctl' },
    @{ nombre = 'dirprobe.exe';     paquete = './internal/dirprobe' },
    @{ nombre = 'engineprobe.exe';  paquete = './internal/engineprobe' },
    @{ nombre = 'netcfgprobe.exe';  paquete = './internal/netcfgprobe' },
    @{ nombre = 'fwprobe.exe';      paquete = './internal/fwprobe' }
)

Push-Location $repo
try {
    foreach ($b in $binarios) {
        $destino = Join-Path $Stage $b.nombre
        & go build -o $destino $b.paquete
        if ($LASTEXITCODE -ne 0) {
            Mal ("no compilo " + $b.paquete)
            $fallos++
            continue
        }
        $kb = [math]::Round((Get-Item $destino).Length / 1KB)
        Bien ("{0,-18} {1} KB" -f $b.nombre, $kb)
    }
}
finally {
    Pop-Location
}

Paso "copiando lo que no se compila"

$copias = @(
    @{ origen = 'daemon\adapter\catalog\jsonfile\builtin.json'; nombre = 'builtin.json' },
    @{ origen = 'third_party\easytier\Packet.dll';              nombre = 'Packet.dll' },
    @{ origen = 'third_party\easytier\wintun.dll';              nombre = 'wintun.dll' },
    @{ origen = 'third_party\easytier\WinDivert64.sys';         nombre = 'WinDivert64.sys' }
)

foreach ($c in $copias) {
    $origen = Join-Path $repo $c.origen
    if (-not (Test-Path $origen)) {
        Mal ("falta en el repositorio: " + $c.origen)
        $fallos++
        continue
    }
    Copy-Item -Path $origen -Destination (Join-Path $Stage $c.nombre) -Force
    Bien $c.nombre
}

Paso "el motor, que viene de otro repositorio"

$motor = Join-Path $Stage 'kanpachi-engine.exe'
if (Test-Path $motor) {
    $mb = [math]::Round((Get-Item $motor).Length / 1MB, 1)
    Bien "kanpachi-engine.exe ya estaba, $mb MB, y no se toco"
} else {
    Nota "kanpachi-engine.exe NO esta en el stage"
    Nota "se construye en el repositorio kanpachi-engine y se copia a mano aca"
    Nota "sin el se puede hablar con el daemon, no se puede abrir una sala"
}

Paso "resultado"

if ($fallos -eq 0) {
    Bien "el stage esta listo en $Stage"
    exit 0
}

Mal "$fallos cosa(s) sin poner"
exit 1
