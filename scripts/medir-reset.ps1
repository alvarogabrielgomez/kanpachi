<#
.SYNOPSIS
    Ensucia la maquina a proposito y comprueba que --reset la deja limpia.

.DESCRIPTION
    El hard reset existe para la maquina donde el daemon YA NO LEVANTA. Medirlo
    con todo funcionando no probaria nada, asi que este script fabrica el caso:
    abre una sala de verdad y mata el daemon SUCIAMENTE, sin darle ninguna
    oportunidad de limpiar.

    Lo que queda entonces es lo que hay que medir: reglas del grupo Kanpachi
    puestas, filtros de compuerta en WFP, un hosted-room.json que dice que hay sala, y
    posiblemente un motor huerfano. Despues corre --reset y comprueba lo unico
    que importa de verdad:

      1. Cero reglas del grupo Kanpachi.
      2. Cero filtros de la compuerta.
      3. La cuarentena de base ENTERA, porque el reset la repone y no la quita.
      4. Sin motor huerfano.
      5. hosted-room.json borrado, y last-room.json intacto.
      6. Y despues del reset se puede volver a crear una sala.

    El punto 3 es el que separa este comando del desinstalador. Un reset se pide
    cuando nada funciona; quitar ahi la cuarentena destruiria justo lo que
    protege del caso que lo motivo.

    Necesita consola elevada.
#>
[CmdletBinding()]
param(
    [string]$Stage = "C:\kt\stage",
    [string]$Data = "$env:ProgramData\Kanpachi",
    [int]$Espera = 45
)

$ErrorActionPreference = 'Stop'

function Paso($t) { Write-Host "`n=== $t ===" -ForegroundColor Cyan }
function Bien($t) { Write-Host "  OK  $t" -ForegroundColor Green }
function Mal($t) { Write-Host "  MAL $t" -ForegroundColor Red }

$esAdmin = ([Security.Principal.WindowsPrincipal] `
    [Security.Principal.WindowsIdentity]::GetCurrent()
).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if (-not $esAdmin) { throw "Hace falta una consola elevada." }

$fallos = 0
# Devuelve CUANTAS y no la lista, a proposito. PowerShell 5.1 desenvuelve un
# array de un elemento al devolverlo, asi que `(ReglasDe 'x').Count` sobre una
# sola regla da $null en vez de 1: la medicion se lee como cero justo cuando
# queda una regla suelta, que es el caso que mas importa ver.
function CuantasReglasDe($grupo) {
    $fw = New-Object -ComObject HNetCfg.FwPolicy2
    $r = @($fw.Rules | Where-Object { $_.Grouping -eq $grupo })
    $r.Count
}
function FiltrosDeLaCompuerta() {
    $texto = & netsh.exe wfp show filters file=- 2>&1 | Out-String
    ([regex]::Matches($texto, 'bloqueo de todo')).Count
}

Paso "cuantas reglas tiene la cuarentena de base ANTES de todo"
$baseAntes = CuantasReglasDe 'Kanpachi-base'
if ($baseAntes -eq 0) { throw "no hay cuarentena de base puesta: este script no probaria nada" }
Bien "$baseAntes reglas de cuarentena"

Paso "abriendo una sala de verdad"
$out = Join-Path $env:TEMP 'kanpachi-reset.out'
$daemon = Start-Process -FilePath (Join-Path $Stage 'kanpachid.exe') `
    -ArgumentList '--console', '--data', $Data -PassThru -WindowStyle Minimized `
    -RedirectStandardOutput $out -RedirectStandardError "$out.err"
Start-Sleep -Seconds 3

$params = (@{ nickname = 'Alvaro'; name = 'Prueba' } | ConvertTo-Json -Compress).Replace('"', '\"')
$antes = $ErrorActionPreference
$ErrorActionPreference = 'Continue'
& (Join-Path $Stage 'kanpctl.exe') -data $Data -params $params create_room 2>&1 | Out-Null
$codigo = $LASTEXITCODE
$ErrorActionPreference = $antes
if ($codigo -ne 0) { throw "no se pudo crear la sala (exit $codigo), asi que no hay nada sucio que limpiar" }

$reloj = [Diagnostics.Stopwatch]::StartNew()
while ($reloj.Elapsed.TotalSeconds -lt $Espera) {
    if (Get-NetAdapter -Name "kanpachi0" -ErrorAction SilentlyContinue) { break }
    Start-Sleep -Milliseconds 500
}
Start-Sleep -Seconds 4
Bien "sala abierta"

Paso "matando el daemon SUCIAMENTE"
# Sin Stop-Process -Force no habria caso que medir: una salida limpia ya limpia
# sola, y lo que este script comprueba es el camino que no pasa por ahi.
Stop-Process -Id $daemon.Id -Force
Start-Sleep -Seconds 2

$sucio = @{
    reglas  = CuantasReglasDe 'Kanpachi'
    filtros = FiltrosDeLaCompuerta
    sala    = Test-Path (Join-Path $Data 'hosted-room.json')
    motor   = @(Get-Process kanpachi-engine -ErrorAction SilentlyContinue).Count
}
Write-Host ("  reglas Kanpachi={0} filtros={1} hosted-room.json={2} motores={3}" -f `
    $sucio.reglas, $sucio.filtros, $sucio.sala, $sucio.motor)
