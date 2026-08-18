<#
.SYNOPSIS
    Builds kanpachi-engine.exe, delegating to the engine repository's own
    build script.

.DESCRIPTION
    The engine lives in ANOTHER repository (kanpachi-engine) because a failure
    of the engine must not be able to open this machine, and that separation is
    easiest to audit when the code bases do not share a tree. The price was
    that this repository had no way to build it: locally you had to know to go
    to the other repo and run ITS script, and CI re-implemented the recipe
    inline in YAML. This script closes that gap without duplicating the recipe:
    the engine repo's scripts\build.ps1 stays the ONLY place that knows how the
    engine is built (MSVC env, protoc, libclang, 7-Zip, short target dir), and
    this one only finds it, calls it, and says where the binary ended up.

    Where the binary lands: <TargetDir>\release\kanpachi-engine.exe, which is
    C:\kt\release\kanpachi-engine.exe by default. That path is FIRST in the
    shared resolution list (scripts\lib\engine.ps1), so after running this,
    every build-* script finds the fresh engine on its own, with no -Engine
    flag needed.

.PARAMETER Repo
    The engine repository. Defaults to kanpachi-engine cloned beside this
    repository. CI passes its own checkout under motor\.

.PARAMETER TargetDir
    Passed through to the engine's script as its cargo target dir. Its default
    C:\kt is short ON PURPOSE: cl.exe is not long-path aware.

.PARAMETER Clean
    Passed through: cargo clean first.

.EXAMPLE
    .\scripts\build-engine.ps1
    Builds the sibling ..\kanpachi-engine into C:\kt\release\.

.EXAMPLE
    .\scripts\build-engine.ps1 -Repo motor -TargetDir motor\target
    What release.yml runs against its own engine checkout.
#>
[CmdletBinding()]
param(
    [string]$Repo = "",
    [string]$TargetDir = "C:\kt",
    [switch]$Clean
)

$ErrorActionPreference = 'Stop'

# The repository comes from where the script lives and never from the current
# directory: an elevated console starts in system32.
$kanpachi = Split-Path -Parent $PSScriptRoot
. (Join-Path $PSScriptRoot 'lib\engine.ps1')

if (-not $Repo) { $Repo = Find-KanpachiEngineRepo -KanpachiRepo $kanpachi }
if (-not (Test-Path (Join-Path $Repo 'Cargo.toml'))) {
    throw "the kanpachi-engine repo is not at $Repo. Clone it beside kanpachi, or pass -Repo"
}
$engineBuild = Join-Path $Repo 'scripts\build.ps1'
if (-not (Test-Path $engineBuild)) {
    throw "$engineBuild is missing, which sets up MSVC, protoc, libclang and 7-Zip before building"
}

# A relative -TargetDir would be resolved by cargo against the ENGINE repo,
# because its script cd's there before building. Made absolute here, against
# where the caller stands, so `-TargetDir motor\target` means what it says.
# Not GetFullPath(path, base): that overload is .NET Core, and this runs on
# Windows PowerShell 5.1.
if (-not [System.IO.Path]::IsPathRooted($TargetDir)) {
    $TargetDir = Join-Path (Get-Location).Path $TargetDir
}

$buildArgs = @{ TargetDir = $TargetDir }
if ($Clean) { $buildArgs.Clean = $true }
& $engineBuild @buildArgs

$exe = Join-Path $TargetDir 'release\kanpachi-engine.exe'
if (-not (Test-Path $exe)) { throw "the engine built and did not show up at $exe" }

Write-Host ''
Write-Host "  The engine is at $exe" -ForegroundColor Cyan
Write-Host "  Every build-* script finds it there on its own." -ForegroundColor DarkGray
