# Points engine.pin at an engine release, hashes included.
#
# The pin needs three lines to move together: the tag, and the SHA256 of each
# platform's binary as published in that release's SHA256SUMS-engine. Copying
# the two hashes by hand is exactly the kind of transcription that goes wrong
# silently, so this script does the mechanical half: it downloads the sums
# file from the release and rewrites the three values in place.
#
# What it deliberately does NOT do is commit. Adopting a new engine is a
# decision, and the decision is the commit — review the diff, then commit.
#
#   .\scripts\pin-engine.ps1 v0.1.0
#
# Fails if the release does not exist yet, if the sums file is missing either
# binary, or if the tag on the release does not match what was asked for.
param(
    [Parameter(Mandatory)]
    [string]$Tag
)

$ErrorActionPreference = 'Stop'

$repo = 'alvarogabrielgomez/kanpachi-engine'
$pin = Join-Path $PSScriptRoot '..\engine.pin'
if (-not (Test-Path $pin)) { throw "engine.pin not found at $pin" }

Write-Host "Reading SHA256SUMS-engine from $repo@$Tag..."
$sums = gh release download $Tag --repo $repo --pattern SHA256SUMS-engine --output - | Out-String
if ($LASTEXITCODE -ne 0) { throw "no release $Tag on $repo, or it has no SHA256SUMS-engine yet" }

# The sums file is `sha256sum` output: "<hash>  <name>" per line.
$windows = $null
$linux = $null
foreach ($line in $sums -split "`n") {
    if ($line -match '^([0-9a-f]{64})\s+kanpachi-engine\.exe\s*$') { $windows = $Matches[1] }
    elseif ($line -match '^([0-9a-f]{64})\s+kanpachi-engine\s*$') { $linux = $Matches[1] }
}
if (-not $windows) { throw "SHA256SUMS-engine of $Tag has no line for kanpachi-engine.exe" }
if (-not $linux) { throw "SHA256SUMS-engine of $Tag has no line for kanpachi-engine" }

# Rewrite only the three value lines; the header explains itself and stays.
$lines = Get-Content $pin
$rewritten = $lines | ForEach-Object {
    if ($_ -match '^tag\s') { "tag $Tag" }
    elseif ($_ -match '^sha256-windows\s') { "sha256-windows $windows" }
    elseif ($_ -match '^sha256-linux\s') { "sha256-linux $linux" }
    else { $_ }
}
Set-Content -Path $pin -Value $rewritten -Encoding utf8

Write-Host ""
Write-Host "engine.pin now says:"
Write-Host "  tag            $Tag"
Write-Host "  sha256-windows $windows"
Write-Host "  sha256-linux   $linux"
Write-Host ""
Write-Host "Review with git diff, then commit. The commit is the adoption."
