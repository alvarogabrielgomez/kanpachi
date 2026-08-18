<#
.SYNOPSIS
    Downloads the official EasyTier binaries into third_party\easytier.

.DESCRIPTION
    /third_party/ is in .gitignore because it weighs around 80 MB, so a fresh
    clone arrives WITHOUT Packet.dll, wintun.dll and WinDivert64.sys. On the
    development machine they are there from before and nobody notices; on a
    clean runner there is nowhere to get them from, and build-production.ps1 cuts
    with "missing third_party\easytier\Packet.dll".

    This used to live inline in release.yml. It lives here so the release runner
    and a fresh clone populate that directory the same way.

    # Why all SEVEN and not the three that get packaged

    With the complete directory, the supply-chain guard in internal/arch
    (TestLosBinariosDelMotorSonLosQueSeProbaron) verifies the WHOLE manifest
    against easytier.sums, instead of skipping itself because the directory is
    missing. That is where the check goes from an intention to a fact.

    # Where the version lives, and where the hashes do NOT

    The version and the URL live HERE and nowhere else. The hashes live in
    internal/arch/easytier.sums and in docs/02-decisiones-de-diseno.md, tied
    together by TestElPinDelMotorEstáTambiénEnLosDocs. A third place to copy
    hashes into would be a third place where they go stale, so this script does
    not verify anything: `go test ./internal/arch/...` does, over the directory
    this leaves behind.

    Updating EasyTier means bumping the version here ON PURPOSE, regenerating
    easytier.sums, and writing the new sums into docs/02.

.PARAMETER Destination
    Where to leave the seven files. Defaults to third_party\easytier at the
    repository root. Point it at a temporary directory to check a download
    without overwriting the binaries this machine already has.

.PARAMETER Version
    The EasyTier tag to fetch. Defaults to the pinned one. It is a parameter so
    a measurement can try another one, NOT so a build can drift: what release
    publishes is whatever the default says.
#>
[CmdletBinding()]
param(
    [string]$Destination = "",
    [string]$Version = "v2.6.4"
)

$ErrorActionPreference = 'Stop'

$root = Split-Path -Parent $PSScriptRoot
if (-not $Destination) { $Destination = Join-Path $root 'third_party\easytier' }

function Step($t) { Write-Host "`n=== $t ===" -ForegroundColor Cyan }
function Ok($t) { Write-Host "  OK   $t" -ForegroundColor Green }

$url = "https://github.com/EasyTier/EasyTier/releases/download/$Version/easytier-windows-x86_64-$Version.zip"
$temp = Join-Path ([IO.Path]::GetTempPath()) ("easytier-" + [Guid]::NewGuid().ToString('N'))
$zip = "$temp.zip"

try {
    Step "downloading EasyTier $Version"
    Write-Host "  $url"
    Invoke-WebRequest -Uri $url -OutFile $zip
    Ok "$([math]::Round((Get-Item $zip).Length / 1MB, 1)) MB"

    Step "expanding"
    Expand-Archive -Path $zip -DestinationPath $temp -Force

    # The zip carries everything under an `easytier-windows-x86_64\` folder, and
    # the manifest lists paths relative to third_party\easytier, so it is
    # flattened here. Getting this wrong produces a directory the guard reads as
    # seven missing files plus seven that appeared.
    $inside = Join-Path $temp 'easytier-windows-x86_64'
    if (-not (Test-Path $inside)) { throw "the zip does not carry easytier-windows-x86_64\: its layout changed" }

    Step "copying to $Destination"
    New-Item -ItemType Directory -Force -Path $Destination | Out-Null
    Copy-Item -Path (Join-Path $inside '*') -Destination $Destination -Force -Recurse

    Get-ChildItem $Destination -File | Format-Table Name, Length
    Ok "$((Get-ChildItem $Destination -File).Count) files"
    Write-Host "  Their sums are checked by:  go test -count=1 ./internal/arch/..."
} finally {
    Remove-Item $zip -Force -ErrorAction SilentlyContinue
    Remove-Item $temp -Recurse -Force -ErrorAction SilentlyContinue
}
