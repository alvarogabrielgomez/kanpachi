<#
.SYNOPSIS
    Dirties the machine on purpose and checks that --reset leaves it clean.

.DESCRIPTION
    The hard reset exists for the machine where the daemon NO LONGER STARTS.
    Measuring it with everything working would prove nothing, so this script
    manufactures the case: it opens a real room and kills the daemon DIRTILY,
    giving it no chance to clean up.

    What is left then is what has to be measured: rules of the Kanpachi group
    in place, gate filters in WFP, a hosted-room.json claiming there is a room,
    and possibly an orphaned engine. Then it runs --reset and checks the only
    things that really matter:

      1. Zero rules of the Kanpachi group.
      2. Zero gate filters.
      3. The base quarantine WHOLE, because the reset restores it and does not
         remove it.
      4. No orphaned engine.
      5. hosted-room.json deleted, and last-room.json untouched.
      6. And after the reset a room can be created again.

    Point 3 is what separates this command from the uninstaller. A reset is
    asked for when nothing works; removing the quarantine there would destroy
    exactly what protects against the case that prompted it.

    Needs an elevated console.
#>
[CmdletBinding()]
param(
    [string]$Stage = "C:\kt\stage",
    [string]$Data = "$env:ProgramData\Kanpachi",
    [int]$Timeout = 45
)

$ErrorActionPreference = 'Stop'

function Step($t) { Write-Host "`n=== $t ===" -ForegroundColor Cyan }
function Ok($t) { Write-Host "  OK   $t" -ForegroundColor Green }
function Fail($t) { Write-Host "  FAIL $t" -ForegroundColor Red }

$isAdmin = ([Security.Principal.WindowsPrincipal] `
    [Security.Principal.WindowsIdentity]::GetCurrent()
).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if (-not $isAdmin) { throw "An elevated console is needed." }

$failures = 0
# Returns HOW MANY and not the list, on purpose. PowerShell 5.1 unwraps a
# single-element array on return, so `(RulesOf 'x').Count` over one rule gives
# $null instead of 1: the measurement reads as zero exactly when one loose rule
# is left, which is the case that matters most to see.
function HowManyRulesOf($group) {
    $fw = New-Object -ComObject HNetCfg.FwPolicy2
    $r = @($fw.Rules | Where-Object { $_.Grouping -eq $group })
    $r.Count
}
function GateFilters() {
    $text = & netsh.exe wfp show filters file=- 2>&1 | Out-String
    ([regex]::Matches($text, 'bloqueo de todo')).Count
}

Step "how many rules the base quarantine has BEFORE anything"
$baseBefore = HowManyRulesOf 'Kanpachi-base'
if ($baseBefore -eq 0) { throw "there is no base quarantine in place: this script would prove nothing" }
Ok "$baseBefore quarantine rules"

Step "opening a real room"
$out = Join-Path $env:TEMP 'kanpachi-reset.out'
$daemon = Start-Process -FilePath (Join-Path $Stage 'kanpachid.exe') `
    -ArgumentList '--console', '--data', $Data -PassThru -WindowStyle Minimized `
    -RedirectStandardOutput $out -RedirectStandardError "$out.err"
Start-Sleep -Seconds 3

$params = (@{ nickname = 'Alvaro'; name = 'Prueba' } | ConvertTo-Json -Compress).Replace('"', '\"')
$before = $ErrorActionPreference
$ErrorActionPreference = 'Continue'
& (Join-Path $Stage 'pipeprobe.exe') -data $Data -params $params create_room 2>&1 | Out-Null
$code = $LASTEXITCODE
$ErrorActionPreference = $before
if ($code -ne 0) { throw "the room could not be created (exit $code), so there is nothing dirty to clean" }

$clock = [Diagnostics.Stopwatch]::StartNew()
while ($clock.Elapsed.TotalSeconds -lt $Timeout) {
    if (Get-NetAdapter -Name "kanpachi0" -ErrorAction SilentlyContinue) { break }
    Start-Sleep -Milliseconds 500
}
Start-Sleep -Seconds 4
Ok "room open"

Step "killing the daemon DIRTILY"
# Without Stop-Process -Force there would be no case to measure: a clean exit
# already cleans up on its own, and what this script checks is the path that
# does not go through there.
Stop-Process -Id $daemon.Id -Force
Start-Sleep -Seconds 2

$dirty = @{
    rules   = HowManyRulesOf 'Kanpachi'
    filters = GateFilters
    room    = Test-Path (Join-Path $Data 'hosted-room.json')
    engine  = @(Get-Process kanpachi-engine -ErrorAction SilentlyContinue).Count
}
Write-Host ("  Kanpachi rules={0} filters={1} hosted-room.json={2} engines={3}" -f `
    $dirty.rules, $dirty.filters, $dirty.room, $dirty.engine)
