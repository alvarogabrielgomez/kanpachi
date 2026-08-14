<#
.SYNOPSIS
    The seed registry, measured against the real kanpachi.accentio.dev.

.DESCRIPTION
    The contract test already speaks the whole protocol, with both real ends, in
    process and without a network. What is left unmeasured is exactly what that
    test cannot touch: the droplet's server, with its TLS, its reverse proxy and
    its rate limit, and the daemon talking to it from Windows as SYSTEM.

    What gets checked, and why each thing:

      1. Creating a room LEAVES A CARD. Until now every room opened with a
         permanent warning in the log and the page showed the generic one.

      2. Renaming CHANGES the card's bytes. It is the only way to see from the
         outside that the second publication path works.

      3. Renewing the code issues a NEW ID that resolves. The old one stops
         resolving, which is the half that makes renewing mean anything.

      4. A dirty death and reopening GIVES the card back. It is the hole that
         got closed: the registry keeps cards in memory and with an expiry, so
         a room surviving a blackout found itself standing and its card dead.

      5. A FOREIGN key cannot overwrite the invite ID. `dirprobe` does this,
         because PowerShell 5.1 cannot sign Ed25519.

    There are pauses on purpose: the registry limits to 30 requests per minute
    per IP, and it counts the failed ones too.

    Needs an elevated console.
