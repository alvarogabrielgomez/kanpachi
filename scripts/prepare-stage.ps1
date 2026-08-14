<#
.SYNOPSIS
    Builds and populates the stage every measurement script takes for granted.

.DESCRIPTION
    Each measure-*.ps1 gets -Stage "C:\kt\stage" and ASSUMES the binaries and
    the DLLs are in there. Nothing in the repository populated it: it was put
    together by hand, and the symptom of a half-built stage does not look like
    its cause. Packet.dll missing and the engine dies with 0xC0000135 without
    naming which one it wanted; builtin.json missing and the room opens with no
    game to pick.

    What it leaves:

      kanpachid.exe        the daemon
      pipeprobe.exe        the pipe probe: methods by their wire name
      dirprobe.exe         the seed registry probe
      engineprobe.exe      the engine probe
      netcfgprobe.exe      the adapter settings probe
      fwprobe.exe          the firewall and gate probe
      builtin.json         the catalog that ships with the app
      Packet.dll           a HARD import of the engine, it will not start without it
      wintun.dll           the virtual adapter
      WinDivert64.sys      consumed by the engine

    What it does NOT leave, and why: kanpachi-engine.exe lives in another
    repository and is built separately. If it is already in the stage it is left
    alone, and if it is missing it says so without failing, which is the only
    honest thing that can be done from here.

    NO elevated console needed: this compiles and copies. Elevation is needed to
    run what it leaves behind.
#>
[CmdletBinding()]
param(
    [string]$Stage = "C:\kt\stage"
)

$ErrorActionPreference = 'Stop'

function Step($t) { Write-Host "`n=== $t ===" -ForegroundColor Cyan }
function Ok($t) { Write-Host "  OK   $t" -ForegroundColor Green }
function Fail($t) { Write-Host "  FAIL $t" -ForegroundColor Red }
function Note($t) { Write-Host "  --   $t" -ForegroundColor DarkGray }

# The repository comes from where the script lives and never from the current
# directory: an elevated console starts in system32.
$repo = Split-Path -Parent $PSScriptRoot

$failures = 0

if (-not (Test-Path $Stage)) {
    New-Item -ItemType Directory -Force -Path $Stage | Out-Null
    Note "created $Stage"
}

Step "building"

# No -ldflags "-s -w": stripping symbols sets off Defender false positives over
# Go binaries, and the binary being measured has to be the one being shipped.
#
# kanpachid DOES carry -H windowsgui, and only it. The installer's shortcut
# points at it, so a console-subsystem binary would open a black window on a
# double click, which is exactly what the product promises nobody sees. The
# output of --console and --reset is not lost: the binary reattaches to the
# parent console when there is one. See reengancharConsola.
#
# The probes do NOT carry it: they are command line tools and the window is the
# whole point there.
$binaries = @(
    @{ name = 'kanpachid.exe';    package = './daemon/cmd/kanpachid'; gui = $true },
    @{ name = 'pipeprobe.exe';    package = './internal/pipeprobe' },
    @{ name = 'dirprobe.exe';     package = './internal/dirprobe' },
    @{ name = 'engineprobe.exe';  package = './internal/engineprobe' },
    @{ name = 'netcfgprobe.exe';  package = './internal/netcfgprobe' },
    @{ name = 'fwprobe.exe';      package = './internal/fwprobe' },
    @{ name = 'watchprobe.exe';   package = './internal/watchprobe' }
)

Push-Location $repo
try {
    foreach ($b in $binaries) {
        $target = Join-Path $Stage $b.name
        if ($b.gui) {
            & go build -ldflags "-H windowsgui" -o $target $b.package
        } else {
            & go build -o $target $b.package
        }
        if ($LASTEXITCODE -ne 0) {
            Fail ("did not build: " + $b.package)
            $failures++
            continue
        }
        $kb = [math]::Round((Get-Item $target).Length / 1KB)
        Ok ("{0,-18} {1} KB" -f $b.name, $kb)
    }
}
finally {
    Pop-Location
}

Step "copying what is not built"

$copies = @(
    @{ source = 'daemon\adapter\catalog\jsonfile\builtin.json'; name = 'builtin.json' },
    @{ source = 'third_party\easytier\Packet.dll';              name = 'Packet.dll' },
    @{ source = 'third_party\easytier\wintun.dll';              name = 'wintun.dll' },
    @{ source = 'third_party\easytier\WinDivert64.sys';         name = 'WinDivert64.sys' }
)

foreach ($c in $copies) {
    $source = Join-Path $repo $c.source
    if (-not (Test-Path $source)) {
        Fail ("missing from the repository: " + $c.source)
        $failures++
        continue
    }
    Copy-Item -Path $source -Destination (Join-Path $Stage $c.name) -Force
    Ok $c.name
}

Step "the engine, which comes from another repository"

$engine = Join-Path $Stage 'kanpachi-engine.exe'
if (Test-Path $engine) {
    $mb = [math]::Round((Get-Item $engine).Length / 1MB, 1)
    Ok "kanpachi-engine.exe was already there, $mb MB, and was left alone"
} else {
    Note "kanpachi-engine.exe is NOT in the stage"
    Note "it is built in the kanpachi-engine repository and copied here by hand"
    Note "without it you can talk to the daemon, you cannot open a room"
}

Step "result"

if ($failures -eq 0) {
    Ok "the stage is ready at $Stage"
    exit 0
}

Fail "$failures thing(s) missing"
exit 1
