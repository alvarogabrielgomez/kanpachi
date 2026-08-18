<#
.SYNOPSIS
    Builds kanpachi-portable.exe: the whole portable Kanpachi in ONE executable.

.DESCRIPTION
    What comes out of here is the file you hand a friend over chat. It carries
    the daemon, the interface with its whole Flutter bundle, the engine, the
    DLLs and the marker inside. On the other side it is a double click, Windows
    asks for administrator once, and Kanpachi opens.

    IT INSTALLS NOTHING. No service, no autostart entry, no shortcuts, no
    ProgramData. It drops everything into a temporary folder, runs the portable
    daemon and deletes the folder on close. The only thing left behind is
    kanpachi.log, beside the executable, which is what gets asked for when
    something fails.

    HOW IT IS BUILT, in three steps

      1. build-portable.ps1 -NoLaunch builds the real portable folder. It is
         the same one used by hand, so there are no two recipes that can drift
         apart.
      2. That folder is copied into internal\kanpachibundle\carga\, because
         go:embed only embeds things INSIDE its package directory: it does not
         accept ..\.
      3. go build -tags bundle. Without that tag the binary builds anyway and
         refuses to run saying why, which is on purpose: a normal go build ./...
         cannot produce an empty bundle that only fails on somebody else's
         machine.

    WHAT TO KNOW BEFORE SENDING IT

    It goes unsigned, so SmartScreen will greet it with "Unknown publisher" and
    Defender may complain. An executable that drops binaries and a driver into a
    temporary folder and asks for administrator has the exact shape of a
    dropper. Whoever receives it has to press "More info" and "Run anyway".

.PARAMETER Output
    Where the .exe ends up. Defaults to dist\kanpachi-portable.exe.

.PARAMETER Engine
    Path to the engine, to force one. Empty picks the MOST RECENT among the
    places where it ends up built. Either way it is checked against being older
    than the engine's source, and if it is, this stops.

.PARAMETER RebuildEngine
    Rebuilds the engine from its repository before packaging.

.PARAMETER KeepPayload
    Does not delete internal\kanpachibundle\carga\ at the end. Only to debug the
    bundle itself: it is 90 MB inside the code tree.

.EXAMPLE
    .\scripts\build-portable-bundle.ps1

.EXAMPLE
    .\scripts\build-portable-bundle.ps1 -Output D:\share\kanpachi.exe
#>
[CmdletBinding()]
param(
    [string]$Output,
    [string]$Engine,
    [switch]$RebuildEngine,
    [switch]$KeepPayload
)

$ErrorActionPreference = 'Stop'

function Step($t) { Write-Host ''; Write-Host "=== $t ===" -ForegroundColor Cyan }
function Ok($t) { Write-Host "  OK   $t" -ForegroundColor Green }
function Fail($t) { Write-Host "  FAIL $t" -ForegroundColor Red }
function Note($t) { Write-Host "  --   $t" -ForegroundColor DarkGray }

# The repository comes from where the script lives and never from the current
# directory: an elevated console starts in system32.
$repo = Split-Path -Parent $PSScriptRoot
if (-not $Output) { $Output = Join-Path $repo 'dist\kanpachi-portable.exe' }

$payloadDir = Join-Path $repo 'internal\kanpachibundle\carga'
# The stage is built outside the code tree and copied afterwards. Building it
# straight inside carga\ would leave a half-finished tree if something failed on
# the way, and the next build would embed it without complaining.
$stage = Join-Path ([System.IO.Path]::GetTempPath()) ("kanpachi-bundle-stage-" + [System.Guid]::NewGuid().ToString('N'))

Write-Host ''
Write-Host 'kanpachi-portable: one executable with everything inside' -ForegroundColor White

# ---------------------------------------------------------------------------
Step 'the CI gate, run here'

