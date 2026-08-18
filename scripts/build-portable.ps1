<#
.SYNOPSIS
    Builds a Kanpachi folder that works on its own, and starts it.

.DESCRIPTION
    One command that builds everything needed, leaves it in a folder, and starts
    the daemon with the permissions it needs. It replaces the manual sequence of
    building the daemon, building the interface, copying the catalog and the
    DLLs, opening an elevated terminal and starting each piece separately.

    WHAT IT LEAVES, and why the folder is portable

    The whole product goes inside: the daemon, the interface with its Flutter
    bundle, the factory catalog, the engine and its DLLs. Plus a
    kanpachi.portable file, which is what turns the folder portable: with it
    present, the daemon keeps its data in kanpachi-data\ right there instead of
    in ProgramData, and starts in its own process instead of as a service. The
    interface reads the same marker and reaches the same conclusion, without
    anybody telling it.

    So the folder gets copied to a USB stick, sent in a ZIP, and on the other
    side you double click kanpachid.exe. There is nothing to install and nothing
    to uninstall: delete the folder and no trace is left.

    WHAT IT COSTS, said plainly

      - One UAC per start. The installed version asks for one only, at install
        time, and in exchange the installer grants the user permission to start
        the service. A folder that was copied granted nothing, so it elevates
        every time.
      - The data inherits the permissions of wherever the folder is. The
        installed version puts its own ACL in ProgramData; here there is no
        installer to put one.
      - It does not start with Windows. There is no registered service for
        Windows to bring up.

    THE TWO MODES

      prod   (default)  interface in release, daemon as a portable daemon.
                        It is what gets sent to a person.
      debug             interface in debug talking over the console pipe, daemon
                        with --console in an elevated terminal where its log is
                        visible.

.EXAMPLE
    .\scriptsuild-portable.ps1
    Builds .\Kanpachi and starts it in production mode.

.EXAMPLE
    .\scriptsuild-portable.ps1 debug
    The same in development mode: console daemon in sight, interface in debug.

.EXAMPLE
    .\scriptsuild-portable.ps1 -Output D:\share\Kanpachi -NoLaunch
    Only builds the folder, to zip it up and send it.
#>
[CmdletBinding()]
param(
    [Parameter(Position = 0)]
    [ValidateSet('prod', 'debug')]
    [string]$Mode = 'prod',

    # Where it gets built. By default a subfolder of the current directory, and
    # not the current directory itself: the folder IS the unit that gets copied,
    # and mixed with whatever else was beside it, it cannot even be zipped.
    [string]$Output = (Join-Path (Get-Location).Path 'Kanpachi'),

    # The engine comes from another repository. Empty picks the MOST RECENT
    # among the places where it ends up built, and stops if it is older than
    # its own source: the list and the reasons live in lib\engine.ps1.
    [string]$Engine = '',

    # Build and do not start. It is what you want before zipping.
    [switch]$NoLaunch,

    # Also delete data\. It takes this installation's key and the last room with
    # it, so it is not the default.
    [switch]$Clean
)

$ErrorActionPreference = 'Stop'

function Step($t) { Write-Host ''; Write-Host "=== $t ===" -ForegroundColor Cyan }
function Ok($t) { Write-Host "  OK   $t" -ForegroundColor Green }
function Fail($t) { Write-Host "  FAIL $t" -ForegroundColor Red }
function Note($t) { Write-Host "  --   $t" -ForegroundColor DarkGray }

# The repository comes from where the script lives and never from the current
# directory: an elevated console starts in system32.
$repo = Split-Path -Parent $PSScriptRoot
$failures = 0
. (Join-Path $PSScriptRoot 'lib\engine.ps1')

$daemonExe = Join-Path $Output 'kanpachid.exe'
$uiExe = Join-Path $Output 'kanpachiui.exe'
$marker = Join-Path $Output 'kanpachi.portable'
# kanpachi-data and NOT data: the Flutter Windows bundle brings its own data\
# with icudtl.dat, app.so and flutter_assets\, and here the two executables
# share a directory. It was measured on this script's first run: they mixed. The
# name is written the same way in portable.go and in pipe_names.dart.
$data = Join-Path $Output 'kanpachi-data'

Write-Host ''
Write-Host "Kanpachi portable, mode $Mode" -ForegroundColor White
Note "output: $Output"

# ---------------------------------------------------------------------------
Step 'shutting down whatever was running from this folder'

# It has to happen BEFORE building: Windows locks the .exe of a live process,
# and go build would fail with an access denied message that says nothing about
# this. It filters by PATH and not by name, so as not to take down an installed
# Kanpachi that has nothing to do with this folder.
$alive = @()
foreach ($name in @('kanpachid', 'kanpachiui', 'kanpachi-engine')) {
    try {
        $alive += Get-Process -Name $name -ErrorAction Stop |
            Where-Object { $_.Path -and $_.Path.StartsWith($Output, [System.StringComparison]::OrdinalIgnoreCase) }
    }
    catch {
        # There is none with that name. That is the normal case.
        Write-Verbose "no $name processes: $($_.Exception.Message)"
    }
}

