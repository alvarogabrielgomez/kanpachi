<#
.SYNOPSIS
    Los cuatro fallos del motor, medidos con el producto entero corriendo.

.DESCRIPTION
    Los cuatro se arreglaron en su momento y ninguno lo puede ver un test de
    paquete: los cuatro son de CABLEADO o de TIEMPO. Este script los ejercita
    con el daemon de verdad, contra kanpachi.accentio.dev.

      1. El canal de eventos. Antes se creaba uno por proceso y se devolvia
         CERRADO mientras no hubiera ninguno, asi que "todavia no arranco" y
         "se murio" eran el mismo hecho para el supervisor. El sintoma medido
         fue el watchdog gastando sus ocho intentos con el daemon recien
         arrancado y sin ninguna sala, y cerrando una sala que el usuario nunca
         creo. Se mide al reves: con el daemon en reposo NO puede haber ni un
         reinicio.

      2. El contexto de vida del proceso. Antes se le pasaba a spawn el
         contexto de la LLAMADA, que lleva un defer cancel, asi que el motor
         moria en cuanto contestaba a la orden que lo habia arrancado. Nada lo
         delataba porque la respuesta llegaba antes. Se mide matando el motor a
         lo bruto y comprobando que el watchdog lo repone y la sala vuelve.

      3. La salida por stdin. Cerrar el daemon LIMPIO tiene que llevarse el
         motor, sin huerfanos.

      4. Basura por el canal de ordenes tiene que volver como error con id, y
         no colgar la llamada ni tumbar el motor.

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
$out = Join-Path $env:TEMP 'kanpachi-motor.out'
$daemon = $null

function Ctl($metodo, $params) {
    $antes = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    $args = @('-data', $Data)
    if ($params) { $args += @('-params', $params) }
    $args += $metodo
    $salida = & (Join-Path $Stage 'pipeprobe.exe') @args 2>&1 | Out-String
    $ErrorActionPreference = $antes
    $salida
}

