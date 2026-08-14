<#
.SYNOPSIS
    That the virtual adapter's settings come back on their own when something
    breaks them.

.DESCRIPTION
    The pending test was written as "switch from WiFi to cable with the room
    open". That mixes TWO different questions, and only one of them needs the
    network to change:

      A. If something reverts the metric, the MTU or the routes of the virtual
         adapter, the reapply puts them back. That is the half with risk, and
         it does NOT need the network touched to be measured: they get broken
         by hand and you watch whether they come back. The periodic fallback
         exists exactly for this: AdapterReapplyEvery = 8 beats, about two
         minutes, and it is what covers a dead subscription.

      B. That Windows announces the change and the announcement reaches the
         supervisor. That half does need the network to really change. With no
         cable, the stand-in is disabling and re-enabling the WiFi NIC: the
         whole transport disappears, which is MORE aggressive than unplugging
         a cable.

    The two acts are independent on purpose. Act A is deterministic and
    repeatable; act B depends on Windows classifying the network that comes
    back, and it may not re-identify it because it is the same one as always.
    If A goes green and B does not, what fails is the announcement and not the
    reapply, and the periodic fallback already covers that. Telling them apart
    is the whole point of splitting it.

    It also checks something nobody was looking at: netcfg resolves ONLY
    kanpachi0, so a default route over kanpachi1 would be deleted by nobody.

    Needs an elevated console.

.PARAMETER NoWifi
    Skips act B. Leaves only the part that does not cut the connection.

.PARAMETER WithSSIDChange
    Act C: waits for you to switch networks by hand (a phone hotspot) and
    measures against a DIFFERENT subnet and gateway, which is the exact analogue
    of the cable. It asks nothing: it detects the change by polling.
#>
[CmdletBinding()]
param(
    [string]$Stage = "C:\kt\stage",
    [string]$Data = "$env:ProgramData\Kanpachi",
    [switch]$NoWifi,
    [switch]$OnlyTheCut,
    [switch]$WithSSIDChange,
    [int]$ReapplyTimeout = 200,
    [int]$SSIDTimeout = 180
)

$ErrorActionPreference = 'Stop'

function Step($t) { Write-Host "`n=== $t ===" -ForegroundColor Cyan }
function Ok($t) { Write-Host "  OK   $t" -ForegroundColor Green }
function Fail($t) { Write-Host "  FAIL $t" -ForegroundColor Red }
function Note($t) { Write-Host "  --   $t" -ForegroundColor DarkGray }

