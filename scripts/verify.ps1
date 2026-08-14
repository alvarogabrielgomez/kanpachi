<#
.SYNOPSIS
    Runs the project's checks. One surface per CI job, so the workflows and a
    person run the very same file.

.DESCRIPTION
    The recipe used to live inside .github/workflows/ci.yml only, so checking
    anything before pushing meant typing a sixteen-entry package list by hand.
    What is typed differently is checked differently.

    This script is that recipe carved in stone. Each CI job calls it with its
    own -Surface, which is what keeps the two copies from drifting: a new
    package that has to enter the gate enters HERE, and nowhere else.

    # The surfaces

      all              what the development machine can answer. The default.
      ci-linux         ci.yml, job "core en Linux"
      ci-windows       ci.yml, job "daemon en Windows"
      ci-ui            ci.yml, job "la UI de Flutter"
      release-windows  release.yml, job "instalador"
      release-linux    release.yml, job "paquete-linux"
      release-seed     release-seed.yml

    Every surface runs EXACTLY what its job runs today, no more and no less.
    Two differences look like mistakes and are not:

      - `dart format` runs on ci-ui and NOT on release-windows. A badly
        formatted file should not block a publication; formatting does not
        break the product.
      - release-windows and release-linux run `./core/... ./internal/arch/...`
        and nothing else. What gets published passes the tests that gate it,
        and the full sweep is ci.yml's job.

    # What it does NOT do

    `-race` on the development machine. On Windows it needs a C toolchain, and
    the same code already runs with -race in the Linux job. The 'all' surface
    says so when it finishes instead of letting you believe otherwise.

    It also starts nothing: no rooms, no firewall, no elevated console. That is
    what the measure-*.ps1 scripts are for, and each one states its own terms.

.PARAMETER Surface
    Which job's checks to run. Named -Surface and not -Profile because $Profile
    is a PowerShell automatic variable and binding a parameter over it trips
    PSAvoidAssignmentToAutomaticVariable.
#>
[CmdletBinding()]
param(
    [ValidateSet('all', 'ci-linux', 'ci-windows', 'ci-ui', 'release-windows', 'release-linux', 'release-seed')]
    [string]$Surface = 'all'
)

$ErrorActionPreference = 'Stop'

$root = Split-Path -Parent $PSScriptRoot
Push-Location $root

function Step($t) { Write-Host "`n=== $t ===" -ForegroundColor Cyan }
function Ok($t) { Write-Host "  OK   $t" -ForegroundColor Green }
function Fail($t) { Write-Host "  FAIL $t" -ForegroundColor Red }

$failures = @()

# Run executes a step and does NOT abort on the first failure, on purpose.
#
# A gate that stops at the first error forces one full round trip per problem.
# What is useful is the complete list of what needs fixing, so it is recorded
# and the run goes on. The exit code comes out at the end.
function Run($title, [scriptblock]$block) {
    Step $title
    $before = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    & $block
    $code = $LASTEXITCODE
    $ErrorActionPreference = $before
    if ($code -ne 0) {
        Fail "$title (exit $code)"
        $script:failures += $title
    } else {
        Ok $title
    }
}

# The vet list, copied from the Linux job. Explicit and not ./... because the
# engine and netcfg are Windows only and their packages do not compile there.
$vetPackages = @(
    './core/...', './internal/...', './daemon/service/...',
    './daemon/transport/protocol/...', './daemon/transport/wire/...',
    './daemon/transport/control/...', './daemon/transport/pipe/...',
    './daemon/adapter/sinimplementar/...', './daemon/adapter/safewrite/...',
    './daemon/adapter/state/...', './daemon/adapter/catalog/...',
    './daemon/adapter/canary/...', './daemon/adapter/directory/...',
    './daemon/adapter/identity/...', './daemon/adapter/firewall/...',
    './registry/...'
)

# The portable half of daemon/: what is NOT Windows, which is why it is tested
# on Linux. `./daemon/adapter/firewall` has no `/...` on purpose: the composite
# package is tested here, and its subpackages are listed one by one below it.
$portablePackages = @(
    './daemon/service/...', './daemon/transport/protocol/...',
    './daemon/transport/wire/...', './daemon/transport/control/...',
    './daemon/transport/pipe/...', './daemon/adapter/sinimplementar/...',
    './daemon/adapter/safewrite/...', './daemon/adapter/state/...',
    './daemon/adapter/catalog/...', './daemon/adapter/probe/...',
    './daemon/adapter/canary/...', './daemon/adapter/identity/...',
    './daemon/adapter/directory/...', './daemon/adapter/firewall',
    './daemon/adapter/firewall/gate/...',
    './daemon/adapter/firewall/windows/wfp/...',
    './daemon/adapter/firewall/windows/netfw/...'
)

