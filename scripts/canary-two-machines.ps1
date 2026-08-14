<#
.SYNOPSIS
  Measures Kanpachi Protection with two real machines, in three phases.

.DESCRIPTION
  This header IS the runbook. It used to live loose in fwprobe's usage text,
  which meant only somebody who already knew it existed ever read it.

  # What gets measured, and why THREE phases are needed

  On 2026-08-04 it was measured against the droplet over Tailscale that Windows
  returns no RST inbound even with the port allowed. That is stealth mode, and
  its consequence governs this whole design: a silent port does not distinguish
  "blocked" from "nobody is listening".

  The canary removes that ambiguity the only way left: it puts somebody behind
  the door ON PURPOSE. Knowing for certain there is a listener, the silence has
  only one reading.

  With two phases that still proves nothing about the DIRECTION of the change.
  If the firewall was already up from an earlier run, "silence" does not say who
  put it there. That is why there are three:

    Phase 0   gate purged     the canary ANSWERS over TCP and UDP, and is touched
    Phase 1   gate IN PLACE   SILENCE over both, and it is NOT touched
    Phase 2   gate purged     it answers over both again, and is touched

  Phase 2 is not optional. Without it, a silent phase 1 is explained just as
  well by "the gate works" as by "the canary never got to open".

  # The two assertions that catch real defects

  UDP as well as TCP in phase 1. A TCP-only silence passes with a gate that only
  blocks TCP, and UDP is the protocol the most worrying tool speaks over.

  Touched has to MATCH the remote report in all three phases. A disagreement is
  a bug in the canary or in the probe, and it is exactly the CanaryMismatch the
  domain models: the host says one thing and the member another.

  # The discipline

  The gate goes up or down in its OWN step, separate from the one that measures.
  A loose state proves nothing: what gets measured is the transition.

  The finally runs on every path, Ctrl+C included: it purges the gate and kills
  the canary. The gate cannot survive a failed run, and neither can an open
  socket in a process next door to SYSTEM.

.PARAMETER Remote
  Where to run the probe, in ssh form. E.g. user@100.64.1.5

.PARAMETER LocalIP
  THIS machine's IP on the virtual adapter, which is where the canary binds.
  Never 0.0.0.0: binding on every interface would open a port on the home
  network of whoever runs it, and the adapter itself rejects that.

.PARAMETER Adapter
  Name of the virtual adapter, the one from `fwprobe adapters`.

.PARAMETER PeerIP
  The other machine's virtual IP, for the gate's permit.

.PARAMETER DataDir
  The data directory fwprobe uses.

.PARAMETER RemoteFwprobe
  Path to the binary on the other machine. It has to be there before starting:

    GOOS=linux go build -o /tmp/fwprobe ./internal/fwprobe
    scp /tmp/fwprobe user@100.64.1.5:~/fwprobe

.EXAMPLE
  # ELEVATED console, because the gate writes into the filtering engine.
  pwsh scripts/canary-two-machines.ps1 `
      -Remote user@100.64.1.5 -LocalIP 100.64.1.1 `
      -Adapter kanpachi0 -PeerIP 100.64.1.5 -DataDir C:\kanpachi-datos
#>

[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$Remote,
    [Parameter(Mandatory = $true)][string]$LocalIP,
    [Parameter(Mandatory = $true)][string]$Adapter,
    [Parameter(Mandatory = $true)][string]$PeerIP,
    [Parameter(Mandatory = $true)][string]$DataDir,
    [string]$RemoteFwprobe = './fwprobe',

    # The room's range. It has to be a /24 inside the space where rooms live,
    # and `wfp.Scope.Valid` demands it: a wider prefix would turn the
    # block-everything into the block of a network that is not ours.
    #
    # It exists as a parameter because the gate's block is emitted TWICE, by
    # adapter and by range. Leaving the default, the second one would not match
    # this test's real traffic and the adapter one would be left as the only
    # handhold, which is exactly the situation the design emits two filters to
    # avoid. Pass the /24 that contains -LocalIP.
    [string]$RoomCIDR = '100.64.1.0/24',

    # The port the gate leaves open in phase 1. It is NOT the canary's: the
    # canary takes an ephemeral one nobody chooses, and the whole point is that
    # it falls into "everything else". This one exists because the gate needs at
    # least one permit so as not to be a total block.
    [int]$OpenPort = 45871,

    # Budgets, against the measured baseline of 3376 ms for a real round with
    # ssh included. A generous deadline stops being a safety net.
    [int]$PhaseTimeoutSec = 15,
    [int]$TotalTimeoutSec = 60
)

