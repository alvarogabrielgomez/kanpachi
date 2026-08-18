# The ONE list of places where a built engine lands, and the one staleness
# check. Dot-source this file; it is not an entry point.
#
# Before this file existed, five scripts each carried their own idea of where
# the engine lives — C:\kt\stage hardcoded, C:\kt\release hardcoded, the engine
# repo's target\release, "newest of three" — and none of them matched what CI
# does. The engine failing to be found, or worse being found stale, was
# discovered at the END of a build, or on somebody else's machine.
#
# The places, and why each is on the list:
#
#   C:\kt\release\        where the engine repo's scripts\build.ps1 leaves it:
#                         it sets CARGO_TARGET_DIR=C:\kt because cl.exe is not
#                         long-path aware, so the target dir has to be short.
#   C:\kt\stage\          the measurement bench (prepare-stage.ps1), where
#                         build.ps1 -Stage also copies it next to its DLLs.
#   ..\kanpachi-engine\target\release\
#                         a plain `cargo build --release` run in the engine
#                         repo without its script, which is what CI does with
#                         its own checkout under motor\.
#
# NEWEST wins, not first-found: which place holds the good one depends on how
# the engine was built last time, and picking by fixed order is picking by
# habit. Already bitten: a C:\kt\stage engine three days stale, missing the
# commit that lets the host accept its own guests, almost shipped inside a
# bundle.

# Where the engine repository is expected: cloned beside this one. Callers can
# override, CI passes motor\ explicitly.
function Find-KanpachiEngineRepo {
    param([Parameter(Mandatory = $true)][string]$KanpachiRepo)
    Join-Path (Split-Path -Parent $KanpachiRepo) 'kanpachi-engine'
}

# The newest built engine among the known places, or $null when there is none.
# The caller decides what "none" means: every product build treats it as a
# stop, with scripts\build-engine.ps1 named as the fix.
function Resolve-KanpachiEngine {
    param([Parameter(Mandatory = $true)][string]$EngineRoot)
    @(
        'C:\kt\release\kanpachi-engine.exe',
        'C:\kt\stage\kanpachi-engine.exe',
        (Join-Path $EngineRoot 'target\release\kanpachi-engine.exe')
    ) | Where-Object { Test-Path $_ } |
        Sort-Object { (Get-Item $_).LastWriteTime } -Descending |
        Select-Object -First 1
}

# Stops when the chosen binary is OLDER than the engine's own source. A stale
# engine is not discovered where it was packaged: it is discovered on the other
# person's machine, with no log on the side of whoever built it. When the
# engine repo is not there to compare against, it says so and lets it pass:
# a machine without the source has nothing newer to be stale against.
function Assert-KanpachiEngineFresh {
    param(
        [Parameter(Mandatory = $true)][string]$Engine,
        [Parameter(Mandatory = $true)][string]$EngineRoot
    )
    if (-not (Test-Path $EngineRoot)) {
        Write-Host "  --   the engine repo is not at $EngineRoot, staleness cannot be checked" -ForegroundColor DarkGray
        return
    }
    $engineInfo = Get-Item $Engine
    $sources = @(Get-ChildItem (Join-Path $EngineRoot 'src') -Recurse -File -ErrorAction SilentlyContinue) +
               @(Get-ChildItem $EngineRoot -File -Filter 'Cargo.toml' -ErrorAction SilentlyContinue) +
               @(Get-ChildItem $EngineRoot -File -Filter 'build.rs' -ErrorAction SilentlyContinue)
    $newest = $sources | Sort-Object LastWriteTime -Descending | Select-Object -First 1
    if ($newest -and $newest.LastWriteTime -gt $engineInfo.LastWriteTime) {
        Write-Host "  FAIL the engine is OLDER than its source code" -ForegroundColor Red
        Write-Host ("  --     {0} is from {1}" -f $newest.Name, $newest.LastWriteTime) -ForegroundColor DarkGray
        Write-Host ("  --     the binary is from {0}" -f $engineInfo.LastWriteTime) -ForegroundColor DarkGray
        Write-Host "  --   rebuild it with scripts\build-engine.ps1, or pass the good one with -Engine" -ForegroundColor DarkGray
        throw 'an old engine does not get packaged: the failure shows up on the other person''s machine'
    }
    Write-Host '  OK   the engine is newer than its source, it is not stale' -ForegroundColor Green
}
