<#
.SYNOPSIS
    Measures a guest going back to a room, across two real machines, with the
    clocks cut short.

.DESCRIPTION
    Fourteen scenarios. The first seven are the return feature's cases; 8 to 11
    are the 2026-08-16 fixes — the reattach, the member key, and the control
    rule that stopped accumulating; 12 to 14 are the 2026-08-23 ones: the leave
    button, the startup race between the two state files, and what displacing
    writes to them. None of them can be
    proven by a test: they need a host that goes away, a registry that keeps
    answering, and a guest that gives up on its own.

      1  going back after Kanpachi is closed and opened
      2  the host reopening its room, same code, guest reconnecting
      3  the guest arriving BEFORE the host is up
      4  the host up first, the guest entering afterwards
      5  the guest retrying several times until the host appears
      6  the host never coming back, the room expiring, the guest stopping
      7  a kicked guest NOT coming back after a close and open
      8  an induced flap ending in a reattach: same address, no new credential
      9  a dirty restart coming back as itself: credential handed back
      10 the control rule scoped to who is there, never an accumulation
      11 a kicked member returning as a stranger: new address, nothing back
      12 pressing leave while going back, and it staying off across a restart
      13 both state files at once: the machine's own room wins the startup
      14 displacing: closing ends the room, leaving keeps the way back

    8 to 11 run in that order and feed each other; the default full run does
    1-6, then 8-11, then 7, because 7 wants a guest inside to kick and 11
    leaves one. 12 to 14 stand alone and go last: 13 and 14 open a room of their
    own on the guest machine, so they leave the most behind.

    # The two machines

    The host is Linux, on the droplet, reached over Tailscale and never over the
    internet. The guest is THIS Windows machine, running the portable, which is
    the shape most people use. The registry is the same one both talk to.

    # What it drives, and what it reads

    Both ends are driven through the terminal client and read through `--json`,
    never by looking at a screen or at a state file. The wire is a contract with
    locks on it; what a screen prints changes when somebody improves it.

    The one exception is the EXISTENCE of last-room.json and hosted-room.json,
    which is the point of scenarios 6, 12 and 13: the files are sealed and cannot
    be read from here, and whether they are there is exactly what is measured.

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

.PARAMETER LogFile
    A copy of everything printed, one line at a time. The run takes over the
    machine for a quarter of an hour and the console it opens is elevated, so
    whoever wants to follow it from somewhere else needs the lines as they
    happen. `Start-Transcript` does not give that: it holds what it writes and
    flushes it when the run ends, which on 2026-08-24 showed a scenario header
    and nothing else while the scenario was already three checks in.
#>
[CmdletBinding()]
param(
    [switch]$Deploy,
    [switch]$Restore,
    [int[]]$Only,
    [string]$Droplet = 'accentio-droplet',
    [string]$Seed = 'kanpachi.accentio.dev',
    [string]$Build = 'dist/measure',
    [string]$PortableRoot = 'C:\kt\measure',
    [string]$LogFile
)

$ErrorActionPreference = 'Stop'