if ($alive.Count -eq 0) {
    Note 'there was nothing running'
}
else {
    foreach ($p in $alive) {
        try {
            Stop-Process -Id $p.Id -Force -ErrorAction Stop
            Ok ("stopped {0} (pid {1})" -f $p.ProcessName, $p.Id)
        }
        catch {
            # The daemon runs elevated. From an unelevated terminal it cannot be
            # touched, and the symptom without this explanation would be a go
            # build failing with access denied.
            Fail ("could not stop {0} (pid {1}): {2}" -f $p.ProcessName, $p.Id, $_.Exception.Message)
            Note 'close it from the tray icon, or run this in an elevated terminal'
            $failures++
        }
    }
    # The daemon's job takes the interface down with it, and that takes a moment
    # to show up in the file system.
    Start-Sleep -Milliseconds 700
}

if ($failures -gt 0) { exit 1 }

# ---------------------------------------------------------------------------
Step 'preparing the folder'

if (Test-Path $Output) {
    # The contents get deleted and NOT the folder: kanpachi-data\ survives
    # unless asked otherwise. Inside it is this installation's key, which is
    # what stops somebody else from passing for this machine to whoever has
    # already played with it, and throwing it away on every build would turn
    # every build into a new machine.
    Get-ChildItem -Path $Output -Force | Where-Object {
        $Clean -or $_.Name -ne 'kanpachi-data'
    } | Remove-Item -Recurse -Force
    if ($Clean) { Note 'kanpachi-data\ deleted too' }
    else { Note 'kanpachi-data\ kept' }
}
else {
    New-Item -ItemType Directory -Force -Path $Output | Out-Null
}

# ---------------------------------------------------------------------------
Step 'the daemon'

Push-Location $repo
try {
    # -H windowsgui: this binary is what gets double clicked, so a console
    # subsystem one would open a black window. The output of --console is not
    # lost: it reattaches to the parent console when there is one, which is
    # exactly what the debug mode below does.
    #
    # No -ldflags "-s -w": stripping symbols sets off Defender false positives
    # over Go binaries, and the binary being sent has to be the one that was
    # tested.
    & go build -trimpath -ldflags '-H windowsgui' -o $daemonExe ./daemon/cmd/kanpachid
    if ($LASTEXITCODE -ne 0) {
        Fail 'the daemon did not build'
        $failures++
    }
    else {
        $kb = [math]::Round((Get-Item $daemonExe).Length / 1KB)
        Ok ("kanpachid.exe        {0} KB" -f $kb)
    }
}
finally { Pop-Location }

# ---------------------------------------------------------------------------
Step 'the interface'

# In debug it is built pointing at the console pipe. It is a BUILD-time define
# and not an option on disk on purpose: a published binary has that branch
# pruned, so no file on anybody's machine can point the app at a pipe any
# process can create.
if ($Mode -eq 'debug') {
    $flutterArgs = @('build', 'windows', '--debug', '--dart-define=KANPACHI_CONSOLE_PIPE=true')
    $bundle = Join-Path $repo 'ui\build\windows\x64\runner\Debug'
}
else {
    $flutterArgs = @('build', 'windows', '--release')
    $bundle = Join-Path $repo 'ui\build\windows\x64\runner\Release'
}

Push-Location (Join-Path $repo 'ui')
try {
    & flutter @flutterArgs 2>&1 | Select-Object -Last 3 | ForEach-Object { Note $_ }
    if ($LASTEXITCODE -ne 0) {
        Fail 'the interface did not build'
        $failures++
    }
    elseif (-not (Test-Path (Join-Path $bundle 'kanpachiui.exe'))) {
        Fail "the build did not leave kanpachiui.exe in $bundle"
        $failures++
    }
    else {
        # The WHOLE bundle. The executable does not start without
        # flutter_windows.dll, without data\ and without the plugins: copying
        # only the .exe gives a binary that dies at startup without saying why.
        Copy-Item -Path (Join-Path $bundle '*') -Destination $Output -Recurse -Force
        $kb = [math]::Round((Get-Item $uiExe).Length / 1KB)
        Ok ("kanpachiui.exe       {0} KB, with its bundle" -f $kb)
    }
}
finally { Pop-Location }

# ---------------------------------------------------------------------------
Step 'what is not built'

