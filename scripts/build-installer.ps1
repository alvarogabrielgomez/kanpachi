<#
.SYNOPSIS
    Builds the whole Windows client: kanpachi-setup.exe, kanpachi-portable.exe
    and their SHA256SUMS.

.DESCRIPTION
    The release used to do this in six inline steps, so reproducing a
    publication by hand meant reading the YAML and repeating them by eye. This
    is that recipe, and the runner calls the very same file.

    Five steps, and three of them carry a reason worth keeping:

      1. Stamp the version into the Windows resources.
      2. build-production.ps1: daemon, interface, catalog, DLLs and engine.
      3. Inno Setup over installer\kanpachi.iss.
      4. build-portable-bundle.ps1: the same product in one file.
      5. SHA256SUMS-windows, with BOTH executables in it.

    # Why the .syso files are regenerated here

    They are committed on purpose: they carry the icons and the manifest that
    tells Windows these binaries do NOT ask for elevation, and without them a
    hand-run `go build` would change that behaviour. What cannot stay committed
    is the NUMBER, and it did: they were generated once with `--file-version
    git-tag`, which resolves `git describe` at generation time, so every
    published binary said whatever version the day somebody ran `go generate`.
    v0.2.0 shipped with kanpachid.exe claiming v0.1.9-10-g3f1c599 in its
    properties, and the portable claiming v0.1.9-9-gc1da1b5, a different commit
    because they were generated on different days.

    The version is passed EXPLICITLY and not as `git-tag`: an Actions checkout is
    shallow, so `git describe` has nothing to work from there.

    -VersionNum goes into `--file-version` for the same reason Inno's
    VersionInfoVersion needs it: the binary VERSIONINFO field takes no suffixes,
    so a `0.2.0-rc1` loses its tail for that field only. `--product-version`
    carries the whole tag, which is what people read.

    # Why the portable is built here and not in its own workflow

    Because it comes out of the SAME tag as the installer. They are two ways of
    handing over the same product: published separately, a friend on the
    portable and another on the installed one would be on different versions
    with nothing saying so, and the protocol between the daemon and the engine
    decodes strictly.

    # Why both executables share one manifest

    Whoever produces the artifact produces its manifest. They are the same
    payload handed over two ways, so whoever verifies one has to be able to
    verify the other without looking somewhere else. The file is named
    SHA256SUMS-windows and not SHA256SUMS because three workflows write into one
    release, and a shared name means the last upload overwrites the others.

    Needs no elevated console. Needs Inno Setup installed, and the engine
    already built: this does not build the engine, it lives in another
    repository.

.PARAMETER Version
    The full version, suffix included: 0.2.0 or 0.2.0-rc1.

.PARAMETER VersionNum
    The numeric part only: 0.2.0. Defaults to Version with its suffix cut off.

.PARAMETER Engine
    Path to kanpachi-engine.exe. Passed straight through to both scripts.

.PARAMETER Repo
    owner/name, for the update check compiled into the installer.

.PARAMETER Output
    Where the artifacts go. Defaults to dist\ at the repository root.

.EXAMPLE
    scripts\build-installer.ps1 -Version 0.2.0 -Engine C:\kt\release\kanpachi-engine.exe -Repo accentiostudios/kanpachi
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$Version,
    [string]$VersionNum = "",
    [Parameter(Mandatory = $true)]
    [string]$Engine,
    [string]$Repo = "accentiostudios/kanpachi",
    [string]$Output = ""
)

$ErrorActionPreference = 'Stop'

$root = Split-Path -Parent $PSScriptRoot
if (-not $Output) { $Output = Join-Path $root 'dist' }
if (-not $VersionNum) { $VersionNum = ($Version -split '-')[0] }

function Step($t) { Write-Host "`n=== $t ===" -ForegroundColor Cyan }
function Ok($t) { Write-Host "  OK   $t" -ForegroundColor Green }

# Everything this needs is checked BEFORE anything is written. The first step
# rewrites the committed .syso files, so a missing Inno Setup found at step
# three would leave the working tree stamped with a version that was never
# packaged.
$iscc = "C:\Program Files (x86)\Inno Setup 6\ISCC.exe"
if (-not (Test-Path $Engine)) { throw "the engine is not at $Engine" }
if (-not (Test-Path $iscc)) { throw "Inno Setup is not at $iscc (choco install innosetup)" }

New-Item -ItemType Directory -Force -Path $Output | Out-Null

Push-Location $root
try {
    Step "stamping $Version into the Windows resources"
    foreach ($p in @('daemon\cmd\kanpachid', 'internal\kanpachibundle')) {
        Push-Location $p
        try {
            & go run github.com/tc-hib/go-winres@latest make `
                --arch amd64 --out rsrc `
                --file-version $VersionNum `
                --product-version $Version
            if ($LASTEXITCODE -ne 0) { throw "go-winres exited with $LASTEXITCODE in $p" }
        } finally { Pop-Location }
    }
    Ok "file-version $VersionNum, product-version $Version"

    Step "the payload"
    $payload = Join-Path $Output 'carga'
    # LASTEXITCODE is reset by hand before calling a .ps1, and it is not
    # paranoia: a script that ends WITHOUT `exit` leaves the previous command's
    # code in place. build-production.ps1 exits 1 when the payload is incomplete
    # and just ends when it is fine, so without this a failure would be read as
    # whatever go-winres left behind.
    $global:LASTEXITCODE = 0
    & (Join-Path $PSScriptRoot 'build-production.ps1') -Output $payload -Engine $Engine -Version $Version
    if ($LASTEXITCODE -ne 0) { throw "build-production.ps1 exited with $LASTEXITCODE" }
    Ok $payload

    Step "the installer"
    & $iscc `
        "/DCarga=$payload" `
        "/DAppVersion=$Version" `
        "/DRepo=$Repo" `
        "/DVersionInfo=$VersionNum" `
        "/O$Output" `
        (Join-Path $root 'installer\kanpachi.iss')
    if ($LASTEXITCODE -ne 0) { throw "ISCC exited with $LASTEXITCODE" }
    Ok "kanpachi-setup.exe"

    Step "the portable"
    $global:LASTEXITCODE = 0
    & (Join-Path $PSScriptRoot 'build-portable-bundle.ps1') `
        -Output (Join-Path $Output 'kanpachi-portable.exe') `
        -Engine $Engine
    if ($LASTEXITCODE -ne 0) { throw "build-portable-bundle.ps1 exited with $LASTEXITCODE" }
    Ok "kanpachi-portable.exe"

    Step "sums"
    # The same format `sha256sum -c` expects, so both payloads of the release
    # are verified the same way. Two spaces between the hash and the name.
    $sums = @()
    foreach ($name in @('kanpachi-setup.exe', 'kanpachi-portable.exe')) {
        $file = Join-Path $Output $name
        if (-not (Test-Path $file)) { throw "$name did not come out of the packaging" }
        $hash = (Get-FileHash -Algorithm SHA256 $file).Hash.ToLower()
        $sums += "$hash  $name"
    }
    $manifest = Join-Path $Output 'SHA256SUMS-windows'
    $sums | Out-File -FilePath $manifest -Encoding ascii
    Get-Content $manifest | ForEach-Object { Write-Host "  $_" }
    Ok $manifest
} finally { Pop-Location }