# Say sale a la consola y, si se pidió, al archivo, con la hora delante.
#
# Se abre y se cierra el archivo en cada línea a propósito: lo que se quiere de
# este log es la última línea mientras la corrida sigue viva, y un descriptor
# abierto con buffer es exactamente lo que no la da.
function Say([string]$linea, [string]$color) {
    Write-Host $linea -ForegroundColor $color
    if ($LogFile) {
        try {
            Add-Content -Path $LogFile -Value ("{0} {1}" -f (Get-Date -Format 'HH:mm:ss'), $linea) `
                -Encoding UTF8
        }
        catch {
            # Un log que puede tumbar la medición es peor que no tener log.
        }
    }
}

function Step($t) { Say '' 'Cyan'; Say "=== $t ===" 'Cyan' }
function Ok($t) { Say "  OK   $t" 'Green' }
function Fail($t) { Say "  FAIL $t" 'Red' }
function Info($t) { Say "       $t" 'DarkGray' }
function Note($t) { Say "  ..   $t" 'Yellow' }

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

# Host-Count counts occurrences of a daemon-log line on the host. The patterns
# are Spanish because the daemon's log is Spanish; deltas of this are how a
# scenario proves which path ran (a reattach leaves no issuance line, a member
# coming back leaves "devuelta" instead of "emitida").
#
# The daemon does NOT log to the journal: its log is a root-owned file under
# /var/lib/kanpachi/logs. `tail` runs under the one sudoers line written for it
# (exact arguments, read-only, one file); grep runs unprivileged. Measured
# 2026-08-16: the journal only ever holds systemd's own Start/Stop lines, so a
# journalctl-based count returns 0 forever and every delta check passes vacuously.
function Host-Count([string]$pattern) {
    $out = Host-Try "sudo -n tail -n +1 /var/lib/kanpachi/logs/kanpachi.log | grep -cF -- '$pattern'"
    $n = 0
    if ([int]::TryParse($out.Trim(), [ref]$n)) { return $n }
    return 0
}

function Peer-Json([string]$verb) {
    $raw = Native { & $cli --pipe $peerPipe --data $peerData --json $verb.Split(' ') }
    if (-not $raw.Trim()) { Info "peer $verb -> (empty, exit $script:lastCode)"; return $null }
    try { return $raw | ConvertFrom-Json } catch { Info "peer $verb -> $($raw.Trim())"; return $null }
}

# Peer-Join is Peer-Json for the one verb whose answer must never be thrown
# away: a join that the CLI refuses client-side answers VALID json that no
# check ever looks at, and the scenario then waits 120s for a room the daemon
# was never told about. Found 2026-08-16: the manual join worked while the
# harness join silently did not.
#
# # Why `--quarantine off` travels with every host and join
#
# Because `--yes` does NOT answer that question, on purpose: it already means
# "trust the registry" and "open the foreign firewall", and the quarantine is a
# third decision with its own flag (`daemon/cmd/kanpachi/firewall.go`). With no
# saved answer the daemon asks, and the CLI, seeing a real terminal in the
# elevated console, PRINTS A MENU AND WAITS FOR A KEY. Nothing times out: the
# run sat on it for twelve minutes on 2026-08-24 while the log showed a scenario
# header and no checks.
#
# It went unnoticed for as long as it did because the guest's data directory
# carried a `quarantine-decision.json` from an earlier run, so scenarios 1 to 11
# never met the question. A harness that only works on a machine it has already
# run on is a harness that lies about being reproducible.
#
# `off` and not `on`: this runs on somebody's daily machine, the quarantine
# reaches THE WHOLE MACHINE and not just the adapter, and none of the fourteen
# scenarios measures it. Leaving the firewall as it was found is the answer that
# does not change what is being measured.
function Peer-Join([string]$code) {
    # Se anuncia antes porque `join` BLOQUEA hasta que la sala está: sin esta
    # línea, los minutos que tarda son minutos sin nada escrito, y no se
    # distinguen de un cuelgue.
    Note "the guest is joining $code"
    $j = Peer-Json "join $code --yes --quarantine off"
    if ($null -eq $j) { return }
    if ($j.conn -ne 'connected') {
        Info "join answered ($($script:lastCode)): $($j | ConvertTo-Json -Compress -Depth 4)"
    }
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

# Registry-Status pregunta por un código al registro y devuelve SOLO el número.
#
# Devuelve el código y no un sí o un no a propósito. `GET /api/i/{id}` contesta
# 200 mientras la sala está y 404 en cuanto se cierra, y un `catch` que traduzca
# cualquier fallo a "no está" convierte un DNS caído o un TLS que no negoció en
# un verde: el escenario diría que la sala se cerró sin haber hablado con nadie.
# Comparando contra 404, un cero delata la avería.
function Registry-Status([string]$id) {
    # PowerShell 5.1 arranca sin TLS 1.2 y el registro no sirve nada por debajo.
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
    try {
        $r = Invoke-WebRequest -Uri "https://$Seed/api/i/$id" -UseBasicParsing -TimeoutSec 20
        return [int]$r.StatusCode
    }
    catch [Net.WebException] {
        $resp = $_.Exception.Response
        if ($null -eq $resp) { Info "the registry did not answer: $($_.Exception.Message)"; return 0 }
        return [int]$resp.StatusCode
    }
    catch {
        Info "asking the registry blew up: $_"
        return 0
    }
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
        # Este es el UNICO sitio del script que necesita un sudo de verdad, y no
        # se puede evitar: instalar el paquete entero pone unidades de systemd y
        # el catalogo, y eso no cabe en los cuatro `install -m755` que el
        # sudoers enumera. Pasa una sola vez, en un droplet donde Kanpachi nunca
        # se instalo. Lo demas, deploy y restore incluidos, va por esos cuatro.
        $out = Host-Try 'sudo -n dpkg -i /tmp/kanpachi-released.deb'
        if ($script:lastCode -ne 0) {
            throw ("installing the published package needs a real sudo, and `sudo -n` was refused.`n" +
                "  Do it once by hand and run this again:`n" +
                "    ssh $Droplet 'sudo dpkg -i /tmp/kanpachi-released.deb'`n" +
                $out.Trim())
        }
        Ok 'published package installed'
    }
    else { Ok 'published package already installed' }

    # One unit per command: the sudoers lines match exact arguments, so the
    # two-unit form gets refused and Host-Try swallows it — measured 2026-08-17
    # as a daemon still running a build from three deploys earlier.
    Host-Try 'sudo -n systemctl stop kanpachid' | Out-Null
    Host-Try 'sudo -n systemctl stop kanpseed-registry' | Out-Null
    Native { & scp -q (Join-Path $buildDir 'linux/kanpachid') "${Droplet}:/tmp/kanpachid" } | Out-Null
    Native { & scp -q (Join-Path $buildDir 'linux/kanpachi') "${Droplet}:/tmp/kanpachi" } | Out-Null
    Native { & scp -q (Join-Path $buildDir 'kanpseed-linux-amd64') "${Droplet}:/tmp/kanpseed" } | Out-Null
    Host-Run ('sudo -n install -m755 /tmp/kanpachid /usr/libexec/kanpachi/kanpachid' +
        ' && sudo -n install -m755 /tmp/kanpachi /usr/bin/kanpachi' +
        ' && sudo -n install -m755 /tmp/kanpseed /usr/local/bin/kanpseed') | Out-Null

    # The engine rides along when the cycle rebuilt it. The published .deb's
    # engine is the default, and this cycle IS about the engine — the fork's
    # backports and the owner election — so a fresh linux/kanpachi-engine in
    # the build dir replaces the packaged one. -Restore undoes it with the
    # .deb, same as the other two.
    $engineBin = Join-Path $buildDir 'linux/kanpachi-engine'
    if (Test-Path $engineBin) {
        Native { & scp -q $engineBin "${Droplet}:/tmp/kanpachi-engine" } | Out-Null
        Host-Run 'sudo -n install -m755 /tmp/kanpachi-engine /usr/libexec/kanpachi/kanpachi-engine' | Out-Null
        Ok 'engine replaced with the rebuilt one'
    }
    else { Note 'no rebuilt engine in the build dir; the packaged one stays' }

    # The daemon comes back too. It was missing, and what that buys is a deploy
    # that replaces the file and keeps the OLD process serving: every check
    # after it measures a build that is not the one on disk.
    Host-Run 'sudo -n systemctl start kanpachid' | Out-Null
    Host-Run 'sudo -n systemctl start kanpseed-registry' | Out-Null
    Ok 'kanpachid, kanpachi and kanpseed replaced, both services back up'
    Info (Host-Run 'sudo -n systemctl is-active kanpachid; sudo -n systemctl is-active kanpseed-registry; /usr/bin/kanpachi version 2>/dev/null || true').Trim()

    # El host tiene que saber a QUIEN pedirle un codigo, y una instalacion recien
    # hecha no lo sabe: desde que no hay seed compilado por defecto, `host` sin
    # registro configurado se niega antes de tocar la red. Es lo correcto —abrir
    # una sala es elegir en la maquina de quien vive—, y aca esa eleccion se hace
    # una vez, al desplegar, y no en mitad de una medicion.
    Host-Run 'sudo -n systemctl start kanpachid' | Out-Null
    Start-Sleep -Seconds 8
    Host-Run "sudo -n kanpachi seed $Seed" | Out-Null
    Ok "host registry set to $Seed"
    return
}