# Unformatted answers which files are unformatted, IGNORING line endings.
#
# It is needed because plain `gofmt -l` lies on Windows: with core.autocrlf=true
# the working tree is CRLF and the index is LF, so it flags files that come out
# clean in CI. It flagged ten, and only four really were. A gate that goes red
# over something that is not stops being looked at, which is worse than no gate.
function Unformatted($directories) {
    $suspects = & gofmt -l @directories
    $real = @()
    foreach ($f in $suspects) {
        if (-not $f) { continue }
        $tmp = Join-Path ([IO.Path]::GetTempPath()) ("verify-" + [Guid]::NewGuid().ToString('N') + ".go")
        [IO.File]::WriteAllText($tmp, ([IO.File]::ReadAllText($f) -replace "`r`n", "`n"))
        $still = & gofmt -l $tmp
        Remove-Item $tmp -Force
        if ($still) { $real += $f }
    }
    $real
}

# ─── The individual checks ───────────────────────────────────────────────────

function Invoke-BuildAll { Run "build everything" { go build ./... } }

# The only piece of the Linux job this machine can answer: that the WHOLE daemon
# compiles there. Nobody else builds the Linux adapters until CI runs.
function Invoke-CrossBuildLinux {
    Run "build for Linux" {
        $before = $env:GOOS
        $env:GOOS = 'linux'
        go build ./...
        $env:GOOS = $before
    }
}

function Invoke-Vet { Run "go vet" { go vet @vetPackages } }

# Separate and first so its failure is unmistakable in the log. It is an
# architecture violation, not one more test that broke.
function Invoke-ArchGuards { Run "architecture guards" { go test -count=1 ./internal/arch/... } }

function Invoke-CoreTests([switch]$Race) {
    if ($Race) { Run "core tests" { go test -race -count=1 ./core/... } }
    else { Run "core tests" { go test -count=1 ./core/... } }
}

function Invoke-RegistryTests([switch]$Race) {
    if ($Race) { Run "seed registry tests" { go test -race -count=1 ./registry/... } }
    else { Run "seed registry tests" { go test -count=1 ./registry/... } }
}

function Invoke-PortableDaemonTests {
    Run "portable daemon tests" { go test -race -count=1 @portablePackages }
}

# Without -race, same as the Windows job: this code already runs with -race in
# the Linux job, and what this step adds is what does not compile there.
function Invoke-DaemonAndInternalTests {
    Run "daemon and internal tests" { go test -count=1 ./daemon/... ./internal/... }
}

# What both release jobs run before packaging: the domain and the use cases,
# plus the guards that tie the two ends of the wire together.
function Invoke-PublishTests {
    Run "tests that gate a publication" { go test -count=1 ./core/... ./internal/arch/... }
}

function Invoke-SeedTests {
    Run "tests that gate the seed" { go test -race -count=1 ./core/... ./registry/... ./internal/arch/... }
}

# Flutter runs from ui/, so the caller does not need a working-directory.
function Invoke-FlutterChecks([switch]$WithFormat) {
    Push-Location (Join-Path $root 'ui')
    try {
        Run "flutter pub get" { flutter pub get }
        if ($WithFormat) {
            Run "dart format" { dart format --output=none --set-exit-if-changed lib test }
        }
        # --fatal-infos and not --fatal-warnings: today it passes clean, so the
        # bar goes where the project is and not one step below.
        Run "flutter analyze" { flutter analyze --fatal-infos }
        Run "flutter test" { flutter test }
    } finally { Pop-Location }
}

function Invoke-GofmtCheck {
    Step "gofmt"
    $unformatted = Unformatted @('./core', './internal', './daemon')
    if ($unformatted) {
        Fail "unformatted:"
        $unformatted | ForEach-Object { Write-Host "    $_" }
        Write-Host "  fix with:  gofmt -w $($unformatted -join ' ')"
        $script:failures += 'gofmt'
    } else {
        Ok "gofmt"
    }
}

# ─── One branch per surface ──────────────────────────────────────────────────

try {
    switch ($Surface) {
        'all' {
            Invoke-BuildAll
            Invoke-CrossBuildLinux
            Invoke-Vet
            Invoke-ArchGuards
            Invoke-CoreTests
            Invoke-RegistryTests
            Invoke-DaemonAndInternalTests
            Invoke-GofmtCheck
        }
        'ci-linux' {
            Invoke-BuildAll
            Invoke-Vet
            Invoke-ArchGuards
            Invoke-CoreTests -Race
            Invoke-PortableDaemonTests
            Invoke-RegistryTests -Race
            Invoke-GofmtCheck
        }
        'ci-windows' {
            Invoke-BuildAll
            Invoke-DaemonAndInternalTests
        }
        'ci-ui' {
            Invoke-FlutterChecks -WithFormat
        }
        'release-windows' {
            Invoke-PublishTests
            Invoke-FlutterChecks
        }
        'release-linux' {
            Invoke-PublishTests
        }
        'release-seed' {
            Invoke-SeedTests
        }
    }

    Step "result"
    if ($failures.Count -eq 0) {
        Ok "green, surface '$Surface'"
        if ($Surface -eq 'all') {
            Write-Host "  Missing what only runs in CI: the -race tests of the Linux job."
        }
        exit 0
    }
    Fail "$($failures.Count) step(s) red:"
    $failures | ForEach-Object { Write-Host "    $_" }
    exit 1
} finally {
    Pop-Location
}