# The Linux job does `go vet ./internal/...`, and a new bundle is exactly the
# kind of package that breaks it by importing x/sys/windows without a tag. It
# costs seconds and saves finding out after a push.
$env:GOOS = 'linux'
try {
    Push-Location $repo
    try {
        go vet ./internal/...
        if ($LASTEXITCODE -ne 0) { throw 'the bundle package breaks the Linux CI job' }
    } finally { Pop-Location }
} finally { Remove-Item Env:GOOS }
Ok 'the Linux go vet is clean'

# ---------------------------------------------------------------------------
Step 'the engine, which comes from another repository'

# This is NOT excess zeal, and it has already bitten: the previous default
# pointed at plain C:\kt\stage, which held an engine from three days earlier,
# without the commit "turn secure mode on for the host and the lobby too". That
# is precisely the one that makes the host accept the guests it issued
# credentials to itself, so the bundle would have gone out with the product
# broken and nothing saying so.
#
# The bundle is what gets sent to another person. An old engine inside it is not
# discovered here: it is discovered on their machine, with no log on the side of
# whoever built it.
. (Join-Path $PSScriptRoot 'lib\engine.ps1')
$engineRoot = Find-KanpachiEngineRepo -KanpachiRepo $repo

if ($RebuildEngine) {
    if (-not (Test-Path (Join-Path $engineRoot 'Cargo.toml'))) {
        throw "the kanpachi-engine repo is not at $engineRoot"
    }
    $engineBuild = Join-Path $engineRoot 'scripts\build.ps1'
    if (-not (Test-Path $engineBuild)) {
        throw "$engineBuild is missing, which sets up MSVC, protoc, libclang and 7-Zip before building"
    }
    Note 'rebuilding the engine, this takes a while'
    & $engineBuild
    if ($LASTEXITCODE -ne 0) { throw 'the engine did not build' }
}

# The resolution and the staleness check live in lib\engine.ps1, shared with
# build-portable.ps1 and build-production.ps1: one list of places, one check,
# instead of the five private conventions that used to disagree.
if (-not $Engine) { $Engine = Resolve-KanpachiEngine -EngineRoot $engineRoot }

if (-not $Engine -or -not (Test-Path $Engine)) {
    Fail 'there is no engine built anywhere'
    Note 'build it with scripts\build-engine.ps1 or -RebuildEngine, or pass its path with -Engine'
    throw 'no engine, no bundle: you can talk to the daemon, you cannot open a room'
}
$engineInfo = Get-Item $Engine
Ok ("engine: {0}" -f $Engine)
Note ("built on {0}, {1} MB" -f $engineInfo.LastWriteTime, [math]::Round($engineInfo.Length / 1MB, 1))
Assert-KanpachiEngineFresh -Engine $Engine -EngineRoot $engineRoot

# ---------------------------------------------------------------------------
Step 'building the portable folder'

# The SAME recipe used by hand. -NoLaunch because here it is only being
# packaged, and -Clean so the identity key and the last room of an earlier run
# do not slip in: that would travel inside the .exe all the way to somebody
# else's machine.
& (Join-Path $PSScriptRoot 'build-portable.ps1') -Output $stage -Engine $Engine -NoLaunch -Clean
if ($LASTEXITCODE -ne 0) { throw 'the portable folder could not be built' }

# ---------------------------------------------------------------------------
Step 'checking that the folder is complete'

# By NAME, one by one. A bundle missing Packet.dll starts anyway and dies later
# inside the engine with a 0xC0000135 that names no file at all; without
# flutter_windows.dll the interface closes without saying anything.
$essentials = @(
    'kanpachid.exe',
    'kanpachiui.exe',
    'kanpachi-engine.exe',
    'flutter_windows.dll',
    'wintun.dll',
    'Packet.dll',
    'builtin.json',
    'kanpachi.portable',
    'data\app.so',
    'data\icudtl.dat'
)
foreach ($f in $essentials) {
    if (-not (Test-Path (Join-Path $stage $f))) {
        Fail "missing from the portable folder: $f"
        throw "the portable folder is incomplete, a broken bundle does not get built"
    }
}
Ok "$($essentials.Count) essential pieces, all present"