if ($Restore) {
    Step 'the droplet goes back to what it was serving'
    $deb = Join-Path $buildDir 'released.deb'
    if (-not (Test-Path $deb)) {
        Native { & gh release download --pattern 'kanpachi-amd64.deb' --output $deb --clobber } | Out-Null
    }
    Native { & gh release download --pattern 'kanpseed-linux-amd64' --output (Join-Path $buildDir 'kanpseed-released') --clobber } | Out-Null
    # One unit per command: the sudoers lines match exact arguments, so the
    # two-unit form gets refused and Host-Try swallows it — measured 2026-08-17
    # as a daemon still running a build from three deploys earlier.
    Host-Try 'sudo -n systemctl stop kanpachid' | Out-Null
    Host-Try 'sudo -n systemctl stop kanpseed-registry' | Out-Null
    Native { & scp -q $deb "${Droplet}:/tmp/kanpachi-released.deb" } | Out-Null
    Native { & scp -q (Join-Path $buildDir 'kanpseed-released') "${Droplet}:/tmp/kanpseed" } | Out-Null

    # Se desempaqueta sin privilegio y se instalan los binarios uno a uno.
    #
    # `sudo -n dpkg -i` no existe para este usuario: el sudoers del droplet da
    # cuatro `install -m755` con la ruta de origen Y la de destino escritas
    # enteras, y nada más. Un `dpkg` pide contraseña, `sudo -n` se rinde, y esta
    # rama fallaba entera. Descubierto el 2026-08-24, restaurando a mano.
    #
    # Desempaquetar en `/tmp` y copiar a los nombres exactos que el sudoers
    # nombra es lo que queda, y es más honesto que ampliar el permiso: cada
    # cosa que este script puede escribir como root sigue estando enumerada.
    Host-Run ('rm -rf /tmp/rel && mkdir -p /tmp/rel' +
        ' && dpkg-deb -x /tmp/kanpachi-released.deb /tmp/rel' +
        ' && cp /tmp/rel/usr/libexec/kanpachi/kanpachid /tmp/kanpachid' +
        ' && cp /tmp/rel/usr/bin/kanpachi /tmp/kanpachi' +
        ' && cp /tmp/rel/usr/libexec/kanpachi/kanpachi-engine /tmp/kanpachi-engine') | Out-Null
    Host-Run 'sudo -n install -m755 /tmp/kanpachid /usr/libexec/kanpachi/kanpachid' | Out-Null
    Host-Run 'sudo -n install -m755 /tmp/kanpachi /usr/bin/kanpachi' | Out-Null
    Host-Run 'sudo -n install -m755 /tmp/kanpachi-engine /usr/libexec/kanpachi/kanpachi-engine' | Out-Null
    Host-Run 'sudo -n install -m755 /tmp/kanpseed /usr/local/bin/kanpseed' | Out-Null
    Host-Run 'sudo -n systemctl start kanpseed-registry' | Out-Null
    Host-Run 'sudo -n systemctl start kanpachid' | Out-Null
    Ok 'released kanpseed, daemon, client and engine back in place'
    Info ('kanpseed-registry: ' + (Host-Run 'sudo -n systemctl is-active kanpseed-registry').Trim())
    Info ('kanpachid: ' + (Host-Try 'sudo -n systemctl is-active kanpachid').Trim())
    Info (Host-Run 'kanpachi version | head -2').Trim()
    return
}