if ($dirty.rules -eq 0 -and $dirty.filters -eq 0 -and -not $dirty.room) {
    throw "the dirty death left NOTHING to clean, so the reset would prove nothing"
}
Ok "there is real dirt to clean"

# The Job Object takes the engine down with the daemon, so normally none is
# orphaned. Whatever is there gets recorded instead of demanded: manufacturing
# an orphan by hand would measure the harness and not the product.
Step "running kanpachid --reset"
$before = $ErrorActionPreference
$ErrorActionPreference = 'Continue'
& (Join-Path $Stage 'kanpachid.exe') --reset --data $Data 2>&1 | ForEach-Object { Write-Host "  $_" }
$code = $LASTEXITCODE
$ErrorActionPreference = $before
if ($code -ne 0) { Fail "the reset exited with $code"; $failures++ }

Step "what was left afterwards"
$rules = HowManyRulesOf 'Kanpachi'
if ($rules -eq 0) { Ok "zero rules of the Kanpachi group" }
else { Fail "$rules rules of the Kanpachi group were left"; $failures++ }

$filters = GateFilters
if ($filters -eq 0) { Ok "zero gate filters" }
else { Fail "$filters gate filters were left, and no system tool shows them"; $failures++ }

# What separates the reset from the uninstaller.
$baseAfter = HowManyRulesOf 'Kanpachi-base'
if ($baseAfter -eq $baseBefore) { Ok "the base quarantine is still whole: $baseAfter rules" }
else { Fail "the quarantine went from $baseBefore to $baseAfter rules. The reset RESTORES it, it does not remove it"; $failures++ }

$engines = @(Get-Process kanpachi-engine -ErrorAction SilentlyContinue).Count
if ($engines -eq 0) { Ok "no orphaned engine" }
else { Fail "$engines engine(s) were left alive"; $failures++ }

if (Test-Path (Join-Path $Data 'hosted-room.json')) {
    Fail "hosted-room.json is still there, so the next start would offer to reopen a room that no longer exists"
    $failures++
}
else { Ok "hosted-room.json deleted" }

# last-room.json is kept on purpose: resetting the configuration is not
# forgetting which room to go back to.
if (Test-Path (Join-Path $Data 'last-room.json')) { Ok "last-room.json untouched" }
else { Write-Host "  --   there was no last-room.json to keep" -ForegroundColor Yellow }

Step "and after the reset a room can be created again"
$daemon2 = Start-Process -FilePath (Join-Path $Stage 'kanpachid.exe') `
    -ArgumentList '--console', '--data', $Data -PassThru -WindowStyle Minimized `
    -RedirectStandardOutput "$out.2" -RedirectStandardError "$out.2.err"
Start-Sleep -Seconds 3
$before = $ErrorActionPreference
$ErrorActionPreference = 'Continue'
& (Join-Path $Stage 'pipeprobe.exe') -data $Data -params $params create_room 2>&1 | Out-Null
$code = $LASTEXITCODE
$ErrorActionPreference = $before
if ($code -eq 0) { Ok "the new room was created" }
else { Fail "a room could not be created after the reset (exit $code)"; $failures++ }

Stop-Process -Id $daemon2.Id -Force -ErrorAction SilentlyContinue
Start-Sleep -Seconds 2
Get-Process kanpachi-engine -ErrorAction SilentlyContinue | Stop-Process -Force
& (Join-Path $Stage 'kanpachid.exe') --reset --data $Data 2>&1 | Out-Null

Step "result"
if ($failures -eq 0) {
    Write-Host "  A dirty death cleans up in one go, and the quarantine survives." -ForegroundColor Green
    exit 0
}
Write-Host "  $failures check(s) failed." -ForegroundColor Red
exit 1
