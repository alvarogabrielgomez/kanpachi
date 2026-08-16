<#
.SYNOPSIS
    Measures a guest going back to a room, across two real machines, with the
    clocks cut short.

.DESCRIPTION
    Seven scenarios, in order, each one leaving the pair in the state the next
    one needs. They are the cases the return feature was written for, and none
    of them can be proven by a test: they need a host that goes away, a registry
    that keeps answering, and a guest that gives up on its own.

      1  going back after Kanpachi is closed and opened
      2  the host reopening its room, same code, guest reconnecting
      3  the guest arriving BEFORE the host is up
      4  the host up first, the guest entering afterwards
      5  the guest retrying several times until the host appears
      6  the host never coming back, the room expiring, the guest stopping
      7  a kicked guest NOT coming back after a close and open

    # The two machines

    The host is Linux, on the droplet, reached over Tailscale and never over the
    internet. The guest is THIS Windows machine, running the portable, which is
    the shape most people use. The registry is the same one both talk to.

    # What it drives, and what it reads

    Both ends are driven through the terminal client and read through `--json`,
    never by looking at a screen or at a state file. The wire is a contract with
    locks on it; what a screen prints changes when somebody improves it.

    The one exception is the EXISTENCE of last-room.json, which is the point of
    scenario 6: the file is sealed and cannot be read from here, and its absence
    is exactly what is being measured.

    # Before running it

      1. scripts/build-measure-clocks.ps1        (builds the three pieces)
      2. scripts/measure-return.ps1 -Deploy      (puts them on the droplet)
      3. scripts/measure-return.ps1 -Only 1      (and onward, one at a time)
      4. scripts/measure-return.ps1 -Restore     (puts the droplet back)

    Needs an elevated console: the portable's pipe lives under the protected
    prefix, and reading it is an administrator's business.

.PARAMETER Deploy
    Installs the published .deb on the droplet if it is not there, then puts the
    short-clock kanpseed, kanpachid and kanpachi on top of it.

.PARAMETER Restore
    Puts the droplet back: the released kanpseed, and the released .deb over the
    two replaced binaries.

.PARAMETER Only
    Which scenarios to run. All of them by default.
#>
[CmdletBinding()]
param(
    [switch]$Deploy,
    [switch]$Restore,
    [int[]]$Only,
    [string]$Droplet = 'accentio-droplet',
    [string]$Seed = 'kanpachi.accentio.dev',
    [string]$Build = 'dist/measure',
    [string]$PortableRoot = 'C:\kt\measure'
)

$ErrorActionPreference = 'Stop'

function Step($t) { Write-Host "`n=== $t ===" -ForegroundColor Cyan }
function Ok($t) { Write-Host "  OK   $t" -ForegroundColor Green }
function Fail($t) { Write-Host "  FAIL $t" -ForegroundColor Red }
function Info($t) { Write-Host "       $t" -ForegroundColor DarkGray }
function Note($t) { Write-Host "  ..   $t" -ForegroundColor Yellow }

$repo = Split-Path -Parent $PSScriptRoot
Set-Location $repo
$buildDir = Join-Path $repo $Build