$ErrorActionPreference = 'Stop'

# The repo root is derived from where the script is, NEVER from the current
# directory.
#
# Measured: launched through UAC, the elevated process starts in system32, so a
# relative `go build ./internal/fwprobe` finds nothing. And this is how this
# script will always be run, because phases 1 and 2 write into the filtering
# engine and that demands elevation.
$root = Split-Path -Parent $PSScriptRoot

# The binary is built once and reused. Running it with `go run` would put the
# build time inside each phase's budget.
$fwprobe = Join-Path $env:TEMP 'kanpachi-fwprobe.exe'

# State the finally needs to see. It goes up here because the finally can run on
# Ctrl+C before the phase that would fill it has even started.
$script:canary = $null
$script:gateUp = $false
$script:failures = @()

function Write-Step {
    param([string]$Text)
    Write-Host ''
    Write-Host "== $Text" -ForegroundColor Cyan
}

# Every command is printed before running it. Without that, a run that goes
# wrong cannot be reproduced by hand, which is the first thing anybody wants to
# do.
#
# # Failure is decided by the EXIT CODE, never by stderr
#
# With `$ErrorActionPreference = 'Stop'` set above, a `2>&1` over a native
# executable wraps every stderr line in an ErrorRecord, and that ABORTS the
# script even though the program finished successfully. Measured: the first run
# died there.
#
# It is not a detail of this function. `ssh` writes warnings to stderr
# routinely (new host key, deprecated algorithm), so phase 1 would have fallen
# over without saying why, and a phase that never gets to measure reads just as
# badly as one that measures and fails.
#
# The fix is the right one: the preference is lowered while the child process
# runs, its stderr is joined with its stdout so it can be shown, and it is
# judged by $LASTEXITCODE, which is the signal an exe actually uses to say it
# failed.
function Invoke-Showing {
    param([string]$Exe, [string[]]$Arguments, [switch]$AllowFailure)

    Write-Host "   $Exe $($Arguments -join ' ')" -ForegroundColor DarkGray

    $previous = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        $output = & $Exe @Arguments 2>&1 | Out-String
        $code = $LASTEXITCODE
    } finally {
        $ErrorActionPreference = $previous
    }

    if ($output.Trim()) { Write-Host $output.TrimEnd() }
    if ($code -ne 0 -and -not $AllowFailure) {
        throw "$Exe exited with code $code"
    }
    return $output
}

function Assert-Equal {
    param([string]$What, $Expected, $Actual, [string]$Why)

    if ($Expected -eq $Actual) {
        Write-Host "   OK   $What = $Actual" -ForegroundColor Green
        return
    }
    Write-Host "   FAIL $What : expected $Expected and got $Actual" -ForegroundColor Red
    Write-Host "        $Why" -ForegroundColor Red
    $script:failures += "$What : expected $Expected, actual $Actual. $Why"
}

# Starts the canary and returns its port and nonce.
#
# It is launched as a SEPARATE process and not in the foreground because it has
# to keep listening while the other machine dials. Its output goes to a file,
# which is where the port, the nonce and, at the end, its own verdict are read
# from.
function Start-Canary {
    param([string]$Addr)

    $log = Join-Path $env:TEMP "kanpachi-canary-$([System.Guid]::NewGuid().ToString('N')).txt"
    Write-Host "   $fwprobe canary -addr $Addr" -ForegroundColor DarkGray
    $p = Start-Process -FilePath $fwprobe -ArgumentList @('canary', '-addr', $Addr) `
        -RedirectStandardOutput $log -RedirectStandardError "$log.err" `
        -NoNewWindow -PassThru

    # It waits for the port to be PRINTED, not for a fixed time. A hand-picked
    # sleep either measures before it opens, or gives away seconds of the
    # phase's budget.
    $deadline = (Get-Date).AddSeconds(8)
    $port = 0
    $nonce = ''
    while ((Get-Date) -lt $deadline) {
        if (Test-Path $log) {
            $text = Get-Content $log -Raw -ErrorAction SilentlyContinue
            if ($text -match 'canary-probe -host \S+ -port (\d+) -nonce ([0-9a-fA-F]+)') {
                $port = [int]$Matches[1]
                $nonce = $Matches[2]
                break
            }
        }
        Start-Sleep -Milliseconds 100
    }

    if ($port -eq 0) {
        throw "the canary never got to open in 8 s. Its output is in $log"
    }

    Write-Host "   canary listening on ${Addr}:$port" -ForegroundColor DarkGray
    return [pscustomobject]@{
        Process = $p
        Log     = $log
        Port    = $port
        Nonce   = $nonce
    }
}