#>
[CmdletBinding()]
param(
    [string]$Stage = "C:\kt\stage",
    [string]$Data = "$env:ProgramData\Kanpachi",
    [string]$Seed = "kanpachi.accentio.dev",
    [int]$Pause = 3
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
$out = Join-Path $env:TEMP 'kanpachi-directory.out'
$daemon = $null

# Ctl calls pipeprobe building the command line BY HAND.
#
# # Why it is not invoked with the call operator and a list
#
# Because PowerShell 5.1 SPLITS the argument on spaces once it already contains
# escaped quotes, and it does it silently. Measured with an argv dumper:
#
#   no spaces:    [1] "{\"nickname\":\"Alvaro\",\"name\":\"Prueba\"}"      whole
#   with spaces:  [1] "{\"nickname\":\"Alvaro\",\"name\":\"Los"            cut
#                 [2] "panas\"}"
#
# The symptom does not look like the cause: pipeprobe gets truncated JSON and
# answers "unexpected end of JSON input", three steps before where the script
# falls over. No combination of quotes, escapes or backticks fixes it; the only
# thing that works is building the whole line so nobody reinterprets it.
#
# The engine measurement script does not suffer it by luck: its room is called
# "Prueba", with no spaces.
function Ctl($method, $params) {
    $line = "-data `"$Data`""
    if ($params) {
        $line += ' -params "' + $params.Replace('"', '\"') + '"'
    }
    $line += " $method"

    $psi = New-Object Diagnostics.ProcessStartInfo
    $psi.FileName = Join-Path $Stage 'pipeprobe.exe'
    $psi.Arguments = $line
    $psi.UseShellExecute = $false
    $psi.RedirectStandardOutput = $true
    $psi.RedirectStandardError = $true
    $p = [Diagnostics.Process]::Start($psi)
    $output = $p.StandardOutput.ReadToEnd() + $p.StandardError.ReadToEnd()
    $p.WaitForExit()
    $output
}

# AnswerTo pulls out the line of the requested METHOD's answer.
#
# pipeprobe GREETS before every call, and that greeting always carries "result".
# So searching for "result" in the whole output goes green over any error, which
# is exactly what happened the first time this was run: create_room failed, the
# script took it as good, and the symptom showed up three steps later as "the
# room reported no code".
function AnswerTo($output, $method) {
    foreach ($l in ($output -split "`r?`n")) {
        if ($l.StartsWith("$method ")) { return $l.Trim() }
    }
    return ""
}

# ErrorIn returns the error text, or empty if the call went fine.
function ErrorIn($output, $method) {
    $l = AnswerTo $output $method
    if (-not $l) {
        # With no line for the method, what is useful is the WHOLE output:
        # that is where the failed greeting, the client timeout, or whatever
        # happened before is.
        return "$method did not answer. Whole output: " + ($output -replace "`r?`n", " | ")
    }
    if ($l -match '"error"') { return $l }
    return ""
}

function StartDaemon() {
    $p = Start-Process -FilePath (Join-Path $Stage 'kanpachid.exe') `
        -ArgumentList '--console', '--data', $Data -PassThru -WindowStyle Minimized `
        -RedirectStandardOutput $out -RedirectStandardError "$out.err"
    Start-Sleep -Seconds 3
    if ($p.HasExited) { throw "the daemon died on startup" }
    $p
}

# Resolve returns the view the registry publishes for an invite ID, or $null.
# It is the SAME query the invitation page makes.
function Resolve($id) {
    Start-Sleep -Seconds $Pause
    $raw = $id.Replace('-', '')
    try {
        $r = Invoke-WebRequest -Uri "https://$Seed/api/i/$raw" -UseBasicParsing -TimeoutSec 20
        return ($r.Content | ConvertFrom-Json)
    }
    catch {
        return $null
    }
}

function CodeFromStatus() {
    $st = Ctl 'status' $null
    if ($st -match '"code"\s*:\s*"([A-Z0-9-]+)"') { return $Matches[1] }
    return ""
}

try {
    Step "starting the daemon"
    $daemon = StartDaemon

    Step "1. creating a room leaves a card in the registry"
    $params = @{ nickname = 'Alvaro'; name = 'Los panas' } | ConvertTo-Json -Compress
    $r = Ctl 'create_room' $params
    $e = ErrorIn $r 'create_room'
    if ($e) { throw "the room could not be created: $e" }
    $code = CodeFromStatus
    if (-not $code) { throw "the room reported no code" }
    Note "code $code"

    $view = Resolve $code
    if (-not $view) {
        Fail "the registry does not know the room that was just created"
        $failures++
    }
    elseif (-not $view.card) {
        Fail "the registry knows the room and has no card"
        $failures++
    }
    else {
        Ok "card published, $($view.card.Length) base64 characters"
        Note "host key $($view.host_key.Substring(0,12))..."
        # An absent members is what is expected if the droplet's counter does
        # not talk to the engine. It gets recorded, not judged: absent tells the
        # truth.
        if ($null -eq $view.members) { Note "members absent, the counter does not know" }
        else { Note "members = $($view.members)" }
    }
    $createdCard = $view.card

    # The check no script can make: the key travels in the link's FRAGMENT,
    # which the browser does not send to the server, so decrypting needs the
    # whole link.
    $link = Ctl 'invite_link' $null
    if ($link -match '(https://[^\s"]+)') {
        Note "open it by hand to see the name: $($Matches[1])"
    }

    Step "2. renaming changes the card"
    $params = @{ name = 'Los panas 2' } | ConvertTo-Json -Compress
    $r = Ctl 'rename_room' $params
    $e = ErrorIn $r 'rename_room'
    if ($e) { Fail "renaming failed: $e"; $failures++ }

    $view = Resolve $code
    if (-not $view) {
        Fail "after renaming the registry stopped knowing the room"
        $failures++
    }
    elseif ($view.card -eq $createdCard) {
        Fail "the card did not change on rename"
        $failures++
    }
    else { Ok "the card changed on rename" }
    $renamedCard = $view.card

    Step "3. renewing the code issues a new one that resolves"
    $r = Ctl 'rotate_invite_code' $null
    $e = ErrorIn $r 'rotate_invite_code'
    if ($e) { Fail "renewing failed: $e"; $failures++ }
    $new = CodeFromStatus
    if (-not $new -or $new -eq $code) {
        Fail "the code did not change: was $code, now $new"
        $failures++
    }
    else {
        Ok "new code $new"
        $view = Resolve $new
        if ($view -and $view.card) { Ok "the new code resolves with a card" }
        else { Fail "the new code does not resolve"; $failures++ }
    }

    Step "4. a dirty death and reopening gives the card back"
    $live = CodeFromStatus
    Stop-Process -Id $daemon.Id -Force
    Start-Sleep -Seconds 4
    Get-Process kanpachi-engine -ErrorAction SilentlyContinue | Stop-Process -Force
    Start-Sleep -Seconds 2

    if (-not (Test-Path (Join-Path $Data 'hosted-room.json'))) {
        Fail "the dirty death left no hosted-room.json, so there is nothing to reopen"
        $failures++
    }
    else { Ok "hosted-room.json was left, which is the sign of a bad close" }

    $daemon = StartDaemon
    $r = Ctl 'resume_room' $null
    $e = ErrorIn $r 'resume_room'
    if ($e) {
        Fail "the room could not be reopened: $e"
        $failures++
    }
    else {
        Ok "room reopened with the code $live"
        $view = Resolve $live
        if ($view -and $view.card) {
            Ok "the card is still published after reopening"
            if ($view.card -ne $renamedCard -and $renamedCard) {
                Note "the bytes changed with respect to before renewing, and that is right:"
                Note "renewing sealed a new card for the new code"
            }
        }
        else { Fail "after reopening the room was left with no card"; $failures++ }
    }

    Step "5. a foreign key cannot overwrite the invite ID"
    $probe = Join-Path $Stage 'dirprobe.exe'
    if (-not (Test-Path $probe)) {
        Fail "dirprobe.exe is missing from the stage: go build -o $probe ./internal/dirprobe"
        $failures++
    }
    else {
        Start-Sleep -Seconds $Pause
        $before = $ErrorActionPreference
        $ErrorActionPreference = 'Continue'
        & $probe -seed $Seed -code $live 2>&1 | ForEach-Object { Write-Host "      $_" -ForegroundColor DarkGray }
        $exitCode = $LASTEXITCODE
        $ErrorActionPreference = $before
        if ($exitCode -eq 0) { Ok "the registry rejected the foreign key" }
        else { Fail "a foreign key got to write"; $failures++ }
    }

    Step "6. and the room still belongs to its owner"
    $view = Resolve $live
    if ($view -and $view.card) { Ok "the host's card is still standing" }
    else { Fail "the host's card disappeared"; $failures++ }
}
catch {
    Fail "the script broke: $_"
    Write-Host $_.ScriptStackTrace -ForegroundColor DarkGray
    $failures++
}
finally {
    if ($daemon -and -not $daemon.HasExited) {
        Ctl 'leave_room' $null | Out-Null
        Start-Sleep -Seconds 2
        Stop-Process -Id $daemon.Id -Force -ErrorAction SilentlyContinue
    }
    Start-Sleep -Seconds 3
    Get-Process kanpachi-engine -ErrorAction SilentlyContinue | Stop-Process -Force
}

Step "this installation's key"
$key = Join-Path $Data 'identity.key'
if (Test-Path $key) {
    $fi = Get-Item $key
    Ok "identity.key exists, $($fi.Length) bytes"
    if ($fi.Length -ne 32) { Fail "and it has to be 32"; $failures++ }
    # The permissions: only SYSTEM and Administrators, without inheritance.
    $acl = Get-Acl $key
    Note "permissions: $(($acl.Access | ForEach-Object { $_.IdentityReference.Value }) -join ', ')"
    if ($acl.AreAccessRulesProtected) { Ok "it does not inherit the directory's permissions" }
    else { Fail "it inherits from the directory, which means every user can read it"; $failures++ }

    # Compared by SID and NEVER by name. This Windows is in Portuguese and
    # SYSTEM is called "AUTORIDADE NT\SISTEMA", so looking for the word SYSTEM
    # gave a red over a correct ACL. SIDs are not translated:
    #   S-1-5-18      LocalSystem
    #   S-1-5-32-544  BUILTIN\Administrators
    $allowed = @('S-1-5-18', 'S-1-5-32-544')
    $foreign = @()
    foreach ($rule in $acl.Access) {
        try {
            $sid = $rule.IdentityReference.Translate([Security.Principal.SecurityIdentifier]).Value
        }
        catch {
            $sid = $rule.IdentityReference.Value
        }
        if ($allowed -notcontains $sid) { $foreign += "$($rule.IdentityReference.Value) ($sid)" }
    }
    if ($foreign.Count -eq 0) { Ok "only SYSTEM and Administrators, checked by SID" }
    else { Fail "there are extra permissions: $($foreign -join ', ')"; $failures++ }
}
else { Fail "identity.key was not created"; $failures++ }

Step "the daemon log"
$log = Get-Content $out -Encoding UTF8 -ErrorAction SilentlyContinue
$warnings = @($log | Where-Object { $_ -match '^(aviso|error) ' })
if ($warnings.Count -gt 0) {
    Write-Host "  --   $($warnings.Count) warning(s):" -ForegroundColor Yellow
    $warnings | Select-Object -Last 12 | ForEach-Object { Write-Host "      $_" -ForegroundColor DarkGray }
}
else { Ok "not one warning in the whole run" }
if ($log -match 'la sala va sin tarjeta') {
    Fail "the log says the room went without a card, which is exactly what this closes"
    $failures++
}

Step "result"
if ($failures -eq 0) {
    Write-Host "  The registry publishes, updates, reopens and refuses whoever is not the owner." -ForegroundColor Green
    exit 0
}
Write-Host "  $failures check(s) failed." -ForegroundColor Red
exit 1