if ($sucio.reglas -eq 0 -and $sucio.filtros -eq 0 -and -not $sucio.sala) {
    throw "la muerte sucia no dejo NADA que limpiar, asi que el reset no probaria nada"
}
Bien "quedo suciedad de verdad que limpiar"

# El Job Object se lleva el motor con el daemon, asi que normalmente no queda
# huerfano. Se anota lo que haya en vez de exigir uno: fabricar un huerfano a
# mano seria medir el arnes y no el producto.
Paso "corriendo kanpachid --reset"
$antes = $ErrorActionPreference
$ErrorActionPreference = 'Continue'
& (Join-Path $Stage 'kanpachid.exe') --reset --data $Data 2>&1 | ForEach-Object { Write-Host "  $_" }
$codigo = $LASTEXITCODE
$ErrorActionPreference = $antes
if ($codigo -ne 0) { Mal "el reset salio con $codigo"; $fallos++ }

Paso "lo que quedo despues"
$reglas = CuantasReglasDe 'Kanpachi'
if ($reglas -eq 0) { Bien "cero reglas del grupo Kanpachi" }
else { Mal "quedaron $reglas reglas del grupo Kanpachi"; $fallos++ }

$filtros = FiltrosDeLaCompuerta
if ($filtros -eq 0) { Bien "cero filtros de la compuerta" }
else { Mal "quedaron $filtros filtros de la compuerta, y no los ve ninguna herramienta del sistema"; $fallos++ }

# Lo que separa el reset del desinstalador.
$baseDespues = CuantasReglasDe 'Kanpachi-base'
if ($baseDespues -eq $baseAntes) { Bien "la cuarentena de base sigue entera: $baseDespues reglas" }
else { Mal "la cuarentena paso de $baseAntes a $baseDespues reglas. El reset la REPONE, no la quita"; $fallos++ }

$motores = @(Get-Process kanpachi-engine -ErrorAction SilentlyContinue).Count
if ($motores -eq 0) { Bien "sin motor huerfano" }
else { Mal "quedaron $motores motor(es) vivos"; $fallos++ }

if (Test-Path (Join-Path $Data 'hosted-room.json')) {
    Mal "hosted-room.json sigue ahi, asi que el arranque siguiente ofreceria reabrir una sala que ya no existe"
    $fallos++
}
else { Bien "hosted-room.json borrado" }

# last-room.json se conserva a proposito: resetear la configuracion no es
# olvidar a que sala volver.
if (Test-Path (Join-Path $Data 'last-room.json')) { Bien "last-room.json intacto" }
else { Write-Host "  --  no habia last-room.json que conservar" -ForegroundColor Yellow }

Paso "y despues del reset se puede volver a crear una sala"
$daemon2 = Start-Process -FilePath (Join-Path $Stage 'kanpachid.exe') `
    -ArgumentList '--console', '--data', $Data -PassThru -WindowStyle Minimized `
    -RedirectStandardOutput "$out.2" -RedirectStandardError "$out.2.err"
Start-Sleep -Seconds 3
$antes = $ErrorActionPreference
$ErrorActionPreference = 'Continue'
& (Join-Path $Stage 'kanpctl.exe') -data $Data -params $params create_room 2>&1 | Out-Null
$codigo = $LASTEXITCODE
$ErrorActionPreference = $antes
if ($codigo -eq 0) { Bien "la sala nueva se creo" }
else { Mal "no se pudo crear una sala despues del reset (exit $codigo)"; $fallos++ }

Stop-Process -Id $daemon2.Id -Force -ErrorAction SilentlyContinue
Start-Sleep -Seconds 2
Get-Process kanpachi-engine -ErrorAction SilentlyContinue | Stop-Process -Force
& (Join-Path $Stage 'kanpachid.exe') --reset --data $Data 2>&1 | Out-Null

Paso "resultado"
if ($fallos -eq 0) {
    Write-Host "  Una muerte sucia se limpia de un solo golpe, y la cuarentena sobrevive." -ForegroundColor Green
    exit 0
}
Write-Host "  $fallos comprobacion(es) fallaron." -ForegroundColor Red
exit 1