# Reads the canary's OWN verdict, which is the only unfalsifiable fact in this
# whole measurement: the host's socket saw it, not a message from anybody.
#
# # How the two cases are read, and they are not symmetric
#
# The YES is a direct statement: the canary prints "LO TOCARON" and stops on its
# own.
#
# The NO is the ABSENCE of that line. An untouched canary has to be stopped, and
# it is stopped by force, so it never gets to print its closing line. Reading an
# absence is only valid because Start-Canary already proved the socket opened:
# without that earlier proof, "it said nothing" and "it never existed" would
# look the same, which is exactly the ambiguity this script exists to remove.
#
# The line is searched without accents on purpose. "LO TOCARON" is pure ASCII
# and survives any console; the rest of the canary's text carries accents that
# arrive mangled depending on the code page.
function Read-Touched {
    param($Canary, [int]$WaitSec)

    # It gets some room to write its final line. When it is touched it stops on
    # its own and that is immediate; when it is not, somebody has to stop it.
    $deadline = (Get-Date).AddSeconds($WaitSec)
    while ((Get-Date) -lt $deadline -and -not $Canary.Process.HasExited) {
        Start-Sleep -Milliseconds 100
    }
    if (-not $Canary.Process.HasExited) {
        Stop-Process -Id $Canary.Process.Id -Force -ErrorAction SilentlyContinue
        $Canary.Process.WaitForExit(3000) | Out-Null
    }

    $text = Get-Content $Canary.Log -Raw -ErrorAction SilentlyContinue
    if ($text) { Write-Host $text.TrimEnd() -ForegroundColor DarkGray }
    return [bool]($text -match 'LO TOCARON')
}

function Stop-Canary {
    if ($null -eq $script:canary) { return }
    if (-not $script:canary.Process.HasExited) {
        Stop-Process -Id $script:canary.Process.Id -Force -ErrorAction SilentlyContinue
    }
    $script:canary = $null
}

function Set-Gate {
    param([switch]$Up)

    if ($Up) {
        Invoke-Showing $fwprobe @(
            'apply', '-data', $DataDir, '-adapter', $Adapter, '-room', $RoomCIDR,
            '-peer', $PeerIP, '-open', "$OpenPort", '-yes') | Out-Null
        $script:gateUp = $true
        return
    }
    Invoke-Showing $fwprobe @('purge', '-data', $DataDir) | Out-Null
    $script:gateUp = $false
}

