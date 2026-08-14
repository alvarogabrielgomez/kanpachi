<#
.SYNOPSIS
    The engine's four failures, measured with the whole product running.

.DESCRIPTION
    All four were fixed at the time and no package test can see any of them:
    all four are about WIRING or TIMING. This script exercises them with the
    real daemon, against kanpachi.accentio.dev.

      1. The event channel. It used to create one per process and returned it
         CLOSED while there was none, so "it has not started yet" and "it died"
         were the same fact to the supervisor. The measured symptom was the
         watchdog burning its eight attempts with the daemon just started and
         no room at all, and closing a room the user never created. It is
         measured the other way round: with the daemon idle there cannot be a
         single restart.

      2. The process lifetime context. It used to hand spawn the CALL's
         context, which carries a defer cancel, so the engine died as soon as
         it answered the order that had started it. Nothing gave it away
         because the answer arrived first. It is measured by killing the engine
         brutally and checking that the watchdog brings it back and the room
         returns.

      3. The exit over stdin. Closing the daemon CLEANLY has to take the engine
         with it, no orphans.

      4. Garbage over the command channel has to come back as an error with an
         id, and neither hang the call nor bring the engine down.

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
$out = Join-Path $env:TEMP 'kanpachi-engine.out'
$daemon = $null

# $argv and not $args: $args is a PowerShell automatic variable and assigning
# over it is the kind of thing that works until it does not.
function Ctl($method, $params) {
    $before = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    $argv = @('-data', $Data)
    if ($params) { $argv += @('-params', $params) }
    $argv += $method
    $output = & (Join-Path $Stage 'pipeprobe.exe') @argv 2>&1 | Out-String
    $ErrorActionPreference = $before
    $output
}

