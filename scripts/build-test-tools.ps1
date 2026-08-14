<#
.SYNOPSIS
    Builds testTools\: roomprobe.exe with the engine and its DLLs beside it.

.DESCRIPTION
    roomprobe exercises a room's lifecycle with the real adapters, without a
    daemon and without an interface. It needs kanpachi-engine.exe NEXT TO it,
    because it does not look for it on the PATH on purpose: a PATH somebody can
    write to is a way for an elevated process to run some other binary with that
    name.

    NO elevated console needed for this: here things are only built and copied.
    roomprobe needs elevation when it runs, and it elevates itself.

.PARAMETER RebuildEngine
    Obsolete: the engine is always rebuilt. It is accepted so as not to break
    whatever somebody has in their console history.

.PARAMETER Yes
    Asks nothing. Useful to run it unattended.

.PARAMETER Clean
    Also deletes roomprobe-data, data and roomprobe.log. That takes identity.key
    and this machine's fingerprint book with it, which means the probe comes
    back with a different face and remembering nobody. Without this switch only
    what was built gets deleted.
#>
[CmdletBinding()]
param(
    [switch]$RebuildEngine,
    [switch]$Yes,
    [switch]$Clean
)

$ErrorActionPreference = "Stop"

# The repo is derived from the script's path and NEVER from the current
# directory: an elevated console starts in system32.
$repoRoot   = Split-Path -Parent $PSScriptRoot
$engineRoot = Join-Path (Split-Path -Parent $repoRoot) "kanpachi-engine"
$output     = Join-Path $repoRoot "testTools"

if (-not (Test-Path $output)) { New-Item -ItemType Directory -Path $output | Out-Null }

function Step($t) { Write-Host "`n$t" -ForegroundColor Cyan }
function Ok($t) { Write-Host "  $t" -ForegroundColor Green }
function Note($t) { Write-Host "  $t" -ForegroundColor DarkGray }
function Warn($t) { Write-Host "  $t" -ForegroundColor Yellow }

# ── 0. Everything built gets DELETED, always ─────────────────────────────────
#
# Nothing from an earlier run is reused. A directory where binaries from
# different days live together does not fail when building: it fails when
# running, with an error that talks about something else -- a JSON field the
# engine does not know, a menu that does not match the code -- and it is paid
# for by whoever is testing, not by whoever built it.
#
# The products get deleted: the .exe files, the DLLs, the driver and the payload
# embedded into the bundle. `roomprobe-data` and `roomprobe.log` do NOT get
# deleted: that is where `identity.key`, the fingerprint book and the evidence
# of the last run live, which is exactly what gets sent over chat. That is what
# -Clean is for.
Step "Deleting what was built before (nothing is reused)"
$products = @("roomprobe.exe", "roombundle.exe", "kanpachi-engine.exe",
              "wintun.dll", "Packet.dll", "WinDivert64.sys")
foreach ($f in $products) {
    $path = Join-Path $output $f
    if (Test-Path $path) { Remove-Item $path -Force; Note "deleted $f" }
}
Get-ChildItem $output -Filter "*.exe~" -ErrorAction SilentlyContinue | Remove-Item -Force
$payload = Join-Path $repoRoot "internal\roombundle\carga"
if (Test-Path $payload) {
    Remove-Item (Join-Path $payload "*") -Force -Recurse -ErrorAction SilentlyContinue
    Note "emptied internal\roombundle\carga"
}
$loose = Join-Path $repoRoot "roomprobe.exe"
if (Test-Path $loose) { Remove-Item $loose -Force; Note "deleted the loose roomprobe.exe at the root" }

if ($Clean) {
    foreach ($d in @("roomprobe-data", "data")) {
        $path = Join-Path $output $d
        if (Test-Path $path) { Remove-Item $path -Recurse -Force; Warn "deleted $d (identity and fingerprint book included)" }
    }
    $log = Join-Path $output "roomprobe.log"
    if (Test-Path $log) { Remove-Item $log -Force; Warn "deleted roomprobe.log" }
}

