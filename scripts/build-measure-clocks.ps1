<#
.SYNOPSIS
    Builds the three binaries of the return measurement, with the clocks cut
    short, and leaves the source tree exactly as it found it.

.DESCRIPTION
    Going back to a room is governed by clocks measured in minutes, hours and
    weeks. Watching a guest give up on a room that expired means waiting three
    WEEKS at the real values, so what gets measured has to be built with short
    ones.

    The deadlines are compile-time constants on purpose, and that is not
    negotiable: `internal/arch/corte_test.go` fails if any of them stops being
    `const` or grows a setter, because a deadline an external agent can widen is
    a deadline that does not protect anybody. So there is no switch to flip.
    What there is, is this: the values are edited, the REAL build scripts run,
    and the edit is undone.

    # Why a script and not by hand

    Because by hand it gets left behind. The tree has to be clean before it
    starts and it is checked clean when it ends, in a `finally` that runs even
    if a build blows up halfway. An escaped edit here would ship a Kanpachi that
    drops people out of a room after ninety seconds.

    # What it produces, in dist/measure

      kanpseed-linux-amd64   the registry, with RoomTTL in minutes
      linux/kanpachid        the Linux host's daemon
      linux/kanpachi         its terminal client
      kanpachi-portable.exe  the Windows guest

    # No .deb, and no engine

    The droplet installs the PUBLISHED package, with its engine, its systemd
    units and its catalogue exactly as they ship, and then these two binaries go
    on top of it. Building a package here would change three more things than
    the one being measured, and what is being measured is clocks, which live in
    Go. Undoing it is reinstalling the package.

    # The values, and why these

    Six of these are only correct RELATIVE to another one, and cutting them
    without keeping the ratios would measure a different product. The table
    keeps them: three announces still fit inside the host-silence window, the
    reconnect limit is still shorter than the absence one, republishing still
    happens many times over inside the room's lifetime.

.PARAMETER Out
    Where the three artifacts land. Wiped before building.

.PARAMETER SkipPortable
    Builds only the two Linux pieces. The portable takes minutes because it
    builds the whole Flutter bundle, and while iterating on the Linux side it is
    dead weight.
#>
[CmdletBinding()]
param(
    [string]$Out = "dist/measure",
    [switch]$SkipPortable
)

$ErrorActionPreference = 'Stop'

function Step($t) { Write-Host "`n=== $t ===" -ForegroundColor Cyan }
function Ok($t) { Write-Host "  OK   $t" -ForegroundColor Green }
function Info($t) { Write-Host "       $t" -ForegroundColor DarkGray }
function Fail($t) { Write-Host "  FAIL $t" -ForegroundColor Red }

# Native corre un binario externo y decide por su CODIGO DE SALIDA.
#
# El rodeo con $ErrorActionPreference es el mismo que hace verify.ps1, y hace
# falta por lo mismo: en PowerShell 5.1 lo que un exe escribe en stderr se
# convierte en ErrorRecord, y con 'Stop' eso aborta el script. `go: downloading`
# sale por stderr y no es un fallo, asi que sin esto un build limpio se muere en
# la primera dependencia que haya que bajar.
function Native($what, [scriptblock]$block) {
    $before = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    & $block
    $code = $LASTEXITCODE
    $ErrorActionPreference = $before
    if ($code -ne 0) { throw "$what failed (exit $code)" }
}

$repo = Split-Path -Parent $PSScriptRoot
Set-Location $repo

$timingFile = Join-Path $repo 'core/timing/timing.go'