# La elevación se exige donde hace falta, que es al correr escenarios, y no acá.
#
# `-Deploy` y `-Restore` solo hablan con el droplet por ssh: pedir administrador
# para eso obligaría a abrir una consola elevada para no usarla, y el permiso que
# no hace falta es el que se acaba concediendo por costumbre.
function Require-Admin() {
    $isAdmin = ([Security.Principal.WindowsPrincipal] `
            [Security.Principal.WindowsIdentity]::GetCurrent()
    ).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
    if (-not $isAdmin) {
        throw 'An elevated console is needed to run the scenarios: the portable pipe lives under the protected prefix, and launching the portable raises UAC.'
    }
}

# ─── Los dos extremos ────────────────────────────────────────────────────────

$peerPipe = '\\.\pipe\ProtectedPrefix\Administrators\kanpachi-portable'
$peerExe = Join-Path $PortableRoot 'kanpachi-portable.exe'
$peerData = Join-Path $PortableRoot 'kanpachi-data'
$cli = Join-Path $buildDir 'kanpachi.exe'

# Native corre algo externo sin que su stderr mate el script. Mismo rodeo que
# verify.ps1: en PowerShell 5.1 lo que un exe escribe en stderr se convierte en
# ErrorRecord, y con 'Stop' eso aborta.
function Native([scriptblock]$block) {
    $before = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    $out = & $block 2>&1 | Out-String
    $script:lastCode = $LASTEXITCODE
    $ErrorActionPreference = $before
    return $out
}

# Host-Run corre algo en el host de Linux y FALLA si falla.
#
# Comprobar el código de salida no es celo: sin esto, un `sudo` que pide password
# devuelve error, el script sigue, e imprime OK sobre un despliegue que no
# ocurrió. Pasó en la primera ejecución, y una medición que dice que sí cuando
# fue que no es peor que no medir.
function Host-Run([string]$cmd) {
    $out = Native { & ssh $Droplet $cmd }
    if ($script:lastCode -ne 0) {
        throw "on the host: ``$cmd`` failed (exit $script:lastCode)`n$($out.Trim())"
    }
    return $out
}

# Host-Try es lo mismo para lo que SE PUEDE ir mal sin que importe: parar algo
# que no estaba corriendo, o preguntar por un paquete que no está.
function Host-Try([string]$cmd) {
    return Native { & ssh $Droplet $cmd }
}

# Host-Json pide algo al host de Linux y devuelve el objeto. `sudo` porque el
# canal y el token son de root, que es lo que su propia ayuda dice.
function Host-Json([string]$verb) {
    $raw = Host-Run "sudo -n kanpachi --json $verb"
    if (-not $raw.Trim()) { return $null }
    try { return $raw | ConvertFrom-Json } catch { Info "host $verb -> $($raw.Trim())"; return $null }
}

function Peer-Json([string]$verb) {
    $raw = Native { & $cli --pipe $peerPipe --data $peerData --json $verb.Split(' ') }
    if (-not $raw.Trim()) { return $null }
    try { return $raw | ConvertFrom-Json } catch { Info "peer $verb -> $($raw.Trim())"; return $null }
}

function Peer-Running() {
    return [bool](Get-Process -Name 'kanpachid' -ErrorAction SilentlyContinue)
}

# Peer-Kill mata el portable, que es el cierre SUCIO y el caso interesante.
#
# Cerrar por la bandeja sería una salida limpia, y las dos tienen que llevar al
# mismo sitio: ninguna de las dos es una petición formal de salir de la sala.
function Peer-Kill() {
    Get-Process -Name 'kanpachid', 'kanpachi-portable', 'kanpachi_ui' -ErrorAction SilentlyContinue |
    Stop-Process -Force -ErrorAction SilentlyContinue
    $null = Wait-Until { -not (Peer-Running) } 20 'the guest to be gone'
}

function Peer-Start() {
    Start-Process -FilePath $peerExe -WorkingDirectory $PortableRoot | Out-Null
    if (-not (Wait-Until { Peer-Running } 90 'the guest daemon to come up')) {
        throw 'the portable did not start'
    }
    # El pipe abre DESPUÉS de purgar el firewall y levantar el motor, así que
    # que el proceso exista no significa que se le pueda hablar.
    $null = Wait-Until { $null -ne (Peer-Json 'status') } 90 'the guest pipe to answer'
}

function Wait-Until([scriptblock]$cond, [int]$seconds, [string]$what) {
    $deadline = (Get-Date).AddSeconds($seconds)
    while ((Get-Date) -lt $deadline) {
        if (& $cond) { return $true }
        Start-Sleep -Seconds 2
    }
    Info "gave up waiting for $what after ${seconds}s"
    return $false
}

function Peer-InRoom() {
    $st = Peer-Json 'status'
    if ($null -eq $st) { return $false }
    return $st.conn -eq 'connected'
}

