# Builds the PAYLOAD the installer packages.
#
# It is what goes inside Program Files\Kanpachi\, and nothing else. Unlike
# prepare-stage.ps1, which builds a test bench with the probes in it, this
# produces exactly what a person is handed.
#
# The differences with the stage, and all three matter:
#
#   - No probes. dirprobe, fwprobe and company are measurement tools and have
#     no business on anybody's machine.
#   - Flutter in RELEASE, not debug. A debug build carries the debug runtime,
#     runs slow and shows the banner.
#   - With -trimpath, so this machine's build paths do not travel inside the
#     binary, and with a SHA256SUMS beside it.
#
# No -ldflags "-s -w": stripping symbols sets off Defender false positives over
# Go binaries, and the binary being signed has to be the one that was tested.
# Same criterion as release-seed.yml.
#
#   .\scripts\prepare-payload.ps1
#   .\scripts\prepare-payload.ps1 -Output .\dist\carga -Engine ..\kanpachi-engine\target\release\kanpachi-engine.exe
#
# The defaults are RELATIVE TO THE REPOSITORY. They used to point at C:\kt,
# which is one machine's working directory and exists on no other one and not in
# CI: anybody cloning the repository and running this got a payload written into
# a path that means nothing.
#
# No elevation needed: this only compiles and copies.

[CmdletBinding()]
param(
    [string]$Output = "",
    # The engine comes from the other repository, and is not built here: it is
    # Rust, with its own toolchain. Empty looks for it beside this repository.
    [string]$Engine = "",
    # The version being packaged, without the "v". The workflow writes it from
    # the tag; by hand it stays "dev".
    #
    # It is not decoration: it is the only thing the interface can compare the
    # latest published version against to know whether its own has gone stale.
    # And "dev" is not filler, it is an answer: a development build does NOT
    # announce new versions, because there is nothing sensible to compare
    # against and because whoever built it does not need to be told to download
    # an installer.
    [string]$Version = "dev"
)

$ErrorActionPreference = 'Stop'

function Step($t) { Write-Host ""; Write-Host "=== $t ===" -ForegroundColor Cyan }
function Ok($t) { Write-Host "  OK   $t" -ForegroundColor Green }
function Fail($t) { Write-Host "  FAIL $t" -ForegroundColor Red }
function Note($t) { Write-Host "  --   $t" -ForegroundColor DarkGray }

# The repository comes from where the script lives and never from the current
# directory.
$repo = Split-Path -Parent $PSScriptRoot
$failures = 0

if ([string]::IsNullOrWhiteSpace($Output)) { $Output = Join-Path $repo 'dist\carga' }
if ([string]::IsNullOrWhiteSpace($Engine)) {
    $Engine = Join-Path (Split-Path -Parent $repo) 'kanpachi-engine\target\release\kanpachi-engine.exe'
}

if (Test-Path $Output) {
    # It is deleted whole. A payload with leftovers from an earlier version is
    # exactly the failure an installer cannot have: a file nobody built in this
    # round gets shipped.
    Remove-Item -Recurse -Force $Output
    Note "previous payload deleted"
}
New-Item -ItemType Directory -Force -Path $Output | Out-Null

Step "the daemon"

Push-Location $repo
try {
    $target = Join-Path $Output 'kanpachid.exe'
    # -H windowsgui: the shortcut points at this binary, so a console subsystem
    # one would open a black window on a double click. The output of --console
    # is not lost: it reattaches to the parent console.
    & go build -trimpath -ldflags "-H windowsgui" -o $target ./daemon/cmd/kanpachid
    if ($LASTEXITCODE -ne 0) { Fail "the daemon did not build"; $failures++ }
    else {
        $kb = [math]::Round((Get-Item $target).Length / 1KB)
        Ok ("kanpachid.exe    {0} KB" -f $kb)
    }
}
finally { Pop-Location }

Step "the interface"