$copies = @(
    @{ source = 'daemon\adapter\catalog\jsonfile\builtin.json'; name = 'builtin.json'; required = $true },
    @{ source = 'third_party\easytier\Packet.dll'; name = 'Packet.dll'; required = $true },
    @{ source = 'third_party\easytier\wintun.dll'; name = 'wintun.dll'; required = $true },
    @{ source = 'third_party\easytier\WinDivert64.sys'; name = 'WinDivert64.sys'; required = $false }
)
foreach ($c in $copies) {
    $src = Join-Path $repo $c.source
    if (-not (Test-Path $src)) {
        if ($c.required) {
            Fail ('missing from the repository: ' + $c.source)
            $failures++
        }
        else {
            Note ('not there and not required: ' + $c.source)
        }
        continue
    }
    Copy-Item -Path $src -Destination (Join-Path $Output $c.name) -Force
    Ok $c.name
}

# ---------------------------------------------------------------------------
Step 'the engine, which comes from another repository'

$engineRoot = Find-KanpachiEngineRepo -KanpachiRepo $repo
if (-not $Engine) { $Engine = Resolve-KanpachiEngine -EngineRoot $engineRoot }
if ($Engine -and (Test-Path $Engine)) {
    Assert-KanpachiEngineFresh -Engine $Engine -EngineRoot $engineRoot
    Copy-Item -Path $Engine -Destination (Join-Path $Output 'kanpachi-engine.exe') -Force
    $mb = [math]::Round((Get-Item $Engine).Length / 1MB, 1)
    Ok ('kanpachi-engine.exe  {0} MB ({1})' -f $mb, $Engine)
}
else {
    Fail 'there is no engine built anywhere'
    Note 'build it with scriptsuild-engine.ps1, or pass its path with -Engine'
    Note 'without it you can talk to the daemon, you cannot open a room'
    $failures++
}

# ---------------------------------------------------------------------------
Step 'the marker'

# ASCII and a single line. It is read by an os.Stat in Go and an existsSync in
# Dart: neither of them looks at the content, which is there for whoever opens
# the folder and wonders what this file is doing.
'Kanpachi portable. Con este fichero presente, el daemon guarda sus datos en kanpachi-data\ y corre sin servicio. Borralo para volver a ProgramData.' |
    Out-File -FilePath $marker -Encoding ascii
Ok 'kanpachi.portable'

if (-not (Test-Path $data)) {
    # The daemon would create it on its own first start. It is done here so the
    # folder being zipped already has its place and does not depend on having
    # been started once.
    New-Item -ItemType Directory -Force -Path $data | Out-Null
    Ok 'kanpachi-data\'
}

# ---------------------------------------------------------------------------
Step 'result'

if ($failures -gt 0) {
    Fail "$failures problem(s): the folder is NOT ready"
    exit 1
}

$total = [math]::Round(((Get-ChildItem -Path $Output -Recurse -File | Measure-Object Length -Sum).Sum / 1MB), 1)
Ok "the folder is at $Output, $total MB"

if ($NoLaunch) {
    Note 'nothing gets started: -NoLaunch'
    Note 'zip it whole and on the other side double click kanpachid.exe'
    exit 0
}

# ---------------------------------------------------------------------------
Step 'starting'

if ($Mode -eq 'debug') {
    # The console daemon, elevated and WITH its console in sight.
    #
    # It goes through cmd.exe /k and not directly, and that is needed:
    # kanpachid.exe is linked with -H windowsgui, so it creates no console of
    # its own; what it does is attach to the parent's. Started with -Verb RunAs
    # there is no parent with a console, and the daemon's whole log would go
    # nowhere, which is exactly what debug mode exists to avoid.
    #
    # Elevated because the pipe lives under ProtectedPrefix\Administrators and
    # writing to the firewall demands administrator.
    Note 'the daemon opens its own elevated terminal. Windows will ask for permission'
    Start-Process -FilePath 'cmd.exe' -Verb RunAs -ArgumentList @('/k', ('"' + $daemonExe + '" --console'))
    Ok 'daemon in console mode'

    # The interface separately: the console daemon does NOT host the interface,
    # on purpose. Whoever uses --console has a terminal in front of them and
    # starts it when they want; opening a window on every start would cover up
    # the case the real product has to solve, which is the daemon launching it.
    #
    # The daemon gets a moment first: the interface asks for the catalog when it
    # opens, and against a pipe that is not listening yet it would show the no
    # service banner right at startup.
    Start-Sleep -Seconds 3
    Start-Process -FilePath $uiExe -ArgumentList '--show'
    Ok 'interface in debug mode'
    Note 'the interface talks over the console pipe, which is the one that daemon opened'
}
else {
    # The real path, the same one a double click takes: this starts the
    # LAUNCHER. It probes the pipe, sees nobody there, and relaunches itself
    # elevated as a portable daemon. The daemon opens the interface.
    #
    # --show because whoever just ran this is watching.
    Note 'Windows will ask for administrator: the portable daemon is asking'
    Start-Process -FilePath $daemonExe -ArgumentList '--show'
    Ok 'Kanpachi starting'
    Note "the daemon log ends up at $data\logs\kanpachi.log"
}