try {
    Paso "arrancando el daemon"
    $daemon = Start-Process -FilePath (Join-Path $Stage 'kanpachid.exe') `
        -ArgumentList '--console', '--data', $Data -PassThru -WindowStyle Minimized `
        -RedirectStandardOutput $out -RedirectStandardError "$out.err"
    Start-Sleep -Seconds 3
    if ($daemon.HasExited) { throw "el daemon murio al arrancar" }

    # FALLO 1. El daemon en reposo, SIN sala, no puede ver morir un motor que
    # nunca arranco. Antes el canal cerrado se leia como muerte y el watchdog
    # gastaba sus ocho intentos aqui mismo.
    Paso "1. el daemon en reposo no reinicia nada"
    Start-Sleep -Seconds 12
    $log = Get-Content $out -ErrorAction SilentlyContinue | Out-String
    if ($log -match 'reiniciando el motor' -or $log -match 'el watchdog se rinde') {
        Mal "el watchdog actuo con el daemon en reposo y sin sala"
        $fallos++
    }
    else { Bien "ni un reinicio con el daemon en reposo" }
    if (Get-Process kanpachi-engine -ErrorAction SilentlyContinue) {
        Mal "hay un motor corriendo sin que nadie haya pedido una sala"
        $fallos++
    }
    else { Bien "no se levanto ningun motor hasta que hizo falta" }

    # FALLO 4. Antes de abrir nada, porque no necesita sala.
    Paso "4. basura por el canal de ordenes vuelve como error"
    $r = Ctl 'no_existe_este_metodo' $null
    if ($r -match '"error"' -and $r -match '"id"') { Bien "error con id: $($r.Trim())" }
    else { Mal "una orden desconocida no devolvio un error con id: $r"; $fallos++ }
    if (-not (Get-Process -Id $daemon.Id -ErrorAction SilentlyContinue)) {
        Mal "el daemon se cayo con una orden desconocida"; $fallos++
    }

    Paso "abriendo la sala"
    $params = (@{ nickname = 'Alvaro'; name = 'Prueba' } | ConvertTo-Json -Compress).Replace('"', '\"')
    $r = Ctl 'create_room' $params
    if ($r -notmatch '"result"') { throw "no se pudo crear la sala: $r" }
    $reloj = [Diagnostics.Stopwatch]::StartNew()
    while ($reloj.Elapsed.TotalSeconds -lt $Espera) {
        if (Get-NetAdapter -Name "kanpachi0" -ErrorAction SilentlyContinue) { break }
        Start-Sleep -Milliseconds 500
    }
    $motor = Get-Process kanpachi-engine -ErrorAction SilentlyContinue
    if (-not $motor) { throw "la sala se creo sin motor, asi que no hay nada que medir" }
    Bien "sala abierta, motor PID $($motor.Id)"

    # FALLO 2. Matarlo A LO BRUTO es el punto: una salida limpia no ejercita al
    # watchdog. Lo que se mide es que el motor VUELVE y la sala sigue en pie.
    Paso "2. matando el motor a lo bruto, el watchdog lo repone"
    $viejo = $motor.Id
    Stop-Process -Id $viejo -Force
    Start-Sleep -Seconds 2

    $reloj = [Diagnostics.Stopwatch]::StartNew()
    $nuevo = $null
    while ($reloj.Elapsed.TotalSeconds -lt 60) {
        $p = Get-Process kanpachi-engine -ErrorAction SilentlyContinue
        if ($p -and $p.Id -ne $viejo) { $nuevo = $p; break }
        Start-Sleep -Milliseconds 500
    }
    if (-not $nuevo) {
        Mal "el motor no volvio en 60 s, y el watchdog tiene 198 s de escalera"
        $fallos++
    }
    else {
        Bien "el motor volvio como PID $($nuevo.Id) en $([int]$reloj.Elapsed.TotalSeconds) s"
        # Y la sala tiene que seguir siendo la misma, no una nueva.
        $reloj2 = [Diagnostics.Stopwatch]::StartNew()
        $vuelta = $false
        while ($reloj2.Elapsed.TotalSeconds -lt 60) {
            $st = Ctl 'status' $null
            if ($st -match '"conn":"connected"') { $vuelta = $true; break }
            Start-Sleep -Seconds 2
        }
        if ($vuelta) { Bien "la sala volvio a connected" }
        else { Mal "la sala no volvio a connected tras el reinicio del motor"; $fallos++ }

        # LOS DOS. Mirar solo kanpachi0 fue como este script dio verde sobre un
        # fallo real: volvia la sala, no volvia el vestibulo, y a partir de ahi
        # la regla de la puerta no se podia escribir porque su adaptador ya no
        # existia. La sala seguia en pie con la puerta cerrada para siempre.
        foreach ($nombre in 'kanpachi0', 'kanpachi1') {
            $reloj3 = [Diagnostics.Stopwatch]::StartNew()
            $ad = $null
            while ($reloj3.Elapsed.TotalSeconds -lt 30) {
                $ad = Get-NetAdapter -Name $nombre -ErrorAction SilentlyContinue
                if ($ad) { break }
                Start-Sleep -Milliseconds 500
            }
            if ($ad) { Bien "$nombre volvio a estar arriba" }
            else { Mal "$nombre NO volvio tras reiniciar el motor"; $fallos++ }
        }

        # Y la regla de la puerta tiene que estar escrita otra vez. Es la
        # consecuencia de lo anterior, y es lo que se le ve al usuario: sin ella
        # no puede entrar nadie nuevo.
        $fw = New-Object -ComObject HNetCfg.FwPolicy2
        $puerta = @($fw.Rules | Where-Object { $_.Grouping -eq 'Kanpachi' -and $_.Name -like '*puerta*' })
        if ($puerta.Count -gt 0) {
            Bien "la regla de la puerta esta escrita, sobre [$($puerta[0].Interfaces -join ',')]"
        }
        else { Mal "no quedo regla de la puerta: no puede entrar nadie nuevo"; $fallos++ }

        # Y la compuerta tiene que cubrir los adaptadores NUEVOS. Un adaptador
        # nuevo tiene LUID nuevo, asi que una compuerta que no se reacote queda
        # apuntando a uno que ya no existe, sin que nada falle.
        $wfp = & netsh.exe wfp show filters file=- 2>&1 | Out-String
        $cubiertos = 0
        foreach ($p in 'por adaptador de la sala', 'por adaptador del vest') {
            if ($wfp -match [regex]::Escape($p)) { $cubiertos++ }
        }
        if ($cubiertos -eq 2) { Bien "la compuerta cubre los dos adaptadores otra vez" }
        else { Mal "la compuerta solo cubre $cubiertos de 2 adaptadores tras el reinicio"; $fallos++ }
    }

    # FALLO 3. La salida LIMPIA. Es la otra mitad del Job Object, que ya se mide
    # con una muerte sucia en sala-de-verdad.ps1: aqui se comprueba que la
    # salida por stdin tambien se lo lleva, sin dejar huerfanos.
    Paso "3. cerrando el daemon LIMPIO, el motor se va con el"
    $daemon.CloseMainWindow() | Out-Null
    Stop-Process -Id $daemon.Id -ErrorAction SilentlyContinue
    $daemon.WaitForExit(20000) | Out-Null
    $daemon = $null
    Start-Sleep -Seconds 4
    $huerfanos = @(Get-Process kanpachi-engine -ErrorAction SilentlyContinue)
    if ($huerfanos.Count -eq 0) { Bien "sin motores huerfanos" }
    else {
        Mal "quedaron $($huerfanos.Count) motor(es) con el daemon cerrado, o sea una red virtual arriba y el firewall ya purgado"
        $huerfanos | Stop-Process -Force
        $fallos++
    }
    $sobra = Get-NetAdapter -Name "kanpachi*" -ErrorAction SilentlyContinue
    if ($sobra) { Mal "quedaron adaptadores: $($sobra.Name -join ', ')"; $fallos++ }
    else { Bien "sin adaptadores virtuales sueltos" }
}
catch {
    Mal "el script se rompio: $_"
    Write-Host $_.ScriptStackTrace -ForegroundColor DarkGray
    $fallos++
}
finally {
    if ($daemon -and -not $daemon.HasExited) { Stop-Process -Id $daemon.Id -Force }
    Get-Process kanpachi-engine -ErrorAction SilentlyContinue | Stop-Process -Force
    Start-Sleep -Seconds 1
}

Paso "el log del daemon"
$log = Get-Content $out -ErrorAction SilentlyContinue
$avisos = @($log | Where-Object { $_ -match '^(aviso|error) ' })
if ($avisos.Count -gt 0) {
    Write-Host "  --  $($avisos.Count) aviso(s):" -ForegroundColor Yellow
    $avisos | Select-Object -Last 10 | ForEach-Object { Write-Host "      $_" -ForegroundColor DarkGray }
}
else { Bien "ni un aviso en toda la corrida" }

Paso "resultado"
if ($fallos -eq 0) {
    Write-Host "  Los cuatro fallos del motor siguen cerrados, con el producto entero corriendo." -ForegroundColor Green
    exit 0
}
Write-Host "  $fallos comprobacion(es) fallaron." -ForegroundColor Red
exit 1
