<#
.SYNOPSIS
    Que los ajustes del adaptador virtual vuelvan solos cuando algo los rompe.

.DESCRIPTION
    La prueba pendiente estaba escrita como "cambiar de WiFi a cable con la sala
    abierta". Mezcla DOS preguntas distintas, y solo una necesita cambiar de red:

      A. Si algo revierte la metrica, el MTU o las rutas del adaptador virtual,
         el reaplicado las repone. Esa es la mitad con riesgo, y NO hace falta
         tocar la red para medirla: se rompen a mano y se mira si vuelven.
         El respaldo periodico existe justo para esto: AdapterReapplyEvery = 8
         latidos, o sea unos dos minutos, y es lo que cubre una suscripcion
         muerta.

      B. Que Windows avise del cambio y el aviso llegue al supervisor. Esa mitad
         si necesita que la red cambie de verdad. Sin cable, el sustituto es
         apagar y encender la NIC de WiFi: el transporte desaparece entero, que
         es MAS agresivo que desenchufar un cable.

    Los dos actos son independientes a proposito. El acto A es determinista y
    repetible; el acto B depende de que Windows clasifique la red que vuelve, y
    puede no re-identificarla por ser la misma de siempre. Si A da verde y B no,
    lo que falla es el aviso, no el reaplicado, y el respaldo periodico ya lo
    cubre. Distinguirlos es todo el punto de partirlo.

    Ademas comprueba algo que nadie miraba: netcfg resuelve SOLO kanpachi0, asi
    que una ruta por defecto sobre kanpachi1 no la borraria nadie.

    Necesita consola elevada.

.PARAMETER SinTocarLaWifi
    Salta el acto B. Deja solo la parte que no corta la conexion.

.PARAMETER ConCambioDeSSID
    Acto C: espera a que cambies de red a mano (hotspot del movil) y mide contra
    una subred y una puerta de enlace DISTINTAS, que es el analogo exacto del
    cable. No pregunta nada: detecta el cambio sondeando.
#>
[CmdletBinding()]
param(
    [string]$Stage = "C:\kt\stage",
    [string]$Data = "$env:ProgramData\Kanpachi",
    [switch]$SinTocarLaWifi,
    [switch]$SoloElCorte,
    [switch]$ConCambioDeSSID,
    [int]$EsperaReaplicado = 200,
    [int]$EsperaSSID = 180
)

$ErrorActionPreference = 'Stop'

function Paso($t) { Write-Host "`n=== $t ===" -ForegroundColor Cyan }
function Bien($t) { Write-Host "  OK  $t" -ForegroundColor Green }
function Mal($t) { Write-Host "  MAL $t" -ForegroundColor Red }
function Nota($t) { Write-Host "  --  $t" -ForegroundColor DarkGray }

