<#
.SYNOPSIS
    Exercises the netcfg paths a normal room never touches.

.DESCRIPTION
    Comparing what is written against what is executed left an uncomfortable
    gap: creating a room tests the metric, the MTU and the route sweep, and
    NOTHING ELSE. The broadcast and multicast routes are only asked for by a
    game profile, so is the prefix policy, and deleting a default route only
    runs when there is one to delete, which on a healthy machine never happens.

    This script opens a real room, leaves the adapter up, and runs
    netcfgprobe.exe against it. It verifies by asking the SYSTEM, never netcfg's
    own memory.

    It also goes through the daemon log looking for the Peers() failure, which
    was in there the whole time without anybody seeing it: the log is UTF-16 and
    a naive grep finds nothing inside it.

    Needs an elevated console.
#>
[CmdletBinding()]
param(
    [string]$Stage = "C:\kt\stage",
    [string]$Data = "$env:ProgramData\Kanpachi",
    [string]$Room = "Prueba",
    [string]$Nick = "Alvaro",
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

foreach ($f in 'kanpachid.exe', 'kanpachi-engine.exe', 'pipeprobe.exe', 'netcfgprobe.exe') {
    if (-not (Test-Path (Join-Path $Stage $f))) { throw "$f is missing from $Stage" }
}

$failures = 0
$daemon = $null
$daemonOut = Join-Path $env:TEMP 'kanpachi-netcfg.out'
$daemonErr = Join-Path $env:TEMP 'kanpachi-netcfg.err'

try {
    Step "starting the daemon"
    $daemon = Start-Process -FilePath (Join-Path $Stage 'kanpachid.exe') `
        -ArgumentList '--console', '--data', $Data `
        -PassThru -WindowStyle Minimized `
        -RedirectStandardOutput $daemonOut -RedirectStandardError $daemonErr
    Start-Sleep -Seconds 3
    if ($daemon.HasExited) {
        Write-Host (Get-Content $daemonOut, $daemonErr -ErrorAction SilentlyContinue | Out-String)
        throw "The daemon died on startup (exit $($daemon.ExitCode))."
    }
    Ok "daemon PID $($daemon.Id)"

    Step "creating the room"
    $params = (@{ nickname = $Nick; name = $Room } | ConvertTo-Json -Compress).Replace('"', '\"')
    $before = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    $ctl = & (Join-Path $Stage 'pipeprobe.exe') -data $Data -params $params create_room 2>&1
    $code = $LASTEXITCODE
    $ErrorActionPreference = $before
    Write-Host ($ctl -join "`n")
    if ($code -ne 0) { Fail "pipeprobe exited with $code"; $failures++ }

    Step "waiting for the adapter (up to $Timeout s)"
    $clock = [Diagnostics.Stopwatch]::StartNew()
    $ad = $null
    while ($clock.Elapsed.TotalSeconds -lt $Timeout) {
        $ad = Get-NetAdapter -Name "kanpachi0" -ErrorAction SilentlyContinue
        if ($ad) { break }
        Start-Sleep -Milliseconds 500
    }
    if (-not $ad) { throw "kanpachi0 never appeared: with no adapter this harness measures nothing" }
    Ok "kanpachi0 up, status $($ad.Status)"
    Start-Sleep -Seconds 5

    # The member list is asked for on purpose. It was broken through every
    # earlier measurement and none of them noticed, because a room holds up
    # without it: what falls over is knowing who is inside.
    Step "the members, which is what was broken"
    $before = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    & (Join-Path $Stage 'pipeprobe.exe') -data $Data status 2>&1 | ForEach-Object { Write-Host "  $_" }
    $ErrorActionPreference = $before

    # The gate is asked of the SYSTEM, not of the daemon.
    #
    # A WFP filter shows up neither in wf.msc nor in Get-NetFirewallRule, so the
    # only way to see it from outside the product is netsh. Asking the daemon
    # whether it put the gate up would be asking whoever put it up, and its own
    # log already answers that.
    Step "the gate, asking WFP"
    $before = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    $wfp = & netsh.exe wfp show filters file=- 2>&1 | Out-String
    $ErrorActionPreference = $before

    $blocks = @(
        @{ what = 'room, by adapter'; pattern = 'bloqueo de todo, por adaptador de la sala' },
        @{ what = 'room, by range';   pattern = 'bloqueo de todo, por rango de la sala' },
        @{ what = 'room, IPv6';       pattern = 'bloqueo de todo IPv6, por adaptador de la sala' },
        @{ what = 'lobby, by adapter'; pattern = 'bloqueo de todo, por adaptador del vest' },
        @{ what = 'lobby, by range';   pattern = 'bloqueo de todo, por rango del vest' },
        @{ what = 'lobby, IPv6';       pattern = 'bloqueo de todo IPv6, por adaptador del vest' }
    )
    foreach ($b in $blocks) {
        if ($wfp -match [regex]::Escape($b.pattern)) { Ok "in place: $($b.what)" }
        else { Fail "MISSING the block for $($b.what)"; $failures++ }
    }
    # And at least one mirror permit, or the gate would be closing the host's
    # own control channel.
    if ($wfp -match 'permiso espejo') { Ok "there are mirror permits" }
    else { Fail "not one mirror permit: the gate closes even our own channel"; $failures++ }

    # Here is the point of the script. With the room alive, netcfgprobe runs the
    # paths a room on its own never exercises.
    Step "netcfgprobe: the paths a room does not exercise"
    $before = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    & (Join-Path $Stage 'netcfgprobe.exe') -data $Data 2>&1 | ForEach-Object { Write-Host $_ }
    $code = $LASTEXITCODE
    $ErrorActionPreference = $before
    if ($code -ne 0) { Fail "netcfgprobe exited with $code"; $failures++ }
    else { Ok "netcfgprobe green" }

    # The state the harness leaves has to be the one it found. If a made-up
    # route survives the harness, the harness is worse than not measuring.
    Step "the harness left no leftovers"
    $left = @(Get-NetRoute -InterfaceAlias "kanpachi0" -ErrorAction SilentlyContinue |
        Where-Object {
            $_.DestinationPrefix -eq '0.0.0.0/0' -or
            $_.DestinationPrefix -eq '255.255.255.255/32' -or
            $_.DestinationPrefix -eq '224.0.0.0/4'
        })
    if ($left.Count -gt 0) {
        Fail "$($left.Count) route(s) of the harness were left behind:"
        $left | ForEach-Object { Fail "    $($_.DestinationPrefix)" }
        $failures++
    }
    else { Ok "no route of the harness survived" }
}
catch {
    Fail "the script broke: $_"
    Write-Host $_.ScriptStackTrace -ForegroundColor DarkGray
    $failures++
}
finally {
    if ($daemon -and -not $daemon.HasExited) { Stop-Process -Id $daemon.Id -Force }
    Get-Process kanpachi-engine -ErrorAction SilentlyContinue | Stop-Process -Force
    Start-Sleep -Seconds 2
}

# The daemon log is read ALWAYS, and not as decoration. The Peers() failure was
# written in there while an earlier measurement went green, because the script
# looked at the adapter, the metric and the routes, and nobody looked at the
# log. A warning nobody reads is a failure nobody sees.
Step "the daemon log"
if (-not (Test-Path $daemonOut)) { Write-Host "  --   there is no $daemonOut" -ForegroundColor Yellow }
else {
    $text = Get-Content $daemonOut -ErrorAction SilentlyContinue
    $peers = @($text | Where-Object { $_ -match 'miembros de la sala' })
    if ($peers.Count -gt 0) {
        Fail "Peers() is still failing, $($peers.Count) time(s):"
        $peers | Select-Object -Last 3 | ForEach-Object { Fail "    $_" }
        $failures++
    }
    else { Ok "no error querying the room members" }

    $errors = @($text | Where-Object { $_ -match '^(aviso|error) ' })
    if ($errors.Count -gt 0) {
        Write-Host "  --   $($errors.Count) warning or error line(s) in the log:" -ForegroundColor Yellow
        $errors | Select-Object -Last 12 | ForEach-Object { Write-Host "      $_" -ForegroundColor DarkGray }
    }
    else { Ok "not one warning and not one error in the whole startup" }
}

Step "result"
if ($failures -eq 0) {
    Write-Host "  The paths a room does not exercise do what they say." -ForegroundColor Green
    exit 0
}
Write-Host "  $failures check(s) failed." -ForegroundColor Red
exit 1