# That the engine left in the folder is THE ONE CHOSEN above. Checking the
# choice and packaging something else is the failure this whole section exists
# to prevent, and two different scripts decide and copy.
#
# By hash and not by size: two builds of the same engine weigh almost the same,
# so size would say yes in exactly the case that has to be caught.
$chosenHash = (Get-FileHash $Engine -Algorithm SHA256).Hash
$stageHash = (Get-FileHash (Join-Path $stage 'kanpachi-engine.exe') -Algorithm SHA256).Hash
if ($chosenHash -ne $stageHash) {
    Fail 'the engine in the folder is not the one that was checked'
    Note "  chosen:    $chosenHash"
    Note "  packaged:  $stageHash"
    throw 'an engine different from the checked one got packaged'
}
Ok ("the packaged engine is the checked one (SHA256 {0}...)" -f $chosenHash.Substring(0, 12))

# The data directory does NOT travel. The daemon creates it at the destination,
# and putting it in here would send THIS machine's identity key inside the
# executable.
$stageData = Join-Path $stage 'kanpachi-data'
if (Test-Path $stageData) {
    Remove-Item $stageData -Recurse -Force
    Note 'kanpachi-data\ removed: the data is created on the destination machine'
}

# ---------------------------------------------------------------------------
Step 'copying the payload into the package'

if (-not (Test-Path $payloadDir)) { New-Item -ItemType Directory -Path $payloadDir | Out-Null }
Get-ChildItem $payloadDir -Exclude '.gitignore' -Force -ErrorAction SilentlyContinue |
    Remove-Item -Recurse -Force
Copy-Item -Path (Join-Path $stage '*') -Destination $payloadDir -Recurse -Force

$payloadMb = [math]::Round(((Get-ChildItem $payloadDir -Recurse -File | Measure-Object Length -Sum).Sum / 1MB), 1)
Ok "payload ready ($payloadMb MB)"

# ---------------------------------------------------------------------------
Step 'building the bundle'

$outputDir = Split-Path -Parent $Output
if ($outputDir -and -not (Test-Path $outputDir)) { New-Item -ItemType Directory -Path $outputDir | Out-Null }

Push-Location $repo
try {
    # -H windowsgui: NO console. With one there was a black window open for the
    # whole gaming session and a SECOND taskbar icon, which makes it look like
    # Kanpachi opened twice. What is lost are the progress messages; what is
    # left for a failure is a dialog box, which is more visible than a console
    # nobody is looking at.
    go build -tags bundle -ldflags '-H=windowsgui' -o $Output ./internal/kanpachibundle
    if ($LASTEXITCODE -ne 0) { throw 'the bundle did not build' }
} finally {
    Pop-Location
    if (-not $KeepPayload) {
        # Always deleted: it is 90 MB inside the code tree, and leaving it there
        # makes the next `go build ./...` slow and startles a `git status`.
        Get-ChildItem $payloadDir -Exclude '.gitignore' -Force -ErrorAction SilentlyContinue |
            Remove-Item -Recurse -Force
    }
    if (Test-Path $stage) { Remove-Item $stage -Recurse -Force }
}

$mb = [math]::Round((Get-Item $Output).Length / 1MB, 1)
Ok "kanpachi-portable.exe ready ($mb MB)"

Write-Host ''
Write-Host "  This is what gets sent over chat:" -ForegroundColor Cyan
Write-Host "  $Output" -ForegroundColor Cyan
Write-Host ''
Note 'On the other side: double click, accept the UAC once, and that is it.'
Note 'It goes unsigned: SmartScreen will say "Unknown publisher". More info > Run anyway.'
Note 'If something fails, the log ends up as kanpachi.log beside the .exe.'