$esAdmin = ([Security.Principal.WindowsPrincipal] `
    [Security.Principal.WindowsIdentity]::GetCurrent()
).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if (-not $esAdmin) { throw "Hace falta una consola elevada." }

$fallos = 0
$out = Join-Path $env:TEMP 'kanpachi-cambio-red.out'
$daemon = $null
$wifi = $null
$wifiApagada = $false

function Ctl($metodo, $params) {
    $antes = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    $a = @('-data', $Data)
    if ($params) { $a += @('-params', $params) }
    $a += $metodo
    $salida = & (Join-Path $Stage 'pipeprobe.exe') @a 2>&1 | Out-String
    $ErrorActionPreference = $antes
    $salida
}

# EstadoDe lee del SISTEMA lo que netcfg escribe. Preguntarle a Windows es la
# unica forma de que la medicion no repita la creencia del propio daemon.
function EstadoDe($nombre) {
    $v4 = Get-NetIPInterface -InterfaceAlias $nombre -AddressFamily IPv4 -ErrorAction SilentlyContinue
    $v6 = Get-NetIPInterface -InterfaceAlias $nombre -AddressFamily IPv6 -ErrorAction SilentlyContinue
    $porDefecto = @(Get-NetRoute -InterfaceAlias $nombre -ErrorAction SilentlyContinue |
        Where-Object { $_.DestinationPrefix -eq '0.0.0.0/0' -or $_.DestinationPrefix -eq '::/0' })
    [pscustomobject]@{
        Existe     = [bool]$v4
        MetricaV4  = if ($v4) { [int]$v4.InterfaceMetric } else { -1 }
        MetricaV6  = if ($v6) { [int]$v6.InterfaceMetric } else { -1 }
        Mtu        = if ($v4) { [int]$v4.NlMtu } else { -1 }
        PorDefecto = $porDefecto.Count
    }
}

function Pintar($nombre, $e) {
    Nota "$nombre  metrica v4=$($e.MetricaV4) v6=$($e.MetricaV6)  mtu=$($e.Mtu)  rutas por defecto=$($e.PorDefecto)"
}

# EsperarA sondea hasta que la condicion se cumple y devuelve los segundos que
# tardo, o -1. Devolver el TIEMPO importa: separa "lo repuso el evento" de "lo
# repuso el respaldo periodico", que tarda hasta dos minutos.
function EsperarA([scriptblock]$cond, [int]$segundos, [string]$que) {
    $reloj = [Diagnostics.Stopwatch]::StartNew()
    while ($reloj.Elapsed.TotalSeconds -lt $segundos) {
        if (& $cond) { return [int]$reloj.Elapsed.TotalSeconds }
        Start-Sleep -Milliseconds 1000
    }
    return -1
}

function SubredLocal() {
    $r = Get-NetRoute -DestinationPrefix '0.0.0.0/0' -ErrorAction SilentlyContinue |
        Where-Object { $_.InterfaceAlias -notlike 'kanpachi*' } |
        Sort-Object { $_.RouteMetric + (Get-NetIPInterface -InterfaceIndex $_.InterfaceIndex -AddressFamily IPv4).InterfaceMetric } |
        Select-Object -First 1
    if ($r) { "$($r.InterfaceAlias)/$($r.NextHop)" } else { "" }
}

# ConnDe devuelve el estado LITERAL de la sala, no un si-o-no.
#
# Comparar contra "connected" a secas fue lo que hizo inutil la primera corrida:
# el acto B dijo "no volvio a connected" y no decia a QUE volvio, que es toda la
# diferencia entre "el tunel no volvio" y "el tunel volvio y la etiqueta se
# quedo pegada". Son dos fallos distintos con dos arreglos distintos.
$script:UltimoStatus = ''
function ConnDe() {
    $s = Ctl 'status' $null
    $script:UltimoStatus = $s
    if ($s -match '"conn"\s*:\s*"([a-z_]+)"') { $Matches[1] } else { '(sin conn)' }
}

function SalaConectada([int]$segundos) {
    EsperarA { (ConnDe) -eq 'connected' } $segundos 'sala conectada'
}

try {
    Paso "arrancando el daemon y abriendo una sala"
    $daemon = Start-Process -FilePath (Join-Path $Stage 'kanpachid.exe') `
        -ArgumentList '--console', '--data', $Data -PassThru -WindowStyle Minimized `
        -RedirectStandardOutput $out -RedirectStandardError "$out.err"
    Start-Sleep -Seconds 3
    if ($daemon.HasExited) { throw "el daemon murio al arrancar" }

    $params = (@{ nickname = 'Alvaro'; name = 'Prueba' } | ConvertTo-Json -Compress).Replace('"', '\"')
    $r = Ctl 'create_room' $params
    if ($r -notmatch '"result"') { throw "no se pudo crear la sala: $r" }

    if ((EsperarA { (EstadoDe 'kanpachi0').Existe } 60 'kanpachi0') -lt 0) {
        throw "kanpachi0 no aparecio, asi que no hay nada que medir"
    }
    if ((SalaConectada 90) -lt 0) { throw "la sala no llego a connected" }

    # La linea base sale del sistema, NO de constantes: asi el MTU esperado es el
    # que sondeo esta maquina en esta red, y la comprobacion no depende de que la
    # red de casa siga dando 1500 el dia que alguien vuelva a correr esto.
    $base0 = EstadoDe 'kanpachi0'
    $base1 = EstadoDe 'kanpachi1'
    Bien "sala abierta"
    Pintar 'kanpachi0' $base0
    Pintar 'kanpachi1' $base1
    $redAntes = SubredLocal
    Nota "salida a internet por [$redAntes]"

    if ($base0.MetricaV4 -ne 1) { Mal "la metrica IPv4 no era 1 al empezar"; $fallos++ }
    if ($base0.PorDefecto -ne 0) { Mal "kanpachi0 nacio con ruta por defecto"; $fallos++ }
    if ($base1.Existe -and $base1.PorDefecto -ne 0) {
        Mal "kanpachi1 tiene ruta por defecto, y a kanpachi1 no lo mira netcfg"
        $fallos++
    }

    # ---------------------------------------------------------------- ACTO A
    # Romper a mano lo que netcfg escribe. Metrica 9999 y ruta por defecto con
    # metrica 9999 suman muy por encima de la salida real, asi que la ruta que se
    # mete NO puede robar trafico mientras dura la prueba. Esa es la unica razon
    # por la que es seguro meterla.
    Paso "A. romper los ajustes a mano y mirar si vuelven solos"
    if ($SoloElCorte) { Nota "saltado por -SoloElCorte" }
    else {

    Set-NetIPInterface -InterfaceAlias 'kanpachi0' -AddressFamily IPv4 `
        -InterfaceMetric 9999 -ErrorAction SilentlyContinue
    try {
        New-NetRoute -DestinationPrefix '0.0.0.0/0' -InterfaceAlias 'kanpachi0' `
            -NextHop '0.0.0.0' -RouteMetric 9999 -PolicyStore ActiveStore `
            -Confirm:$false -ErrorAction Stop | Out-Null
    }
    catch { Nota "no se pudo meter la ruta por defecto de mentira: $_" }

    $roto = EstadoDe 'kanpachi0'
    Pintar 'kanpachi0 roto' $roto
    if ($roto.MetricaV4 -ne 9999 -and $roto.PorDefecto -eq 0) {
        Mal "no se pudo romper nada, asi que el acto A no mide nada"
        $fallos++
    }

    $t = EsperarA {
        $e = EstadoDe 'kanpachi0'
        $e.MetricaV4 -eq $base0.MetricaV4 -and $e.PorDefecto -eq 0
    } $EsperaReaplicado 'reaplicado'

    $tras = EstadoDe 'kanpachi0'
    Pintar 'kanpachi0 despues' $tras
    if ($t -ge 0) {
        Bien "los ajustes volvieron solos en $t s"
        if ($t -le 20) { Nota "tan rapido que lo disparo el aviso de Windows, no el respaldo" }
        else {
            Nota "tardo lo que tarda el respaldo periodico, o sea que el aviso no llego"
            Nota "hoy eso es lo esperado: SystemEvents es un provisional y sus canales"
            Nota "nunca emiten. Cuando se escriba el de verdad, esto tiene que bajar"
            Nota "a unos pocos segundos, y ese salto es su prueba de aceptacion"
        }
    }
    else {
        Mal "los ajustes NO volvieron en $EsperaReaplicado s: metrica=$($tras.MetricaV4) rutas por defecto=$($tras.PorDefecto)"
        $fallos++
    }
    if ($tras.Mtu -ne $base0.Mtu) {
        Mal "el MTU quedo en $($tras.Mtu) y era $($base0.Mtu)"
        $fallos++
    }

    # La prueba de que lo repuso KANPACHI y no Windows por su cuenta. Sin esta
    # linea, una ruta que desaparece sola se lee igual que una que borramos.
    $log = Get-Content $out -Encoding UTF8 -ErrorAction SilentlyContinue | Out-String
    if ($log -match 'ruta por defecto sobre el adaptador virtual') {
        Bien "el log dice que la ruta por defecto la quito el daemon"
    }
    else {
        Mal "la ruta se fue y el daemon no dice haberla quitado, asi que no se sabe quien"
        $fallos++
    }

    } # fin del acto A

    # ---------------------------------------------------------------- ACTO B
    if (-not $SinTocarLaWifi) {
        Paso "B. apagar y encender la WiFi con la sala abierta"
        $wifi = Get-NetAdapter | Where-Object {
            $_.Status -eq 'Up' -and $_.InterfaceDescription -notlike '*Tailscale*' -and
            $_.Name -notlike 'kanpachi*' -and $_.Name -notlike 'vEthernet*'
        } | Select-Object -First 1
        if (-not $wifi) { throw "no se encontro la NIC por la que sale esta maquina" }

        # El control. Si la sala ya venia degradada de antes, cortar la WiFi no
        # prueba nada sobre el corte. Se anota ANTES de tocar la red.
        $connAntes = ConnDe
        Nota "antes de tocar la red, la sala esta en [$connAntes]"
        if ($connAntes -ne 'connected') {
            Mal "la sala ya estaba en [$connAntes] antes del corte, asi que el acto B no mide el corte"
            $fallos++
        }
        Nota "cortando por [$($wifi.Name)], vuelve sola en unos segundos"

        Disable-NetAdapter -Name $wifi.Name -Confirm:$false
        $wifiApagada = $true
        Start-Sleep -Seconds 12

        $durante = EstadoDe 'kanpachi0'
        Nota "con la WiFi caida: kanpachi0 existe=$($durante.Existe)"

        Nota "con la WiFi caida: la sala esta en [$(ConnDe)]"

        Enable-NetAdapter -Name $wifi.Name -Confirm:$false
        $wifiApagada = $false
        $tRed = EsperarA { (Get-NetAdapter -Name $wifi.Name).Status -eq 'Up' } 60 'wifi arriba'
        if ($tRed -lt 0) { throw "la WiFi no volvio en 60 s" }
        Bien "la WiFi volvio en $tRed s"

        # Lo que se mide del otro lado del corte: los dos adaptadores, los
        # ajustes, la sala, la puerta y la compuerta. Mirar solo kanpachi0 fue lo
        # que dejo pasar el fallo del reinicio del motor.
        foreach ($n in 'kanpachi0', 'kanpachi1') {
            if ((EsperarA { (EstadoDe $n).Existe } 90 $n) -ge 0) { Bien "$n sigue arriba" }
            else { Mal "$n no volvio tras el corte de red"; $fallos++ }
        }

        $tSala = SalaConectada 150
        if ($tSala -ge 0) { Bien "la sala volvio a connected en $tSala s" }
        else {
            $q = ConnDe
            Mal "la sala se quedo en [$q] y no volvio a connected en 150 s"
            $fallos++
            Nota "status crudo: $($script:UltimoStatus.Trim())"
            if ($q -eq 'degraded') {
                Nota "degradado es una puerta de un solo sentido: el motor solo emite"
                Nota "connected al SUBIR el TUN, y el TUN no se cayo en ningun momento"
            }
        }

        $tAjustes = EsperarA {
            $e = EstadoDe 'kanpachi0'
            $e.MetricaV4 -eq $base0.MetricaV4 -and $e.PorDefecto -eq 0 -and $e.Mtu -eq $base0.Mtu
        } $EsperaReaplicado 'ajustes tras el corte'
        $fin = EstadoDe 'kanpachi0'
        Pintar 'kanpachi0 tras el corte' $fin
        if ($tAjustes -ge 0) { Bien "los ajustes siguen puestos tras el corte ($tAjustes s)" }
        else { Mal "los ajustes no quedaron bien tras el corte de red"; $fallos++ }

        $fin1 = EstadoDe 'kanpachi1'
        if ($fin1.Existe -and $fin1.PorDefecto -ne 0) {
            Mal "aparecio una ruta por defecto sobre kanpachi1 y nadie la mira"
            $fallos++
        }

        $fw = New-Object -ComObject HNetCfg.FwPolicy2
        $puerta = @($fw.Rules | Where-Object { $_.Grouping -eq 'Kanpachi' -and $_.Name -like '*puerta*' })
        if ($puerta.Count -gt 0) { Bien "la regla de la puerta sigue escrita" }
        else { Mal "no quedo regla de la puerta tras el corte de red"; $fallos++ }

        $wfp = & netsh.exe wfp show filters file=- 2>&1 | Out-String
        $cubiertos = 0
        foreach ($p in 'por adaptador de la sala', 'por adaptador del vest') {
            if ($wfp -match [regex]::Escape($p)) { $cubiertos++ }
        }
        if ($cubiertos -eq 2) { Bien "la compuerta cubre los dos adaptadores" }
        else { Mal "la compuerta solo cubre $cubiertos de 2 tras el corte"; $fallos++ }
    }
    else { Paso "B. saltado por -SinTocarLaWifi" }

    # ---------------------------------------------------------------- ACTO C
    if ($ConCambioDeSSID) {
        Paso "C. cambia de red a mano (hotspot del movil) y esto lo detecta"
        Nota "esperando hasta $EsperaSSID s a que cambie la salida a internet..."
        $tSSID = EsperarA { (SubredLocal) -ne $redAntes -and (SubredLocal) -ne "" } $EsperaSSID 'otra red'
        if ($tSSID -lt 0) {
            Nota "no cambio la red en $EsperaSSID s, acto C sin datos"
        }
        else {
            $redDespues = SubredLocal
            Bien "la salida a internet cambio de [$redAntes] a [$redDespues] en $tSSID s"
            $t2 = EsperarA {
                $e = EstadoDe 'kanpachi0'
                $e.MetricaV4 -eq $base0.MetricaV4 -and $e.PorDefecto -eq 0
            } $EsperaReaplicado 'ajustes en la red nueva'
            if ($t2 -ge 0) { Bien "los ajustes aguantaron el cambio de red ($t2 s)" }
            else { Mal "los ajustes no sobrevivieron al cambio de red"; $fallos++ }

            $t3 = SalaConectada 150
            if ($t3 -ge 0) { Bien "la sala volvio a connected en la red nueva ($t3 s)" }
            else { Mal "la sala no volvio a connected en la red nueva"; $fallos++ }
        }
    }
}
catch {
    Mal "el script se rompio: $_"
    Write-Host $_.ScriptStackTrace -ForegroundColor DarkGray
    $fallos++
}
finally {
    # Lo primero de todo: si el script murio con la WiFi apagada, la maquina se
    # queda sin red. Reponerla va antes que limpiar nada nuestro.
    if ($wifiApagada -and $wifi) {
        Enable-NetAdapter -Name $wifi.Name -Confirm:$false -ErrorAction SilentlyContinue
        Nota "se repuso [$($wifi.Name)] desde la limpieza"
    }
    foreach ($n in 'kanpachi0', 'kanpachi1') {
        Get-NetRoute -InterfaceAlias $n -DestinationPrefix '0.0.0.0/0' -ErrorAction SilentlyContinue |
            Remove-NetRoute -Confirm:$false -ErrorAction SilentlyContinue
    }
    if ($daemon -and -not $daemon.HasExited) {
        Stop-Process -Id $daemon.Id -Force -ErrorAction SilentlyContinue
    }
    Start-Sleep -Seconds 3
    Get-Process kanpachi-engine -ErrorAction SilentlyContinue | Stop-Process -Force
}

Paso "el log del daemon"
$log = Get-Content $out -Encoding UTF8 -ErrorAction SilentlyContinue
$avisos = @($log | Where-Object { $_ -match '^(aviso|error) ' })
if ($avisos.Count -gt 0) {
    Write-Host "  --  $($avisos.Count) aviso(s):" -ForegroundColor Yellow
    $avisos | Select-Object -Last 12 | ForEach-Object { Write-Host "      $_" -ForegroundColor DarkGray }
}
else { Bien "ni un aviso en toda la corrida" }

Paso "resultado"
if ($fallos -eq 0) {
    Write-Host "  Los ajustes del adaptador vuelven solos, y aguantan que se caiga la red." -ForegroundColor Green
    exit 0
}
Write-Host "  $fallos comprobacion(es) fallaron." -ForegroundColor Red
exit 1