$isAdmin = ([Security.Principal.WindowsPrincipal] `
    [Security.Principal.WindowsIdentity]::GetCurrent()
).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if (-not $isAdmin) { throw "An elevated console is needed." }

$failures = 0
$out = Join-Path $env:TEMP 'kanpachi-network-change.out'
$daemon = $null
$wifi = $null
$wifiDown = $false

function Ctl($method, $params) {
    $before = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    $a = @('-data', $Data)
    if ($params) { $a += @('-params', $params) }
    $a += $method
    $output = & (Join-Path $Stage 'pipeprobe.exe') @a 2>&1 | Out-String
    $ErrorActionPreference = $before
    $output
}

# StateOf reads from the SYSTEM what netcfg writes. Asking Windows is the only
# way for the measurement not to repeat the daemon's own belief.
function StateOf($name) {
    $v4 = Get-NetIPInterface -InterfaceAlias $name -AddressFamily IPv4 -ErrorAction SilentlyContinue
    $v6 = Get-NetIPInterface -InterfaceAlias $name -AddressFamily IPv6 -ErrorAction SilentlyContinue
    $default = @(Get-NetRoute -InterfaceAlias $name -ErrorAction SilentlyContinue |
        Where-Object { $_.DestinationPrefix -eq '0.0.0.0/0' -or $_.DestinationPrefix -eq '::/0' })
    [pscustomobject]@{
        Exists     = [bool]$v4
        MetricV4   = if ($v4) { [int]$v4.InterfaceMetric } else { -1 }
        MetricV6   = if ($v6) { [int]$v6.InterfaceMetric } else { -1 }
        Mtu        = if ($v4) { [int]$v4.NlMtu } else { -1 }
        Default    = $default.Count
    }
}

function Show($name, $e) {
    Note "$name  metric v4=$($e.MetricV4) v6=$($e.MetricV6)  mtu=$($e.Mtu)  default routes=$($e.Default)"
}

# WaitFor polls until the condition holds and returns how many seconds it took,
# or -1. Returning the TIME matters: it separates "the event put it back" from
# "the periodic fallback put it back", which takes up to two minutes.
function WaitFor([scriptblock]$cond, [int]$seconds, [string]$what) {
    $clock = [Diagnostics.Stopwatch]::StartNew()
    while ($clock.Elapsed.TotalSeconds -lt $seconds) {
        if (& $cond) { return [int]$clock.Elapsed.TotalSeconds }
        Start-Sleep -Milliseconds 1000
    }
    return -1
}

function LocalSubnet() {
    $r = Get-NetRoute -DestinationPrefix '0.0.0.0/0' -ErrorAction SilentlyContinue |
        Where-Object { $_.InterfaceAlias -notlike 'kanpachi*' } |
        Sort-Object { $_.RouteMetric + (Get-NetIPInterface -InterfaceIndex $_.InterfaceIndex -AddressFamily IPv4).InterfaceMetric } |
        Select-Object -First 1
    if ($r) { "$($r.InterfaceAlias)/$($r.NextHop)" } else { "" }
}

# ConnOf returns the room's LITERAL state, not a yes-or-no.
#
# Comparing against plain "connected" is what made the first run useless: act B
# said "it did not go back to connected" and did not say WHAT it went back to,
# which is the whole difference between "the tunnel did not come back" and "the
# tunnel came back and the label got stuck". Two different failures with two
# different fixes.
$script:LastStatus = ''
function ConnOf() {
    $s = Ctl 'status' $null
    $script:LastStatus = $s
    if ($s -match '"conn"\s*:\s*"([a-z_]+)"') { $Matches[1] } else { '(no conn)' }
}

function RoomConnected([int]$seconds) {
    WaitFor { (ConnOf) -eq 'connected' } $seconds 'room connected'
}

try {
    Step "starting the daemon and opening a room"
    $daemon = Start-Process -FilePath (Join-Path $Stage 'kanpachid.exe') `
        -ArgumentList '--console', '--data', $Data -PassThru -WindowStyle Minimized `
        -RedirectStandardOutput $out -RedirectStandardError "$out.err"
    Start-Sleep -Seconds 3
    if ($daemon.HasExited) { throw "the daemon died on startup" }

    $params = (@{ nickname = 'Alvaro'; name = 'Prueba' } | ConvertTo-Json -Compress).Replace('"', '\"')
    $r = Ctl 'create_room' $params
    if ($r -notmatch '"result"') { throw "the room could not be created: $r" }

    if ((WaitFor { (StateOf 'kanpachi0').Exists } 60 'kanpachi0') -lt 0) {
        throw "kanpachi0 never appeared, so there is nothing to measure"
    }
    if ((RoomConnected 90) -lt 0) { throw "the room never reached connected" }

    # The baseline comes from the system, NOT from constants: that way the
    # expected MTU is the one this machine probed on this network, and the check
    # does not depend on the home network still giving 1500 the day somebody
    # runs this again.
    $base0 = StateOf 'kanpachi0'
    $base1 = StateOf 'kanpachi1'
    Ok "room open"
    Show 'kanpachi0' $base0
    Show 'kanpachi1' $base1
    $netBefore = LocalSubnet
    Note "internet goes out through [$netBefore]"

    if ($base0.MetricV4 -ne 1) { Fail "the IPv4 metric was not 1 to begin with"; $failures++ }
    if ($base0.Default -ne 0) { Fail "kanpachi0 was born with a default route"; $failures++ }
    if ($base1.Exists -and $base1.Default -ne 0) {
        Fail "kanpachi1 has a default route, and netcfg does not look at kanpachi1"
        $failures++
    }

    # ------------------------------------------------------------------ ACT A
    # Break by hand what netcfg writes. Metric 9999 and a default route with
    # metric 9999 add up to well above the real way out, so the route being
    # inserted CANNOT steal traffic while the test lasts. That is the only
    # reason it is safe to insert it.
    Step "A. break the settings by hand and see whether they come back"
    if ($OnlyTheCut) { Note "skipped by -OnlyTheCut" }
    else {

    Set-NetIPInterface -InterfaceAlias 'kanpachi0' -AddressFamily IPv4 `
        -InterfaceMetric 9999 -ErrorAction SilentlyContinue
    try {
        New-NetRoute -DestinationPrefix '0.0.0.0/0' -InterfaceAlias 'kanpachi0' `
            -NextHop '0.0.0.0' -RouteMetric 9999 -PolicyStore ActiveStore `
            -Confirm:$false -ErrorAction Stop | Out-Null
    }
    catch { Note "the fake default route could not be inserted: $_" }

    $broken = StateOf 'kanpachi0'
    Show 'kanpachi0 broken' $broken
    if ($broken.MetricV4 -ne 9999 -and $broken.Default -eq 0) {
        Fail "nothing could be broken, so act A measures nothing"
        $failures++
    }

    $t = WaitFor {
        $e = StateOf 'kanpachi0'
        $e.MetricV4 -eq $base0.MetricV4 -and $e.Default -eq 0
    } $ReapplyTimeout 'reapply'

    $after = StateOf 'kanpachi0'
    Show 'kanpachi0 after' $after
    if ($t -ge 0) {
        Ok "the settings came back on their own in $t s"
        if ($t -le 20) { Note "fast enough that the Windows announcement triggered it, not the fallback" }
        else {
            Note "it took what the periodic fallback takes, which means the announcement never arrived"
            Note "today that is what is expected: SystemEvents is a stand-in and its channels"
            Note "never emit. When the real one is written, this has to drop to a"
            Note "few seconds, and that jump is its acceptance test"
        }
    }
    else {
        Fail "the settings did NOT come back in $ReapplyTimeout s: metric=$($after.MetricV4) default routes=$($after.Default)"
        $failures++
    }
    if ($after.Mtu -ne $base0.Mtu) {
        Fail "the MTU was left at $($after.Mtu) and it was $($base0.Mtu)"
        $failures++
    }

    # The proof that KANPACHI put it back and not Windows on its own. Without
    # this line, a route that disappears by itself reads the same as one we
    # deleted.
    $log = Get-Content $out -Encoding UTF8 -ErrorAction SilentlyContinue | Out-String
    if ($log -match 'ruta por defecto sobre el adaptador virtual') {
        Ok "the log says the daemon removed the default route"
    }
    else {
        Fail "the route went away and the daemon does not claim to have removed it, so nobody knows who did"
        $failures++
    }

    } # end of act A

    # ------------------------------------------------------------------ ACT B
    if (-not $NoWifi) {
        Step "B. turn the WiFi off and on with the room open"
        $wifi = Get-NetAdapter | Where-Object {
            $_.Status -eq 'Up' -and $_.InterfaceDescription -notlike '*Tailscale*' -and
            $_.Name -notlike 'kanpachi*' -and $_.Name -notlike 'vEthernet*'
        } | Select-Object -First 1
        if (-not $wifi) { throw "the NIC this machine goes out through was not found" }

        # The control. If the room was already degraded beforehand, cutting the
        # WiFi proves nothing about the cut. It gets recorded BEFORE touching
        # the network.
        $connBefore = ConnOf
        Note "before touching the network, the room is at [$connBefore]"
        if ($connBefore -ne 'connected') {
            Fail "the room was already at [$connBefore] before the cut, so act B does not measure the cut"
            $failures++
        }
        Note "cutting through [$($wifi.Name)], it comes back on its own in a few seconds"

        Disable-NetAdapter -Name $wifi.Name -Confirm:$false
        $wifiDown = $true
        Start-Sleep -Seconds 12

        $during = StateOf 'kanpachi0'
        Note "with the WiFi down: kanpachi0 exists=$($during.Exists)"

        Note "with the WiFi down: the room is at [$(ConnOf)]"

        Enable-NetAdapter -Name $wifi.Name -Confirm:$false
        $wifiDown = $false
        $tNet = WaitFor { (Get-NetAdapter -Name $wifi.Name).Status -eq 'Up' } 60 'wifi up'
        if ($tNet -lt 0) { throw "the WiFi did not come back in 60 s" }
        Ok "the WiFi came back in $tNet s"

        # What gets measured on the other side of the cut: both adapters, the
        # settings, the room, the door and the gate. Looking only at kanpachi0
        # is what let the engine restart failure through.
        foreach ($n in 'kanpachi0', 'kanpachi1') {
            if ((WaitFor { (StateOf $n).Exists } 90 $n) -ge 0) { Ok "$n is still up" }
            else { Fail "$n did not come back after the network cut"; $failures++ }
        }

        $tRoom = RoomConnected 150
        if ($tRoom -ge 0) { Ok "the room went back to connected in $tRoom s" }
        else {
            $q = ConnOf
            Fail "the room stayed at [$q] and did not go back to connected in 150 s"
            $failures++
            Note "raw status: $($script:LastStatus.Trim())"
            if ($q -eq 'degraded') {
                Note "degraded is a one-way door: the engine only emits connected"
                Note "when the TUN comes UP, and the TUN never went down"
            }
        }

        $tSettings = WaitFor {
            $e = StateOf 'kanpachi0'
            $e.MetricV4 -eq $base0.MetricV4 -and $e.Default -eq 0 -and $e.Mtu -eq $base0.Mtu
        } $ReapplyTimeout 'settings after the cut'
        $end = StateOf 'kanpachi0'
        Show 'kanpachi0 after the cut' $end
        if ($tSettings -ge 0) { Ok "the settings are still in place after the cut ($tSettings s)" }
        else { Fail "the settings were not right after the network cut"; $failures++ }

        $end1 = StateOf 'kanpachi1'
        if ($end1.Exists -and $end1.Default -ne 0) {
            Fail "a default route appeared over kanpachi1 and nobody is looking at it"
            $failures++
        }

        $fw = New-Object -ComObject HNetCfg.FwPolicy2
        $door = @($fw.Rules | Where-Object { $_.Grouping -eq 'Kanpachi' -and $_.Name -like '*puerta*' })
        if ($door.Count -gt 0) { Ok "the door rule is still written" }
        else { Fail "no door rule was left after the network cut"; $failures++ }

        $wfp = & netsh.exe wfp show filters file=- 2>&1 | Out-String
        $covered = 0
        foreach ($p in 'por adaptador de la sala', 'por adaptador del vest') {
            if ($wfp -match [regex]::Escape($p)) { $covered++ }
        }
        if ($covered -eq 2) { Ok "the gate covers both adapters" }
        else { Fail "the gate only covers $covered of 2 after the cut"; $failures++ }
    }
    else { Step "B. skipped by -NoWifi" }

    # ------------------------------------------------------------------ ACT C
    if ($WithSSIDChange) {
        Step "C. switch networks by hand (a phone hotspot) and this detects it"
        Note "waiting up to $SSIDTimeout s for the way out to internet to change..."
        $tSSID = WaitFor { (LocalSubnet) -ne $netBefore -and (LocalSubnet) -ne "" } $SSIDTimeout 'another network'
        if ($tSSID -lt 0) {
            Note "the network did not change in $SSIDTimeout s, act C has no data"
        }
        else {
            $netAfter = LocalSubnet
            Ok "the way out to internet changed from [$netBefore] to [$netAfter] in $tSSID s"
            $t2 = WaitFor {
                $e = StateOf 'kanpachi0'
                $e.MetricV4 -eq $base0.MetricV4 -and $e.Default -eq 0
            } $ReapplyTimeout 'settings on the new network'
            if ($t2 -ge 0) { Ok "the settings survived the network change ($t2 s)" }
            else { Fail "the settings did not survive the network change"; $failures++ }

            $t3 = RoomConnected 150
            if ($t3 -ge 0) { Ok "the room went back to connected on the new network ($t3 s)" }
            else { Fail "the room did not go back to connected on the new network"; $failures++ }
        }
    }
}
catch {
    Fail "the script broke: $_"
    Write-Host $_.ScriptStackTrace -ForegroundColor DarkGray
    $failures++
}
finally {
    # First of all: if the script died with the WiFi off, the machine is left
    # with no network. Putting it back comes before cleaning anything of ours.
    if ($wifiDown -and $wifi) {
        Enable-NetAdapter -Name $wifi.Name -Confirm:$false -ErrorAction SilentlyContinue
        Note "[$($wifi.Name)] was restored from the cleanup"
    }
    foreach ($n in 'kanpachi0', 'kanpachi1') {
        Get-NetRoute -InterfaceAlias $n -DestinationPrefix '0.0.0.0/0' -ErrorAction SilentlyContinue |
            Remove-NetRoute -Confirm:$false -ErrorAction SilentlyContinue
    }
    if ($daemon -and -not $daemon.HasExited) {
        Stop-Process -Id $daemon.Id -Force -ErrorAction SilentlyContinue
    }
    Start-Sleep -Seconds 3
    Get-Process kanpachi-engine -ErrorAction SilentlyContinue | Stop-Process -Force
}

Step "the daemon log"
$log = Get-Content $out -Encoding UTF8 -ErrorAction SilentlyContinue
$warnings = @($log | Where-Object { $_ -match '^(aviso|error) ' })
if ($warnings.Count -gt 0) {
    Write-Host "  --   $($warnings.Count) warning(s):" -ForegroundColor Yellow
    $warnings | Select-Object -Last 12 | ForEach-Object { Write-Host "      $_" -ForegroundColor DarkGray }
}
else { Ok "not one warning in the whole run" }

Step "result"
if ($failures -eq 0) {
    Write-Host "  The adapter settings come back on their own, and survive the network going down." -ForegroundColor Green
    exit 0
}
Write-Host "  $failures check(s) failed." -ForegroundColor Red
exit 1