try {
    Step "starting the daemon"
    $daemon = Start-Process -FilePath (Join-Path $Stage 'kanpachid.exe') `
        -ArgumentList '--console', '--data', $Data -PassThru -WindowStyle Minimized `
        -RedirectStandardOutput $out -RedirectStandardError "$out.err"
    Start-Sleep -Seconds 3
    if ($daemon.HasExited) { throw "the daemon died on startup" }

    # FAILURE 1. An idle daemon, with NO room, cannot see an engine die that
    # never started. The closed channel used to read as death and the watchdog
    # burned its eight attempts right here.
    Step "1. an idle daemon restarts nothing"
    Start-Sleep -Seconds 12
    $log = Get-Content $out -ErrorAction SilentlyContinue | Out-String
    if ($log -match 'reiniciando el motor' -or $log -match 'el watchdog se rinde') {
        Fail "the watchdog acted with the daemon idle and no room"
        $failures++
    }
    else { Ok "not one restart with the daemon idle" }
    if (Get-Process kanpachi-engine -ErrorAction SilentlyContinue) {
        Fail "there is an engine running without anybody asking for a room"
        $failures++
    }
    else { Ok "no engine was started until it was needed" }

    # FAILURE 4. Before opening anything, because it needs no room.
    Step "4. garbage over the command channel comes back as an error"
    $r = Ctl 'no_existe_este_metodo' $null
    if ($r -match '"error"' -and $r -match '"id"') { Ok "error with id: $($r.Trim())" }
    else { Fail "an unknown order did not return an error with an id: $r"; $failures++ }
    if (-not (Get-Process -Id $daemon.Id -ErrorAction SilentlyContinue)) {
        Fail "the daemon went down over an unknown order"; $failures++
    }

    Step "opening the room"
    $params = (@{ nickname = 'Alvaro'; name = 'Prueba' } | ConvertTo-Json -Compress).Replace('"', '\"')
    $r = Ctl 'create_room' $params
    if ($r -notmatch '"result"') { throw "the room could not be created: $r" }
    $clock = [Diagnostics.Stopwatch]::StartNew()
    while ($clock.Elapsed.TotalSeconds -lt $Timeout) {
        if (Get-NetAdapter -Name "kanpachi0" -ErrorAction SilentlyContinue) { break }
        Start-Sleep -Milliseconds 500
    }
    $engine = Get-Process kanpachi-engine -ErrorAction SilentlyContinue
    if (-not $engine) { throw "the room was created with no engine, so there is nothing to measure" }
    Ok "room open, engine PID $($engine.Id)"

    # FAILURE 2. Killing it BRUTALLY is the point: a clean exit does not
    # exercise the watchdog. What is measured is that the engine COMES BACK and
    # the room stays standing.
    Step "2. killing the engine brutally, the watchdog brings it back"
    $old = $engine.Id
    Stop-Process -Id $old -Force
    Start-Sleep -Seconds 2

    $clock = [Diagnostics.Stopwatch]::StartNew()
    $new = $null
    while ($clock.Elapsed.TotalSeconds -lt 60) {
        $p = Get-Process kanpachi-engine -ErrorAction SilentlyContinue
        if ($p -and $p.Id -ne $old) { $new = $p; break }
        Start-Sleep -Milliseconds 500
    }
    if (-not $new) {
        Fail "the engine did not come back in 60 s, and the watchdog has a 198 s ladder"
        $failures++
    }
    else {
        Ok "the engine came back as PID $($new.Id) in $([int]$clock.Elapsed.TotalSeconds) s"
        # And the room has to still be the same one, not a new one.
        $clock2 = [Diagnostics.Stopwatch]::StartNew()
        $back = $false
        while ($clock2.Elapsed.TotalSeconds -lt 60) {
            $st = Ctl 'status' $null
            if ($st -match '"conn":"connected"') { $back = $true; break }
            Start-Sleep -Seconds 2
        }
        if ($back) { Ok "the room went back to connected" }
        else { Fail "the room did not go back to connected after the engine restart"; $failures++ }

        # BOTH OF THEM. Looking only at kanpachi0 is how this script went green
        # over a real failure: the room came back, the lobby did not, and from
        # then on the door rule could not be written because its adapter no
        # longer existed. The room stayed standing with the door shut forever.
        foreach ($name in 'kanpachi0', 'kanpachi1') {
            $clock3 = [Diagnostics.Stopwatch]::StartNew()
            $ad = $null
            while ($clock3.Elapsed.TotalSeconds -lt 30) {
                $ad = Get-NetAdapter -Name $name -ErrorAction SilentlyContinue
                if ($ad) { break }
                Start-Sleep -Milliseconds 500
            }
            if ($ad) { Ok "$name came back up" }
            else { Fail "$name did NOT come back after restarting the engine"; $failures++ }
        }

        # And the door rule has to be written again. It follows from the above,
        # and it is what the user sees: without it nobody new can come in.
        $fw = New-Object -ComObject HNetCfg.FwPolicy2
        $door = @($fw.Rules | Where-Object { $_.Grouping -eq 'Kanpachi' -and $_.Name -like '*puerta*' })
        if ($door.Count -gt 0) {
            Ok "the door rule is written, over [$($door[0].Interfaces -join ',')]"
        }
        else { Fail "no door rule was left: nobody new can come in"; $failures++ }

        # And the gate has to cover the NEW adapters. A new adapter has a new
        # LUID, so a gate that is not rescoped ends up pointing at one that no
        # longer exists, without anything failing.
        $wfp = & netsh.exe wfp show filters file=- 2>&1 | Out-String
        $covered = 0
        foreach ($p in 'por adaptador de la sala', 'por adaptador del vest') {
            if ($wfp -match [regex]::Escape($p)) { $covered++ }
        }
        if ($covered -eq 2) { Ok "the gate covers both adapters again" }
        else { Fail "the gate only covers $covered of 2 adapters after the restart"; $failures++ }
    }

    # FAILURE 3. The CLEAN exit. It is the other half of the Job Object, which
    # is already measured with a dirty death in measure-reset.ps1: here what is
    # checked is that the exit over stdin also takes it, leaving no orphans.
    Step "3. closing the daemon CLEANLY, the engine goes with it"
    $daemon.CloseMainWindow() | Out-Null
    Stop-Process -Id $daemon.Id -ErrorAction SilentlyContinue
    $daemon.WaitForExit(20000) | Out-Null
    $daemon = $null
    Start-Sleep -Seconds 4
    $orphans = @(Get-Process kanpachi-engine -ErrorAction SilentlyContinue)
    if ($orphans.Count -eq 0) { Ok "no orphaned engines" }
    else {
        Fail "$($orphans.Count) engine(s) were left with the daemon closed, which is a virtual network up and the firewall already purged"
        $orphans | Stop-Process -Force
        $failures++
    }
    $left = Get-NetAdapter -Name "kanpachi*" -ErrorAction SilentlyContinue
    if ($left) { Fail "adapters were left: $($left.Name -join ', ')"; $failures++ }
    else { Ok "no loose virtual adapters" }
}
catch {
    Fail "the script broke: $_"
    Write-Host $_.ScriptStackTrace -ForegroundColor DarkGray
    $failures++
}
finally {
    if ($daemon -and -not $daemon.HasExited) { Stop-Process -Id $daemon.Id -Force }
    Get-Process kanpachi-engine -ErrorAction SilentlyContinue | Stop-Process -Force
    Start-Sleep -Seconds 1
}

Step "the daemon log"
$log = Get-Content $out -ErrorAction SilentlyContinue
$warnings = @($log | Where-Object { $_ -match '^(aviso|error) ' })
if ($warnings.Count -gt 0) {
    Write-Host "  --   $($warnings.Count) warning(s):" -ForegroundColor Yellow
    $warnings | Select-Object -Last 10 | ForEach-Object { Write-Host "      $_" -ForegroundColor DarkGray }
}
else { Ok "not one warning in the whole run" }

Step "result"
if ($failures -eq 0) {
    Write-Host "  The engine's four failures are still closed, with the whole product running." -ForegroundColor Green
    exit 0
}
Write-Host "  $failures check(s) failed." -ForegroundColor Red
exit 1