# A whole phase: four separate steps, and the gate's is one of them.
function Invoke-Phase {
    param(
        [int]$Number,
        [string]$Title,
        [bool]$WithGate,
        [bool]$ExpectedToArrive
    )

    Write-Step "PHASE $Number  $Title"
    $clock = [System.Diagnostics.Stopwatch]::StartNew()

    # Step 1: the gate, alone and in its own step.
    if ($WithGate) { Set-Gate -Up } else { Set-Gate }

    # Step 2: the local canary.
    $script:canary = Start-Canary -Addr $LocalIP

    # Step 3: the other machine dials, over BOTH protocols.
    # AllowFailure: a dead ssh cannot abort the whole run. The phase is left
    # without a measurement, and the assertions below catch that with a message
    # saying which one it was, instead of dying halfway with the gate up.
    $probe = Invoke-Showing 'ssh' @(
        $Remote, $RemoteFwprobe, 'canary-probe',
        '-host', $LocalIP, '-port', "$($script:canary.Port)",
        '-nonce', $script:canary.Nonce) -AllowFailure

    # The prefix is searched without its accent on purpose. The probe prints
    # "CONTESTO" with an accent and the other machine is Linux in UTF-8, so the
    # accent can arrive mangled depending on the console. "CONTEST" does not
    # collide with "SILENCIO", which is the only other possible output, and does
    # not depend on how anything is decoded.
    $remoteArrived = $probe -match 'CONTEST'

    # Step 4: what the canary itself saw.
    $touched = Read-Touched -Canary $script:canary -WaitSec 3
    Stop-Canary

    Assert-Equal "phase ${Number}: the remote reaches the canary" $ExpectedToArrive ([bool]$remoteArrived) `
        $(if ($WithGate) {
            'With the gate up a packet crossed anyway. It is exactly the leak the product exists to prevent.'
        } else {
            'With no gate the canary has to answer. If it does not, it never opened or the probe never dialled, and then phase 1 would prove nothing.'
        })

    # The two sources have to say the same thing. A disagreement is
    # CanaryMismatch: the host and the member do not see the same thing, and
    # with the gate up that can be somebody lying.
    Assert-Equal "phase ${Number}: Touched matches the remote report" ([bool]$remoteArrived) $touched `
        'The host socket and the member report do not match. It is a bug in the canary or the probe, or somebody over-reporting.'

    # And separately, the fact of our own against what was expected.
    Assert-Equal "phase ${Number}: Touched" $ExpectedToArrive $touched `
        'It is the only unfalsifiable fact in the measurement: the host socket saw it.'

    # The phase budget gets checked, not just declared. A phase that runs over
    # is usually the canary being slow to open or the probe waiting out a whole
    # deadline, and both change what the measurement means: a silence that
    # arrived because nobody dialled in time reads the same as a silence the
    # gate produced.
    $sec = [int]$clock.Elapsed.TotalSeconds
    if ($sec -gt $PhaseTimeoutSec) {
        Write-Host "   FAIL phase ${Number}: took $sec s, budget $PhaseTimeoutSec s" -ForegroundColor Red
        $script:failures += "phase ${Number}: took $sec s, over the budget of $PhaseTimeoutSec s. A silence from an expired deadline reads the same as one the gate produced."
        return
    }
    Write-Host "   OK   phase ${Number}: $sec s of $PhaseTimeoutSec" -ForegroundColor Green
}

$total = [System.Diagnostics.Stopwatch]::StartNew()
try {
    Write-Step 'Building fwprobe'
    Push-Location $root
    try {
        Invoke-Showing 'go' @('build', '-o', $fwprobe, './internal/fwprobe') | Out-Null
    } finally {
        Pop-Location
    }

    Invoke-Phase -Number 0 -Title 'gate purged: the canary has to answer' `
        -WithGate $false -ExpectedToArrive $true

    Invoke-Phase -Number 1 -Title 'GATE UP: silence over TCP and over UDP' `
        -WithGate $true -ExpectedToArrive $false

    Invoke-Phase -Number 2 -Title 'purged again: it answers once more' `
        -WithGate $false -ExpectedToArrive $true

    if ($total.Elapsed.TotalSeconds -gt $TotalTimeoutSec) {
        $script:failures += "the whole run took $([int]$total.Elapsed.TotalSeconds) s, over the budget of $TotalTimeoutSec s"
    }
}
finally {
    # Runs on EVERY path, Ctrl+C included. The gate cannot survive a failed run,
    # and neither can a canary left open in a process next door to SYSTEM.
    Write-Step 'Cleanup'
    Stop-Canary
    if ($script:gateUp) {
        Write-Host '   the gate was left up: purging' -ForegroundColor Yellow
        # No `throw` and with the preference lowered: this runs INSIDE the
        # finally, so an exception here would cover up the error that got us
        # here, and on top of that would leave the gate up for mis-reporting the
        # attempt to remove it.
        Invoke-Showing $fwprobe @('purge', '-data', $DataDir) -AllowFailure | Out-Null
    } else {
        Write-Host '   the gate was already purged' -ForegroundColor DarkGray
    }
    Write-Host '   if something looks odd, rebooting also fixes it: the gate filters are not persistent' -ForegroundColor DarkGray
}

Write-Host ''
if ($script:failures.Count -eq 0) {
    Write-Host "ALL GREEN in $([int]$total.Elapsed.TotalSeconds) s. The gate contains, and it was proved by the transition." -ForegroundColor Green
    exit 0
}

Write-Host 'FAILURES:' -ForegroundColor Red
foreach ($f in $script:failures) { Write-Host "  - $f" -ForegroundColor Red }
exit 1