function Peer-Returning() {
    $st = Peer-Json 'status'
    if ($null -eq $st) { return $null }
    return $st.returning
}

# ─── Desplegar y deshacer ────────────────────────────────────────────────────

if ($Deploy) {
    Step 'the droplet gets the short-clock binaries'
    if (-not (Test-Path (Join-Path $buildDir 'linux/kanpachid'))) {
        throw "no measurement build in $Build. Run scripts/build-measure-clocks.ps1 first."
    }

    $installed = (Host-Try 'dpkg -l kanpachi 2>/dev/null | grep -c "^ii" || true').Trim()
    if ($installed -eq '0') {
        Note 'the published package is not installed; installing it for its engine, units and catalogue'
        $deb = Join-Path $buildDir 'released.deb'
        if (-not (Test-Path $deb)) {
            Native { & gh release download --pattern 'kanpachi-amd64.deb' --output $deb --clobber } | Out-Null
        }
        Native { & scp -q $deb "${Droplet}:/tmp/kanpachi-released.deb" } | Out-Null
        Host-Run 'sudo -n dpkg -i /tmp/kanpachi-released.deb' | Out-Null
        Ok 'published package installed'
    }
    else { Ok 'published package already installed' }

    Host-Try 'sudo -n systemctl stop kanpachid kanpseed-registry' | Out-Null
    Native { & scp -q (Join-Path $buildDir 'linux/kanpachid') "${Droplet}:/tmp/kanpachid" } | Out-Null
    Native { & scp -q (Join-Path $buildDir 'linux/kanpachi') "${Droplet}:/tmp/kanpachi" } | Out-Null
    Native { & scp -q (Join-Path $buildDir 'kanpseed-linux-amd64') "${Droplet}:/tmp/kanpseed" } | Out-Null
    Host-Run ('sudo -n install -m755 /tmp/kanpachid /usr/libexec/kanpachi/kanpachid' +
        ' && sudo -n install -m755 /tmp/kanpachi /usr/bin/kanpachi' +
        ' && sudo -n install -m755 /tmp/kanpseed /usr/local/bin/kanpseed') | Out-Null
    Host-Run 'sudo -n systemctl start kanpseed-registry' | Out-Null
    Ok 'kanpachid, kanpachi and kanpseed replaced, registry back up'
    Info (Host-Run 'sudo -n systemctl is-active kanpseed-registry; /usr/bin/kanpachi version 2>/dev/null || true').Trim()
    return
}

if ($Restore) {
    Step 'the droplet goes back to what it was serving'
    $deb = Join-Path $buildDir 'released.deb'
    if (-not (Test-Path $deb)) {
        Native { & gh release download --pattern 'kanpachi-amd64.deb' --output $deb --clobber } | Out-Null
    }
    Native { & gh release download --pattern 'kanpseed-linux-amd64' --output (Join-Path $buildDir 'kanpseed-released') --clobber } | Out-Null
    Host-Try 'sudo -n systemctl stop kanpachid kanpseed-registry' | Out-Null
    Native { & scp -q $deb "${Droplet}:/tmp/kanpachi-released.deb" } | Out-Null
    Native { & scp -q (Join-Path $buildDir 'kanpseed-released') "${Droplet}:/tmp/kanpseed" } | Out-Null
    Host-Run ('sudo -n dpkg -i /tmp/kanpachi-released.deb' +
        ' && sudo -n install -m755 /tmp/kanpseed /usr/local/bin/kanpseed' +
        ' && sudo -n systemctl start kanpseed-registry') | Out-Null
    Ok 'released kanpseed and package back in place'
    Info (Host-Run 'sudo -n systemctl is-active kanpseed-registry').Trim()
    return
}

# ─── Los escenarios ──────────────────────────────────────────────────────────

$script:failures = @()
$script:code = ''

function Check($what, [bool]$cond) {
    if ($cond) { Ok $what } else { Fail $what; $script:failures += $what }
}