# ── 1. The CI gate, run here ─────────────────────────────────────────────────
# The Linux job does `go vet ./internal/...`, and roomprobe broke it once by
# importing x/sys/windows without a build tag. It costs a few seconds and saves
# finding out after a push.
Step "Checking that the Linux CI job does not break..."
$env:GOOS = "linux"
try {
    Push-Location $repoRoot
    try {
        go vet ./internal/...
        if ($LASTEXITCODE -ne 0) { throw "roomprobe breaks the Linux CI job" }
    } finally { Pop-Location }
} finally { Remove-Item Env:GOOS }
Ok "Linux compiles and vets"

# ── 2. The engine, ALWAYS rebuilt ────────────────────────────────────────────
#
# One lying around is never reused. Reusing one is where the 2026-08-14 failure
# came from: the daemon sends `log_dir` with every order, the .exe sitting in
# testTools predated the field, and opening a room died in
#
#   unreadable command: unknown field `log_dir`, expected one of `dev_name`...
#
# That message looks nothing like "that binary is six days old", so it is paid
# for in investigation by whoever is testing. It costs a few minutes of Rust and
# removes a whole class of failure.
$engineTarget = Join-Path $output "kanpachi-engine.exe"

Step "Rebuilding the engine from $engineRoot..."
if (-not (Test-Path (Join-Path $engineRoot "Cargo.toml"))) {
    throw "The kanpachi-engine repo is not at $engineRoot. Clone it beside kanpachi."
}
$engineBuild = Join-Path $engineRoot "scripts\build.ps1"
if (-not (Test-Path $engineBuild)) {
    throw "$engineBuild is missing, which sets up MSVC, protoc, libclang and 7-Zip before building."
}
& $engineBuild -Stage $output
$engineExe = "C:\kt\release\kanpachi-engine.exe"
if (-not (Test-Path $engineExe)) {
    throw "The engine was built but did not show up at $engineExe."
}

Copy-Item $engineExe $engineTarget -Force
$mb = [math]::Round((Get-Item $engineTarget).Length / 1MB, 1)
Ok "Engine copied from $engineExe ($mb MB)"

# Packet.dll is a hard import of the engine and wintun.dll is what creates the
# virtual adapter: without them the engine starts and brings up no network.
$engineDir = Split-Path -Parent $engineExe
foreach ($dep in @('Packet.dll', 'wintun.dll', 'WinDivert64.sys')) {
    $src = Join-Path $engineDir $dep
    if (Test-Path $src) { Copy-Item $src (Join-Path $output $dep) -Force; Note "+ $dep" }
}

# ── 3. roomprobe ─────────────────────────────────────────────────────────────
# Without `-ldflags "-s -w"` on purpose: it sets off Defender false positives
# over Go binaries, and there is no size saving here worth that.
Step "Building roomprobe..."
$probeTarget = Join-Path $output "roomprobe.exe"
Push-Location $repoRoot
try {
    go build -o $probeTarget ./internal/roomprobe
    if ($LASTEXITCODE -ne 0) { throw "roomprobe did not build" }
} finally { Pop-Location }
Ok "roomprobe.exe ready"

# ── 4. Check what was built ──────────────────────────────────────────────────
# A green `go build` with the engine missing is still a tool that brings up no
# room at all.
$theFive = @('roomprobe.exe', 'kanpachi-engine.exe', 'wintun.dll', 'Packet.dll', 'WinDivert64.sys')
$missing = $theFive | Where-Object { -not (Test-Path (Join-Path $output $_)) }
if ($missing) { throw "Missing from testTools: $($missing -join ', ')" }

