<#
.SYNOPSIS
    Ejercita los caminos de netcfg que una sala normal nunca toca.

.DESCRIPTION
    Contrastar lo escrito contra lo ejecutado dejo una brecha incomoda: crear
    una sala prueba la metrica, el MTU y el barrido de rutas, y NADA MAS. Las
    rutas de broadcast y multicast solo las pide un perfil de juego, la
    politica de prefijo tambien, y el borrado de una ruta por defecto solo
    corre cuando hay una que borrar, cosa que en una maquina sana no pasa.

    Este script abre una sala de verdad, deja el adaptador arriba, y corre
    netcfgprobe.exe contra el. Verifica preguntandole al SISTEMA, nunca a la
    memoria de netcfg.

    Ademas revisa el log del daemon buscando el fallo de Peers(), que estuvo
    ahi todo el rato sin que nadie lo viera: el log es UTF-16 y un grep
    ingenuo no encuentra nada dentro.

    Necesita consola elevada.
#>
[CmdletBinding()]
param(
    [string]$Stage = "C:\kt\stage",
    [string]$Data = "$env:ProgramData\Kanpachi",
    [string]$Room = "Prueba",
    [string]$Nick = "Alvaro",
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

foreach ($f in 'kanpachid.exe', 'kanpachi-engine.exe', 'kanpctl.exe', 'netcfgprobe.exe') {
    if (-not (Test-Path (Join-Path $Stage $f))) { throw "Falta $f en $Stage" }
}

$fallos = 0
$daemon = $null
$daemonOut = Join-Path $env:TEMP 'kanpachi-netcfg.out'
$daemonErr = Join-Path $env:TEMP 'kanpachi-netcfg.err'

try {
    Paso "arrancando el daemon"
    $daemon = Start-Process -FilePath (Join-Path $Stage 'kanpachid.exe') `
        -ArgumentList '--console', '--data', $Data `
        -PassThru -WindowStyle Minimized `
        -RedirectStandardOutput $daemonOut -RedirectStandardError $daemonErr
    Start-Sleep -Seconds 3
    if ($daemon.HasExited) {
        Write-Host (Get-Content $daemonOut, $daemonErr -ErrorAction SilentlyContinue | Out-String)
        throw "El daemon murio al arrancar (exit $($daemon.ExitCode))."
    }
    Bien "daemon PID $($daemon.Id)"

    Paso "creando la sala"
    $params = (@{ nickname = $Nick; name = $Room } | ConvertTo-Json -Compress).Replace('"', '\"')
    $antes = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    $ctl = & (Join-Path $Stage 'kanpctl.exe') -data $Data -params $params create_room 2>&1
    $codigo = $LASTEXITCODE
    $ErrorActionPreference = $antes
    Write-Host ($ctl -join "`n")
    if ($codigo -ne 0) { Mal "kanpctl salio con $codigo"; $fallos++ }

    Paso "esperando el adaptador (hasta $Espera s)"
    $reloj = [Diagnostics.Stopwatch]::StartNew()
    $ad = $null
    while ($reloj.Elapsed.TotalSeconds -lt $Espera) {
        $ad = Get-NetAdapter -Name "kanpachi0" -ErrorAction SilentlyContinue
        if ($ad) { break }
        Start-Sleep -Milliseconds 500
    }
    if (-not $ad) { throw "no aparecio kanpachi0: sin adaptador este arnes no mide nada" }
    Bien "kanpachi0 arriba, estado $($ad.Status)"
    Start-Sleep -Seconds 5

    # La lista de miembros se pide a proposito. Estuvo rota en todas las
    # mediciones anteriores y ninguna lo noto, porque una sala se sostiene sin
    # ella: lo que se cae es saber quien esta dentro.
    Paso "los miembros, que es lo que estaba roto"
    $antes = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    & (Join-Path $Stage 'kanpctl.exe') -data $Data status 2>&1 | ForEach-Object { Write-Host "  $_" }
    $ErrorActionPreference = $antes

    # Aqui esta el punto del script. Con la sala viva, netcfgprobe corre los
    # caminos que la sala sola jamas ejercita.
    Paso "netcfgprobe: los caminos que una sala no ejercita"
    $antes = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    & (Join-Path $Stage 'netcfgprobe.exe') -data $Data 2>&1 | ForEach-Object { Write-Host $_ }
    $codigo = $LASTEXITCODE
    $ErrorActionPreference = $antes
    if ($codigo -ne 0) { Mal "netcfgprobe salio con $codigo"; $fallos++ }
    else { Bien "netcfgprobe en verde" }

    # El estado que deja el arnes tiene que ser el mismo que encontro. Si una
    # ruta de mentira sobrevive al arnes, el arnes es peor que no medir.
    Paso "el arnes no dejo restos"
    $sobra = @(Get-NetRoute -InterfaceAlias "kanpachi0" -ErrorAction SilentlyContinue |
        Where-Object {
            $_.DestinationPrefix -eq '0.0.0.0/0' -or
            $_.DestinationPrefix -eq '255.255.255.255/32' -or
            $_.DestinationPrefix -eq '224.0.0.0/4'
        })
    if ($sobra.Count -gt 0) {
        Mal "quedaron $($sobra.Count) ruta(s) del arnes:"
        $sobra | ForEach-Object { Mal "    $($_.DestinationPrefix)" }
        $fallos++
    }
    else { Bien "ninguna ruta del arnes sobrevivio" }
}
catch {
    Mal "el script se rompio: $_"
    Write-Host $_.ScriptStackTrace -ForegroundColor DarkGray
    $fallos++
}
finally {
    if ($daemon -and -not $daemon.HasExited) { Stop-Process -Id $daemon.Id -Force }
    Get-Process kanpachi-engine -ErrorAction SilentlyContinue | Stop-Process -Force
    Start-Sleep -Seconds 2
}

# El log del daemon se lee SIEMPRE, y no como adorno. El fallo de Peers()
# estuvo escrito ahi mientras una medicion anterior daba verde, porque el
# script miraba adaptador, metrica y rutas, y nadie miraba el log. Un aviso que
# nadie lee es un fallo que nadie ve.
Paso "el log del daemon"
if (-not (Test-Path $daemonOut)) { Write-Host "  --  no hay $daemonOut" -ForegroundColor Yellow }
else {
    $texto = Get-Content $daemonOut -ErrorAction SilentlyContinue
    $peers = @($texto | Where-Object { $_ -match 'miembros de la sala' })
    if ($peers.Count -gt 0) {
        Mal "Peers() sigue fallando, $($peers.Count) vez/veces:"
        $peers | Select-Object -Last 3 | ForEach-Object { Mal "    $_" }
        $fallos++
    }
    else { Bien "ningun error consultando los miembros de la sala" }

    $errores = @($texto | Where-Object { $_ -match '^(aviso|error) ' })
    if ($errores.Count -gt 0) {
        Write-Host "  --  $($errores.Count) linea(s) de aviso o error en el log:" -ForegroundColor Yellow
        $errores | Select-Object -Last 12 | ForEach-Object { Write-Host "      $_" -ForegroundColor DarkGray }
    }
    else { Bien "ni un aviso ni un error en todo el arranque" }
}

Paso "resultado"
if ($fallos -eq 0) {
    Write-Host "  Los caminos que una sala no ejercita hacen lo que dicen." -ForegroundColor Green
    exit 0
}
Write-Host "  $fallos comprobacion(es) fallaron." -ForegroundColor Red
exit 1