# Una LISTA con el id dentro, y no un diccionario ordenado.
#
# `[ordered]@{ 1 = ...; 2 = ... }` indexado con un entero devuelve por POSICION y
# no por clave, asi que `-Only 1` corria el segundo escenario. Falla en silencio:
# imprime un titulo, hace otra cosa, y lo unico que delata la diferencia es leer
# el nombre con atencion.
$scenarios = @(

    @{
        id    = 1
        name  = 'going back after Kanpachi is closed and opened'
        run   = {
            Host-Run 'sudo -n systemctl restart kanpachid' | Out-Null
            Start-Sleep -Seconds 8
            $h = Host-Json 'host Medicion'
            $script:code = $h.room.code + '@' + $h.room.seed
            Check 'the host opened a room' ($null -ne $h -and $h.room.code)
            Info "code $script:code"

            Peer-Start
            Peer-Json "join $script:code" | Out-Null
            Check 'the guest is in' (Wait-Until { Peer-InRoom } 120 'the guest to be in')

            $last = Peer-Json 'last'
            Check 'the room is saved with auto-return on' ($last.found -and $last.room.auto_return)

            Note 'killing the guest, which is the dirty close'
            Peer-Kill
            Peer-Start
            Check 'it went back on its own, with nobody typing a code' (Wait-Until { Peer-InRoom } 120 'the guest to come back')
        }
    }

    @{
        id    = 2
        name  = 'the host reopens its room, same code, and the guest reconnects'
        run   = {
            $before = (Host-Json 'status').room.code
            Note 'restarting the host daemon, which is a reboot as far as the room is concerned'
            Host-Run 'sudo -n systemctl restart kanpachid' | Out-Null
            Check 'the host brought the room back by itself' (Wait-Until {
                    $s = Host-Json 'status'; $null -ne $s -and $s.conn -eq 'connected'
                } 180 'the host room to be back')
            $after = (Host-Json 'status').room.code
            Check "the code is the same one it handed out ($before)" ($before -eq $after)
            Check 'the guest is back in without being told' (Wait-Until { Peer-InRoom } 240 'the guest to reconnect')
        }
    }

    @{
        id    = 3
        name  = 'the guest arrives BEFORE the host is up'
        run   = {
            Note 'stopping the host daemon without closing the room'
            Host-Run 'sudo -n systemctl stop kanpachid' | Out-Null
            Peer-Kill
            Peer-Start
            Check 'the guest is not in a room' (-not (Peer-InRoom))
            $r = Wait-Until { $null -ne (Peer-Returning) } 90 'the guest to report it is going back'
            Check 'and it says it is going back to that room' $r
            $ret = Peer-Returning
            if ($ret) { Info "room $($ret.name), next try in $($ret.next_in_ms)ms, attempt $($ret.attempts), last: $($ret.reason)" }
        }
    }

    @{
        id    = 4
        name  = 'the host comes up and the guest walks in on the next try'
        run   = {
            $before = (Peer-Returning).attempts
            Host-Run 'sudo -n systemctl start kanpachid' | Out-Null
            Check 'the host room is up' (Wait-Until {
                    $s = Host-Json 'status'; $null -ne $s -and $s.conn -eq 'connected'
                } 180 'the host room')
            Check "the guest got in on its own (it was on attempt $before)" (Wait-Until { Peer-InRoom } 120 'the guest to get in')
        }
    }

    @{
        id    = 5
        name  = 'the guest retries several times while the host stays down'
        run   = {
            Host-Run 'sudo -n systemctl stop kanpachid' | Out-Null
            Peer-Kill
            Peer-Start
            $seen = 0
            $deadline = (Get-Date).AddSeconds(150)
            while ((Get-Date) -lt $deadline -and $seen -lt 3) {
                $ret = Peer-Returning
                if ($ret -and $ret.attempts -gt $seen) {
                    $seen = $ret.attempts
                    Info "attempt $seen, next in $([int]($ret.next_in_ms/1000))s, last: $($ret.reason)"
                }
                Start-Sleep -Seconds 3
            }
            Check 'it kept trying, at least three times, with nobody asking' ($seen -ge 3)
            Check 'and it still has the room saved' ((Peer-Json 'last').found)

            Host-Run 'sudo -n systemctl start kanpachid' | Out-Null
            Check 'and it got in as soon as the host was there' (Wait-Until { Peer-InRoom } 150 'the guest to get in')
        }
    }

    @{
        id    = 6
        name  = 'the host never comes back, the room expires, and the guest stops'
        run   = {
            Note 'closing the host daemon and waiting out RoomTTL, which is four minutes in this build'
            Host-Run 'sudo -n systemctl stop kanpachid' | Out-Null
            Peer-Kill
            Peer-Start
            Check 'the guest is going back' (Wait-Until { $null -ne (Peer-Returning) } 90 'the guest to start going back')

            Note 'waiting for the registry to sweep the room; up to six minutes'
            $gone = Wait-Until { -not (Peer-Json 'last').found } 400 'the saved room to be forgotten'
            Check 'the guest forgot the room once the registry said it does not exist' $gone
            Check 'and it is not going back to anything any more' ($null -eq (Peer-Returning))
            Check 'and the file is gone from disk' (-not (Test-Path (Join-Path $peerData 'last-room.json')))
        }
    }

    @{
        id    = 7
        name  = 'a kicked guest does NOT come back on its own'
        run   = {
            Host-Run 'sudo -n systemctl start kanpachid' | Out-Null
            Start-Sleep -Seconds 8
            $h = Host-Json 'status'
            if ($h.conn -ne 'connected') { $h = Host-Json 'host Medicion' }
            $script:code = $h.room.code + '@' + $h.room.seed
            Peer-Json "join $script:code" | Out-Null
            Check 'the guest is in' (Wait-Until { Peer-InRoom } 120 'the guest to be in')

            $me = (Peer-Json 'status').local_ip
            Note "the host kicks $me"
            Host-Run "sudo -n kanpachi kick $me" | Out-Null
            Check 'the guest is out' (Wait-Until { -not (Peer-InRoom) } 120 'the guest to be out')

            $last = Peer-Json 'last'
            Check 'the room is still saved, because kicking is not banning' ($last.found)
            Check 'but it will not go back on its own' (-not $last.room.auto_return)

            Peer-Kill
            Peer-Start
            Start-Sleep -Seconds 45
            Check 'and after a close and open it is still out' (-not (Peer-InRoom))
            Check 'with nothing scheduled to go back' ($null -eq (Peer-Returning))
        }
    }
)

Require-Admin

Step 'the pieces'
if (-not (Test-Path $cli)) { throw "no kanpachi.exe in $Build. Run scripts/build-measure-clocks.ps1 first." }
if (-not (Test-Path $peerExe)) {
    New-Item -ItemType Directory -Force $PortableRoot | Out-Null
    Copy-Item (Join-Path $buildDir 'kanpachi-portable.exe') $peerExe -Force
}
Ok "guest in $PortableRoot"
Ok "host on $Droplet, registry at $Seed"

$run = if ($Only) { $Only } else { $scenarios.id }
foreach ($n in $run) {
    $s = $scenarios | Where-Object { $_.id -eq $n }
    if (-not $s) { Fail "there is no scenario $n"; continue }
    Step "$n. $($s.name)"
    try { & $s.run }
    catch {
        Fail "scenario $n blew up: $_"
        $script:failures += "scenario $n"
    }
}

Step 'result'
if ($script:failures.Count -eq 0) {
    Ok 'every scenario that ran came out as designed'
}
else {
    Fail "$($script:failures.Count) check(s) red:"
    $script:failures | ForEach-Object { Info $_ }
}
Info 'When done: scripts/measure-return.ps1 -Restore'
if ($script:failures.Count -gt 0) { exit 1 }