# ── 5. roombundle: the five inside ONE executable ────────────────────────────
# `go:embed` only embeds files INSIDE the package directory (it does not accept
# `../`), so they have to be copied there right before building.
#
# The `-tags bundle` tag is what makes the payload go in. Without it the package
# builds anyway and the binary refuses to run saying why: that is on purpose, so
# a normal `go build ./...` does not produce an empty bundle that only fails on
# somebody else's machine.
Step "Building roombundle (the five in a single executable)..."
$payloadDir = Join-Path $repoRoot "internal\roombundle\carga"
if (-not (Test-Path $payloadDir)) { New-Item -ItemType Directory -Path $payloadDir | Out-Null }
Get-ChildItem $payloadDir -Exclude ".gitignore" -ErrorAction SilentlyContinue | Remove-Item -Force
foreach ($f in $theFive) { Copy-Item (Join-Path $output $f) (Join-Path $payloadDir $f) -Force }

$bundleTarget = Join-Path $output "roombundle.exe"
Push-Location $repoRoot
try {
    go build -tags bundle -o $bundleTarget ./internal/roombundle
    if ($LASTEXITCODE -ne 0) { throw "roombundle did not build" }
} finally {
    Pop-Location
    # The payload is always deleted: it is 48 MB inside the code tree, and
    # leaving it there makes the next `go build ./...` slower and gives an
    # inattentive `git status` a fright.
    Get-ChildItem $payloadDir -Exclude ".gitignore" -ErrorAction SilentlyContinue | Remove-Item -Force
}
$mb = [math]::Round((Get-Item $bundleTarget).Length / 1MB, 1)
Ok "roombundle.exe ready ($mb MB) - this is what gets sent over chat"

# ── 5.5 The five have to be there, and freshly built ─────────────────────────
#
# Step 0 deletes them all, so whatever is here came out of this run. It gets
# checked anyway: the engine's script copies its DLLs from the stage, and if
# that stage is empty it warns in grey and carries on. A testTools without
# Packet.dll builds fine and dies bringing up the network, on somebody else's
# machine.
Step "Checking that the five are there"
# Two are BUILT here and have to be from this run. The other three are third
# party: they are downloaded once, their date is the day they came down and they
# are pinned by hash in internal/arch/suministro_test.go, so of those only
# presence is demanded.
$startedAt = (Get-Date).AddMinutes(-90)
foreach ($f in @("roomprobe.exe", "kanpachi-engine.exe")) {
    $path = Join-Path $output $f
    if (-not (Test-Path $path)) { throw "$f is missing from testTools: the bundle would be incomplete." }
    if ((Get-Item $path).LastWriteTime -lt $startedAt) {
        throw "$f is dated $((Get-Item $path).LastWriteTime): it did not come out of this run."
    }
}
foreach ($f in @("wintun.dll", "Packet.dll", "WinDivert64.sys")) {
    if (-not (Test-Path (Join-Path $output $f))) {
        throw "$f is missing from testTools. It is third party and the engine build leaves it; without it the engine brings up no network."
    }
}
Ok "the five are there, and the two that get built are from this run"

# ── 6. The service, which they will fight over ───────────────────────────────
$svc = Get-Service kanpachi-daemon -ErrorAction SilentlyContinue
if ($svc -and $svc.Status -eq 'Running') {
    Warn "kanpachi-daemon is RUNNING."
    Warn "roomprobe refuses to start except with -force, and rightly so: building"
    Warn "the session purges that daemon's firewall rules."
    Warn "  Stop it with:  Stop-Service kanpachi-daemon"
}

Step "Done. Contents of testTools:"
Get-ChildItem $output | ForEach-Object {
    $t = if ($_.PSIsContainer) { "dir" } else { "$([math]::Round($_.Length / 1MB, 1)) MB" }
    Write-Host "  $($_.Name)  ($t)"
}
Write-Host "`n  Run:  $probeTarget" -ForegroundColor Cyan
Write-Host "  The log ends up at: $(Join-Path $output 'roomprobe.log')" -ForegroundColor Cyan