# El nombre, el valor real y el de medición. El valor real se busca TAL CUAL en
# el fuente: si alguien lo cambió, esto no adivina, falla y dice cuál.
$clocks = @(
    @{ n = 'ReturnInterval';    from = 'ReturnInterval = 5 * time.Minute';       to = 'ReturnInterval = 20 * time.Second' }
    @{ n = 'ReturnJitter';      from = 'ReturnJitter = 30 * time.Second';        to = 'ReturnJitter = 3 * time.Second' }
    @{ n = 'HostAbsenceLimit';  from = 'HostAbsenceLimit = 20 * time.Minute';    to = 'HostAbsenceLimit = 90 * time.Second' }
    @{ n = 'HostSilenceLimit';  from = 'HostSilenceLimit = 6 * time.Minute';     to = 'HostSilenceLimit = 30 * time.Second' }
    @{ n = 'ReconnectLimit';    from = 'ReconnectLimit = 10 * time.Minute';      to = 'ReconnectLimit = 45 * time.Second' }
    @{ n = 'AnnounceInterval';  from = 'AnnounceInterval = 2 * time.Minute';     to = 'AnnounceInterval = 10 * time.Second' }
    @{ n = 'RejoinAfter';       from = 'RejoinAfter = 2 * time.Minute';          to = 'RejoinAfter = 15 * time.Second' }
    @{ n = 'RejoinInterval';    from = 'RejoinInterval = 30 * time.Second';      to = 'RejoinInterval = 10 * time.Second' }
    @{ n = 'RejoinJitter';      from = 'RejoinJitter   = 30 * time.Second';      to = 'RejoinJitter   = 5 * time.Second' }
    @{ n = 'RoomTTL';           from = 'RoomTTL = 21 * 24 * time.Hour';          to = 'RoomTTL = 4 * time.Minute' }
    @{ n = 'RepublishInterval'; from = 'RepublishInterval = time.Hour';          to = 'RepublishInterval = 30 * time.Second' }
    @{ n = 'RegistrySweep';     from = 'RegistrySweep = 10 * time.Minute';       to = 'RegistrySweep = 20 * time.Second' }
    @{ n = 'CredentialTTL';     from = 'CredentialTTL = 24 * time.Hour';         to = 'CredentialTTL = 10 * time.Minute' }
    @{ n = 'RenewInterval';     from = 'RenewInterval = time.Hour';              to = 'RenewInterval = 2 * time.Minute' }
    @{ n = 'ArrivalGrace';      from = 'ArrivalGrace = 10 * time.Minute';        to = 'ArrivalGrace = 60 * time.Second' }
)

Step 'the tree has to be clean'
$dirty = & git status --porcelain
if ($dirty) {
    Fail 'there are uncommitted changes'
    Info 'This script edits core/timing/timing.go and puts it back. With other'
    Info 'changes in flight there is no way to tell its edit from yours if it'
    Info 'dies halfway, so it refuses to start.'
    $dirty | ForEach-Object { Info $_ }
    exit 1
}
Ok 'clean'

# Se lee y se escribe con .NET y no con Get-Content/Set-Content, y no es
# preferencia de estilo: en PowerShell 5.1 `-Encoding UTF8` escribe BOM. Un BOM
# nuevo al restaurar deja el fichero distinto del que estaba, o sea el Ã¡rbol
# sucio con un cambio que nadie hizo, que es exactamente lo que este script
# existe para que no pase.
$utf8NoBom = New-Object System.Text.UTF8Encoding $false
$original = [System.IO.File]::ReadAllText($timingFile)
$restored = $false

function Restore-Timing {
    if ($script:restored) { return }
    [System.IO.File]::WriteAllText($script:timingFile, $script:original, $script:utf8NoBom)
    $script:restored = $true
    $left = & git status --porcelain
    if ($left) {
        Fail 'the tree was left dirty, which must not happen'
        $left | ForEach-Object { Info $_ }
        exit 1
    }
    Ok 'core/timing/timing.go is back to its real values'
}