Push-Location (Join-Path $repo 'ui')
try {
    Note "version $Version"
    # --build-name on top of the dart-define, and they are two different things:
    #
    #   - the dart-define is what the window BELIEVES it is, and what it
    #     compares against the latest published one.
    #   - --build-name is what WINDOWS says it is in the .exe properties.
    #     Without it, Flutter takes it from pubspec.yaml's version, which is
    #     written by hand and nobody moves: v0.2.0 shipped with the window
    #     saying 0.1.2+3 in its properties.
    #
    # It gets the numeric part: the VERSIONINFO field takes no suffixes, same as
    # Inno's VersionInfoVersion. A "dev" is not a version, and there whatever
    # pubspec carries is left alone, which is the honest answer for a hand
    # build.
    $flutterArgs = @('build', 'windows', '--release', "--dart-define=KANPACHI_VERSION=$Version")
    $numeric = ($Version -split '-')[0]
    if ($numeric -match '^\d+\.\d+\.\d+$') {
        $flutterArgs += "--build-name=$numeric"
        Note "the .exe properties: $numeric"
    }
    & flutter @flutterArgs 2>&1 |
        Select-Object -Last 3 | ForEach-Object { Note $_ }
    if ($LASTEXITCODE -ne 0) {
        Fail "the interface did not build"
        $failures++
    }
    else {
        $bundle = Join-Path $repo 'ui\build\windows\x64\runner\Release'
        if (-not (Test-Path (Join-Path $bundle 'kanpachiui.exe'))) {
            Fail "the build did not leave kanpachiui.exe where it was expected"
            $failures++
        }
        else {
            # The WHOLE bundle: the executable does not start without
            # flutter_windows.dll, without data\ and without the plugins.
            # Copying only the .exe gives a binary that dies at startup without
            # saying why.
            Copy-Item -Path (Join-Path $bundle '*') -Destination $Output -Recurse -Force
            $kb = [math]::Round((Get-Item (Join-Path $Output 'kanpachiui.exe')).Length / 1KB)
            Ok ("kanpachiui.exe   {0} KB, with its bundle" -f $kb)
        }
    }
}
finally { Pop-Location }

Step "what is not built here"

$copies = @(
    @{ source = 'daemon\adapter\catalog\jsonfile\builtin.json'; name = 'builtin.json' },
    @{ source = 'third_party\easytier\Packet.dll';              name = 'Packet.dll' },
    @{ source = 'third_party\easytier\wintun.dll';              name = 'wintun.dll' },
    # It was missing, and docs/03 has listed it inside the install directory
    # from the start. The portable folder did copy it, so the payload that gets
    # packaged was the only one of the two ways of handing Kanpachi over that
    # was short a file.
    @{ source = 'third_party\easytier\WinDivert64.sys';         name = 'WinDivert64.sys' }
)
foreach ($c in $copies) {
    $src = Join-Path $repo $c.source
    if (-not (Test-Path $src)) { Fail ("missing " + $c.source); $failures++; continue }
    Copy-Item -Path $src -Destination (Join-Path $Output $c.name) -Force
    Ok $c.name
}

Step "the engine, which comes from another repository"

if (Test-Path $Engine) {
    Copy-Item -Path $Engine -Destination (Join-Path $Output 'kanpachi-engine.exe') -Force
    $mb = [math]::Round((Get-Item $Engine).Length / 1MB, 1)
    Ok ("kanpachi-engine.exe  {0} MB" -f $mb)
}
else {
    Fail "the engine is not at $Engine"
    Note "build it in kanpachi-engine and pass its path with -Engine"
    $failures++
}

Step "the sums"

# A SHA256SUMS beside the payload, so whoever receives it can check that what
# arrived is what went out. Same criterion as release-seed.yml.
$lines = Get-ChildItem -Path $Output -Recurse -File |
    Where-Object { $_.Name -ne 'SHA256SUMS' } |
    Sort-Object FullName |
    ForEach-Object {
        $rel = $_.FullName.Substring($Output.Length).TrimStart('\')
        $h = (Get-FileHash -Algorithm SHA256 $_.FullName).Hash.ToLower()
        "$h  $rel"
    }
$lines | Out-File -FilePath (Join-Path $Output 'SHA256SUMS') -Encoding ascii
Ok ("SHA256SUMS with {0} files" -f $lines.Count)

Step "result"

if ($failures -gt 0) {
    Fail "$failures problem(s): the payload is NOT ready"
    exit 1
}
$total = [math]::Round(((Get-ChildItem -Path $Output -Recurse -File | Measure-Object Length -Sum).Sum / 1MB), 1)
Ok "the payload is at $Output, $total MB"