# ─── Los escenarios ──────────────────────────────────────────────────────────

$script:failures = @()
$script:code = ''

# Check sin tipar la condición, a propósito.
#
# Con `[bool]$cond` un `$null` no da rojo: da una excepción de conversión, la
# atrapa el `try` del bucle, y el escenario se corta en esa línea sin correr el
# resto. Y `$null` es justo lo que sale de `(Peer-Json 'last').found` cuando el
# daemon no contesta, o sea el caso en el que más se quiere ver el resto de las
# comprobaciones. Sin tipo, la regla de PowerShell hace lo correcto: `$null`,
# cadena vacía, cero y lista vacía son falso.
function Check($what, $cond) {
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
            # `--yes` no es comodidad, es la unica forma de que esto corra.
            #
            # Abrir o entrar a una sala pide confirmar el registro, y sin
            # terminal el CLI se NIEGA con `refused` en vez de resolver la
            # ausencia como un si. Del lado del host eso es exacto: ssh sin `-t`
            # no da tty. Del lado del invitado el peligro es el contrario, que si
            # haya terminal y el comando se quede colgado esperando una respuesta
            # que ningun script va a dar.
            $h = Host-Json 'host Medicion --yes --quarantine off'
            $script:code = $h.code + '@' + $h.seed
            Check 'the host opened a room' ($null -ne $h -and $h.code)
            Info "code $script:code"

            Peer-Start
            Peer-Join $script:code
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
            $before = (Host-Json 'status').code
            Note 'restarting the host daemon, which is a reboot as far as the room is concerned'
            Host-Run 'sudo -n systemctl restart kanpachid' | Out-Null
            Check 'the host brought the room back by itself' (Wait-Until {
                    $s = Host-Json 'status'; $null -ne $s -and $s.conn -eq 'connected'
                } 180 'the host room to be back')
            $after = (Host-Json 'status').code
            Check "the code is the same one it handed out ($before)" ($null -ne $before -and $before -eq $after)
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
        id    = 8
        name  = 'an induced flap ends in a reattach: same address, no new credential'
        run   = {
            # A room with the guest inside, however this scenario was reached.
            Host-Run 'sudo -n systemctl start kanpachid' | Out-Null
            Start-Sleep -Seconds 8
            $h = Host-Json 'status'
            if ($h.conn -ne 'connected') { $h = Host-Json 'host Medicion --yes --quarantine off' }
            $script:code = $h.code + '@' + $h.seed
            if (-not (Peer-Running)) { Peer-Start }
            if (-not (Peer-InRoom)) { Peer-Join $script:code }
            Check 'the guest is in' (Wait-Until { Peer-InRoom } 120 'the guest to be in')

            $ipBefore = (Peer-Json 'status').local_ip
            $issuedBefore = Host-Count 'credencial emitida'
            Info "guest at $ipBefore, host has issued $issuedBefore so far"

            # The flap: the engine's outbound traffic dies for 45 seconds, which
            # under the short clocks is enough for the connection to be torn
            # down, the host to read as absent, and the rejoin to fire. This is
            # the measured defect's exact shape: a fresh engine instance with a
            # new random peer id and the SAME credential key, electing against
            # its own ghost in the host's route table.
            $engine = Get-Process -Name 'kanpachi-engine' -ErrorAction SilentlyContinue
            if (-not $engine) { throw 'no kanpachi-engine process to flap' }
            $rule = 'kanpachi-measure-flap'
            Note "blocking the engine's outbound traffic for 45s"
            New-NetFirewallRule -DisplayName $rule -Direction Outbound `
                -Program $engine.Path -Action Block | Out-Null
            try {
                Start-Sleep -Seconds 45
            }
            finally {
                Remove-NetFirewallRule -DisplayName $rule -ErrorAction SilentlyContinue
                Note 'traffic restored'
            }

            Check 'the guest is back in' (Wait-Until { Peer-InRoom } 180 'the guest to recover')
            $ipAfter = (Peer-Json 'status').local_ip
            Check "it kept its address ($ipBefore)" ($null -ne $ipBefore -and $ipAfter -eq $ipBefore)
            $issuedAfter = Host-Count 'credencial emitida'
            Check 'and the host issued NO new credential for the recovery' ($issuedAfter -eq $issuedBefore)
        }
    }

    @{
        id    = 9
        name  = 'a dirty restart comes back as itself: same address, credential handed back'
        run   = {
            if (-not (Peer-InRoom)) { throw 'scenario 9 needs the guest in a room; run 8 first' }
            $ipBefore = (Peer-Json 'status').local_ip
            $returnedBefore = Host-Count 'credencial devuelta al que vuelve'

            Note 'killing the guest, which is the dirty close, and starting it again'
            Peer-Kill
            Peer-Start
            Check 'it went back on its own' (Wait-Until { Peer-InRoom } 180 'the guest to come back')
            $ipAfter = (Peer-Json 'status').local_ip
            Check "with the SAME address ($ipBefore)" ($null -ne $ipBefore -and $ipAfter -eq $ipBefore)

            $returnedAfter = Host-Count 'credencial devuelta al que vuelve'
            Check 'because the host recognized its member key and handed the credential back' ($returnedAfter -gt $returnedBefore)
        }
    }

    @{
        id    = 10
        name  = 'the control rule stays scoped to who is actually there'
        run   = {
            if (-not (Peer-InRoom)) { throw 'scenario 10 needs the guest in a room; run 8 and 9 first' }
            # After the laps of 8 and 9 the old code accumulated one address per
            # re-entry, 73 measured. There are TWO control rules: the gate,
            # which opens the lobby /24 and is structural, and the room rule,
            # whose members list is exactly where the 73 piled up. That list
            # must now hold this one guest and nobody else.
            $guestIp = (Peer-Json 'status').local_ip
            $exp = Host-Json 'exposure'
            # The outer @() is load-bearing: a one-member pipeline answers a
            # bare string, and [0] on a string is its first CHARACTER.
            $members = @($exp.ports | Where-Object { $_.control } | ForEach-Object { $_.members } |
                Where-Object { $_ } | ForEach-Object { ($_ -split '/')[0] } | Select-Object -Unique)
            Info "control members: [$($members -join ', ')], guest at $guestIp"
            Check "the room control rule holds exactly this guest (saw $($members.Count))" (
                $members.Count -eq 1 -and $members[0] -eq $guestIp)
        }
    }

    @{
        id    = 11
        name  = 'a kicked member returns as a stranger: new address, nothing handed back'
        run   = {
            if (-not (Peer-InRoom)) { throw 'scenario 11 needs the guest in a room; run 8 first' }
            # Each scenario runs in its own process, so $script:code from 8 is
            # not here: an empty code made the return join die with `usage`.
            if (-not $script:code) {
                $h = Host-Json 'status'
                $script:code = $h.code + '@' + $h.seed
            }
            $ipBefore = (Peer-Json 'status').local_ip
            $returnedBefore = Host-Count 'credencial devuelta al que vuelve'

            Note "the host kicks $ipBefore"
            Host-Run "sudo -n kanpachi kick $ipBefore" | Out-Null
            Check 'the guest is out' (Wait-Until { -not (Peer-InRoom) } 120 'the guest to be out')

            Note 'the kicked one comes back on purpose, with the same code'
            Peer-Join $script:code
            Check 'and it gets in, because kicking is not banning' (Wait-Until { Peer-InRoom } 120 'the kicked guest to re-enter')

            $ipAfter = (Peer-Json 'status').local_ip
            Check "as a NEW member: another address (was $ipBefore, got $ipAfter)" ($ipAfter -ne $ipBefore)
            $returnedAfter = Host-Count 'credencial devuelta al que vuelve'
            Check 'and the revoked credential was NOT handed back' ($returnedAfter -eq $returnedBefore)
        }
    }

    @{
        id    = 7
        name  = 'a kicked guest does NOT come back on its own'
        run   = {
            Host-Run 'sudo -n systemctl start kanpachid' | Out-Null
            Start-Sleep -Seconds 8
            $h = Host-Json 'status'
            if ($h.conn -ne 'connected') { $h = Host-Json 'host Medicion --yes --quarantine off' }
            $script:code = $h.code + '@' + $h.seed
            Peer-Join $script:code
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

    @{
        id    = 12
        name  = 'pressing leave while going back switches it off'
        run   = {
            Host-Run 'sudo -n systemctl start kanpachid' | Out-Null
            Start-Sleep -Seconds 8
            $h = Host-Json 'status'
            if ($h.conn -ne 'connected') { $h = Host-Json 'host Medicion --yes --quarantine off' }
            $script:code = $h.code + '@' + $h.seed
            Peer-Join $script:code
            Check 'the guest is in' (Wait-Until { Peer-InRoom } 120 'the guest to be in')

            # The host goes away WITHOUT closing the room, so the guest starts
            # going back on its own. That is the state the button acts on.
            Host-Run 'sudo -n systemctl stop kanpachid' | Out-Null
            Check 'the guest is going back' (Wait-Until {
                    $null -ne (Peer-Returning) } 180 'the guest to start going back')

            Peer-Json 'leave' | Out-Null
            Check 'it stopped going back' (Wait-Until {
                    $null -eq (Peer-Returning) } 30 'the return to stop')

            $last = Peer-Json 'last'
            Check 'the room is still saved, so going back by hand is on offer' ($last.found)
            Check 'and it will not go back on its own any more' (-not $last.room.auto_return)
            Check 'and the file is still on disk' (Test-Path (Join-Path $peerData 'last-room.json'))

            # It has to survive a restart. Switching it off only in memory would
            # come back armed on the next start, which is not what leaving means.
            Peer-Kill
            Peer-Start
            Start-Sleep -Seconds 20
            Check 'and after a close and open it is still not going back' (
                $null -eq (Peer-Returning))
        }
    }

    @{
        id    = 13
        name  = 'a machine holding BOTH state files reopens its own room'
        run   = {
            # The race this exists for: `hosted-room.json` says there is a room
            # of this machine to reopen, `last-room.json` says it was going back
            # to somebody else's. Both fire at startup, and before the fix
            # whoever took the lock first won: either the room did not come back
            # at all, or it came back with the return left armed and asleep.
            Host-Run 'sudo -n systemctl start kanpachid' | Out-Null
            Start-Sleep -Seconds 8
            $h = Host-Json 'status'
            if ($h.conn -ne 'connected') { $h = Host-Json 'host Medicion --yes --quarantine off' }
            Peer-Join ($h.code + '@' + $h.seed)
            Check 'the guest is in somebody elses room' (
                Wait-Until { Peer-InRoom } 120 'the guest to be in')

            # Killed dirty, so `last-room.json` keeps auto_return on: only
            # leaving on purpose and being kicked switch it off.
            Peer-Kill
            $last = Join-Path $peerData 'last-room.json'
            Peer-Start
            Check 'the return is armed' ((Peer-Json 'last').room.auto_return)

            # Now this machine also has a room of its own, saved. Opening it
            # goes through the gate and stops the return, so the file is put
            # back by hand with the daemon down, which is the state a laptop
            # reaches when a reopen failed and it later joined somebody else.
            $copiaLast = Get-Content -Raw -Encoding Byte $last
            Peer-Json 'host Propia --yes --quarantine off' | Out-Null
            Check 'the guest hosts its own room now' (
                Wait-Until { (Peer-Json 'status').role -eq 'host' } 150 'the room to open')
            Peer-Kill
            Set-Content -Path $last -Value $copiaLast -Encoding Byte
            Check 'both state files are on disk' (
                (Test-Path $last) -and (Test-Path (Join-Path $peerData 'hosted-room.json')))

            Peer-Start
            Start-Sleep -Seconds 60
            Check 'the machines OWN room came back' (
                (Peer-Json 'status').role -eq 'host')
            Check 'and the return was switched off instead of left asleep' (
                -not (Peer-Json 'last').room.auto_return)

            Peer-Json 'leave' | Out-Null
        }
    }

    @{
        id    = 14
        name  = 'displacing writes the two state files, each in its own way'
        run   = {
            # What the confirmation promises has to be true on disk, and the two
            # kinds promise different things: closing your own room ENDS it,
            # leaving somebody else's keeps the way back and only switches the
            # automatic part off.
            $hosted = Join-Path $peerData 'hosted-room.json'
            $last = Join-Path $peerData 'last-room.json'

            Host-Run 'sudo -n systemctl start kanpachid' | Out-Null
            Start-Sleep -Seconds 8
            $h = Host-Json 'status'
            if ($h.conn -ne 'connected') { $h = Host-Json 'host Medicion --yes --quarantine off' }
            $ajena = $h.code + '@' + $h.seed

            # The guest opens a room of its OWN first, so there is something to
            # close, and its code is asked to the registry before and after.
            $propia = Peer-Json 'host Propia --yes --quarantine off'
            Check 'the guest hosts a room of its own' ($null -ne $propia.code)
            Check 'and its file is on disk' (Test-Path $hosted)
            $suId = $propia.code
            # Se pregunta ANTES, y ese 200 es lo que le da valor al 404 de
            # después: sin él, un registro inalcanzable daría el mismo resultado
            # que una sala cerrada.
            Check 'and the registry resolves its code while it is open' (
                (Registry-Status $suId) -eq 200)

            # close_room: entering another room ends this one.
            Peer-Join $ajena
            Check 'the guest got into the other room' (
                Wait-Until { Peer-InRoom } 150 'the guest to be in')
            Check 'its own room file is GONE' (-not (Test-Path $hosted))
            # Al registro directamente, porque la terminal no tiene un verbo que
            # resuelva un código sin entrar. Retirada la entrada, `GET /api/i/{id}`
            # contesta 404; ver registry/API.md.
            Check 'and its code no longer resolves, so there is nothing to reopen' (
                (Registry-Status $suId) -eq 404)

            # leave_room: leaving keeps the way back and switches the automatic
            # part off. Same shape as the kick in scenario 7, by the button.
            Peer-Json 'leave' | Out-Null
            Check 'the guest is out' (Wait-Until { -not (Peer-InRoom) } 120 'the guest to be out')
            Check 'the last room is still on disk' (Test-Path $last)
            $l = Peer-Json 'last'
            Check 'so going back by hand is still on offer' ($l.found)
            Check 'but not on its own, because leaving was deliberate' (
                -not $l.room.auto_return)
        }
    }
)

Require-Admin

Step 'the pieces'
if (-not (Test-Path $cli)) { throw "no kanpachi.exe in $Build. Run scripts/build-measure-clocks.ps1 first." }
New-Item -ItemType Directory -Force $PortableRoot | Out-Null

# El portable se repone cuando NO es el recién construido, y no solo cuando
# falta.
#
# `C:\kt\measure` sobrevive entre corridas, así que copiar solo si el archivo no
# está deja la copia de la vez pasada midiendo el código de la vez pasada, y
# enseñándolo como si fuera el de ahora. Pasó el 2026-08-23: 13 y 14 corrieron
# contra un binario de una semana antes, o sea contra el fallo que arreglaban.
$fuente = Join-Path $buildDir 'kanpachi-portable.exe'
if (-not (Test-Path $fuente)) {
    throw "no kanpachi-portable.exe in $Build. Run scripts/build-measure-clocks.ps1 first."
}
if ((-not (Test-Path $peerExe)) -or
    (Get-FileHash $fuente).Hash -ne (Get-FileHash $peerExe).Hash) {
    Peer-Kill
    Copy-Item $fuente $peerExe -Force
    Info 'the guest binary is not the one just built, so it was replaced'
}
Ok "guest in $PortableRoot"

# El daemon del invitado tiene que estar en pie antes del primer escenario.
#
# Solo lo levantan los que reinician a propósito, y el primero de la lista es
# uno de ellos. Con `-Only 12` sobre una máquina apagada, cada verbo contesta
# `no_daemon`, cada espera agota su plazo, y el escenario sale rojo por algo que
# no es lo que estaba midiendo.
if (-not (Peer-Running)) {
    Note 'the guest daemon was down; starting it before the first scenario'
    Peer-Start
}
Ok 'the guest daemon answers'

# Hospedar pide dos cosas que entrar no pide, y 13 y 14 son los primeros
# escenarios que le piden al INVITADO abrir una sala.
#
#  1. **Un registro elegido.** Entrar a la sala de otro no lo elige, y eso está
#     escrito en la ayuda de `seed` como decisión y no como olvido: la siguiente
#     sala que abriera esta máquina se serviría desde el servidor de un
#     desconocido sin que nadie lo hubiera decidido.
#  2. **La credencial de ese registro, si está cerrado.** El de producción lo
#     está, y sin ella `host` contesta `seed_password`.
#
# Las dos se comprueban acá y no dentro de un escenario. Un `host` que falla a
# mitad del 13 deja cuatro checks en rojo que no tienen nada que ver con lo que
# el 13 mide, y el motivo real no aparece por ningún lado: el escenario tira la
# respuesta del `host` y lo único que queda es el plazo agotado de la espera de
# después. Medido el 2026-08-24, así, con siete rojos y ninguna pista.
#
# Solo se exige cuando va a correr un escenario que hospeda desde el invitado.
# El 12 no lo necesita, y corrió entero en verde sin credencial ninguna.
$vanAHospedar = (-not $Only) -or ($Only -contains 13) -or ($Only -contains 14)

if ($vanAHospedar -and (Peer-Json 'seed').seed -ne $Seed) {
    Note "the guest had no registry of its own; pointing it at $Seed"
    Peer-Json "seed $Seed" | Out-Null
}
if ($vanAHospedar) {
    if ((Peer-Json 'seed').seed -ne $Seed) {
        throw "the guest would not take $Seed as its registry, so it cannot open a room of its own."
    }
    Ok "the guest opens its rooms on $Seed"
}

$seedToken = Join-Path $peerData 'seed-token.json'
if ($vanAHospedar -and -not (Test-Path $seedToken)) {
    throw ("the guest has no credential for $Seed, so scenarios 13 and 14 cannot open a room there.`n" +
        "  The password is never an argument and there is no flag for it: see ``kanpachi help password``.`n" +
        "  Answer it once, in this elevated console, and it stays in $seedToken :`n" +
        "    & '$cli' --pipe '$peerPipe' --data '$peerData' password`n" +
        "  Or from a file, which is the door written for a script:`n" +
        "    Get-Content <file> | & '$cli' --pipe '$peerPipe' --data '$peerData' password")
}
if ($vanAHospedar) { Ok "the guest has a credential for $Seed, so it can host" }
Ok "host on $Droplet, registry at $Seed"

# Host-Count must be able to read the daemon's log, or every delta check in 8,
# 9 and 11 compares 0 against 0 and lies green. Better to refuse to start.
$null = Host-Try 'sudo -n tail -n +1 /var/lib/kanpachi/logs/kanpachi.log | tail -1'
if ($script:lastCode -ne 0) {
    throw ("the host log cannot be read: `sudo -n tail` was refused. " +
        'The sudoers line for reading /var/lib/kanpachi/logs/kanpachi.log is missing.')
}
Ok 'the host log is readable, so the counts count'

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