try {
    Step 'cutting the clocks short'
    $text = $original
    foreach ($c in $clocks) {
        $hits = ([regex]::Matches($text, [regex]::Escape($c.from))).Count
        if ($hits -ne 1) {
            throw "$($c.n): expected its real value once and found it $hits times. Looked for `"$($c.from)`". If the value changed, this table has to change with it."
        }
        $text = $text.Replace($c.from, $c.to)
        Info ("{0,-18} {1}" -f $c.n, ($c.to -replace '.*= ', ''))
    }
    [System.IO.File]::WriteAllText($timingFile, $text, $utf8NoBom)
    Ok "$($clocks.Count) clocks cut"

    Step 'it still has to compile and pass its own guards'
    Native 'go build with the short clocks' { & go build ./... }
    Native 'the architecture guards with the short clocks' { & go test ./internal/arch/ | Out-Null }
    Ok 'builds, and the deadline guard still holds'

    $outDir = Join-Path $repo $Out
    if (Test-Path $outDir) { Remove-Item -Recurse -Force $outDir }
    New-Item -ItemType Directory -Force $outDir | Out-Null

    Step 'the seed'
    Native 'build-seed.sh' { & bash scripts/build-seed.sh --version v0.0.0-measure --out $Out }
    Ok 'kanpseed built for linux'

    Step 'the Linux host: the two Go binaries and nothing else'
    # No se arma un .deb, y es a propósito.
    #
    # Un paquete nuevo traería motor, unidades de systemd y catálogo, o sea tres
    # cosas más que cambian entre lo publicado y lo que se mide. Lo que esta
    # medición estudia son RELOJES, y los relojes viven en Go. Así que el droplet
    # instala el .deb publicado —con su motor, sus unidades y su catálogo tal
    # cual— y encima se le cambian solo estos dos ficheros.
    #
    # Deshacerlo es reinstalar el paquete, que repone los dos.
    $linux = Join-Path $outDir 'linux'
    New-Item -ItemType Directory -Force $linux | Out-Null
    $env:GOOS = 'linux'
    $env:GOARCH = 'amd64'
    $env:CGO_ENABLED = '0'
    try {
        Native 'kanpachid for linux' { & go build -o (Join-Path $linux 'kanpachid') ./daemon/cmd/kanpachid }
        Native 'kanpachi for linux' { & go build -o (Join-Path $linux 'kanpachi') ./daemon/cmd/kanpachi }
    }
    finally {
        Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED -ErrorAction SilentlyContinue
    }
    Get-ChildItem $linux | ForEach-Object { Ok ("{0,-12} {1,10:N0} KB" -f $_.Name, ($_.Length / 1KB)) }

    Step 'the terminal client for Windows, which is how the guest gets read'
    # El portable no lleva CLI: es un daemon y una ventana. Para que la medición
    # pueda preguntarle el estado sin mirar una pantalla hace falta este, que
    # habla por el pipe del portable con `--pipe` y `--data`.
    Native 'kanpachi for windows' { & go build -o (Join-Path $outDir 'kanpachi.exe') ./daemon/cmd/kanpachi }
    Ok ("kanpachi.exe {0,10:N0} KB" -f ((Get-Item (Join-Path $outDir 'kanpachi.exe')).Length / 1KB))

    if ($SkipPortable) {
        Step 'the Windows guest'
        Info 'skipped with -SkipPortable'
    }
    else {
        Step 'the Windows guest'
        $bundler = Join-Path $PSScriptRoot 'build-portable-bundle.ps1'
        Native 'build-portable-bundle.ps1' { & powershell.exe -NoProfile -ExecutionPolicy Bypass -File $bundler }
        Copy-Item (Join-Path $repo 'dist/kanpachi-portable.exe') (Join-Path $outDir 'kanpachi-portable.exe') -Force
        Ok 'kanpachi-portable.exe built'
    }
}
finally {
    Step 'putting the real values back'
    Restore-Timing
}

Step 'result'
Get-ChildItem $outDir | ForEach-Object { Info ("{0,-26} {1,10:N0} KB" -f $_.Name, ($_.Length / 1KB)) }
Ok "measurement build in $Out"
Info 'Next: scripts/measure-return.ps1 -Deploy, then -Only 1 and onward.'
